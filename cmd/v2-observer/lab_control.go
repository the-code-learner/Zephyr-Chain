package main

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/bootstrap"
	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	v2node "github.com/zephyr-chain/zephyr-chain/internal/v2/node"
)

// The opt-in observer test socket never binds TCP. It is a private local
// diagnostic surface ONLY, never a production monitoring/control endpoint.
type labObserverControl struct {
	server *http.Server
	path   string
}

func (c *labObserverControl) Close() error {
	if c == nil {
		return nil
	}
	serverErr := c.server.Close()
	fileErr := os.Remove(c.path)
	if errors.Is(fileErr, os.ErrNotExist) {
		fileErr = nil
	}
	return errors.Join(serverErr, fileErr)
}

func startLabObserverControl(path, dataDir string, obs *bootstrap.Observer) (*labObserverControl, error) {
	if obs == nil || !filepath.IsAbs(path) ||
		filepath.Clean(path) != path ||
		path != filepath.Join(dataDir, "lab-observer-status.sock") {
		return nil, bootstrap.ErrObserverConfig
	}
	if _, err := os.Lstat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return nil, bootstrap.ErrObserverConfig
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		_ = listener.Close()
		_ = os.Remove(path)
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		snapshot, err := obs.Service.LatestSnapshot()
		if errors.Is(err, v2node.ErrNoSnapshot) {
			writeObserverStatus(w, http.StatusOK, map[string]any{
				"height": 0, "qcVerified": false, "isValidator": false,
			})
			return
		}
		if err != nil || snapshot.Validate() != nil {
			http.Error(w, "observer QC certificate validation failed", http.StatusInternalServerError)
			return
		}
		writeObserverStatus(w, http.StatusOK, map[string]any{
			"height":      snapshot.Header.Height,
			"headerHash":  v2consensus.HeaderConsensusHash(snapshot.Header).String(),
			"qcVerified":  true,
			"voteCount":   len(snapshot.Certificate.Votes),
			"isValidator": false,
		})
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	c := &labObserverControl{server: server, path: path}
	go func() { _ = server.Serve(listener) }()
	return c, nil
}

func writeObserverStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
