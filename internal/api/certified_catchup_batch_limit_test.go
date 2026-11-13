package api

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

// A lagging validator must never receive more than the bounded batch size
// from one failed latest-block POST, and later retry passes must resume at
// its newly QC-verified ancestry instead of skipping intermediate heights.
func TestCertifiedCatchupRespectsEightBlockBoundAndResumesFromVerifiedTip(t *testing.T) {
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	producer := newTestServer(t, Config{
		DataDir: t.TempDir(), NodeID: "bounded-catchup-sender",
		ValidatorPrivateKey:   encodedPrivateKey(t, proposer.privateKey),
		EnableBlockProduction: true, EnablePeerSync: false,
		BlockInterval: 0, SyncInterval: 0, MaxTransactionsPerBlock: 10,
		RequireConsensusCertificates: true,
	})
	replica := newTestServer(t, Config{
		DataDir: t.TempDir(), NodeID: "bounded-catchup-replica",
		ValidatorPrivateKey:   encodedPrivateKey(t, voter.privateKey),
		EnableBlockProduction: false, EnablePeerSync: false,
		BlockInterval: 0, SyncInterval: 0, MaxTransactionsPerBlock: 10,
		RequireConsensusCertificates: true,
	})
	receiver := httptest.NewServer(replica.Handler())
	defer receiver.Close()

	validators := []dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}
	for _, server := range []*Server{producer, replica} {
		if _, err := server.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
			t.Fatalf("set validators: %v", err)
		}
	}

	// Both ledgers must have identical pre-block funding. Any account
	// credited after height 1 would create an artificial state-root fork.
	const targetHeight uint64 = maxCertifiedCatchupBlocks + 2
	envelopes := make([]tx.Envelope, 0, targetHeight)
	for i := uint64(1); i <= targetHeight; i++ {
		envelope := signedEnvelope(t, 25, 1, fmt.Sprintf("bounded-catchup-%d", i))
		envelopes = append(envelopes, envelope)
		for _, server := range []*Server{producer, replica} {
			if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
				t.Fatalf("prefund sender %d: %v", i, err)
			}
		}
	}

	var previousHash string
	blocks := make([]ledger.Block, 0, targetHeight)
	for height := uint64(1); height <= targetHeight; height++ {
		envelope := envelopes[height-1]
		if _, err := producer.ledger.Accept(envelope); err != nil {
			t.Fatalf("accept height %d: %v", height, err)
		}
		scheduledSigner := proposer
		if height%2 == 0 {
			scheduledSigner = voter
		}
		proposal := signedConsensusProposalForServer(t, producer, scheduledSigner, height, 0, previousHash, time.Now().UTC(), []tx.Envelope{envelope})
		if err := producer.ledger.RecordProposal(proposal); err != nil {
			t.Fatalf("proposal height %d: %v", height, err)
		}
		for _, signer := range []consensusSigner{proposer, voter} {
			vote := signedConsensusVote(t, signer, height, 0, proposal.BlockHash)
			if _, _, err := producer.ledger.RecordVote(vote); err != nil {
				t.Fatalf("signed quorum at height %d: %v", height, err)
			}
		}
		block, err := producer.ledger.ProduceBlockWithOptions(10, proposal.ProducedAt, true)
		if err != nil {
			t.Fatalf("finalize height %d: %v", height, err)
		}
		previousHash = block.Hash
		blocks = append(blocks, block)
	}
	if got := replica.ledger.Status().Height; got != 0 {
		t.Fatalf("replica advanced before certified delivery, height=%d", got)
	}

	transport, ok := producer.transport.(certifiedBlockDeliveryTransport)
	if !ok {
		t.Fatal("sender lacks signed certified transport")
	}
	latestEvidence, ok := producer.ledger.CertifiedBlockEvidenceAt(targetHeight)
	if !ok {
		t.Fatal("latest finalized QC not available")
	}
	latest := certifiedBlockDelivery{Block: blocks[len(blocks)-1], Evidence: latestEvidence}

	// First attempt: the target rejects the latest block with HTTP 409;
	// the sender must deliver exactly the oldest eight certified heights.
	err := producer.deliverCertifiedReplay(receiver.URL, latest, transport)
	if err == nil || !strings.Contains(err.Error(), "certified catchup bounded") {
		t.Fatalf("expected explicit bounded-retry signal after first batch: %v", err)
	}
	if got := replica.ledger.Status().Height; got != maxCertifiedCatchupBlocks {
		t.Fatalf("first catchup batch advanced to height %d; expected only %d", got, maxCertifiedCatchupBlocks)
	}
	if got, ok := replica.ledger.BlockAtHeight(maxCertifiedCatchupBlocks); !ok || got.Hash != blocks[maxCertifiedCatchupBlocks-1].Hash {
		t.Fatal("first batch did not preserve certified canonical ancestry")
	}

	// Second attempt refreshes the receiver's actual tip and delivers only
	// heights 9 and 10. Peer sync remains disabled; no unsigned fallback.
	if err := producer.deliverCertifiedReplay(receiver.URL, latest, transport); err != nil {
		t.Fatalf("bounded signed retry failed to resume from height 8: %v", err)
	}
	if got := replica.ledger.Status().Height; got != targetHeight {
		t.Fatalf("replica did not complete certified catchup: got %d want %d", got, targetHeight)
	}
	for _, block := range blocks {
		if imported, ok := replica.ledger.BlockAtHeight(block.Height); !ok || imported.Hash != block.Hash {
			t.Fatalf("canonical block mismatch at height %d", block.Height)
		}
		if _, ok := replica.ledger.Certificate(block.Height, 0, block.Hash); !ok {
			t.Fatalf("replica missing independently signed QC at height %d", block.Height)
		}
	}
}
