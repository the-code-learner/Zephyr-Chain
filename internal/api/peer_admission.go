package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var (
	errPeerIdentityRequired    = errors.New("peer request must include a signed request proof")
	errPeerValidatorNotAllowed = errors.New("peer validator is not admitted by local policy")
)

// A failed initial status request must not permanently strand a validator
// when peer sync is disabled. Retry only transport-unreachable peers at a
// bounded interval; a reachable peer with an invalid/pinned-wrong identity
// stays denied until an explicit operator-driven refresh or restart.
const peerAdmissionRetryInterval = 2 * time.Second

func normalizePeerValidatorBindings(bindings map[string]string) map[string]string {
	if len(bindings) == 0 {
		return nil
	}

	normalized := make(map[string]string, len(bindings))
	for peerURL, validator := range bindings {
		peerURL = strings.TrimSpace(peerURL)
		peerURL = strings.TrimRight(peerURL, "/")
		validator = strings.TrimSpace(validator)
		if peerURL == "" || validator == "" {
			continue
		}
		normalized[peerURL] = validator
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func (s *Server) peerIdentityRequired() bool {
	return s.config.RequirePeerIdentity || len(s.config.PeerValidatorBindings) > 0
}

func (s *Server) expectedPeerValidator(peerURL string) string {
	if len(s.config.PeerValidatorBindings) == 0 {
		return ""
	}
	return strings.TrimSpace(s.config.PeerValidatorBindings[peerURL])
}

func (s *Server) allowedPeerValidators() map[string]struct{} {
	if len(s.config.PeerValidatorBindings) == 0 {
		return nil
	}

	allowed := make(map[string]struct{}, len(s.config.PeerValidatorBindings))
	for _, validator := range s.config.PeerValidatorBindings {
		validator = strings.TrimSpace(validator)
		if validator == "" {
			continue
		}
		allowed[validator] = struct{}{}
	}
	if len(allowed) == 0 {
		return nil
	}
	return allowed
}

func (s *Server) buildPeerView(peerURL string, status StatusResponse, now time.Time) PeerView {
	identityVerified, identityError := verifyPeerTransportIdentity(status, now)
	expectedValidator := s.expectedPeerValidator(peerURL)
	admitted := true
	admissionError := ""

	switch {
	case status.ChainID != s.config.ChainID:
		admitted = false
		admissionError = fmt.Sprintf("peer chain %s does not match local chain %s", status.ChainID, s.config.ChainID)
	case s.peerIdentityRequired() && status.Identity == nil:
		admitted = false
		admissionError = "peer does not expose a signed transport identity"
	case status.Identity != nil && !identityVerified && s.peerIdentityRequired():
		admitted = false
		admissionError = identityError
	case expectedValidator != "" && status.Identity == nil:
		admitted = false
		admissionError = "peer does not expose a signed transport identity"
	case expectedValidator != "" && !identityVerified:
		admitted = false
		admissionError = identityError
	case expectedValidator != "" && status.Identity.ValidatorAddress != expectedValidator:
		admitted = false
		admissionError = fmt.Sprintf("peer validator %s does not match expected %s", status.Identity.ValidatorAddress, expectedValidator)
	}

	validatorAddress := status.ValidatorAddress
	if validatorAddress == "" && status.Identity != nil {
		validatorAddress = status.Identity.ValidatorAddress
	}

	localStatus := s.ledger.Status()
	syncState, heightDelta, _ := derivePeerSyncState(localStatus, status.Status)

	return PeerView{
		URL:               peerURL,
		NodeID:            status.NodeID,
		ValidatorAddress:  validatorAddress,
		Height:            status.Status.Height,
		LatestBlockHash:   status.Status.LatestBlockHash,
		MempoolSize:       status.Status.MempoolSize,
		BlockProduction:   status.BlockProduction,
		IdentityPresent:   status.Identity != nil,
		IdentityVerified:  identityVerified,
		IdentityError:     identityError,
		ExpectedValidator: expectedValidator,
		Admitted:          admitted,
		AdmissionError:    admissionError,
		HeightDelta:       heightDelta,
		SyncState:         syncState,
		LastSeenAt:        &now,
		Reachable:         true,
	}
}

func (s *Server) fetchPeerAdmissionView(peerURL string) (PeerView, bool) {
	attemptedAt := time.Now().UTC()
	previous, _ := s.peerView(peerURL)
	status, err := s.fetchPeerStatus(peerURL)
	if err != nil {
		view := mergePeerSyncHistory(PeerView{
			URL:                    peerURL,
			ExpectedValidator:      s.expectedPeerValidator(peerURL),
			LastAdmissionAttemptAt: cloneAPITimeValue(attemptedAt),
			Reachable:              false,
			SyncState:              "unreachable",
			Error:                  err.Error(),
		}, previous)
		s.recordPeerView(view)
		return view, false
	}

	view := mergePeerSyncHistory(s.buildPeerView(peerURL, status, time.Now().UTC()), previous)
	view.LastAdmissionAttemptAt = cloneAPITimeValue(attemptedAt)
	if !view.Admitted {
		view.SyncState = "unadmitted"
	}
	s.recordPeerView(view)
	return view, view.Admitted
}

func (s *Server) admittedPeerURLs() []string {
	configured := s.peerURLsSnapshot()
	peers := make([]string, 0, len(configured))
	for _, peerURL := range configured {
		if !s.peerIdentityRequired() && s.expectedPeerValidator(peerURL) == "" {
			peers = append(peers, peerURL)
			continue
		}

		if view, ok := s.peerView(peerURL); ok {
			// An old admitted=true flag does not authorize a peer which
			// has since become unreachable. A fresh signed status proof
			// is mandatory before that peer can rejoin broadcasts.
			if view.Reachable && view.Admitted {
				peers = append(peers, peerURL)
			} else if !view.Reachable && !s.config.EnablePeerSync &&
				s.reservePeerAdmissionRetry(peerURL, time.Now().UTC()) {
				if refreshed, admitted := s.fetchPeerAdmissionView(peerURL); admitted && refreshed.Reachable {
					peers = append(peers, peerURL)
				}
			}
			continue
		}

		if view, admitted := s.fetchPeerAdmissionView(peerURL); admitted && view.Reachable {
			peers = append(peers, peerURL)
		}
	}
	return peers
}

// reservePeerAdmissionRetry atomically throttles failed transport probes so
// concurrent consensus/transaction broadcasters cannot stampede a peer that
// is offline. Only a previously unreachable (not a reachable unadmitted)
// peer can be probed again; identity and validator pins are revalidated
// from a NEW signed status response, never borrowed from stale history.
func (s *Server) reservePeerAdmissionRetry(peerURL string, now time.Time) bool {
	s.peerMu.Lock()
	defer s.peerMu.Unlock()
	view, ok := s.peerViews[peerURL]
	if !ok || view.Reachable || (view.LastAdmissionAttemptAt != nil &&
		now.Before(view.LastAdmissionAttemptAt.Add(peerAdmissionRetryInterval))) {
		return false
	}
	view.LastAdmissionAttemptAt = cloneAPITimeValue(now)
	s.peerViews[peerURL] = view
	return true
}

func (s *Server) validatePeerRequest(r *http.Request) error {
	if requestSourceNode(r) == "" {
		if strings.HasPrefix(r.URL.Path, "/v1/internal/") {
			return errPeerIdentityRequired
		}
		return nil
	}

	proof, err := validateAndRememberRequestProof(s, r)
	if err != nil {
		return err
	}
	if proof == nil {
		return errPeerIdentityRequired
	}

	allowed := s.allowedPeerValidators()
	if len(allowed) > 0 {
		if _, ok := allowed[proof.ValidatorAddress]; !ok {
			return errPeerValidatorNotAllowed
		}
	}

	validatorSet := s.ledger.ValidatorSet()
	if len(validatorSet.Validators) > 0 {
		active := false
		for _, validator := range validatorSet.Validators {
			if validator.Address == proof.ValidatorAddress {
				active = true
				break
			}
		}
		if !active {
			return errPeerValidatorNotAllowed
		}
	}
	return nil
}

func (s *Server) peerView(peerURL string) (PeerView, bool) {
	s.peerMu.RLock()
	defer s.peerMu.RUnlock()
	view, ok := s.peerViews[peerURL]
	return view, ok
}
