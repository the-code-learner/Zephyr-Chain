package api

import (
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// A broadcast accepted before Close must finish before Close returns,
// and no new asynchronous filesystem/network work may be scheduled after.
func TestAsyncPeerBroadcastDrainAndCloseAdmission(t *testing.T) {
	s := &Server{stopCh: make(chan struct{}), httpClient: &http.Client{}}
	started := make(chan struct{})
	release := make(chan struct{})
	var completed atomic.Int32
	s.runAsync(func() {
		close(started)
		<-release
		completed.Add(1)
	})
	<-started
	var wg sync.WaitGroup
	wg.Add(1)
	closed := make(chan struct{})
	go func() {
		defer wg.Done()
		s.Close()
		close(closed)
	}()
	select {
	case <-closed:
		t.Fatal("Close returned before pending broadcast finished")
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	if completed.Load() != 1 {
		t.Fatalf("accepted work not drained: %d", completed.Load())
	}
	s.runAsync(func() { completed.Add(1) })
	if completed.Load() != 1 {
		t.Fatal("new broadcast admitted after Close")
	}
	s.Close()
}
