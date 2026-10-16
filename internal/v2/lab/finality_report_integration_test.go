package lab

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"os"
	"testing"
	"time"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/node"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

const (
	finalityReportValidatorCount = 7
	finalityReportTxPerBlock     = 4
	finalityReportWarmupBlocks   = 1
	finalityReportMeasuredBlocks = 2
)

type finalityReportSigner struct {
	key     *ecdsa.PrivateKey
	account types.AccountID
	input   object.Object
}

func TestV2LabFinalityReportFromConsensusCommit(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("v2-finality-report")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	validators, validatorKeys := finalityReportValidators(t, network, finalityReportValidatorCount)
	validatorRoot, err := validators.Root()
	if err != nil {
		t.Fatal(err)
	}

	totalBlocks := finalityReportWarmupBlocks + finalityReportMeasuredBlocks
	totalTransactions := totalBlocks * finalityReportTxPerBlock
	signers := make([]finalityReportSigner, totalTransactions)
	inputs := make([]object.Object, totalTransactions)
	for i := range signers {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
		account := types.AccountIDFromPublicKey(pub)
		out, err := object.NewCoinOutput(account, native, 100)
		if err != nil {
			t.Fatal(err)
		}
		input := object.Object{
			ID:      types.ObjectIDFromTransaction(types.HashBytes("v2-finality-report-input", []byte{byte(i + 1)}), 0),
			Version: 1,
			Owner:   account,
			Kind:    out.Kind,
			Data:    out.Data,
		}
		signers[i] = finalityReportSigner{key: key, account: account, input: input}
		inputs[i] = input
	}

	stores := make([]*worldstate.Memory, finalityReportValidatorCount)
	runtimes := make([]*node.Runtime, finalityReportValidatorCount)
	for i := range stores {
		stores[i] = worldstate.NewMemory()
		if _, err := stores[i].Apply(nil, inputs); err != nil {
			t.Fatal(err)
		}
		runtime, err := node.NewRuntime(
			network,
			native,
			validatorRoot,
			map[uint32]worldstate.Backend{0: stores[i]},
			4,
		)
		if err != nil {
			t.Fatal(err)
		}
		runtimes[i] = runtime
	}

	recipient := types.AccountIDFromPublicKey([]byte("v2-finality-report-recipient"))
	var measuredStart time.Time
	var measuredEnd time.Time
	samples := make([]FinalitySample, 0, finalityReportMeasuredBlocks*finalityReportTxPerBlock)
	warmupFinalized := uint64(0)
	nextSubmission := time.Now()

	for blockIndex := 0; blockIndex < totalBlocks; blockIndex++ {
		if wait := time.Until(nextSubmission); wait > 0 {
			time.Sleep(wait)
		}
		height := uint64(blockIndex + 1)
		startIndex := blockIndex * finalityReportTxPerBlock
		root := stores[0].Root()
		transactions := make([]tx.Transaction, finalityReportTxPerBlock)
		for i := range transactions {
			signer := signers[startIndex+i]
			witness, proof, ok := stores[0].Proof(signer.input.ID)
			if !ok {
				t.Fatalf("missing input proof for block %d tx %d", height, i)
			}
			witnessHash := witness.Hash()
			payment, err := object.NewCoinOutput(recipient, native, 25)
			if err != nil {
				t.Fatal(err)
			}
			change, err := object.NewCoinOutput(signer.account, native, 74)
			if err != nil {
				t.Fatal(err)
			}
			transaction := tx.Transaction{
				Version:   tx.Version,
				Network:   network,
				ShardID:   0,
				StateRoot: root,
				Inputs: []tx.InputRef{{
					ObjectID: signer.input.ID, Version: signer.input.Version, ObjectHash: witnessHash,
				}},
				Outputs:    []object.OutputSpec{payment, change},
				Operations: []tx.Operation{{Kind: tx.OpTransfer}},
				Fee:        1,
				Witnesses:  []tx.Witness{{Object: witness, Proof: proof}},
			}
			transaction.Salt[0] = byte(height)
			transaction.Salt[1] = byte(i + 1)
			if err := transaction.Sign(signer.key); err != nil {
				t.Fatal(err)
			}
			transactions[i] = transaction
		}

		submittedAt := time.Now()
		nextSubmission = submittedAt.Add(time.Millisecond)
		candidates := make([]node.Candidate, finalityReportValidatorCount)
		for i, runtime := range runtimes {
			candidate, err := runtime.BuildCandidate(height, map[uint32]node.ShardBatch{
				0: {Transactions: transactions},
			})
			if err != nil {
				t.Fatalf("validator %d failed candidate build at height %d: %v", i, height, err)
			}
			candidates[i] = candidate
			if i > 0 && v2consensus.HeaderConsensusHash(candidate.Header) != v2consensus.HeaderConsensusHash(candidates[0].Header) {
				t.Fatalf("validator %d derived different header at height %d", i, height)
			}
		}

		proposer, err := validators.Proposer(height, 0)
		if err != nil {
			t.Fatal(err)
		}
		proposal, err := v2consensus.SignProposal(validatorKeys[proposer.ID], candidates[0].Header, 0)
		if err != nil {
			t.Fatal(err)
		}
		headerHash := v2consensus.HeaderConsensusHash(candidates[0].Header)
		votes := make([]v2consensus.Vote, 0, 5)
		for i := 0; i < 5; i++ {
			if err := node.VerifyProposalAgainstCandidate(candidates[i], proposal, validators); err != nil {
				t.Fatalf("validator %d rejected matching proposal at height %d: %v", i, height, err)
			}
			validator := validators.Validators[i]
			vote, err := v2consensus.SignVote(
				validatorKeys[validator.ID],
				network,
				height,
				0,
				headerHash,
			)
			if err != nil {
				t.Fatal(err)
			}
			votes = append(votes, vote)
		}
		certificate, err := validators.BuildCertificate(proposal, votes)
		if err != nil {
			t.Fatal(err)
		}
		for i, runtime := range runtimes {
			if _, err := runtime.Commit(candidates[i], certificate, validators); err != nil {
				t.Fatalf("validator %d failed finalized commit at height %d: %v", i, height, err)
			}
		}
		finalizedAt := time.Now()

		if blockIndex < finalityReportWarmupBlocks {
			warmupFinalized += uint64(len(transactions))
			continue
		}
		if measuredStart.IsZero() {
			measuredStart = submittedAt
		}
		measuredEnd = finalizedAt
		for range transactions {
			samples = append(samples, FinalitySample{
				SubmittedAt: submittedAt,
				FinalizedAt: finalizedAt,
			})
		}
	}

	report, err := BuildFinalityBenchmarkReport(FinalityBenchmarkInput{
		Config: FinalityBenchmarkConfig{
			ShardCount:        1,
			ValidatorCount:    finalityReportValidatorCount,
			TransactionsBlock: finalityReportTxPerBlock,
			BlockCadenceMS:    1,
			CrossShardBPS:     0,
			Workload:          "native-transfer-v2-lab-single-process",
		},
		Environment: FinalityBenchmarkEnvironment{
			CommitSHA: os.Getenv("GITHUB_SHA"),
		},
		WarmupFinalized:   warmupFinalized,
		MeasuredStart:     measuredStart,
		MeasuredEnd:       measuredEnd,
		Samples:           samples,
		SafetyFailures:    0,
		LivenessFailures:  0,
		RejectedTransactions: 0,
		Errors:               0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid ||
		report.WarmupFinalized != finalityReportWarmupBlocks*finalityReportTxPerBlock ||
		report.MeasuredFinalized != finalityReportMeasuredBlocks*finalityReportTxPerBlock ||
		report.FinalizedTPS <= 0 ||
		report.FinalityP50NS <= 0 ||
		report.FinalityP50NS > report.FinalityP95NS ||
		report.FinalityP95NS > report.FinalityP99NS {
		t.Fatalf("unexpected finalized-through-consensus report: %#v", report)
	}
	for i, runtime := range runtimes {
		if runtime.Height != uint64(totalBlocks) {
			t.Fatalf("validator %d ended at height %d, want %d", i, runtime.Height, totalBlocks)
		}
	}
	raw, err := report.JSON()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("FINALIZED_THROUGH_CONSENSUS_REPORT=%s", raw)
}

func finalityReportValidators(
	t *testing.T,
	network types.NetworkID,
	count int,
) (v2consensus.ValidatorSet, map[types.ValidatorID]*ecdsa.PrivateKey) {
	t.Helper()
	set := v2consensus.ValidatorSet{
		Network:    network,
		Validators: make([]v2consensus.Validator, 0, count),
	}
	keys := make(map[types.ValidatorID]*ecdsa.PrivateKey, count)
	for i := 0; i < count; i++ {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
		id := types.ValidatorIDFromPublicKey(pub)
		set.Validators = append(set.Validators, v2consensus.Validator{
			ID: id, PublicKey: pub, Power: 10_000,
		})
		keys[id] = key
	}
	return set, keys
}
