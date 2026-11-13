package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/bootstrap"
	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	v2node "github.com/zephyr-chain/zephyr-chain/internal/v2/node"
)

// labControl is a deliberately local, opt-in test driver. Never expose its
// control plane via HTTP/TCP or reuse it as a production administrative API.
type labControl struct {
	server   *http.Server
	listener net.Listener
	path     string
}

func (c *labControl) Close() error {
	if c == nil {
		return nil
	}
	closeErr := c.server.Close()
	unlinkErr := os.Remove(c.path)
	if errors.Is(unlinkErr, os.ErrNotExist) {
		unlinkErr = nil
	}
	return errors.Join(closeErr, unlinkErr)
}

func startLabControl(path, stateDir string, validator *bootstrap.LabValidator) (*labControl, error) {
	if validator == nil || !filepath.IsAbs(path) ||
		filepath.Clean(path) != path ||
		path != filepath.Join(stateDir, "lab-control.sock") {
		return nil, errLabCommand
	}
	if _, err := os.Lstat(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return nil, errLabCommand
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
		snapshot, err := validator.Service.LatestSnapshot()
		if errors.Is(err, v2node.ErrNoSnapshot) {
			writeLabJSON(w, http.StatusOK, map[string]any{"height": 0, "qcVerified": false})
			return
		}
		if err != nil || validator.Service.Validators.VerifyCertificate(snapshot.Certificate) != nil {
			http.Error(w, "unable to verify locally finalized certificate", http.StatusInternalServerError)
			return
		}
		writeLabJSON(w, http.StatusOK, map[string]any{
			"height":     snapshot.Header.Height,
			"headerHash": v2consensus.HeaderConsensusHash(snapshot.Header).String(),
			"qcVerified": true,
			"voteCount":  len(snapshot.Certificate.Votes),
		})
	})
	mux.HandleFunc("/connect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		defer r.Body.Close()
		var input struct {
			Peers []string `json:"peers"`
		}
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || len(input.Peers) == 0 || len(input.Peers) > 20 {
			http.Error(w, "invalid lab peer list", http.StatusBadRequest)
			return
		}
		var trailing any
		if decoder.Decode(&trailing) != io.EOF {
			http.Error(w, "invalid trailing lab request", http.StatusBadRequest)
			return
		}
		remotes, err := bootstrap.ParsePinnedPeers(input.Peers)
		if err != nil {
			http.Error(w, "peer ID not pinned", http.StatusBadRequest)
			return
		}
		ids := make([]peer.ID, 0, len(remotes))
		for _, remote := range remotes {
			if remote.ID == validator.Transport.ID() {
				http.Error(w, "self connection", http.StatusBadRequest)
				return
			}
			ids = append(ids, remote.ID)
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		if err := validator.ConnectPeers(ctx, remotes); err != nil {
			http.Error(w, "unable to connect to pinned peer", http.StatusServiceUnavailable)
			return
		}
		validator.Service.SetPeers(ids)
		writeLabJSON(w, http.StatusOK, map[string]any{"connectedPeers": len(ids)})
	})
	mux.HandleFunc("/propose", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.ContentLength > 0 {
			http.Error(w, "request body not allowed", http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		snapshot, err := validator.Service.Propose(ctx)
		if errors.Is(err, v2node.ErrNotProposer) {
			http.Error(w, "not scheduled proposer", http.StatusConflict)
			return
		}
		if err != nil || validator.Service.Validators.VerifyCertificate(snapshot.Certificate) != nil {
			http.Error(w, "certified proposal failed", http.StatusServiceUnavailable)
			return
		}
		writeLabJSON(w, http.StatusOK, map[string]any{
			"height":     snapshot.Header.Height,
			"headerHash": v2consensus.HeaderConsensusHash(snapshot.Header).String(),
			"qcVerified": true,
			"voteCount":  len(snapshot.Certificate.Votes),
			"snapshot":   snapshot,
		})
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	control := &labControl{server: server, listener: listener, path: path}
	go func() { _ = server.Serve(listener) }()
	return control, nil
}

func writeLabJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
