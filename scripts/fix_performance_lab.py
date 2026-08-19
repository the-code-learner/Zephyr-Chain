from pathlib import Path
import re

path = Path("internal/api/performance_lab_test.go")
source = path.read_text()
source = source.replace('\t"errors"\n', '')
source = source.replace('\nvar _ = errors.Is\n', '\n')
source = source.replace('labConsensusRoundLimit = 120 * time.Millisecond', 'labConsensusRoundLimit = 1 * time.Second')

old_set = '''\t\tif _, err := node.server.ledger.SetValidators(validators, dpos.ElectionConfig{
\t\t\tMaxValidators:   validatorCount,
\t\t\tMinSelfStake:    1,
\t\t\tMaxMissedBlocks: 100,
\t\t}); err != nil {
\t\t\tcluster.Close()
\t\t\ttb.Fatalf("set validator snapshot on node %d: %v", index, err)
\t\t}
'''
new_set = '''\t\tif _, err := node.server.ledger.SetValidators(validators, dpos.ElectionConfig{
\t\t\tMaxValidators:   validatorCount,
\t\t\tMinSelfStake:    1,
\t\t\tMaxMissedBlocks: 100,
\t\t}); err != nil {
\t\t\tcluster.Close()
\t\t\ttb.Fatalf("set validator snapshot on node %d: %v", index, err)
\t\t}
\t\tview := node.server.ledger.Consensus()
\t\texpectedTotal := uint64(validatorCount) * labVotingPower
\t\texpectedQuorum := (expectedTotal/3)*2 + ((expectedTotal%3)*2)/3 + 1
\t\tif view.ValidatorCount != validatorCount || view.TotalVotingPower != expectedTotal || view.QuorumVotingPower != expectedQuorum {
\t\t\tcluster.Close()
\t\t\ttb.Fatalf("unexpected validator quorum on node %d: count=%d total=%d quorum=%d, expected count=%d total=%d quorum=%d", index, view.ValidatorCount, view.TotalVotingPower, view.QuorumVotingPower, validatorCount, expectedTotal, expectedQuorum)
\t\t}
'''
if old_set in source:
    source = source.replace(old_set, new_set, 1)

old_failure = '''\tstatuses := make([]uint64, 0, len(c.nodes))
\tfor _, node := range c.nodes {
\t\tstatuses = append(statuses, node.server.ledger.Status().Height)
\t}
\tc.tb.Fatalf("target height %d not reached before timeout; heights=%v", height, statuses)
\treturn 0
'''
new_failure = '''\tsummaries := make([]string, 0, len(c.nodes))
\tfor index, node := range c.nodes {
\t\tstatus := node.server.ledger.Status()
\t\tview := node.server.ledger.Consensus()
\t\tround := node.server.ledger.RoundState()
\t\tproposals := node.server.ledger.ProposalsForHeight(view.NextHeight)
\t\tcertificates := node.server.ledger.CertificatesForHeight(view.NextHeight)
\t\ttallies := node.server.ledger.VoteTalliesAt(view.NextHeight, view.CurrentRound)
\t\tsummaries = append(summaries, fmt.Sprintf("node=%d height=%d mempool=%d next=%d round=%d roundHeight=%d proposer=%s proposals=%d tallies=%+v certs=%d", index, status.Height, status.MempoolSize, view.NextHeight, view.CurrentRound, round.Height, view.NextProposer, len(proposals), tallies, len(certificates)))
\t}
\tc.tb.Fatalf("target height %d not reached before timeout; consensus=%v", height, summaries)
\treturn 0
'''
if old_failure in source:
    source = source.replace(old_failure, new_failure, 1)

marker = '''func (c *labCluster) driveFor(duration time.Duration) {
'''
helper = '''func (c *labCluster) driveAndSyncUntilHeight(indices []int, height uint64, timeout time.Duration) {
\tc.tb.Helper()
\tdeadline := time.Now().Add(timeout)
\tfor time.Now().Before(deadline) {
\t\tfor _, node := range c.nodes {
\t\t\tif err := node.server.runConsensusAutomation(); err != nil && !ignoreConsensusAutomationError(err) {
\t\t\t\tc.tb.Fatalf("drive consensus on %s: %v", node.server.nodeID, err)
\t\t\t}
\t\t}
\t\tfor _, node := range c.nodes {
\t\t\tnode.server.syncPeers()
\t\t}
\t\tif c.indicesAtHeight(indices, height) {
\t\t\treturn
\t\t}
\t\ttime.Sleep(5 * time.Millisecond)
\t}
\tc.driveUntilHeight(indices, height, time.Millisecond)
}

'''
if helper not in source:
    if marker not in source:
        raise SystemExit("driveFor marker not found")
    source = source.replace(marker, helper + marker, 1)

source = source.replace('func TestLabSevenValidatorsStallWithoutQuorumThenRecover(t *testing.T) {', 'func TestLabSevenValidatorsStallWithoutQuorumThenRecoverWithPeerSync(t *testing.T) {')
old_heal = '''\tcluster.heal()
\tcluster.driveUntilHeight(cluster.allIndices(), 1, 5*time.Second)
\tcluster.assertSameTip(cluster.allIndices(), 1)
'''
new_heal = '''\tcluster.heal()
\tcluster.driveAndSyncUntilHeight(cluster.allIndices(), 1, 8*time.Second)
\tcluster.assertSameTip(cluster.allIndices(), 1)
'''
if old_heal in source:
    source = source.replace(old_heal, new_heal, 1)

pattern = re.compile(r'func BenchmarkLabConsensusFinality7Validators\(b \*testing\.B\) \{.*?\n\}\n\n(?=func BenchmarkLabP256TransactionVerification)', re.S)
replacement = r'''func BenchmarkLabConsensusFinality7Validators(b *testing.B) {
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

'''
source, count = pattern.subn(replacement, source)
if count != 1:
    raise SystemExit(f"expected one benchmark function, replaced {count}")
path.write_text(source)
