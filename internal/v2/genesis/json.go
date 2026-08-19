package genesis

import (
	"crypto/elliptic"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrGenesisJSON = errors.New("invalid Zephyr v2 genesis JSON")

type jsonValidator struct {
	ConsensusPublicKey string `json:"consensusPublicKey"`
	VotingPower        string `json:"votingPower"`
}

type jsonAllocation struct {
	Owner  string `json:"owner"`
	Amount string `json:"amount"`
}

type jsonConfig struct {
	Version           uint16           `json:"version"`
	ChainName         string           `json:"chainName"`
	GenesisUnix       uint64           `json:"genesisUnix"`
	InitialShardCount uint32           `json:"initialShardCount"`
	MaxShardCount     uint32           `json:"maxShardCount"`
	NativeSymbol      string           `json:"nativeSymbol"`
	Validators        []jsonValidator  `json:"validators"`
	Allocations       []jsonAllocation `json:"allocations"`
}

func LoadJSON(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	return ParseJSON(data)
}

func ParseJSON(data []byte) (Config, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var raw jsonConfig
	if err := decoder.Decode(&raw); err != nil {
		return Config{}, fmt.Errorf("%w: %v", ErrGenesisJSON, err)
	}
	if decoder.More() {
		return Config{}, ErrGenesisJSON
	}
	cfg := Config{
		Version: raw.Version, ChainName: raw.ChainName, GenesisUnix: raw.GenesisUnix,
		InitialShardCount: raw.InitialShardCount, MaxShardCount: raw.MaxShardCount, NativeSymbol: raw.NativeSymbol,
		Validators: make([]Validator, len(raw.Validators)), Allocations: make([]Allocation, len(raw.Allocations)),
	}
	for i, value := range raw.Validators {
		pub, err := hex.DecodeString(strings.TrimSpace(value.ConsensusPublicKey))
		if err != nil || len(pub) != 65 {
			return Config{}, ErrGenesisJSON
		}
		x, y := elliptic.Unmarshal(elliptic.P256(), pub)
		if x == nil || y == nil {
			return Config{}, ErrGenesisJSON
		}
		power, err := strconv.ParseUint(value.VotingPower, 10, 64)
		if err != nil || power == 0 {
			return Config{}, ErrGenesisJSON
		}
		cfg.Validators[i] = Validator{ID: types.ValidatorIDFromPublicKey(pub), ConsensusPublicKey: pub, VotingPower: power}
	}
	for i, value := range raw.Allocations {
		ownerRaw, err := hex.DecodeString(strings.TrimSpace(value.Owner))
		if err != nil || len(ownerRaw) != 32 {
			return Config{}, ErrGenesisJSON
		}
		var owner types.AccountID
		copy(owner[:], ownerRaw)
		amount, err := strconv.ParseUint(value.Amount, 10, 64)
		if err != nil || amount == 0 {
			return Config{}, ErrGenesisJSON
		}
		cfg.Allocations[i] = Allocation{Owner: owner, Amount: amount}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (g Config) NativeTokenID() (types.TokenID, error) {
	network, err := g.NetworkID()
	if err != nil {
		return types.TokenID{}, err
	}
	payload := append(append([]byte(nil), network[:]...), []byte(strings.TrimSpace(g.NativeSymbol))...)
	return types.TokenID(types.HashBytes("zephyr/native-token/v2", payload)), nil
}
