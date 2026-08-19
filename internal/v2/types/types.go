package types

import (
	"encoding/hex"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
)

type Hash [32]byte
type NetworkID [32]byte
type AccountID [32]byte
type NodeID [32]byte
type ValidatorID [32]byte
type ObjectID [32]byte
type TokenID [32]byte
type ContractID [32]byte
type JobID [32]byte

func (h Hash) String() string        { return hex.EncodeToString(h[:]) }
func (n NetworkID) String() string   { return hex.EncodeToString(n[:]) }
func (a AccountID) String() string   { return hex.EncodeToString(a[:]) }
func (n NodeID) String() string      { return hex.EncodeToString(n[:]) }
func (v ValidatorID) String() string { return hex.EncodeToString(v[:]) }
func (o ObjectID) String() string    { return hex.EncodeToString(o[:]) }
func (t TokenID) String() string     { return hex.EncodeToString(t[:]) }
func (c ContractID) String() string  { return hex.EncodeToString(c[:]) }
func (j JobID) String() string       { return hex.EncodeToString(j[:]) }

func IsZero32(v [32]byte) bool {
	var zero [32]byte
	return v == zero
}

func HashBytes(domain string, payload []byte) Hash {
	return Hash(codec.DomainHash(domain, payload))
}

func AccountIDFromPublicKey(publicKey []byte) AccountID {
	return AccountID(codec.DomainHash("zephyr/account-id/v2", publicKey))
}

func NodeIDFromPublicKey(publicKey []byte) NodeID {
	return NodeID(codec.DomainHash("zephyr/node-id/v2", publicKey))
}

func ValidatorIDFromPublicKey(publicKey []byte) ValidatorID {
	return ValidatorID(codec.DomainHash("zephyr/validator-id/v2", publicKey))
}

func ObjectIDFromTransaction(txID Hash, index uint32) ObjectID {
	var w codec.Writer
	w.Fixed(txID[:])
	w.U32(index)
	return ObjectID(codec.DomainHash("zephyr/object-id/v2", w.BytesCopy()))
}

func TokenIDFromTransaction(txID Hash, operationIndex uint32) TokenID {
	var w codec.Writer
	w.Fixed(txID[:])
	w.U32(operationIndex)
	return TokenID(codec.DomainHash("zephyr/token-id/v2", w.BytesCopy()))
}

func ContractIDFromTransaction(txID Hash, operationIndex uint32) ContractID {
	var w codec.Writer
	w.Fixed(txID[:])
	w.U32(operationIndex)
	return ContractID(codec.DomainHash("zephyr/contract-id/v2", w.BytesCopy()))
}

func JobIDFromTransaction(txID Hash, operationIndex uint32) JobID {
	var w codec.Writer
	w.Fixed(txID[:])
	w.U32(operationIndex)
	return JobID(codec.DomainHash("zephyr/compute-job-id/v2", w.BytesCopy()))
}
