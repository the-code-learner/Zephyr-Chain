package main

import (
	"net/http"
	"testing"
)

func TestNewNodeHTTPServerAppliesDefensiveTimeouts(t *testing.T) {
	handler := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	server := newNodeHTTPServer("127.0.0.1:8080", handler)

	if server.Addr != "127.0.0.1:8080" {
		t.Fatalf("expected configured address, got %q", server.Addr)
	}
	if server.Handler == nil {
		t.Fatal("expected handler to be configured")
	}
	if server.ReadHeaderTimeout != nodeReadHeaderTimeout {
		t.Fatalf("unexpected read-header timeout: %s", server.ReadHeaderTimeout)
	}
	if server.ReadTimeout != nodeReadTimeout {
		t.Fatalf("unexpected read timeout: %s", server.ReadTimeout)
	}
	if server.WriteTimeout != nodeWriteTimeout {
		t.Fatalf("unexpected write timeout: %s", server.WriteTimeout)
	}
	if server.IdleTimeout != nodeIdleTimeout {
		t.Fatalf("unexpected idle timeout: %s", server.IdleTimeout)
	}
	if server.MaxHeaderBytes != nodeMaxHeaderBytes {
		t.Fatalf("unexpected max header bytes: %d", server.MaxHeaderBytes)
	}
}
