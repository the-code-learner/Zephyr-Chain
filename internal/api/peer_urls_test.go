package api

import (
	"sync"
	"testing"
)

// Live peer topology is changed during consensus partition/heal tests. Each
// broadcast, status read or recovery peer search must observe a stable
// independent snapshot, even if another goroutine replaces the list.
func TestPeerURLsSnapshotConcurrentReplacement(t *testing.T) {
	server := newTestServer(t, Config{DataDir: t.TempDir()})
	server.setPeerURLs([]string{"http://peer-a.example"})
	first := server.peerURLsSnapshot()
	if len(first) != 1 {
		t.Fatalf("expected one peer, got %v", first)
	}
	first[0] = "http://tampered.example"
	if later := server.peerURLsSnapshot(); len(later) != 1 || later[0] != "http://peer-a.example" {
		t.Fatalf("caller was able to modify peer config through returned slice: %v", later)
	}

	var wg sync.WaitGroup
	for reader := 0; reader < 4; reader++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				current := server.peerURLsSnapshot()
				if len(current) > 2 {
					t.Errorf("invalid snapshot: %v", current)
					return
				}
				// Under the race detector, read every returned element
				// concurrently with replacement and peer admission.
				for _, peer := range current {
					if peer == "" {
						t.Errorf("blank peer in snapshot")
						return
					}
				}
				_ = server.admittedPeerURLs()
				_ = server.peerSnapshot()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			if i%2 == 0 {
				server.setPeerURLs([]string{"http://peer-a.example", "http://peer-b.example"})
			} else {
				server.setPeerURLs(nil)
			}
		}
	}()
	wg.Wait()
}
