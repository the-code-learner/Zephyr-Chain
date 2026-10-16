package node

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/binary"
	"errors"
	"math/big"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	p2p "github.com/zephyr-chain/zephyr-chain/internal/v2/network/p2p"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

const (
	defaultDevnetValidators = 7
	maxDevnetValidators     = 20
)

type protocolV2Devnet struct {
	network        types.NetworkID
	native         types.TokenID
	validators     v2consensus.ValidatorSet
	keys           []*ecdsa.PrivateKey
	nodes          []*p2p.Node
	services       []*Service
	stores         []*worldstate.Memory
	observerNode   *p2p.Node
	observer       *Service
	observerStore  *worldstate.Memory
	clientNode     *p2p.Node
	initialObject  object.Object
	senderKey      *ecdsa.PrivateKey
	sender         types.AccountID
	recipient      types.AccountID
}

func TestProtocolV2DevnetTransactionVotingAndFinality(t *testing.T) {
	validatorCount := devnetValidatorCount(t)
	devnet := newProtocolV2Devnet(t, validatorCount)
	defer devnet.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	proposer := devnet.proposerIndex(t, 1)
	if err := devnet.clientNode.Connect(ctx, devnet.nodes[proposer].AddrInfo()); err != nil {
		t.Fatal(err)
	}

	transaction := devnet.transferTransaction(t)
	raw, err := transaction.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	response, err := devnet.clientNode.SendTransaction(ctx, devnet.nodes[proposer].ID(), raw)
	if err != nil {
		t.Fatal(err)
	}
	transactionID := transaction.ID()
	if len(response) != len(transactionID) || string(response) != string(transactionID[:]) {
		t.Fatalf("transaction acknowledgement mismatch: got %x want %x", response, transactionID)
	}

	snapshot, err := devnet.services[proposer].Propose(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Header.Height != 1 {
		t.Fatalf("finalized height = %d, want 1", snapshot.Header.Height)
	}
	quorum := (2*validatorCount)/3 + 1
	if len(snapshot.Certificate.Votes) < quorum {
		t.Fatalf("certificate votes = %d, want at least quorum %d", len(snapshot.Certificate.Votes), quorum)
	}

	paymentID := types.ObjectIDForShard(transactionID, 0, 0)
	for i, service := range devnet.services {
		if service.Runtime.Height != 1 {
			t.Fatalf("validator %d height = %d, want 1", i, service.Runtime.Height)
		}
		if _, ok := devnet.stores[i].GetObject(paymentID); !ok {
			t.Fatalf("validator %d did not materialize finalized payment", i)
		}
		latest, err := service.LatestSnapshot()
		if err != nil || latest.Header.Height != 1 {
			t.Fatalf("validator %d missing finalized snapshot: %v", i, err)
		}
	}
	if devnet.observer.Runtime.Height != 1 {
		t.Fatalf("observer height = %d, want 1", devnet.observer.Runtime.Height)
	}
	if _, ok := devnet.observerStore.GetObject(paymentID); !ok {
		t.Fatal("observer did not materialize finalized payment")
	}
	if _, err := devnet.observer.Propose(ctx); !errors.Is(err, ErrNotValidator) {
		t.Fatalf("observer proposal error = %v, want ErrNotValidator", err)
	}
}

func TestProtocolV2DevnetQuorumFaultBoundary(t *testing.T) {
	t.Run("two validators offline still finalizes", func(t *testing.T) {
		devnet := newProtocolV2Devnet(t, 7)
		defer devnet.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		proposer := devnet.proposerIndex(t, 1)
		offline := devnet.nonProposerIndexes(proposer, 2)
		for _, index := range offline {
			if err := devnet.nodes[index].Close(); err != nil {
				t.Fatal(err)
			}
		}

		snapshot, err := devnet.services[proposer].Propose(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.Certificate.Votes) < 5 {
			t.Fatalf("certificate votes = %d, want quorum 5", len(snapshot.Certificate.Votes))
		}
		for i, service := range devnet.services {
			if containsInt(offline, i) {
				if service.Runtime.Height != 0 {
					t.Fatalf("offline validator %d unexpectedly committed height %d", i, service.Runtime.Height)
				}
				continue
			}
			if service.Runtime.Height != 1 {
				t.Fatalf("active validator %d height = %d, want 1", i, service.Runtime.Height)
			}
		}
		if devnet.observer.Runtime.Height != 1 {
			t.Fatalf("observer height = %d, want 1", devnet.observer.Runtime.Height)
		}
	})

	t.Run("three validators offline cannot finalize", func(t *testing.T) {
		devnet := newProtocolV2Devnet(t, 7)
		defer devnet.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		proposer := devnet.proposerIndex(t, 1)
		offline := devnet.nonProposerIndexes(proposer, 3)
		for _, index := range offline {
			if err := devnet.nodes[index].Close(); err != nil {
				t.Fatal(err)
			}
		}

		if _, err := devnet.services[proposer].Propose(ctx); !errors.Is(err, ErrNoQuorum) {
			t.Fatalf("proposal error = %v, want ErrNoQuorum", err)
		}
		for i, service := range devnet.services {
			if service.Runtime.Height != 0 {
				t.Fatalf("validator %d committed without quorum at height %d", i, service.Runtime.Height)
			}
		}
		if devnet.observer.Runtime.Height != 0 {
			t.Fatalf("observer committed without quorum at height %d", devnet.observer.Runtime.Height)
		}
	})
}

func newProtocolV2Devnet(t *testing.T, validatorCount int) *protocolV2Devnet {
	t.Helper()
	if validatorCount < 4 || validatorCount > maxDevnetValidators {
		t.Fatalf("validator count %d outside supported devnet range [4,%d]", validatorCount, maxDevnetValidators)
	}

	network := types.NetworkID(types.HashBytes("network", []byte("github-ephemeral-devnet-v2")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	keys := make([]*ecdsa.PrivateKey, validatorCount)
	validators := v2consensus.ValidatorSet{Network: network, Validators: make([]v2consensus.Validator, validatorCount)}
	for i := 0; i < validatorCount; i++ {
		key := devnetP256Key(t, "validator", uint64(i))
		keys[i] = key
		public := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
		validators.Validators[i] = v2consensus.Validator{
			ID:        types.ValidatorIDFromPublicKey(public),
			PublicKey: public,
			Power:     10_000,
		}
	}
	validatorRoot, err := validators.Root()
	if err != nil {
		t.Fatal(err)
	}

	senderKey := devnetP256Key(t, "sender", 0)
	senderPublic := elliptic.Marshal(elliptic.P256(), senderKey.PublicKey.X, senderKey.PublicKey.Y)
	sender := types.AccountIDFromPublicKey(senderPublic)
	recipientKey := devnetP256Key(t, "recipient", 0)
	recipientPublic := elliptic.Marshal(elliptic.P256(), recipientKey.PublicKey.X, recipientKey.PublicKey.Y)
	recipient := types.AccountIDFromPublicKey(recipientPublic)
	initialSpec, err := object.NewCoinOutput(sender, native, 100)
	if err != nil {
		t.Fatal(err)
	}
	initial := object.Object{
		ID:      types.ObjectIDForShard(types.HashBytes("devnet-input", []byte("0")), 0, 0),
		Version: 1,
		Owner:   sender,
		Kind:    initialSpec.Kind,
		Data:    initialSpec.Data,
	}

	devnet := &protocolV2Devnet{
		network:       network,
		native:        native,
		validators:    validators,
		keys:          keys,
		nodes:         make([]*p2p.Node, validatorCount),
		services:      make([]*Service, validatorCount),
		stores:        make([]*worldstate.Memory, validatorCount),
		initialObject: initial,
		senderKey:     senderKey,
		sender:        sender,
		recipient:     recipient,
	}

	for i := 0; i < validatorCount; i++ {
		transport, err := p2p.New(p2p.Config{
			Network:         network,
			ListenAddrs:     []string{"/ip4/127.0.0.1/udp/0/quic-v1"},
			MaxMessageBytes: MaxNetworkMessageBytes,
		})
		if err != nil {
			t.Fatal(err)
		}
		devnet.nodes[i] = transport
		store := worldstate.NewMemory()
		if _, err := store.Apply(nil, []object.Object{initial}); err != nil {
			t.Fatal(err)
		}
		devnet.stores[i] = store
		runtimeNode, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: store}, 2)
		if err != nil {
			t.Fatal(err)
		}
		service, err := NewService(runtimeNode, validators, keys[i], transport)
		if err != nil {
			t.Fatal(err)
		}
		devnet.services[i] = service
	}

	observerTransport, err := p2p.New(p2p.Config{
		Network:         network,
		ListenAddrs:     []string{"/ip4/127.0.0.1/udp/0/quic-v1"},
		MaxMessageBytes: MaxNetworkMessageBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	devnet.observerNode = observerTransport
	observerStore := worldstate.NewMemory()
	if _, err := observerStore.Apply(nil, []object.Object{initial}); err != nil {
		t.Fatal(err)
	}
	devnet.observerStore = observerStore
	observerRuntime, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: observerStore}, 2)
	if err != nil {
		t.Fatal(err)
	}
	observer, err := NewService(observerRuntime, validators, nil, observerTransport)
	if err != nil {
		t.Fatal(err)
	}
	devnet.observer = observer

	client, err := p2p.New(p2p.Config{
		Network:         network,
		ListenAddrs:     []string{"/ip4/127.0.0.1/udp/0/quic-v1"},
		MaxMessageBytes: MaxNetworkMessageBytes,
	})
	if err != nil {
		t.Fatal(err)
	}
	devnet.clientNode = client

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	for i := range devnet.nodes {
		peers := make([]peer.ID, 0, validatorCount)
		for j := range devnet.nodes {
			if i == j {
				continue
			}
			if err := devnet.nodes[i].Connect(ctx, devnet.nodes[j].AddrInfo()); err != nil {
				t.Fatal(err)
			}
			peers = append(peers, devnet.nodes[j].ID())
		}
		if err := devnet.nodes[i].Connect(ctx, observerTransport.AddrInfo()); err != nil {
			t.Fatal(err)
		}
		peers = append(peers, observerTransport.ID())
		devnet.services[i].SetPeers(peers)
	}
	return devnet
}

func (d *protocolV2Devnet) Close() {
	if d == nil {
		return
	}
	if d.clientNode != nil {
		_ = d.clientNode.Close()
	}
	if d.observerNode != nil {
		_ = d.observerNode.Close()
	}
	for _, transport := range d.nodes {
		if transport != nil {
			_ = transport.Close()
		}
	}
}

func (d *protocolV2Devnet) proposerIndex(t *testing.T, height uint64) int {
	t.Helper()
	expected, err := d.validators.Proposer(height, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, key := range d.keys {
		id, err := validatorID(key)
		if err != nil {
			t.Fatal(err)
		}
		if id == expected.ID {
			return i
		}
	}
	t.Fatal("scheduled proposer not found")
	return -1
}

func (d *protocolV2Devnet) nonProposerIndexes(proposer, count int) []int {
	out := make([]int, 0, count)
	for i := range d.nodes {
		if i == proposer {
			continue
		}
		out = append(out, i)
		if len(out) == count {
			break
		}
	}
	return out
}

func (d *protocolV2Devnet) transferTransaction(t *testing.T) tx.Transaction {
	t.Helper()
	root := d.stores[0].Root()
	witness, proof, ok := d.stores[0].Proof(d.initialObject.ID)
	if !ok {
		t.Fatal("missing initial devnet object proof")
	}
	payment, err := object.NewCoinOutput(d.recipient, d.native, 25)
	if err != nil {
		t.Fatal(err)
	}
	change, err := object.NewCoinOutput(d.sender, d.native, 74)
	if err != nil {
		t.Fatal(err)
	}
	transaction := tx.Transaction{
		Version:   tx.Version,
		Network:   d.network,
		ShardID:   0,
		StateRoot: root,
		Inputs: []tx.InputRef{{
			ObjectID:   d.initialObject.ID,
			Version:    d.initialObject.Version,
			ObjectHash: witness.Hash(),
		}},
		Outputs:    []object.OutputSpec{payment, change},
		Operations: []tx.Operation{{Kind: tx.OpTransfer}},
		Fee:        1,
		Witnesses:  []tx.Witness{{Object: witness, Proof: proof}},
	}
	binary.BigEndian.PutUint64(transaction.Salt[:8], 1)
	binary.BigEndian.PutUint64(transaction.Salt[8:16], 1)
	if err := transaction.Sign(d.senderKey); err != nil {
		t.Fatal(err)
	}
	return transaction
}

func devnetValidatorCount(t *testing.T) int {
	t.Helper()
	raw := os.Getenv("ZEPHYR_DEVNET_VALIDATORS")
	if raw == "" {
		return defaultDevnetValidators
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 4 || value > maxDevnetValidators {
		t.Fatalf("invalid ZEPHYR_DEVNET_VALIDATORS=%q; expected integer in [4,%d]", raw, maxDevnetValidators)
	}
	return value
}

func devnetP256Key(t *testing.T, domain string, index uint64) *ecdsa.PrivateKey {
	t.Helper()
	var seed [8]byte
	binary.BigEndian.PutUint64(seed[:], index)
	hash := types.HashBytes("devnet-"+domain, seed[:])
	curve := elliptic.P256()
	n := curve.Params().N
	d := new(big.Int).SetBytes(hash[:])
	d.Mod(d, new(big.Int).Sub(n, big.NewInt(1)))
	d.Add(d, big.NewInt(1))
	x, y := curve.ScalarBaseMult(d.Bytes())
	if x == nil || y == nil {
		t.Fatal("unable to derive deterministic P-256 key")
	}
	return &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{Curve: curve, X: x, Y: y},
		D:         d,
	}
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
