package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

type ElectionRequest struct {
	Candidates []dpos.Candidate    `json:"candidates"`
	Votes      []dpos.Vote         `json:"votes"`
	Config     dpos.ElectionConfig `json:"config"`
}

type ElectionResponse struct {
	Validators []dpos.Validator `json:"validators"`
}

type BroadcastTransactionRequest = tx.Envelope

type BroadcastTransactionResponse struct {
	ID          string    `json:"id"`
	Accepted    bool      `json:"accepted"`
	QueuedAt    time.Time `json:"queuedAt"`
	MempoolSize int       `json:"mempoolSize"`
}

type FaucetRequest struct {
	Address string `json:"address"`
	Amount  uint64 `json:"amount"`
}

type FaucetResponse struct {
	Account ledger.AccountView `json:"account"`
}

type AccountResponse struct {
	Account ledger.AccountView `json:"account"`
}

type Server struct {
	mux        *http.ServeMux
	mu         sync.RWMutex
	validators []dpos.Validator
	ledger     *ledger.Store
}

func NewServer() *Server {
	server := &Server{
		mux:    http.NewServeMux(),
		ledger: ledger.NewStore(),
	}

	server.routes()
	return server
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/v1/election", s.handleElection)
	s.mux.HandleFunc("/v1/validators", s.handleValidators)
	s.mux.HandleFunc("/v1/transactions", s.handleBroadcastTransaction)
	s.mux.HandleFunc("/v1/accounts/", s.handleAccount)
	s.mux.HandleFunc("/v1/dev/faucet", s.handleFaucet)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "zephyr-node-api",
	})
}

func (s *Server) handleElection(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var request ElectionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	service, err := dpos.NewService(request.Config)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	validators, err := service.ElectValidators(request.Candidates, request.Votes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	s.mu.Lock()
	s.validators = validators
	s.mu.Unlock()

	writeJSON(w, http.StatusOK, ElectionResponse{Validators: validators})
}

func (s *Server) handleValidators(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	validators := append([]dpos.Validator(nil), s.validators...)
	s.mu.RUnlock()

	writeJSON(w, http.StatusOK, ElectionResponse{Validators: validators})
}

func (s *Server) handleBroadcastTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var request BroadcastTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	if err := request.ValidateStatic(); err != nil {
		writeJSON(w, statusForTransactionError(err), map[string]string{"error": err.Error()})
		return
	}

	now := time.Now().UTC()
	id, err := s.ledger.Accept(request)
	if err != nil {
		writeJSON(w, statusForTransactionError(err), map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusAccepted, BroadcastTransactionResponse{
		ID:          id,
		Accepted:    true,
		QueuedAt:    now,
		MempoolSize: s.ledger.MempoolSize(),
	})
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	address := strings.TrimPrefix(r.URL.Path, "/v1/accounts/")
	if address == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	writeJSON(w, http.StatusOK, AccountResponse{
		Account: s.ledger.View(address),
	})
}

func (s *Server) handleFaucet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var request FaucetRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	if request.Address == "" || request.Amount == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "address and amount are required"})
		return
	}

	s.ledger.Credit(request.Address, request.Amount)
	writeJSON(w, http.StatusOK, FaucetResponse{
		Account: s.ledger.View(request.Address),
	})
}

func statusForTransactionError(err error) int {
	switch {
	case errors.Is(err, tx.ErrMissingFields),
		errors.Is(err, tx.ErrInvalidAmount),
		errors.Is(err, tx.ErrInvalidPayload),
		errors.Is(err, tx.ErrInvalidPublicKey),
		errors.Is(err, tx.ErrInvalidAddress),
		errors.Is(err, tx.ErrInvalidSignature):
		return http.StatusBadRequest
	case errors.Is(err, ledger.ErrDuplicateTransaction):
		return http.StatusConflict
	case errors.Is(err, ledger.ErrInvalidNonce),
		errors.Is(err, ledger.ErrInsufficientBalance):
		return http.StatusConflict
	default:
		return http.StatusBadRequest
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
