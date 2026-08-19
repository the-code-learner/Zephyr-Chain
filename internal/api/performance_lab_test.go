package api

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/protocol"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

const (
	labVotingPower         = uint64(10_000)
	labMaxTransactions     = 4_096
	labConsensusRoundLimit = 1 * time.Second
)

type labSigner struct {
	privateKey *ecdsa.PrivateKey
	address    string
	publicKey  string
}

type labNode struct {
	server *Server
	http   *httptest.Server
	signer labSigner
	faults *labFaultTransport
}

type labCluster struct {
	tb    testing.TB
	nodes []*labNode
}

type labFaultTransport struct {
	base peerTransport

	mu                 sync.Mutex
	blocked            map[string]bool
	delay              time.Duration
	duplicate          bool
	reorderConsensus   bool
	heldProposals      map[string]consensus.Proposal
	outboundPayloadLen uint64
}

func newLabCluster(tb testing.TB, validatorCount int) *labCluster {
	tb.Helper()
	if validatorCount <= 0 {
		tb.Fatalf("validator count must be positive")
	}

	cluster := &labCluster{tb: tb, nodes: make([]*labNode, 0, validatorCount)}
	for index := 0; index < validatorCount; index++ {
		signer := newLabSigner(tb)
		server, err := NewServerWithConfig(Config{
			ChainID:                      protocol.DefaultChainID,
			DataDir:                      tb.TempDir(),
			NodeID:                       fmt.Sprintf("lab-validator-%02d", index+1),
			ValidatorPrivateKey:          encodeLabPrivateKey(tb, signer.privateKey),
			BlockInterval:                0,
			ConsensusInterval:            0,
			ConsensusRoundTimeout:        labConsensusRoundLimit,
			SyncInterval:                 0,
			MaxTransactionsPerBlock:      labMaxTransactions,
			EnableBlockProduction:        true,
			EnableConsensusAutomation:    false,
			EnablePeerSync:               false,
			RequirePeerIdentity:          false,
			EnforceProposerSchedule:      true,
			RequireConsensusCertificates: true,
		})
		if err != nil {
			cluster.Close()
			tb.Fatalf("create lab node %d: %v", index, err)
		}
		httpServer := httptest.NewServer(server.Handler())
		cluster.nodes = append(cluster.nodes, &labNode{server: server, http: httpServer, signer: signer})
	}

	validators := make([]dpos.Validator, 0, validatorCount)
	for index, node := range cluster.nodes {
		validators = append(validators, dpos.Validator{
			Rank:           index + 1,
			Address:        node.signer.address,
			VotingPower:    labVotingPower,
			SelfStake:      labVotingPower,
			DelegatedStake: 0,
		})
	}

	for index, node := range cluster.nodes {
		peers := make([]string, 0, validatorCount-1)
		for peerIndex, peer := range cluster.nodes {
			if peerIndex == index {
				continue
			}
			peers = append(peers, peer.http.URL)
		}
		node.server.config.PeerURLs = peers
		node.faults = newLabFaultTransport(node.server.transport)
		node.server.transport = node.faults
		if _, err := node.server.ledger.SetValidators(validators, dpos.ElectionConfig{
			MaxValidators:   validatorCount,
			MinSelfStake:    1,
			MaxMissedBlocks: 100,
		}); err != nil {
			cluster.Close()
			tb.Fatalf("set validator snapshot on node %d: %v", index, err)
		}
		view := node.server.ledger.Consensus()
		expectedTotal := uint64(validatorCount) * labVotingPower
		expectedQuorum := (expectedTotal/3)*2 + ((expectedTotal%3)*2)/3 + 1
		if view.ValidatorCount != validatorCount || view.TotalVotingPower != expectedTotal || view.QuorumVotingPower != expectedQuorum {
			cluster.Close()
			tb.Fatalf("unexpected validator quorum on node %d: count=%d total=%d quorum=%d, expected count=%d total=%d quorum=%d", index, view.ValidatorCount, view.TotalVotingPower, view.QuorumVotingPower, validatorCount, expectedTotal, expectedQuorum)
		}
	}

	return cluster
}

func (c *labCluster) Close() {
	if c == nil {
		return
	}
	for _, node := range c.nodes {
		if node == nil {
			continue
		}
		if node.http != nil {
			node.http.Close()
			node.http = nil
		}
		if node.server != nil {
			node.server.Close()
			node.server = nil
		}
	}
}

func (c *labCluster) prepareTransactions(count int) []tx.Envelope {
	c.tb.Helper()
	transactions := make([]tx.Envelope, 0, count)
	for index := 0; index < count; index++ {
		signer := newLabSigner(c.tb)
		envelope := tx.Envelope{
			ChainID:   protocol.DefaultChainID,
			Domain:    protocol.TransactionDomain,
			From:      signer.address,
			To:        "zph_lab_receiver",
			Amount:    1,
			Nonce:     1,
			Memo:      fmt.Sprintf("lab-%06d", index),
			PublicKey: signer.publicKey,
		}
		envelope.Payload = envelope.CanonicalPayload()
		signature, err := tx.SignPayload(signer.privateKey, envelope.Payload)
		if err != nil {
			c.tb.Fatalf("sign lab transaction %d: %v", index, err)
		}
		envelope.Signature = signature
		transactions = append(transactions, envelope)
	}
	return transactions
}

func (c *labCluster) fundTransactions(transactions []tx.Envelope) {
	c.tb.Helper()
	for _, node := range c.nodes {
		for _, envelope := range transactions {
			if _, err := node.server.ledger.Credit(envelope.From, envelope.Amount); err != nil {
				c.tb.Fatalf("fund %s on %s: %v", envelope.From, node.server.nodeID, err)
			}
		}
	}
}

func (c *labCluster) submitTransactions(transactions []tx.Envelope, workers int) {
	c.tb.Helper()
	if len(transactions) == 0 {
		return
	}
	if workers <= 0 {
		workers = 1
	}
	if workers > len(transactions) {
		workers = len(transactions)
	}

	jobs := make(chan tx.Envelope)
	errs := make(chan error, len(transactions))
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for envelope := range jobs {
				body, err := json.Marshal(envelope)
				if err != nil {
					errs <- err
					continue
				}
				response, err := http.Post(c.nodes[0].http.URL+"/v1/transactions", "application/json", bytes.NewReader(body))
				if err != nil {
					errs <- err
					continue
				}
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				if response.StatusCode != http.StatusAccepted {
					errs <- fmt.Errorf("transaction ingress returned %d", response.StatusCode)
				}
			}
		}()
	}
	for _, envelope := range transactions {
		jobs <- envelope
	}
	close(jobs)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			c.tb.Fatalf("submit lab transaction: %v", err)
		}
	}
}

func (c *labCluster) waitForMempools(size int, timeout time.Duration) {
	c.tb.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ready := true
		for _, node := range c.nodes {
			if node.server.ledger.MempoolSize() != size {
				ready = false
				break
			}
		}
		if ready {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	counts := make([]int, 0, len(c.nodes))
	for _, node := range c.nodes {
		counts = append(counts, node.server.ledger.MempoolSize())
	}
	c.tb.Fatalf("mempools did not converge to %d before timeout: %v", size, counts)
}

func (c *labCluster) driveUntilHeight(indices []int, height uint64, timeout time.Duration) time.Duration {
	c.tb.Helper()
	startedAt := time.Now()
	deadline := startedAt.Add(timeout)
	for time.Now().Before(deadline) {
		for _, node := range c.nodes {
			if err := node.server.runConsensusAutomation(); err != nil && !ignoreConsensusAutomationError(err) {
				c.tb.Fatalf("drive consensus on %s: %v", node.server.nodeID, err)
			}
		}
		if c.indicesAtHeight(indices, height) {
			return time.Since(startedAt)
		}
		time.Sleep(2 * time.Millisecond)
	}
	summaries := make([]string, 0, len(c.nodes))
	for index, node := range c.nodes {
		status := node.server.ledger.Status()
		view := node.server.ledger.Consensus()
		round := node.server.ledger.RoundState()
		proposals := node.server.ledger.ProposalsForHeight(view.NextHeight)
		certificates := node.server.ledger.CertificatesForHeight(view.NextHeight)
		tallies := node.server.ledger.VoteTalliesAt(view.NextHeight, view.CurrentRound)
		summaries = append(summaries, fmt.Sprintf("node=%d height=%d mempool=%d next=%d round=%d roundHeight=%d proposer=%s proposals=%d tallies=%+v certs=%d", index, status.Height, status.MempoolSize, view.NextHeight, view.CurrentRound, round.Height, view.NextProposer, len(proposals), tallies, len(certificates)))
	}
	c.tb.Fatalf("target height %d not reached before timeout; consensus=%v", height, summaries)
	return 0
}

func (c *labCluster) driveAndSyncUntilHeight(indices []int, height uint64, timeout time.Duration) {
	c.tb.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, node := range c.nodes {
			if err := node.server.runConsensusAutomation(); err != nil && !ignoreConsensusAutomationError(err) {
				c.tb.Fatalf("drive consensus on %s: %v", node.server.nodeID, err)
			}
		}
		for _, node := range c.nodes {
			node.server.syncPeers()
		}
		if c.indicesAtHeight(indices, height) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	c.driveUntilHeight(indices, height, time.Millisecond)
}

func (c *labCluster) driveFor(duration time.Duration) {
	c.tb.Helper()
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		for _, node := range c.nodes {
			if err := node.server.runConsensusAutomation(); err != nil && !ignoreConsensusAutomationError(err) {
				c.tb.Fatalf("drive consensus on %s: %v", node.server.nodeID, err)
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func (c *labCluster) indicesAtHeight(indices []int, height uint64) bool {
	for _, index := range indices {
		if c.nodes[index].server.ledger.Status().Height < height {
			return false
		}
	}
	return true
}

func (c *labCluster) allIndices() []int {
	indices := make([]int, len(c.nodes))
	for index := range c.nodes {
		indices[index] = index
	}
	return indices
}

func (c *labCluster) assertSameTip(indices []int, height uint64) {
	c.tb.Helper()
	var expected string
	for _, index := range indices {
		status := c.nodes[index].server.ledger.Status()
		if status.Height != height {
			c.tb.Fatalf("node %d expected height %d, got %d", index, height, status.Height)
		}
		if expected == "" {
			expected = status.LatestBlockHash
			continue
		}
		if status.LatestBlockHash != expected {
			c.tb.Fatalf("safety violation: node %d tip %s differs from %s", index, status.LatestBlockHash, expected)
		}
	}
}

func (c *labCluster) latestBlock(index int) ledger.Block {
	c.tb.Helper()
	response, err := http.Get(c.nodes[index].http.URL + "/v1/blocks/latest")
	if err != nil {
		c.tb.Fatalf("fetch latest block: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		c.tb.Fatalf("fetch latest block returned %d", response.StatusCode)
	}
	var payload LatestBlockResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		c.tb.Fatalf("decode latest block: %v", err)
	}
	return payload.Block
}

func (c *labCluster) partition(groups ...[]int) {
	c.tb.Helper()
	membership := make(map[int]int, len(c.nodes))
	for groupIndex, group := range groups {
		for _, nodeIndex := range group {
			membership[nodeIndex] = groupIndex
		}
	}
	for nodeIndex, node := range c.nodes {
		node.faults.clearBlocked()
		groupIndex, grouped := membership[nodeIndex]
		if !grouped {
			continue
		}
		for peerIndex, peer := range c.nodes {
			if peerIndex == nodeIndex {
				continue
			}
			peerGroup, peerGrouped := membership[peerIndex]
			if peerGrouped && peerGroup != groupIndex {
				node.faults.blockPeer(peer.http.URL)
			}
		}
	}
}

func (c *labCluster) heal() {
	for _, node := range c.nodes {
		node.faults.clearBlocked()
	}
}

func (c *labCluster) enableConsensusFaults(delay time.Duration, duplicate bool, reorder bool) {
	for _, node := range c.nodes {
		node.faults.setBehavior(delay, duplicate, reorder)
	}
}

func (c *labCluster) outboundPayloadBytes() uint64 {
	var total uint64
	for _, node := range c.nodes {
		total += node.faults.payloadBytes()
	}
	return total
}

func (c *labCluster) averageStateBytes() float64 {
	var total int64
	for _, node := range c.nodes {
		info, err := os.Stat(filepath.Join(node.server.ledger.DataDir(), "state.json"))
		if err != nil {
			c.tb.Fatalf("stat node state: %v", err)
		}
		total += info.Size()
	}
	return float64(total) / float64(len(c.nodes))
}

func newLabSigner(tb testing.TB) labSigner {
	tb.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		tb.Fatalf("generate P-256 key: %v", err)
	}
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		tb.Fatalf("marshal public key: %v", err)
	}
	publicKey := base64.StdEncoding.EncodeToString(publicKeyBytes)
	address, err := tx.DeriveAddressFromPublicKey(publicKey)
	if err != nil {
		tb.Fatalf("derive Zephyr address: %v", err)
	}
	return labSigner{privateKey: privateKey, address: address, publicKey: publicKey}
}

func encodeLabPrivateKey(tb testing.TB, privateKey *ecdsa.PrivateKey) string {
	tb.Helper()
	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		tb.Fatalf("marshal private key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(encoded)
}

func newLabFaultTransport(base peerTransport) *labFaultTransport {
	return &labFaultTransport{
		base:          base,
		blocked:       make(map[string]bool),
		heldProposals: make(map[string]consensus.Proposal),
	}
}

func (t *labFaultTransport) blockPeer(peerURL string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.blocked[peerURL] = true
}

func (t *labFaultTransport) clearBlocked() {
	t.mu.Lock()
	defer t.mu.Unlock()
	clear(t.blocked)
}

func (t *labFaultTransport) setBehavior(delay time.Duration, duplicate bool, reorder bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.delay = delay
	t.duplicate = duplicate
	t.reorderConsensus = reorder
	if !reorder {
		clear(t.heldProposals)
	}
}

func (t *labFaultTransport) payloadBytes() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.outboundPayloadLen
}

func (t *labFaultTransport) before(peerURL string) error {
	t.mu.Lock()
	blocked := t.blocked[peerURL]
	delay := t.delay
	t.mu.Unlock()
	if blocked {
		return fmt.Errorf("lab partition blocks %s", peerURL)
	}
	if delay > 0 {
		time.Sleep(delay)
	}
	return nil
}

func (t *labFaultTransport) recordPayload(payload any) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	t.mu.Lock()
	t.outboundPayloadLen += uint64(len(encoded))
	t.mu.Unlock()
}

func (t *labFaultTransport) duplicateEnabled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.duplicate
}

func (t *labFaultTransport) FetchStatus(peerURL string) (StatusResponse, error) {
	if err := t.before(peerURL); err != nil {
		return StatusResponse{}, err
	}
	return t.base.FetchStatus(peerURL)
}

func (t *labFaultTransport) FetchBlock(peerURL string, height uint64) (ledger.Block, error) {
	if err := t.before(peerURL); err != nil {
		return ledger.Block{}, err
	}
	return t.base.FetchBlock(peerURL, height)
}

func (t *labFaultTransport) FetchSnapshot(peerURL string) (ledger.Snapshot, error) {
	if err := t.before(peerURL); err != nil {
		return ledger.Snapshot{}, err
	}
	return t.base.FetchSnapshot(peerURL)
}

func (t *labFaultTransport) PostTransaction(peerURL string, envelope tx.Envelope) error {
	if err := t.before(peerURL); err != nil {
		return err
	}
	t.recordPayload(envelope)
	if err := t.base.PostTransaction(peerURL, envelope); err != nil {
		return err
	}
	if t.duplicateEnabled() {
		_ = t.base.PostTransaction(peerURL, envelope)
	}
	return nil
}

func (t *labFaultTransport) PostBlock(peerURL string, block ledger.Block) error {
	if err := t.before(peerURL); err != nil {
		return err
	}
	t.recordPayload(block)
	if err := t.base.PostBlock(peerURL, block); err != nil {
		return err
	}
	if t.duplicateEnabled() {
		_ = t.base.PostBlock(peerURL, block)
	}
	return nil
}

func (t *labFaultTransport) PostFaucet(peerURL string, request FaucetRequest) error {
	if err := t.before(peerURL); err != nil {
		return err
	}
	t.recordPayload(request)
	return t.base.PostFaucet(peerURL, request)
}

func (t *labFaultTransport) PostProposal(peerURL string, proposal consensus.Proposal) error {
	if err := t.before(peerURL); err != nil {
		return err
	}
	t.recordPayload(proposal)
	t.mu.Lock()
	reorder := t.reorderConsensus
	if reorder {
		t.heldProposals[peerURL] = proposal
	}
	t.mu.Unlock()
	if reorder {
		return nil
	}
	if err := t.base.PostProposal(peerURL, proposal); err != nil {
		return err
	}
	if t.duplicateEnabled() {
		_ = t.base.PostProposal(peerURL, proposal)
	}
	return nil
}

func (t *labFaultTransport) PostVote(peerURL string, vote consensus.Vote) error {
	if err := t.before(peerURL); err != nil {
		return err
	}
	t.recordPayload(vote)

	t.mu.Lock()
	proposal, hasProposal := t.heldProposals[peerURL]
	if hasProposal {
		delete(t.heldProposals, peerURL)
	}
	t.mu.Unlock()

	if hasProposal {
		_ = t.base.PostVote(peerURL, vote)
		if err := t.base.PostProposal(peerURL, proposal); err != nil {
			return err
		}
		return nil
	}
	if err := t.base.PostVote(peerURL, vote); err != nil {
		return err
	}
	if t.duplicateEnabled() {
		_ = t.base.PostVote(peerURL, vote)
	}
	return nil
}

func TestLabSevenValidatorsCertifiedFinality(t *testing.T) {
	cluster := newLabCluster(t, 7)
	defer cluster.Close()

	transactions := cluster.prepareTransactions(24)
	cluster.fundTransactions(transactions)
	startedAt := time.Now()
	cluster.submitTransactions(transactions, 12)
	cluster.waitForMempools(len(transactions), 3*time.Second)
	cluster.driveUntilHeight(cluster.allIndices(), 1, 5*time.Second)
	finality := time.Since(startedAt)

	cluster.assertSameTip(cluster.allIndices(), 1)
	block := cluster.latestBlock(0)
	if block.TransactionCount != len(transactions) {
		t.Fatalf("expected %d finalized transactions, got %d", len(transactions), block.TransactionCount)
	}
	t.Logf("7-validator certified finality: tx=%d finality=%s finalized_tps=%.2f", len(transactions), finality, float64(len(transactions))/finality.Seconds())
}

func TestLabSevenValidatorsStallWithoutQuorumThenRecoverWithPeerSync(t *testing.T) {
	cluster := newLabCluster(t, 7)
	defer cluster.Close()

	transactions := cluster.prepareTransactions(8)
	cluster.fundTransactions(transactions)
	cluster.submitTransactions(transactions, 8)
	cluster.waitForMempools(len(transactions), 3*time.Second)

	cluster.partition([]int{0, 1, 2, 3}, []int{4, 5, 6})
	cluster.driveFor(4 * labConsensusRoundLimit)
	for index, node := range cluster.nodes {
		if height := node.server.ledger.Status().Height; height != 0 {
			t.Fatalf("node %d committed without 2/3+ quorum: height=%d", index, height)
		}
	}

	cluster.heal()
	cluster.driveAndSyncUntilHeight(cluster.allIndices(), 1, 8*time.Second)
	cluster.assertSameTip(cluster.allIndices(), 1)
}

func TestLabSevenValidatorsMinorityPartitionRecoversFromCommittedMajority(t *testing.T) {
	cluster := newLabCluster(t, 7)
	defer cluster.Close()

	transactions := cluster.prepareTransactions(8)
	cluster.fundTransactions(transactions)
	cluster.submitTransactions(transactions, 8)
	cluster.waitForMempools(len(transactions), 3*time.Second)

	majority := []int{0, 1, 2, 3, 4}
	minority := []int{5, 6}
	cluster.partition(majority, minority)
	cluster.driveUntilHeight(majority, 1, 5*time.Second)
	cluster.assertSameTip(majority, 1)
	for _, index := range minority {
		if height := cluster.nodes[index].server.ledger.Status().Height; height != 0 {
			t.Fatalf("minority node %d unexpectedly committed during partition: height=%d", index, height)
		}
	}

	cluster.heal()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !cluster.indicesAtHeight(minority, 1) {
		for _, index := range minority {
			cluster.nodes[index].server.syncPeers()
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !cluster.indicesAtHeight(minority, 1) {
		t.Fatalf("minority did not recover after partition heal")
	}
	cluster.assertSameTip(cluster.allIndices(), 1)
}

func TestLabFourValidatorsDuplicateDelayedOutOfOrderMessagesPreserveSafety(t *testing.T) {
	cluster := newLabCluster(t, 4)
	defer cluster.Close()
	cluster.enableConsensusFaults(2*time.Millisecond, true, true)

	transactions := cluster.prepareTransactions(8)
	cluster.fundTransactions(transactions)
	cluster.submitTransactions(transactions, 8)
	cluster.waitForMempools(len(transactions), 3*time.Second)
	cluster.driveUntilHeight(cluster.allIndices(), 1, 6*time.Second)
	cluster.assertSameTip(cluster.allIndices(), 1)
}

func BenchmarkLabConsensusFinality7Validators(b *testing.B) {
	const transactionsPerBlock = 32
	cluster := newLabCluster(b, 7)
	defer cluster.Close()

	finalitySamples := make([]time.Duration, 0, b.N)
	var totalPayloadBytes uint64
	var totalBlockBytes uint64
	var totalStateBytes float64

	for iteration := 0; iteration < b.N; iteration++ {
		b.StopTimer()
		transactions := cluster.prepareTransactions(transactionsPerBlock)
		cluster.fundTransactions(transactions)
		payloadBefore := cluster.outboundPayloadBytes()

		b.StartTimer()
		startedAt := time.Now()
		cluster.submitTransactions(transactions, 16)
		cluster.waitForMempools(len(transactions), 3*time.Second)
		cluster.driveUntilHeight(cluster.allIndices(), uint64(iteration+1), 5*time.Second)
		finality := time.Since(startedAt)
		b.StopTimer()

		cluster.assertSameTip(cluster.allIndices(), uint64(iteration+1))
		block := cluster.latestBlock(0)
		if block.TransactionCount != transactionsPerBlock {
			b.Fatalf("iteration %d: expected %d finalized transactions, got %d", iteration, transactionsPerBlock, block.TransactionCount)
		}
		encodedBlock, err := json.Marshal(block)
		if err != nil {
			b.Fatalf("marshal finalized block: %v", err)
		}
		finalitySamples = append(finalitySamples, finality)
		totalPayloadBytes += cluster.outboundPayloadBytes() - payloadBefore
		totalBlockBytes += uint64(len(encodedBlock))
		totalStateBytes += cluster.averageStateBytes()
	}

	if len(finalitySamples) == 0 {
		return
	}
	var totalFinality time.Duration
	for _, sample := range finalitySamples {
		totalFinality += sample
	}
	finalizedTransactions := float64(transactionsPerBlock * len(finalitySamples))
	b.ReportMetric(finalizedTransactions/totalFinality.Seconds(), "finalized-tx/s")
	b.ReportMetric(durationMillis(percentileDuration(finalitySamples, 0.50)), "finality-p50-ms")
	b.ReportMetric(durationMillis(percentileDuration(finalitySamples, 0.95)), "finality-p95-ms")
	b.ReportMetric(durationMillis(percentileDuration(finalitySamples, 0.99)), "finality-p99-ms")
	b.ReportMetric(float64(totalPayloadBytes)/finalizedTransactions, "protocol-payload-B/finalized-tx")
	b.ReportMetric(float64(totalBlockBytes)/float64(len(finalitySamples)), "finalized-block-B")
	b.ReportMetric(totalStateBytes/float64(len(finalitySamples)), "state-B/node")
}

func BenchmarkLabP256TransactionVerification(b *testing.B) {
	signer := newLabSigner(b)
	envelope := tx.Envelope{
		ChainID: protocol.DefaultChainID, Domain: protocol.TransactionDomain,
		From: signer.address, To: "zph_lab_receiver", Amount: 1, Nonce: 1, Memo: "verify", PublicKey: signer.publicKey,
	}
	envelope.Payload = envelope.CanonicalPayload()
	var err error
	envelope.Signature, err = tx.SignPayload(signer.privateKey, envelope.Payload)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if err := envelope.ValidateForChain(protocol.DefaultChainID); err != nil {
			b.Fatal(err)
		}
	}
}

func percentileDuration(samples []time.Duration, percentile float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	index := int(math.Ceil(percentile*float64(len(ordered)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(ordered) {
		index = len(ordered) - 1
	}
	return ordered[index]
}

func durationMillis(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}

var _ peerTransport = (*labFaultTransport)(nil)
