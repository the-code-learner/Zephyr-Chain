from pathlib import Path
import re

path = Path("internal/api/performance_lab_test.go")
source = path.read_text()
source = source.replace('\t"errors"\n', '')
source = source.replace('\nvar _ = errors.Is\n', '\n')
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
