package lightapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

type fakeProvider struct {
	snapshot Snapshot
	state    worldstate.Backend
}

func (f fakeProvider) LatestSnapshot() (Snapshot, error) { return f.snapshot, nil }
func (f fakeProvider) ShardState(shardID uint32) (worldstate.Backend, bool) {
	return f.state, shardID == 0
}

func TestLightObjectEndpointReturnsVerifiableBundle(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("light-api")))
	owner := types.AccountIDFromPublicKey([]byte("owner"))
	token := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	id := types.ObjectIDFromTransaction(types.HashBytes("seed", []byte("coin")), 0)
	out, _ := object.NewCoinOutput(owner, token, 100)
	obj := object.Object{ID: id, Version: 1, Owner: owner, Kind: out.Kind, Data: out.Data}
	store := worldstate.NewMemory()
	root, err := store.Apply(nil, []object.Object{obj})
	if err != nil {
		t.Fatal(err)
	}
	commitment := sharding.Commitment{ShardID: 0, StateRoot: root, ReceiptRoot: merkle.Root(nil), DataRoot: merkle.Root(nil)}
	commitmentRoot, err := sharding.CommitmentRoot([]sharding.Commitment{commitment})
	if err != nil {
		t.Fatal(err)
	}
	validatorKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub := elliptic.Marshal(elliptic.P256(), validatorKey.PublicKey.X, validatorKey.PublicKey.Y)
	validatorID := types.ValidatorIDFromPublicKey(pub)
	validators := v2consensus.ValidatorSet{Network: network, Validators: []v2consensus.Validator{{ID: validatorID, PublicKey: pub, Power: 10}}}
	header := sharding.GlobalHeader{Version: 2, Network: network, Height: 1, ShardCommitmentRoot: commitmentRoot, ValidatorRoot: types.HashBytes("validators", []byte("root")), DataRoot: merkle.Root(nil)}
	proposal, err := v2consensus.SignProposal(validatorKey, header, 0)
	if err != nil {
		t.Fatal(err)
	}
	headerHash := v2consensus.HeaderConsensusHash(header)
	vote, err := v2consensus.SignVote(validatorKey, network, 1, 0, headerHash)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := validators.BuildCertificate(proposal, []v2consensus.Vote{vote})
	if err != nil {
		t.Fatal(err)
	}
	header.CertificateHash = certificate.Hash()
	provider := fakeProvider{snapshot: Snapshot{Header: header, Certificate: certificate, Commitments: []sharding.Commitment{commitment}, Validators: validators}, state: store}
	server := Server{Provider: provider}.Handler()

	req := httptest.NewRequest(http.MethodGet, "/v2/light/object?shard=0&id="+id.String(), nil)
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", res.Code, res.Body.String())
	}
	if body := res.Body.String(); len(body) < 100 || !contains(body, id.String()) {
		t.Fatalf("proof bundle missing object identity: %s", body)
	}
}

func contains(value, needle string) bool {
	for i := 0; i+len(needle) <= len(value); i++ {
		if value[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
