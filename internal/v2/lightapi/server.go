package lightapi

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

var ErrSnapshot = errors.New("invalid finalized v2 light snapshot")

type Snapshot struct {
	Header      sharding.GlobalHeader
	Certificate v2consensus.Certificate
	Commitments []sharding.Commitment
	Validators  v2consensus.ValidatorSet
}

func (s Snapshot) Validate() error {
	if s.Header.Network != s.Validators.Network || s.Certificate.Network != s.Header.Network ||
		s.Certificate.Height != s.Header.Height || s.Certificate.HeaderHash != v2consensus.HeaderConsensusHash(s.Header) ||
		s.Header.CertificateHash != s.Certificate.Hash() {
		return ErrSnapshot
	}
	if err := s.Validators.VerifyCertificate(s.Certificate); err != nil {
		return ErrSnapshot
	}
	root, err := sharding.CommitmentRoot(s.Commitments)
	if err != nil || root != s.Header.ShardCommitmentRoot {
		return ErrSnapshot
	}
	return nil
}

type Provider interface {
	LatestSnapshot() (Snapshot, error)
	ShardState(shardID uint32) (worldstate.Backend, bool)
}

type Server struct {
	Provider Provider
}

type validatorDTO struct {
	ID        string `json:"id"`
	PublicKey []byte `json:"publicKey"`
	Power     uint64 `json:"power"`
}

type statusResponse struct {
	Network     string         `json:"network"`
	Height      uint64         `json:"height"`
	Header      []byte         `json:"header"`
	Certificate []byte         `json:"certificate"`
	Validators  []validatorDTO `json:"validators"`
}

type objectProofResponse struct {
	Network         string         `json:"network"`
	Height          uint64         `json:"height"`
	ShardID         uint32         `json:"shardId"`
	Header          []byte         `json:"header"`
	Certificate     []byte         `json:"certificate"`
	Commitment      []byte         `json:"commitment"`
	CommitmentProof []byte         `json:"commitmentProof"`
	ObjectID        string         `json:"objectId"`
	ObjectPresent   bool           `json:"objectPresent"`
	Object          []byte         `json:"object,omitempty"`
	StateProof      []byte         `json:"stateProof"`
	Validators      []validatorDTO `json:"validators"`
}

func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/light/status", s.handleStatus)
	mux.HandleFunc("/v2/light/object", s.handleObject)
	return mux
}

func (s Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	snapshot, err := s.snapshot()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "snapshot_unavailable")
		return
	}
	certificate, err := snapshot.Certificate.MarshalBinary()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "snapshot_unavailable")
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{
		Network: snapshot.Header.Network.String(), Height: snapshot.Header.Height,
		Header: snapshot.Header.CanonicalBytes(), Certificate: certificate,
		Validators: validatorList(snapshot.Validators),
	})
}

func (s Server) handleObject(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
		return
	}
	shardValue := r.URL.Query().Get("shard")
	objectValue := r.URL.Query().Get("id")
	shard64, err := strconv.ParseUint(shardValue, 10, 32)
	if err != nil || len(objectValue) != 64 {
		writeError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	objectBytes, err := hex.DecodeString(objectValue)
	if err != nil || len(objectBytes) != 32 {
		writeError(w, http.StatusBadRequest, "invalid_object_id")
		return
	}
	var objectID types.ObjectID
	copy(objectID[:], objectBytes)
	shardID := uint32(shard64)

	snapshot, err := s.snapshot()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "snapshot_unavailable")
		return
	}
	commitment, commitmentProof, err := sharding.CommitmentProof(snapshot.Commitments, shardID)
	if err != nil {
		writeError(w, http.StatusNotFound, "shard_not_found")
		return
	}
	store, ok := s.Provider.ShardState(shardID)
	if !ok || store == nil || store.Root() != commitment.StateRoot {
		writeError(w, http.StatusServiceUnavailable, "shard_state_unavailable")
		return
	}
	obj, proof, present := store.Proof(objectID)
	certificate, err := snapshot.Certificate.MarshalBinary()
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "snapshot_unavailable")
		return
	}
	response := objectProofResponse{
		Network: snapshot.Header.Network.String(), Height: snapshot.Header.Height, ShardID: shardID,
		Header: snapshot.Header.CanonicalBytes(), Certificate: certificate,
		Commitment: commitment.CanonicalBytes(), CommitmentProof: commitmentProof.MarshalBinary(),
		ObjectID: objectID.String(), ObjectPresent: present, StateProof: proof.MarshalBinary(),
		Validators: validatorList(snapshot.Validators),
	}
	if present {
		response.Object = obj.CanonicalBytes()
	}
	writeJSON(w, http.StatusOK, response)
}

func (s Server) snapshot() (Snapshot, error) {
	if s.Provider == nil {
		return Snapshot{}, ErrSnapshot
	}
	snapshot, err := s.Provider.LatestSnapshot()
	if err != nil || snapshot.Validate() != nil {
		return Snapshot{}, ErrSnapshot
	}
	return snapshot, nil
}

func validatorList(set v2consensus.ValidatorSet) []validatorDTO {
	out := make([]validatorDTO, len(set.Validators))
	for i, validator := range set.Validators {
		out[i] = validatorDTO{ID: validator.ID.String(), PublicKey: append([]byte(nil), validator.PublicKey...), Power: validator.Power}
	}
	return out
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
