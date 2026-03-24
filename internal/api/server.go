package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

const (
	sourceNodeHeader            = "X-Zephyr-Source-Node"
	sourceValidatorHeader       = "X-Zephyr-Source-Validator"
	sourceIdentityPayloadHeader = "X-Zephyr-Source-Identity-Payload"
	sourcePublicKeyHeader       = "X-Zephyr-Source-Public-Key"
	sourceSignatureHeader       = "X-Zephyr-Source-Signature"
	sourceSignedAtHeader        = "X-Zephyr-Source-Signed-At"
)

var (
	errBlockProductionDisabled             = errors.New("block production is disabled on this node")
	errValidatorAddressRequired            = errors.New("validator address is required when proposer schedule enforcement is enabled")
	errNotScheduledProposer                = errors.New("local validator is not scheduled to propose the next block")
	errConsensusAutomationRequiresIdentity = errors.New("validator private key is required when consensus automation is enabled")
)

type Config struct {
	DataDir                      string
	NodeID                       string
	ValidatorAddress             string
	ValidatorPrivateKey          string
	PeerURLs                     []string
	PeerValidatorBindings        map[string]string
	BlockInterval                time.Duration
	ConsensusInterval            time.Duration
	ConsensusRoundTimeout        time.Duration
	SyncInterval                 time.Duration
	MaxTransactionsPerBlock      int
	EnableBlockProduction        bool
	EnableConsensusAutomation    bool
	EnablePeerSync               bool
	RequirePeerIdentity          bool
	EnforceProposerSchedule      bool
	RequireConsensusCertificates bool
}

func DefaultConfig() Config {
	return Config{
		DataDir:                      filepath.Join("var", "node"),
		NodeID:                       "node-local",
		ValidatorAddress:             "",
		PeerURLs:                     []string{},
		PeerValidatorBindings:        nil,
		BlockInterval:                15 * time.Second,
		ConsensusInterval:            1 * time.Second,
		ConsensusRoundTimeout:        5 * time.Second,
		SyncInterval:                 5 * time.Second,
		MaxTransactionsPerBlock:      100,
		EnableBlockProduction:        true,
		EnableConsensusAutomation:    false,
		EnablePeerSync:               true,
		RequirePeerIdentity:          false,
		EnforceProposerSchedule:      false,
		RequireConsensusCertificates: false,
	}
}

type ElectionRequest struct {
	Candidates []dpos.Candidate    `json:"candidates"`
	Votes      []dpos.Vote         `json:"votes"`
	Config     dpos.ElectionConfig `json:"config"`
}

type ElectionResponse struct {
	Validators            []dpos.Validator     `json:"validators"`
	ElectionConfig        dpos.ElectionConfig  `json:"electionConfig"`
	ValidatorSetVersion   uint64               `json:"validatorSetVersion"`
	ValidatorSetUpdatedAt *time.Time           `json:"validatorSetUpdatedAt,omitempty"`
	Consensus             ledger.ConsensusView `json:"consensus"`
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
	NodeID                        string                           `json:"nodeId"`
	ValidatorAddress              string                           `json:"validatorAddress,omitempty"`
	PeerCount                     int                              `json:"peerCount"`
	BlockProduction               bool                             `json:"blockProduction"`
	ConsensusAutomationEnabled    bool                             `json:"consensusAutomationEnabled"`
	PeerSyncEnabled               bool                             `json:"peerSyncEnabled"`
	PeerIdentityRequired          bool                             `json:"peerIdentityRequired"`
	ProposerScheduleEnforced      bool                             `json:"proposerScheduleEnforced"`
	ConsensusCertificatesRequired bool                             `json:"consensusCertificatesRequired"`
	Identity                      *TransportIdentity               `json:"identity,omitempty"`
	Status                        ledger.StatusView                `json:"status"`
	Consensus                     ledger.ConsensusView             `json:"consensus"`
	RoundEvidence                 RoundEvidence                    `json:"roundEvidence"`
	RoundHistory                  ledger.ConsensusRoundHistoryView `json:"roundHistory"`
	BlockReadiness                BlockReadiness                   `json:"blockReadiness"`
	Recovery                      ledger.ConsensusRecoveryView     `json:"recovery"`
	Diagnostics                   ledger.ConsensusDiagnosticsView  `json:"diagnostics"`
	PeerSyncHistory               ledger.PeerSyncHistoryView       `json:"peerSyncHistory"`
}

type ConsensusResponse struct {
	NodeID                        string                           `json:"nodeId"`
	ValidatorAddress              string                           `json:"validatorAddress,omitempty"`
	ConsensusAutomationEnabled    bool                             `json:"consensusAutomationEnabled"`
	ProposerScheduleEnforced      bool                             `json:"proposerScheduleEnforced"`
	ConsensusCertificatesRequired bool                             `json:"consensusCertificatesRequired"`
	ValidatorSet                  ledger.ValidatorSnapshot         `json:"validatorSet"`
	Artifacts                     ledger.ConsensusArtifactsView    `json:"artifacts"`
	Consensus                     ledger.ConsensusView             `json:"consensus"`
	RoundEvidence                 RoundEvidence                    `json:"roundEvidence"`
	RoundHistory                  ledger.ConsensusRoundHistoryView `json:"roundHistory"`
	BlockReadiness                BlockReadiness                   `json:"blockReadiness"`
	Recovery                      ledger.ConsensusRecoveryView     `json:"recovery"`
	Diagnostics                   ledger.ConsensusDiagnosticsView  `json:"diagnostics"`
	PeerSyncHistory               ledger.PeerSyncHistoryView       `json:"peerSyncHistory"`
}

type LatestBlockResponse struct {
	Block ledger.Block `json:"block"`
}

type BlockTemplateResponse struct {
	Block           ledger.Block                     `json:"block"`
	Artifacts       ledger.ConsensusArtifactsView    `json:"artifacts"`
	Consensus       ledger.ConsensusView             `json:"consensus"`
	RoundEvidence   RoundEvidence                    `json:"roundEvidence"`
	RoundHistory    ledger.ConsensusRoundHistoryView `json:"roundHistory"`
	BlockReadiness  BlockReadiness                   `json:"blockReadiness"`
	Recovery        ledger.ConsensusRecoveryView     `json:"recovery"`
	Diagnostics     ledger.ConsensusDiagnosticsView  `json:"diagnostics"`
	PeerSyncHistory ledger.PeerSyncHistoryView       `json:"peerSyncHistory"`
}

type ProduceBlockRequest struct {
	ProducedAt *time.Time `json:"producedAt,omitempty"`
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
	mux            *http.ServeMux
	ledger         *ledger.Store
	config         Config
	nodeID         string
	httpClient     *http.Client
	transport      peerTransport
	identitySigner *transportIdentitySigner
	peerMu         sync.RWMutex
	peerViews      map[string]PeerView
	stopCh         chan struct{}
	wg             sync.WaitGroup
	closeOnce      sync.Once
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

	identitySigner, config, err := newTransportIdentitySigner(config)
	if err != nil {
		return nil, err
	}
	if config.EnableConsensusAutomation && identitySigner == nil {
		return nil, errConsensusAutomationRequiresIdentity
	}

	store, err := ledger.NewStore(config.DataDir)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	server := &Server{
		mux:            http.NewServeMux(),
		ledger:         store,
		config:         config,
		nodeID:         config.NodeID,
		httpClient:     client,
		transport:      newHTTPPeerTransport(client, config.NodeID, identitySigner),
		identitySigner: identitySigner,
		peerViews:      make(map[string]PeerView),
		stopCh:         make(chan struct{}),
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
	s.mux.HandleFunc("/v1/consensus", s.handleConsensus)
	s.mux.HandleFunc("/v1/consensus/proposals", s.handleConsensusProposal)
	s.mux.HandleFunc("/v1/consensus/votes", s.handleConsensusVote)
	s.mux.HandleFunc("/v1/election", s.handleElection)
	s.mux.HandleFunc("/v1/validators", s.handleValidators)
	s.mux.HandleFunc("/v1/transactions", s.handleBroadcastTransaction)
	s.mux.HandleFunc("/v1/accounts/", s.handleAccount)
	s.mux.HandleFunc("/v1/blocks/latest", s.handleLatestBlock)
	s.mux.HandleFunc("/v1/blocks/", s.handleBlockAtHeight)
	s.mux.HandleFunc("/v1/dev/faucet", s.handleFaucet)
	s.mux.HandleFunc("/v1/dev/block-template", s.handleBlockTemplate)
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

	now := time.Now().UTC()
	consensusView := s.ledger.Consensus()
	response := StatusResponse{
		NodeID:                        s.nodeID,
		ValidatorAddress:              s.config.ValidatorAddress,
		PeerCount:                     len(s.config.PeerURLs),
		BlockProduction:               s.config.EnableBlockProduction,
		ConsensusAutomationEnabled:    s.config.EnableConsensusAutomation,
		PeerSyncEnabled:               s.config.EnablePeerSync,
		PeerIdentityRequired:          s.peerIdentityRequired(),
		ProposerScheduleEnforced:      s.config.EnforceProposerSchedule,
		ConsensusCertificatesRequired: s.config.RequireConsensusCertificates,
		Status:                        s.ledger.Status(),
		Consensus:                     consensusView,
		RoundEvidence:                 s.buildRoundEvidence(now),
		RoundHistory:                  s.ledger.RoundHistory(consensusView.NextHeight),
		BlockReadiness:                s.buildBlockReadiness(now),
		Recovery:                      s.ledger.ConsensusRecovery(),
		Diagnostics:                   s.ledger.ConsensusDiagnostics(),
		PeerSyncHistory:               s.ledger.PeerSyncHistory(),
	}
	if s.identitySigner != nil {
		identity, err := s.identitySigner.Build(now)
		if err == nil {
			response.Identity = &identity
		}
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handlePeers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, PeersResponse{Peers: s.peerSnapshot()})
}

func (s *Server) handleConsensus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	now := time.Now().UTC()
	consensusView := s.ledger.Consensus()
	writeJSON(w, http.StatusOK, ConsensusResponse{
		NodeID:                        s.nodeID,
		ValidatorAddress:              s.config.ValidatorAddress,
		ConsensusAutomationEnabled:    s.config.EnableConsensusAutomation,
		ProposerScheduleEnforced:      s.config.EnforceProposerSchedule,
		ConsensusCertificatesRequired: s.config.RequireConsensusCertificates,
		ValidatorSet:                  s.ledger.ValidatorSet(),
		Artifacts:                     s.ledger.ConsensusArtifacts(),
		Consensus:                     consensusView,
		RoundEvidence:                 s.buildRoundEvidence(now),
		RoundHistory:                  s.ledger.RoundHistory(consensusView.NextHeight),
		BlockReadiness:                s.buildBlockReadiness(now),
		Recovery:                      s.ledger.ConsensusRecovery(),
		Diagnostics:                   s.ledger.ConsensusDiagnostics(),
		PeerSyncHistory:               s.ledger.PeerSyncHistory(),
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

	snapshot, err := s.ledger.SetValidators(validators, request.Config)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, s.electionResponse(snapshot))
}

func (s *Server) handleValidators(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, s.electionResponse(s.ledger.ValidatorSet()))
}

func (s *Server) handleBroadcastTransaction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := s.validatePeerRequest(r); err != nil {
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
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
	if err := s.validatePeerRequest(r); err != nil {
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
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

func (s *Server) handleBlockTemplate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	now := time.Now().UTC()
	block, err := s.ledger.BuildNextBlock(s.config.MaxTransactionsPerBlock, now)
	if err != nil {
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
		return
	}

	consensusView := s.ledger.Consensus()
	writeJSON(w, http.StatusOK, BlockTemplateResponse{
		Block:           block,
		Artifacts:       s.ledger.ConsensusArtifacts(),
		Consensus:       consensusView,
		RoundEvidence:   s.buildRoundEvidence(now),
		RoundHistory:    s.ledger.RoundHistory(consensusView.NextHeight),
		BlockReadiness:  s.buildBlockReadiness(now),
		Recovery:        s.ledger.ConsensusRecovery(),
		Diagnostics:     s.ledger.ConsensusDiagnostics(),
		PeerSyncHistory: s.ledger.PeerSyncHistory(),
	})
}

func (s *Server) handleProduceBlock(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var request ProduceBlockRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil && !errors.Is(err, io.EOF) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	producedAt := time.Time{}
	if request.ProducedAt != nil {
		producedAt = request.ProducedAt.UTC()
	}
	block, err := s.produceLocalBlock(producedAt)
	if err != nil {
		consensus := s.ledger.Consensus()
		s.recordConsensusDiagnostic("block_commit_rejected", "local_api", err, consensus.NextHeight, consensus.CurrentRound, "", s.config.ValidatorAddress)
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
	if err := s.validatePeerRequest(r); err != nil {
		writeJSON(w, statusForError(err), map[string]string{"error": err.Error()})
		return
	}

	var request ledger.Block
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON payload"})
		return
	}

	if err := s.ledger.ImportBlockWithOptions(request, s.config.RequireConsensusCertificates); err != nil {
		s.recordBlockImportFailure("peer", request, err, requestSourceNode(r))
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

func (s *Server) produceLocalBlock(producedAt time.Time) (ledger.Block, error) {
	if !s.config.EnableBlockProduction {
		return ledger.Block{}, errBlockProductionDisabled
	}

	consensus := s.ledger.Consensus()
	if s.config.EnforceProposerSchedule && consensus.ValidatorCount > 0 {
		if s.config.ValidatorAddress == "" {
			return ledger.Block{}, errValidatorAddressRequired
		}
		if consensus.NextProposer != "" && consensus.NextProposer != s.config.ValidatorAddress {
			return ledger.Block{}, fmt.Errorf("%w: height %d is assigned to %s", errNotScheduledProposer, consensus.NextHeight, consensus.NextProposer)
		}
	}

	block, err := s.ledger.ProduceBlockWithOptions(s.config.MaxTransactionsPerBlock, producedAt, s.config.RequireConsensusCertificates)
	if err != nil {
		return ledger.Block{}, err
	}

	go s.broadcastBlock(block)
	return block, nil
}

func (s *Server) startBackgroundLoops() {
	if s.config.EnableConsensusAutomation && s.config.ConsensusInterval > 0 {
		s.startConsensusAutomation()
	}
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
				if _, err := s.produceLocalBlock(time.Time{}); err != nil &&
					!errors.Is(err, ledger.ErrNoTransactionsToBlock) &&
					!errors.Is(err, errNotScheduledProposer) &&
					!errors.Is(err, errValidatorAddressRequired) &&
					!errors.Is(err, ledger.ErrConsensusProposalRequired) &&
					!errors.Is(err, ledger.ErrConsensusCertificateRequired) {
					recordPeerLog("local-block-producer", err)
				}
			case <-s.stopCh:
				return
			}
		}
	}()
}

func (s *Server) electionResponse(snapshot ledger.ValidatorSnapshot) ElectionResponse {
	return ElectionResponse{
		Validators:            snapshot.Validators,
		ElectionConfig:        snapshot.ElectionConfig,
		ValidatorSetVersion:   snapshot.Version,
		ValidatorSetUpdatedAt: snapshot.UpdatedAt,
		Consensus:             s.ledger.Consensus(),
	}
}

func normalizeConfig(config Config) Config {
	if config.DataDir == "" {
		config.DataDir = filepath.Join("var", "node")
	}
	if config.NodeID == "" {
		config.NodeID = "node-local"
	}
	config.ValidatorAddress = strings.TrimSpace(config.ValidatorAddress)
	config.ValidatorPrivateKey = strings.TrimSpace(config.ValidatorPrivateKey)
	config.PeerURLs = normalizePeerURLs(config.PeerURLs)
	config.PeerValidatorBindings = normalizePeerValidatorBindings(config.PeerValidatorBindings)
	if config.MaxTransactionsPerBlock <= 0 {
		config.MaxTransactionsPerBlock = 100
	}
	if config.BlockInterval < 0 {
		config.BlockInterval = 0
	}
	if config.ConsensusInterval < 0 {
		config.ConsensusInterval = 0
	}
	if config.ConsensusRoundTimeout < 0 {
		config.ConsensusRoundTimeout = 0
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
		errors.Is(err, tx.ErrInvalidSignature),
		errors.Is(err, consensus.ErrMissingFields),
		errors.Is(err, consensus.ErrInvalidPayload),
		errors.Is(err, consensus.ErrInvalidPublicKey),
		errors.Is(err, consensus.ErrInvalidAddress),
		errors.Is(err, consensus.ErrInvalidSignature),
		errors.Is(err, consensus.ErrInvalidHash),
		errors.Is(err, consensus.ErrInvalidHeight),
		errors.Is(err, consensus.ErrInvalidProducedAt),
		errors.Is(err, consensus.ErrInvalidTransactionID),
		errors.Is(err, consensus.ErrMissingTransactions),
		errors.Is(err, consensus.ErrInvalidProposalTransaction),
		errors.Is(err, consensus.ErrTransactionMismatch),
		errors.Is(err, consensus.ErrHashMismatch),
		errors.Is(err, errMissingTransportIdentityFields),
		errors.Is(err, errInvalidTransportIdentityPayload),
		errors.Is(err, errTransportIdentityTimestamp),
		errors.Is(err, errInvalidTransportIdentityPublicKey),
		errors.Is(err, errTransportIdentityAddressMismatch),
		errors.Is(err, errInvalidTransportIdentitySignature),
		errors.Is(err, errTransportIdentityNodeMismatch),
		errors.Is(err, errTransportIdentityValidatorMismatch):
		return http.StatusBadRequest
	case errors.Is(err, errPeerIdentityRequired),
		errors.Is(err, errPeerValidatorNotAllowed):
		return http.StatusForbidden
	case errors.Is(err, ledger.ErrDuplicateTransaction),
		errors.Is(err, ledger.ErrInvalidNonce),
		errors.Is(err, ledger.ErrInsufficientBalance),
		errors.Is(err, ledger.ErrNoTransactionsToBlock),
		errors.Is(err, ledger.ErrBlockOutOfSequence),
		errors.Is(err, ledger.ErrBlockConflict),
		errors.Is(err, ledger.ErrNoValidatorSet),
		errors.Is(err, ledger.ErrValidatorNotActive),
		errors.Is(err, ledger.ErrUnexpectedProposer),
		errors.Is(err, ledger.ErrConsensusHeightMismatch),
		errors.Is(err, ledger.ErrConsensusRoundMismatch),
		errors.Is(err, ledger.ErrConsensusPreviousHash),
		errors.Is(err, ledger.ErrConsensusProposalRequired),
		errors.Is(err, ledger.ErrConsensusTemplateMismatch),
		errors.Is(err, ledger.ErrConsensusCertificateRequired),
		errors.Is(err, ledger.ErrConflictingProposal),
		errors.Is(err, ledger.ErrUnknownProposal),
		errors.Is(err, ledger.ErrConflictingVote),
		errors.Is(err, errBlockProductionDisabled),
		errors.Is(err, errValidatorAddressRequired),
		errors.Is(err, errNotScheduledProposer):
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
