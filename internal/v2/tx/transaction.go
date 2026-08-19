package tx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"math/big"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/state"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	Version uint16 = 2

	OpTransfer       uint16 = 1
	OpCreateToken    uint16 = 2
	OpDeployContract uint16 = 3
	OpContractCall   uint16 = 4
	OpComputeOffer   uint16 = 5
	OpComputeJob     uint16 = 6
	OpComputeResult  uint16 = 7

	MaxInputs      = 4096
	MaxOutputs     = 4096
	MaxOperations  = 64
	MaxOpPayload   = 4 << 20
	MaxWireBytes   = 32 << 20
	MaxIntentBytes = 24 << 20
)

var (
	ErrVersion      = errors.New("invalid transaction version")
	ErrNetwork      = errors.New("invalid transaction network")
	ErrSender       = errors.New("invalid transaction sender")
	ErrSignature    = errors.New("invalid transaction signature")
	ErrCanonicalSig = errors.New("transaction signature must use low-S P-256 form")
	ErrStructure    = errors.New("invalid proof-carrying transaction structure")
	ErrWitness      = errors.New("invalid transaction state witness")
	ErrExpired      = errors.New("transaction expired")
	ErrWire         = errors.New("invalid canonical transaction wire payload")
)

type InputRef struct {
	ObjectID   types.ObjectID
	Version    uint64
	ObjectHash types.Hash
}

type Witness struct {
	Object object.Object
	Proof  state.Proof
}

type Operation struct {
	Kind    uint16
	Payload []byte
}

type Transaction struct {
	Version          uint16
	Network          types.NetworkID
	Sender           types.AccountID
	SenderPublicKey  []byte
	ShardID          uint32
	StateRoot        types.Hash
	Salt             [16]byte
	Inputs           []InputRef
	Outputs          []object.OutputSpec
	Operations       []Operation
	Fee              uint64
	ValidUntilHeight uint64
	Signature        []byte
	Witnesses        []Witness
}

func (t Transaction) IntentBytes() []byte {
	var w codec.Writer
	writeIntent(&w, t)
	return w.BytesCopy()
}

func (t Transaction) SigningDigest() types.Hash {
	return types.Hash(codec.DomainHash("zephyr/transaction-signing/v2", t.IntentBytes()))
}

func (t Transaction) ID() types.Hash {
	return types.Hash(codec.DomainHash("zephyr/transaction-id/v2", t.IntentBytes()))
}

func (t Transaction) MarshalBinary() ([]byte, error) {
	if err := t.ValidateStatic(); err != nil {
		return nil, err
	}
	if len(t.Witnesses) != len(t.Inputs) {
		return nil, ErrWitness
	}
	var w codec.Writer
	w.Bytes(t.IntentBytes())
	w.Bytes(t.Signature)
	w.U32(uint32(len(t.Witnesses)))
	for _, witness := range t.Witnesses {
		w.Bytes(witness.Object.CanonicalBytes())
		w.Bytes(witness.Proof.MarshalBinary())
	}
	payload := w.BytesCopy()
	if len(payload) > MaxWireBytes {
		return nil, ErrWire
	}
	return payload, nil
}

func ParseTransaction(data []byte) (Transaction, error) {
	if len(data) == 0 || len(data) > MaxWireBytes {
		return Transaction{}, ErrWire
	}
	r := codec.NewReader(data)
	intent, err := r.Bytes(MaxIntentBytes)
	if err != nil {
		return Transaction{}, ErrWire
	}
	t, err := parseIntent(intent)
	if err != nil {
		return Transaction{}, err
	}
	signature, err := r.Bytes(64)
	if err != nil || len(signature) != 64 {
		return Transaction{}, ErrWire
	}
	t.Signature = signature
	count, err := r.U32()
	if err != nil || count > MaxInputs || int(count) != len(t.Inputs) {
		return Transaction{}, ErrWitness
	}
	t.Witnesses = make([]Witness, int(count))
	for i := range t.Witnesses {
		objectBytes, err := r.Bytes(object.MaxObjectDataBytes + 128)
		if err != nil {
			return Transaction{}, ErrWire
		}
		obj, err := object.ParseObject(objectBytes)
		if err != nil {
			return Transaction{}, ErrWire
		}
		proofBytes, err := r.Bytes(16 << 10)
		if err != nil {
			return Transaction{}, ErrWire
		}
		proof, err := state.ParseProof(proofBytes)
		if err != nil {
			return Transaction{}, ErrWire
		}
		t.Witnesses[i] = Witness{Object: obj, Proof: proof}
	}
	if err := r.Done(); err != nil {
		return Transaction{}, ErrWire
	}
	if err := t.ValidateStatic(); err != nil {
		return Transaction{}, err
	}
	return t, nil
}

func (t *Transaction) Sign(privateKey *ecdsa.PrivateKey) error {
	if privateKey == nil || privateKey.Curve != elliptic.P256() {
		return ErrSender
	}
	t.Version = Version
	t.SenderPublicKey = elliptic.Marshal(elliptic.P256(), privateKey.PublicKey.X, privateKey.PublicKey.Y)
	t.Sender = types.AccountIDFromPublicKey(t.SenderPublicKey)
	digest := t.SigningDigest()
	r, s, err := ecdsa.Sign(rand.Reader, privateKey, digest[:])
	if err != nil {
		return err
	}
	s = normalizeLowS(s)
	t.Signature = append(pad32(r), pad32(s)...)
	return nil
}

func (t Transaction) ValidateStatic() error {
	if t.Version != Version {
		return ErrVersion
	}
	if types.IsZero32([32]byte(t.Network)) || types.IsZero32([32]byte(t.StateRoot)) {
		return ErrNetwork
	}
	if len(t.SenderPublicKey) != 65 {
		return ErrSender
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), t.SenderPublicKey)
	if x == nil || y == nil {
		return ErrSender
	}
	if types.AccountIDFromPublicKey(t.SenderPublicKey) != t.Sender {
		return ErrSender
	}
	var zeroSalt [16]byte
	if t.Salt == zeroSalt || len(t.Inputs) > MaxInputs || len(t.Outputs) > MaxOutputs ||
		len(t.Operations) == 0 || len(t.Operations) > MaxOperations {
		return ErrStructure
	}
	seenInputs := map[types.ObjectID]struct{}{}
	for _, in := range t.Inputs {
		if types.IsZero32([32]byte(in.ObjectID)) || in.Version == 0 || types.IsZero32([32]byte(in.ObjectHash)) {
			return ErrStructure
		}
		if _, ok := seenInputs[in.ObjectID]; ok {
			return ErrStructure
		}
		seenInputs[in.ObjectID] = struct{}{}
	}
	for _, out := range t.Outputs {
		if err := out.Validate(); err != nil {
			return ErrStructure
		}
	}
	for _, op := range t.Operations {
		if op.Kind == 0 || len(op.Payload) > MaxOpPayload {
			return ErrStructure
		}
	}
	return verifySignature(&ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, t.SigningDigest(), t.Signature)
}

func (t Transaction) ValidateAtHeight(height uint64) error {
	if err := t.ValidateStatic(); err != nil {
		return err
	}
	if t.ValidUntilHeight != 0 && height > t.ValidUntilHeight {
		return ErrExpired
	}
	return nil
}

func (t Transaction) VerifyForNetwork(network types.NetworkID) error {
	if t.Network != network {
		return ErrNetwork
	}
	return t.ValidateStatic()
}

func (t Transaction) VerifyWitnesses() error {
	if len(t.Inputs) != len(t.Witnesses) {
		return ErrWitness
	}
	witnesses := make(map[types.ObjectID]Witness, len(t.Witnesses))
	for _, witness := range t.Witnesses {
		if err := witness.Object.Validate(); err != nil {
			return ErrWitness
		}
		if _, exists := witnesses[witness.Object.ID]; exists {
			return ErrWitness
		}
		witnesses[witness.Object.ID] = witness
	}
	for _, in := range t.Inputs {
		witness, ok := witnesses[in.ObjectID]
		if !ok || witness.Object.Version != in.Version || witness.Object.Hash() != in.ObjectHash || !witness.Proof.Exists {
			return ErrWitness
		}
		key := types.Hash(in.ObjectID)
		value := in.ObjectHash[:]
		if !state.Verify(t.StateRoot, key, value, witness.Proof) {
			return ErrWitness
		}
	}
	return nil
}

func writeIntent(w *codec.Writer, t Transaction) {
	w.U16(t.Version)
	w.Fixed(t.Network[:])
	w.Fixed(t.Sender[:])
	w.Bytes(t.SenderPublicKey)
	w.U32(t.ShardID)
	w.Fixed(t.StateRoot[:])
	w.Fixed(t.Salt[:])
	w.U32(uint32(len(t.Inputs)))
	for _, in := range t.Inputs {
		w.Fixed(in.ObjectID[:])
		w.U64(in.Version)
		w.Fixed(in.ObjectHash[:])
	}
	w.U32(uint32(len(t.Outputs)))
	for _, out := range t.Outputs {
		w.Bytes(out.CanonicalBytes())
	}
	w.U32(uint32(len(t.Operations)))
	for _, op := range t.Operations {
		w.U16(op.Kind)
		w.Bytes(op.Payload)
	}
	w.U64(t.Fee)
	w.U64(t.ValidUntilHeight)
}

func parseIntent(data []byte) (Transaction, error) {
	if len(data) == 0 || len(data) > MaxIntentBytes {
		return Transaction{}, ErrWire
	}
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil {
		return Transaction{}, ErrWire
	}
	networkBytes, err := r.Fixed(32)
	if err != nil {
		return Transaction{}, ErrWire
	}
	senderBytes, err := r.Fixed(32)
	if err != nil {
		return Transaction{}, ErrWire
	}
	publicKey, err := r.Bytes(65)
	if err != nil {
		return Transaction{}, ErrWire
	}
	shardID, err := r.U32()
	if err != nil {
		return Transaction{}, ErrWire
	}
	rootBytes, err := r.Fixed(32)
	if err != nil {
		return Transaction{}, ErrWire
	}
	saltBytes, err := r.Fixed(16)
	if err != nil {
		return Transaction{}, ErrWire
	}
	inputCount, err := r.U32()
	if err != nil || inputCount > MaxInputs {
		return Transaction{}, ErrWire
	}
	t := Transaction{
		Version: version, SenderPublicKey: publicKey, ShardID: shardID,
		Inputs: make([]InputRef, int(inputCount)),
	}
	copy(t.Network[:], networkBytes)
	copy(t.Sender[:], senderBytes)
	copy(t.StateRoot[:], rootBytes)
	copy(t.Salt[:], saltBytes)

	for i := range t.Inputs {
		idBytes, err := r.Fixed(32)
		if err != nil {
			return Transaction{}, ErrWire
		}
		version, err := r.U64()
		if err != nil {
			return Transaction{}, ErrWire
		}
		hashBytes, err := r.Fixed(32)
		if err != nil {
			return Transaction{}, ErrWire
		}
		copy(t.Inputs[i].ObjectID[:], idBytes)
		t.Inputs[i].Version = version
		copy(t.Inputs[i].ObjectHash[:], hashBytes)
	}
	outputCount, err := r.U32()
	if err != nil || outputCount > MaxOutputs {
		return Transaction{}, ErrWire
	}
	t.Outputs = make([]object.OutputSpec, int(outputCount))
	for i := range t.Outputs {
		outputBytes, err := r.Bytes(object.MaxObjectDataBytes + 64)
		if err != nil {
			return Transaction{}, ErrWire
		}
		out, err := object.ParseOutputSpec(outputBytes)
		if err != nil {
			return Transaction{}, ErrWire
		}
		t.Outputs[i] = out
	}
	opCount, err := r.U32()
	if err != nil || opCount == 0 || opCount > MaxOperations {
		return Transaction{}, ErrWire
	}
	t.Operations = make([]Operation, int(opCount))
	for i := range t.Operations {
		kind, err := r.U16()
		if err != nil {
			return Transaction{}, ErrWire
		}
		payload, err := r.Bytes(MaxOpPayload)
		if err != nil {
			return Transaction{}, ErrWire
		}
		t.Operations[i] = Operation{Kind: kind, Payload: payload}
	}
	fee, err := r.U64()
	if err != nil {
		return Transaction{}, ErrWire
	}
	validUntil, err := r.U64()
	if err != nil {
		return Transaction{}, ErrWire
	}
	t.Fee = fee
	t.ValidUntilHeight = validUntil
	if err := r.Done(); err != nil {
		return Transaction{}, ErrWire
	}
	return t, nil
}

func verifySignature(publicKey *ecdsa.PublicKey, digest types.Hash, signature []byte) error {
	if len(signature) != 64 {
		return ErrSignature
	}
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	order := elliptic.P256().Params().N
	if r.Sign() <= 0 || s.Sign() <= 0 || r.Cmp(order) >= 0 || s.Cmp(order) >= 0 {
		return ErrSignature
	}
	if s.Cmp(halfOrder()) > 0 {
		return ErrCanonicalSig
	}
	if !ecdsa.Verify(publicKey, digest[:], r, s) {
		return ErrSignature
	}
	return nil
}

func normalizeLowS(s *big.Int) *big.Int {
	if s.Cmp(halfOrder()) <= 0 {
		return new(big.Int).Set(s)
	}
	return new(big.Int).Sub(elliptic.P256().Params().N, s)
}

func halfOrder() *big.Int {
	return new(big.Int).Rsh(new(big.Int).Set(elliptic.P256().Params().N), 1)
}

func pad32(v *big.Int) []byte {
	raw := v.Bytes()
	out := make([]byte, 32)
	if len(raw) >= 32 {
		copy(out, raw[len(raw)-32:])
	} else {
		copy(out[32-len(raw):], raw)
	}
	return out
}
