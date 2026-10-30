package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/peer"
)

var ErrProviderAdmission = errors.New("compute provider requires explicit experimental execution and an exact peer allowlist")

// parseProviderAdmission is deliberately NOT an on-chain compute-job
// authorization check. It is an operator boundary for the experimental
// provider executable: untrusted network peers cannot request free work.
//
// Before public production, authenticated finalized job/assignment evidence,
// replay protection, quota/accounting and resource controls are still needed.
func parseProviderAdmission(enabled, raw string) (map[peer.ID]struct{}, error) {
	if enabled != "true" || strings.TrimSpace(raw) == "" {
		return nil, ErrProviderAdmission
	}
	result := make(map[peer.ID]struct{})
	for _, fragment := range strings.Split(raw, ",") {
		value := strings.TrimSpace(fragment)
		if value == "" {
			return nil, ErrProviderAdmission
		}
		id, err := peer.Decode(value)
		if err != nil || id == "" {
			return nil, fmt.Errorf("%w: invalid peer ID", ErrProviderAdmission)
		}
		if _, exists := result[id]; exists {
			return nil, fmt.Errorf("%w: duplicate peer ID", ErrProviderAdmission)
		}
		result[id] = struct{}{}
	}
	return result, nil
}

func admitComputePeer(allowed map[peer.ID]struct{}, remote peer.ID) error {
	if remote == "" {
		return ErrProviderAdmission
	}
	if _, ok := allowed[remote]; !ok {
		return ErrProviderAdmission
	}
	return nil
}
