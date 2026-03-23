package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

const sourceNodeHeader = "X-Zephyr-Source-Node"

var errBlockProductionDisabled = errors.New("block production is disabled on this node")

type Config struct {
	DataDir                 string
	NodeID                  string
	PeerURLs                []string
	BlockInterval           time.Duration
	SyncInterval            time.Duration
	MaxTransactionsPerBlock int
	EnableBlockProduction   bool
	EnablePeerSync          bool
}

func DefaultConfig() Config {
	return Config{
		DataDir:                 filepath.Join("var", "node"),
		NodeID:                  "node-local",
		PeerURLs:                []string{},
		BlockInterval:           15 * time.Second,
		SyncInterval:            5 * time.Second,
		MaxTransactionsPerBlock: 100,
		EnableBlockProduction:   true,
		EnablePeerSync:          true,
	}
}

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
	RequestID string `json:"requestId,omitempty"`
	Address   string `json:"address"`
	Amount    uint64 `json:"amount"`
}

type FaucetResponse struct {
	Account ledger.AccountView `json:"account"`
}

type AccountResponse struct {
	Account ledger.AccountView `json:"account"`
}

type StatusResponse struct {
	NodeID          string            `json:"nodeId"`
	PeerCount       int               `json:"peerCount"`
	BlockProduction bool              `json:"blockProduction"`
	PeerSyncEnabled bool              `json:"peerSyncEnabled"`
	Status          ledger.StatusView `json:"status"`
}

type LatestBlockResponse struct {
	Block ledger.Block `json:"block"`
}

type ProduceBlockResponse struct {
	Block ledger.Block `json:"block"`
}

type BlockAtHeightResponse struct {
	Block ledger.Block `json:"block"`
}

type SnapshotResponse struct {
	Snapshot ledger.Snapshot `json:"snapshot"`
}

type Server struct {
	mux        *http.ServeMux
	mu         sync.RWMutex
	validators []dpos.Validator
	ledger     *ledger.Store
	config     Config
	nodeID     string
	httpClient *http.Client
	peerMu     sync.RWMutex
	peerViews  map[string]PeerView
	stopCh     chan struct{}
	wg         sync.WaitGroup
	closeOnce  sync.Once
}

func NewServer() *Server {
	server, err := NewServerWithConfig(DefaultConfig())
	if err != nil {
		panic(err)
	}
	return server
}

func NewServerWithConfig(config Config) (*Server, error) {
	config = normalizeConfig(config)

	store, err := ledger.NewStore(config.DataDir)
	if err != nil {
		return nil, err
	}

	server := &Server{
		mux:        http.NewServeMux(),
		ledger:     store,
		config:     config,
		nodeID:     config.NodeID,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		peerViews:  make(map[string]PeerView),
		stopCh:     make(chan struct{}),
	}

	server.routes()
	server.startBackgroundLoops()
	return server, nil
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) Close() {
	s.closeOnce.Do(func() {
		close(s.stopCh)
		s.wg.Wait()
	})
}
func (s *Server) routes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/v1/status", s.handleStatus)
	s.mux.HandleFunc("/v1/peers", s.handlePeers)
	s.mux.HandleFunc("/v1/election", s.handleElection)
	s.mux.HandleFunc("/v1/validators", s.handleValidators)
	s.mux.HandleFunc("/v1/transactions", s.handleBroadcastTransaction)
	s.mux.HandleFunc("/v1/accounts/", s.handleAccount)
	s.mux.HandleFunc("/v1/blocks/latest", s.handleLatestBlock)
	s.mux.HandleFunc("/v1/blocks/", s.handleBlockAtHeight)
	s.mux.HandleFunc("/v1/dev/faucet", s.handleFaucet)
	s.mux.HandleFunc("/v1/dev/produce-block", s.handleProduceBlock)
	s.mux.HandleFunc("/v1/internal/blocks", s.handleImportBlock)
	s.mux.HandleFunc("/v1/internal/snapshot", s.handleSnapshot)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "zephyr-node-api",
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, StatusResponse{
		NodeID:          s.nodeID,
		PeerCount:       len(s.config.PeerURLs),
		BlockProduction: s.config.EnableBlockProduction,
		PeerSyncEnabled: s.config.EnablePeerSync,
		Status:          s.ledger.Status(),
	})
}

func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, PeersResponse{Peers: s.peerSnapshot()})
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
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
		return
	}
	propagate := requestSourceNode(r) == ""
	now := time.Now().UTC()
	id := tx.ID(request)
	acceptedID, err := s.ledger.Accept(request)
	if err != nil {
		if propagate || !errors.Is(err, ledger.ErrDuplicateTransaction) {
			writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
			return
		}
	} else {
		id = acceptedID
	}

	writeJSON(w, http.StatusAccepted, BroadcastTransactionResponse{
		ID:          id,
		Accepted:    true,
		QueuedAt:    now,
		MempoolSize: s.ledger.MempoolSize(),
	})

	if propagate {
		go s.broadcastTransaction(request)
	}
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

	writeJSON(w, http.StatusOK, AccountResponse{Account: s.ledger.View(address)})
}

func (s *Server) handleLatestBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	block, ok := s.ledger.LatestBlock()
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no committed blocks yet"})
		return
	}

	writeJSON(w, http.StatusOK, LatestBlockResponse{Block: block})
}
func (s *Server) handleBlockAtHeight(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	rawHeight := strings.TrimPrefix(r.URL.Path, "/v1/blocks/")
	if rawHeight == "" || rawHeight == "latest" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	height, err := strconv.ParseUint(rawHeight, 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid block height"})
		return
	}

	block, ok := s.ledger.BlockAtHeight(height)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "block not found"})
		return
	}

	writeJSON(w, http.StatusOK, BlockAtHeightResponse{Block: block})
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
	if request.RequestID == "" {
		request.RequestID = s.nextRequestID("fund")
	}

	if _, err := s.ledger.CreditWithID(request.RequestID, request.Address, request.Amount); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, FaucetResponse{Account: s.ledger.View(request.Address)})

	if requestSourceNode(r) == "" {
		go s.broadcastFaucet(request)
	}
}
func (s *Server) handleProduceBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	block, err := s.produceLocalBlock()
	if err != nil {
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, ProduceBlockResponse{Block: block})
}

func (s *Server) handleImportBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var request ledger.Block
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	if err := s.ledger.ImportBlock(request); err != nil {
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, LatestBlockResponse{Block: request})
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, SnapshotResponse{Snapshot: s.ledger.Snapshot()})
}

func (s *Server) produceLocalBlock() (ledger.Block, error) {
	if !s.config.EnableBlockProduction {
		return ledger.Block{}, errBlockProductionDisabled
	}

	block, err := s.ledger.ProduceBlock(s.config.MaxTransactionsPerBlock)
	if err != nil {
		return ledger.Block{}, err
	}

	go s.broadcastBlock(block)
	return block, nil
}

func (s *Server) startBackgroundLoops() {
	if s.config.EnableBlockProduction && s.config.BlockInterval > 0 {
		s.startBlockProducer()
	}
	if s.config.EnablePeerSync && len(s.config.PeerURLs) > 0 && s.config.SyncInterval > 0 {
		s.startPeerSync()
	}
}
func (s *Server) startBlockProducer() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(s.config.BlockInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if _, err := s.produceLocalBlock(); err != nil && !errors.Is(err, ledger.ErrNoTransactionsToBlock) {
					recordPeerLog("local-block-producer", err)
				}
			case <-s.stopCh:
				return
			}
		}
	}()
}

func normalizeConfig(config Config) Config {
	if config.DataDir == "" {
		config.DataDir = filepath.Join("var", "node")
	}
	if config.NodeID == "" {
		config.NodeID = "node-local"
	}
	config.PeerURLs = normalizePeerURLs(config.PeerURLs)
	if config.MaxTransactionsPerBlock <= 0 {
		config.MaxTransactionsPerBlock = 100
	}
	if config.BlockInterval < 0 {
		config.BlockInterval = 0
	}
	if config.SyncInterval < 0 {
		config.SyncInterval = 0
	}
	return config
}

func requestSourceNode(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get(sourceNodeHeader))
}

func (s *Server) nextRequestID(prefix string) string {
	return fmt.Sprintf("%s-%s-%d", prefix, s.nodeID, time.Now().UTC().UnixNano())
}

func statusForError(err error) int {
	switch {
	case errors.Is(err, tx.ErrMissingFields),
		errors.Is(err, tx.ErrInvalidAmount),
		errors.Is(err, tx.ErrInvalidPayload),
		errors.Is(err, tx.ErrInvalidPublicKey),
		errors.Is(err, tx.ErrInvalidAddress),
		errors.Is(err, tx.ErrInvalidSignature):
		return http.StatusBadRequest
	case errors.Is(err, ledger.ErrDuplicateTransaction),
		errors.Is(err, ledger.ErrInvalidNonce),
		errors.Is(err, ledger.ErrInsufficientBalance),
		errors.Is(err, ledger.ErrNoTransactionsToBlock),
		errors.Is(err, ledger.ErrBlockOutOfSequence),
		errors.Is(err, ledger.ErrBlockConflict),
		errors.Is(err, errBlockProductionDisabled):
		return http.StatusConflict
	case errors.Is(err, ledger.ErrBlockInvariant),
		errors.Is(err, ledger.ErrInvalidBlock):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
