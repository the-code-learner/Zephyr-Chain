package citizen

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/da"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/merkle"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/state"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrFinality = errors.New("global header finality could not be verified")

type FinalityVerifier interface {
	VerifyFinalizedHeader(header sharding.GlobalHeader) error
}

type Verifier struct {
	Network  types.NetworkID
	Finality FinalityVerifier
}

func (v Verifier) VerifyTransaction(t tx.Transaction) error {
	if err := t.VerifyForNetwork(v.Network); err != nil {
		return err
	}
	return t.VerifyWitnesses()
}

func (v Verifier) VerifyObject(root types.Hash, obj object.Object, proof state.Proof) bool {
	if err := obj.Validate(); err != nil {
		return false
	}
	h := obj.Hash()
	return state.Verify(root, types.Hash(obj.ID), h[:], proof)
}

func (v Verifier) VerifyShard(header sharding.GlobalHeader, commitment sharding.Commitment, proof merkle.Proof) error {
	if header.Network != v.Network {
		return ErrFinality
	}
	if v.Finality == nil || v.Finality.VerifyFinalizedHeader(header) != nil {
		return ErrFinality
	}
	if !merkle.Verify(header.ShardCommitmentRoot, commitment.Hash(), proof) {
		return ErrFinality
	}
	return nil
}

func (v Verifier) VerifyDASample(commitment da.Commitment, sample da.Sample, chunk []byte) bool {
	return da.VerifySample(commitment, sample, chunk)
}

type PowerState struct {
	BatteryPercent uint8
	Charging       bool
	WiFi           bool
	LowPower       bool
	AppActive      bool
}

type Mode struct {
	VerifyHeaders bool
	Relay         bool
	SampleDA      bool
	ExecuteRecent bool
	ServeCache    bool
}

func SelectMode(p PowerState) Mode {
	mode := Mode{VerifyHeaders: true}
	if p.LowPower || p.BatteryPercent < 15 {
		return mode
	}
	if p.AppActive {
		mode.Relay = true
	}
	if p.WiFi && p.BatteryPercent >= 30 {
		mode.SampleDA = true
		mode.ServeCache = p.AppActive
	}
	if p.WiFi && p.Charging && p.BatteryPercent >= 50 {
		mode.ExecuteRecent = true
		mode.ServeCache = true
	}
	return mode
}
