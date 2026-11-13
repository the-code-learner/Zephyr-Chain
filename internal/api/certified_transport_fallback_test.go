package api

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

// Legacy transport adapters must not silently downgrade signed finality
// delivery to bare blocks when receiver-side quorum checks are mandatory.
func TestCertifiedBroadcastFailsClosedWithoutSignedTransport(t *testing.T) {
	s := newTestServer(t, Config{
		DataDir: t.TempDir(),
		NodeID: "certified-transport-required",
		BlockInterval: 0, SyncInterval: 0,
		EnablePeerSync: false,
		RequireConsensusCertificates: true,
	})
	s.setPeerURLs([]string{"http://127.0.0.1:19991"})
	barePosts := 0
	s.transport = &testPeerTransport{
		postBlockFunc: func(_ string, _ ledger.Block) error {
			barePosts++
			return nil
		},
	}
	s.broadcastBlock(ledger.Block{Height: 1, Hash: "signed-quorum-missing"})
	if barePosts != 0 {
		t.Fatalf("certificate-required mode downgraded to %d unsigned PostBlock calls", barePosts)
	}

	// Legacy noncertificate networks can still send ordinary blocks:
	// the fail-closed constraint applies only to certified consensus.
	legacy := newTestServer(t, Config{
		DataDir: t.TempDir(),
		NodeID: "uncertified-transport",
		BlockInterval: 0, SyncInterval: 0,
		EnablePeerSync: false,
		RequireConsensusCertificates: false,
	})
	legacy.setPeerURLs([]string{"http://127.0.0.1:19992"})
	legacyPosts := 0
	legacy.transport = &testPeerTransport{
		postBlockFunc: func(_ string, _ ledger.Block) error {
			legacyPosts++
			return nil
		},
	}
	legacy.broadcastBlock(ledger.Block{Height: 1, Hash: "legacy-block"})
	if legacyPosts != 1 {
		t.Fatalf("legacy noncertificate broadcast changed unexpectedly: calls=%d", legacyPosts)
	}
}
