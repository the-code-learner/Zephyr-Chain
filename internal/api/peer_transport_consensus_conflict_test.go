package api

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

// A valid HTTP 409 response is NOT consensus acceptance. Round/height
// mismatch and conflicting proposal/vote errors are mapped to 409 by
// statusForError, so swallowing it would hide lost consensus deliveries
// from replication-incident diagnostics. This test uses a synthetic peer
// response and does not claim to resolve the historical liveness timeout.
func TestHTTPPeerTransportReportsConsensusConflicts(t *testing.T) {
	signer := newConsensusSigner(t)
	config := DefaultConfig()
	config.NodeID = "peer-conflict-sender"
	config.ValidatorPrivateKey = encodedPrivateKey(t, signer.privateKey)
	identity, _, err := newTransportIdentitySigner(config)
	if err != nil || identity == nil {
		t.Fatalf("create transport request signer: %v", err)
	}
	var status atomic.Int32
	status.Store(http.StatusConflict)
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(sourceNodeHeader) != config.NodeID || r.Header.Get(sourceSignatureHeader) == "" {
			http.Error(w, "missing transport proof", http.StatusForbidden)
			return
		}
		w.WriteHeader(int(status.Load()))
	}))
	defer peer.Close()
	transport := newHTTPPeerTransport(peer.Client(), config.NodeID, identity)

	proposal := consensus.Proposal{Height: 1, Round: 2}
	vote := consensus.Vote{Height: 1, Round: 2}
	if err := transport.PostProposal(peer.URL, proposal); err == nil {
		t.Fatal("HTTP 409 was silently accepted as a delivered consensus proposal")
	}
	if err := transport.PostVote(peer.URL, vote); err == nil {
		t.Fatal("HTTP 409 was silently accepted as a delivered consensus vote")
	}
	// Keep the pre-existing policy for a non-consensus duplicate. This
	// change must not alter transaction propagation / retry semantics.
	if err := transport.PostTransaction(peer.URL, tx.Envelope{}); err != nil {
		t.Fatalf("non-consensus conflict handling unexpectedly changed: %v", err)
	}

	status.Store(http.StatusAccepted)
	if err := transport.PostProposal(peer.URL, proposal); err != nil {
		t.Fatalf("accepted proposal delivery failed: %v", err)
	}
	if err := transport.PostVote(peer.URL, vote); err != nil {
		t.Fatalf("accepted vote delivery failed: %v", err)
	}

	status.Store(http.StatusInternalServerError)
	if err := transport.PostProposal(peer.URL, proposal); err == nil {
		t.Fatal("HTTP 500 was silently accepted as a proposal delivery")
	}
	if err := transport.PostVote(peer.URL, vote); err == nil {
		t.Fatal("HTTP 500 was silently accepted as a vote delivery")
	}
}
