package ledger

import (
	"strings"
	"time"
)

const (
	peerSyncIncidentHistoryLimit = 32
	peerSyncIncidentRecentLimit  = 10
)

type PeerSyncIncident struct {
	PeerURL         string    `json:"peerUrl"`
	State           string    `json:"state"`
	Reason          string    `json:"reason,omitempty"`
	LocalHeight     uint64    `json:"localHeight,omitempty"`
	PeerHeight      uint64    `json:"peerHeight,omitempty"`
	HeightDelta     int64     `json:"heightDelta"`
	BlockHash       string    `json:"blockHash,omitempty"`
	ErrorCode       string    `json:"errorCode,omitempty"`
	ErrorMessage    string    `json:"errorMessage,omitempty"`
	FirstObservedAt time.Time `json:"firstObservedAt"`
	LastObservedAt  time.Time `json:"lastObservedAt"`
	Occurrences     int       `json:"occurrences"`
}

type PeerSyncHistoryView struct {
	Recent []PeerSyncIncident `json:"recent"`
}

func (s *Store) PeerSyncHistory() PeerSyncHistoryView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return peerSyncHistoryFromState(s.snapshotLocked())
}

func (s *Store) PeerSyncIncidents(peerURL string, limit int) []PeerSyncIncident {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return peerSyncIncidentsForPeerFromState(s.snapshotLocked(), peerURL, limit)
}

func (s *Store) RecordPeerSyncIncident(incident PeerSyncIncident) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	state = recordPeerSyncIncidentIntoState(state, incident)
	if err := s.writeState(state); err != nil {
		return err
	}

	s.applyStateLocked(state)
	return nil
}

func peerSyncHistoryFromState(state persistedState) PeerSyncHistoryView {
	state = normalizeState(state)
	recent := clonePeerSyncIncidents(state.PeerSyncIncidents)
	if len(recent) > peerSyncIncidentRecentLimit {
		recent = recent[len(recent)-peerSyncIncidentRecentLimit:]
	}
	for left, right := 0, len(recent)-1; left < right; left, right = left+1, right-1 {
		recent[left], recent[right] = recent[right], recent[left]
	}
	return PeerSyncHistoryView{Recent: recent}
}

func peerSyncIncidentsForPeerFromState(state persistedState, peerURL string, limit int) []PeerSyncIncident {
	state = normalizeState(state)
	peerURL = strings.TrimSpace(peerURL)
	if peerURL == "" {
		return make([]PeerSyncIncident, 0)
	}
	if limit <= 0 {
		limit = peerSyncIncidentRecentLimit
	}

	incidents := make([]PeerSyncIncident, 0, limit)
	for index := len(state.PeerSyncIncidents) - 1; index >= 0 && len(incidents) < limit; index-- {
		incident := state.PeerSyncIncidents[index]
		if incident.PeerURL != peerURL {
			continue
		}
		incidents = append(incidents, clonePeerSyncIncident(incident))
	}
	return incidents
}

func recordPeerSyncIncidentIntoState(state persistedState, incident PeerSyncIncident) persistedState {
	state = normalizeState(state)
	incident = normalizePeerSyncIncident(incident)

	for index := len(state.PeerSyncIncidents) - 1; index >= 0; index-- {
		existing := state.PeerSyncIncidents[index]
		if existing.PeerURL != incident.PeerURL {
			continue
		}
		if samePeerSyncIncident(existing, incident) {
			state.PeerSyncIncidents[index] = mergePeerSyncIncident(existing, incident)
		} else {
			state.PeerSyncIncidents = append(state.PeerSyncIncidents, clonePeerSyncIncident(incident))
		}
		if len(state.PeerSyncIncidents) > peerSyncIncidentHistoryLimit {
			state.PeerSyncIncidents = clonePeerSyncIncidents(state.PeerSyncIncidents[len(state.PeerSyncIncidents)-peerSyncIncidentHistoryLimit:])
		} else {
			state.PeerSyncIncidents = clonePeerSyncIncidents(state.PeerSyncIncidents)
		}
		return state
	}

	state.PeerSyncIncidents = append(state.PeerSyncIncidents, clonePeerSyncIncident(incident))
	if len(state.PeerSyncIncidents) > peerSyncIncidentHistoryLimit {
		state.PeerSyncIncidents = clonePeerSyncIncidents(state.PeerSyncIncidents[len(state.PeerSyncIncidents)-peerSyncIncidentHistoryLimit:])
	} else {
		state.PeerSyncIncidents = clonePeerSyncIncidents(state.PeerSyncIncidents)
	}
	return state
}

func normalizePeerSyncIncidents(incidents []PeerSyncIncident) []PeerSyncIncident {
	if incidents == nil {
		return make([]PeerSyncIncident, 0)
	}
	cloned := make([]PeerSyncIncident, len(incidents))
	for i, incident := range incidents {
		cloned[i] = normalizePeerSyncIncident(incident)
	}
	if len(cloned) > peerSyncIncidentHistoryLimit {
		cloned = cloned[len(cloned)-peerSyncIncidentHistoryLimit:]
	}
	return cloned
}

func normalizePeerSyncIncident(incident PeerSyncIncident) PeerSyncIncident {
	incident.PeerURL = strings.TrimSpace(incident.PeerURL)
	incident.State = strings.TrimSpace(incident.State)
	incident.Reason = strings.TrimSpace(incident.Reason)
	incident.BlockHash = strings.TrimSpace(incident.BlockHash)
	incident.ErrorCode = strings.TrimSpace(incident.ErrorCode)
	incident.ErrorMessage = strings.TrimSpace(incident.ErrorMessage)
	if incident.Occurrences <= 0 {
		incident.Occurrences = 1
	}
	switch {
	case incident.FirstObservedAt.IsZero() && incident.LastObservedAt.IsZero():
		now := time.Now().UTC()
		incident.FirstObservedAt = now
		incident.LastObservedAt = now
	case incident.FirstObservedAt.IsZero():
		incident.FirstObservedAt = incident.LastObservedAt.UTC()
	case incident.LastObservedAt.IsZero():
		incident.LastObservedAt = incident.FirstObservedAt.UTC()
	}
	incident.FirstObservedAt = incident.FirstObservedAt.UTC()
	incident.LastObservedAt = incident.LastObservedAt.UTC()
	if incident.LastObservedAt.Before(incident.FirstObservedAt) {
		incident.FirstObservedAt, incident.LastObservedAt = incident.LastObservedAt, incident.FirstObservedAt
	}
	return incident
}

func clonePeerSyncIncident(incident PeerSyncIncident) PeerSyncIncident {
	return PeerSyncIncident{
		PeerURL:         incident.PeerURL,
		State:           incident.State,
		Reason:          incident.Reason,
		LocalHeight:     incident.LocalHeight,
		PeerHeight:      incident.PeerHeight,
		HeightDelta:     incident.HeightDelta,
		BlockHash:       incident.BlockHash,
		ErrorCode:       incident.ErrorCode,
		ErrorMessage:    incident.ErrorMessage,
		FirstObservedAt: incident.FirstObservedAt,
		LastObservedAt:  incident.LastObservedAt,
		Occurrences:     incident.Occurrences,
	}
}

func clonePeerSyncIncidents(incidents []PeerSyncIncident) []PeerSyncIncident {
	cloned := make([]PeerSyncIncident, len(incidents))
	for i, incident := range incidents {
		cloned[i] = clonePeerSyncIncident(incident)
	}
	return cloned
}

func samePeerSyncIncident(left PeerSyncIncident, right PeerSyncIncident) bool {
	return left.PeerURL == right.PeerURL &&
		left.State == right.State &&
		left.Reason == right.Reason &&
		left.LocalHeight == right.LocalHeight &&
		left.PeerHeight == right.PeerHeight &&
		left.HeightDelta == right.HeightDelta &&
		left.BlockHash == right.BlockHash &&
		left.ErrorCode == right.ErrorCode &&
		left.ErrorMessage == right.ErrorMessage
}

func mergePeerSyncIncident(existing PeerSyncIncident, incoming PeerSyncIncident) PeerSyncIncident {
	existing = normalizePeerSyncIncident(existing)
	incoming = normalizePeerSyncIncident(incoming)
	merged := clonePeerSyncIncident(existing)
	if incoming.FirstObservedAt.Before(merged.FirstObservedAt) {
		merged.FirstObservedAt = incoming.FirstObservedAt
	}
	if incoming.LastObservedAt.After(merged.LastObservedAt) {
		merged.LastObservedAt = incoming.LastObservedAt
	}
	merged.Occurrences += incoming.Occurrences
	return merged
}
