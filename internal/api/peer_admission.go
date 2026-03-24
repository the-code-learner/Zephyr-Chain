package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var (
	errPeerIdentityRequired    = errors.New("peer request must include a signed transport identity")
	errPeerValidatorNotAllowed = errors.New("peer validator is not admitted by local policy")
)

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
	previous, _ := s.peerView(peerURL)
	status, err := s.fetchPeerStatus(peerURL)
	if err != nil {
		view := mergePeerSyncHistory(PeerView{
			URL:               peerURL,
			ExpectedValidator: s.expectedPeerValidator(peerURL),
			Reachable:         false,
			SyncState:         "unreachable",
			Error:             err.Error(),
		}, previous)
		s.recordPeerView(view)
		return view, false
	}

	view := mergePeerSyncHistory(s.buildPeerView(peerURL, status, time.Now().UTC()), previous)
	if !view.Admitted {
		view.SyncState = "unadmitted"
	}
	s.recordPeerView(view)
	return view, view.Admitted
}

func (s *Server) admittedPeerURLs() []string {
	peers := make([]string, 0, len(s.config.PeerURLs))
	for _, peerURL := range s.config.PeerURLs {
		if !s.peerIdentityRequired() && s.expectedPeerValidator(peerURL) == "" {
			peers = append(peers, peerURL)
			continue
		}

		if view, ok := s.peerView(peerURL); ok {
			if view.Admitted {
				peers = append(peers, peerURL)
			}
			continue
		}

		if _, admitted := s.fetchPeerAdmissionView(peerURL); admitted {
			peers = append(peers, peerURL)
		}
	}
	return peers
}

func (s *Server) validatePeerRequest(r *http.Request) error {
	identity, err := transportIdentityFromRequest(r)
	if err != nil {
		return err
	}
	if requestSourceNode(r) == "" {
		return nil
	}
	if !s.peerIdentityRequired() {
		return nil
	}
	if identity == nil {
		return errPeerIdentityRequired
	}

	allowed := s.allowedPeerValidators()
	if len(allowed) == 0 {
		return nil
	}
	if _, ok := allowed[identity.ValidatorAddress]; !ok {
		return errPeerValidatorNotAllowed
	}
	return nil
}

func (s *Server) peerView(peerURL string) (PeerView, bool) {
	s.peerMu.RLock()
	defer s.peerMu.RUnlock()
	view, ok := s.peerViews[peerURL]
	return view, ok
}
