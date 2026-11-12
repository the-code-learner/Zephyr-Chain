package api

import (
	"errors"
	"testing"
	"time"
)

// A signed certified POST for the old finalized height may complete only
// after a new block has been committed and reserved for the same peer.
// Both late success and late failure must not destroy the new reservation.
func TestCertifiedReplayFencesStaleDeliveryResponses(t *testing.T) {
	for _, staleErr := range []error{nil, errors.New("previous delivery failed")} {
		t.Run(func() string {
			if staleErr == nil {
				return "stale_ack"
			}
			return "stale_failure"
		}(), func(t *testing.T) {
			s := &Server{}
			peer := "https://validator.example"
			now := time.Now()
			if !s.reserveCertifiedReplay(peer, "hash-height-1", now) {
				t.Fatal("reserve old block")
			}
			if !s.reserveCertifiedReplay(peer, "hash-height-2", now) {
				t.Fatal("new finalized height must replace old replay cursor")
			}
			s.recordCertifiedDelivery(peer, "hash-height-1", staleErr)
			s.certifiedReplayMu.Lock()
			state := s.certifiedReplay[peer]
			s.certifiedReplayMu.Unlock()
			if state.blockHash != "hash-height-2" || !state.inFlight || state.acknowledged {
				t.Fatalf("stale response rewrote newer replay state: %+v", state)
			}
			s.recordCertifiedDelivery(peer, "hash-height-2", nil)
			s.certifiedReplayMu.Lock()
			state = s.certifiedReplay[peer]
			s.certifiedReplayMu.Unlock()
			if state.blockHash != "hash-height-2" || state.inFlight || !state.acknowledged {
				t.Fatalf("current ACK did not complete new block: %+v", state)
			}
			if s.reserveCertifiedReplay(peer, "hash-height-2", now.Add(2*certifiedReplayInterval)) {
				t.Fatal("acknowledged latest block must not be replayed")
			}
		})
	}
}
