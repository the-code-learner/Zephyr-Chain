package mobile

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/citizen"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrCitizenAnchor = errors.New("invalid Citizen trust anchor")

type CitizenNode struct {
	mu     sync.RWMutex
	anchor citizen.TrustAnchor
}

type verifiedObjectJSON struct {
	Network           string `json:"network"`
	Height            uint64 `json:"height"`
	ShardID           uint32 `json:"shardId"`
	ObjectID          string `json:"objectId"`
	ObjectPresent     bool   `json:"objectPresent"`
	StateRoot         string `json:"stateRoot"`
	NextValidatorRoot string `json:"nextValidatorRoot"`
}

type modeJSON struct {
	VerifyHeaders bool `json:"verifyHeaders"`
	Relay         bool `json:"relay"`
	SampleDA      bool `json:"sampleDA"`
	ExecuteRecent bool `json:"executeRecent"`
	ServeCache    bool `json:"serveCache"`
}

// NewCitizenNode exposes only mobile-binding-friendly argument/return types.
// The genesis/checkpoint anchor is the only trusted bootstrap input; subsequent
// validator roots advance only after a locally verified quorum certificate.
func NewCitizenNode(networkHex, validatorRootHex string) (*CitizenNode, error) {
	network, err := parse32(networkHex)
	if err != nil {
		return nil, ErrCitizenAnchor
	}
	root, err := parse32(validatorRootHex)
	if err != nil {
		return nil, ErrCitizenAnchor
	}
	var networkID types.NetworkID
	var validatorRoot types.Hash
	copy(networkID[:], network[:])
	copy(validatorRoot[:], root[:])
	if types.IsZero32([32]byte(networkID)) || types.IsZero32([32]byte(validatorRoot)) {
		return nil, ErrCitizenAnchor
	}
	return &CitizenNode{anchor: citizen.TrustAnchor{Network: networkID, ValidatorRoot: validatorRoot}}, nil
}

func (n *CitizenNode) NetworkID() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.anchor.Network.String()
}

func (n *CitizenNode) ValidatorRoot() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.anchor.ValidatorRoot.String()
}

func (n *CitizenNode) TrustAnchorJSON() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	encoded, _ := json.Marshal(map[string]string{"network": n.anchor.Network.String(), "validatorRoot": n.anchor.ValidatorRoot.String()})
	return string(encoded)
}

// VerifyObjectBundle takes the exact JSON returned by /v2/light/object. It
// updates the mobile trust anchor only after all QC, validator-root, shard and
// Sparse-Merkle proofs verify locally.
func (n *CitizenNode) VerifyObjectBundle(bundleJSON string) (string, error) {
	n.mu.RLock()
	anchor := n.anchor
	n.mu.RUnlock()
	verified, err := citizen.VerifyObjectBundleJSON([]byte(bundleJSON), anchor)
	if err != nil {
		return "", err
	}
	n.mu.Lock()
	n.anchor = verified.NextAnchor
	n.mu.Unlock()
	encoded, err := json.Marshal(verifiedObjectJSON{
		Network: verified.Network.String(), Height: verified.Height, ShardID: verified.ShardID,
		ObjectID: verified.ObjectID.String(), ObjectPresent: verified.Present, StateRoot: verified.StateRoot.String(),
		NextValidatorRoot: verified.NextAnchor.ValidatorRoot.String(),
	})
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// SelectCitizenMode mirrors the resource policy used by the Go verifier while
// exposing primitive parameters friendly to Android/iOS bindings.
func SelectCitizenMode(batteryPercent int, charging, wifi, lowPower, appActive bool) string {
	if batteryPercent < 0 {
		batteryPercent = 0
	}
	if batteryPercent > 100 {
		batteryPercent = 100
	}
	mode := citizen.SelectMode(citizen.PowerState{BatteryPercent: uint8(batteryPercent), Charging: charging, WiFi: wifi, LowPower: lowPower, AppActive: appActive})
	encoded, _ := json.Marshal(modeJSON{VerifyHeaders: mode.VerifyHeaders, Relay: mode.Relay, SampleDA: mode.SampleDA, ExecuteRecent: mode.ExecuteRecent, ServeCache: mode.ServeCache})
	return string(encoded)
}

func parse32(value string) ([32]byte, error) {
	var out [32]byte
	raw, err := hex.DecodeString(value)
	if err != nil || len(raw) != len(out) {
		return out, ErrCitizenAnchor
	}
	copy(out[:], raw)
	return out, nil
}
