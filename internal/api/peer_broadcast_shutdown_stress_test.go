package api

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
)

// TestAsyncPeerBroadcastConcurrentClose forces a simultaneous shutdown and
// broadcast admission race. Every accepted operation must finish before Close
// returns, and no operation may be admitted after Close. The test exercises
// the same gate used for block, transaction and faucet propagation.
func TestAsyncPeerBroadcastConcurrentClose(t *testing.T) {
	for iteration := 0; iteration < 40; iteration++ {
		s := &Server{stopCh: make(chan struct{}), httpClient: &http.Client{}}
		const senders = 24
		start := make(chan struct{})
		release := make(chan struct{})
		var admitted atomic.Int64
		var completed atomic.Int64
		var sendersDone sync.WaitGroup
		sendersDone.Add(senders)
		for i := 0; i < senders; i++ {
			go func() {
				defer sendersDone.Done()
				<-start
				s.runAsync(func() {
					admitted.Add(1)
					<-release
					completed.Add(1)
				})
			}()
		}
		close(start)
		stopped := make(chan struct{})
		go func() {
			s.Close()
			close(stopped)
		}()
		// Releasing these tasks never races with a filesystem cleanup: Close
		// must drain all accepted tasks regardless of admission ordering.
		close(release)
		sendersDone.Wait()
		<-stopped
		if completed.Load() != admitted.Load() {
			t.Fatalf("iteration %d: Close returned with work unfinished: admitted=%d completed=%d", iteration, admitted.Load(), completed.Load())
		}
		before := completed.Load()
		s.runAsync(func() { completed.Add(1) })
		if completed.Load() != before {
			t.Fatalf("iteration %d: work admitted after shutdown", iteration)
		}
		s.Close()
	}
}
