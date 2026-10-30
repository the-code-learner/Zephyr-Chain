package node

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// HTTPWitnessFloorSource fetches an untrusted, bounded canonical bundle. It
// deliberately does NOT authenticate the HTTP server as a witness: the
// QuorumAuthenticatedWitnessFloor must independently verify pinned keys.
// HTTPS is mandatory to reduce accidental transport disclosure/misdirection;
// deployment must still pin trusted signer keys through a separate channel.
type HTTPWitnessFloorSource struct {
	endpoint string
	client   *http.Client
}

func NewHTTPWitnessFloorSource(endpoint string) (*HTTPWitnessFloorSource, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" {
		return nil, ErrWitnessSigningIntent
	}
	return &HTTPWitnessFloorSource{
		endpoint: u.String(),
		client: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// SignedWitnessFloor is a fail-closed one-shot read. Non-200 responses,
// redirects, empty payloads, oversized bodies and unavailable origins fail.
// Callers MUST pass its output to the pinned quorum verification layer.
func (s *HTTPWitnessFloorSource) SignedWitnessFloor() ([]byte, error) {
	if s == nil || s.client == nil {
		return nil, ErrWitnessSigningIntent
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint, nil)
	if err != nil {
		return nil, errors.Join(ErrWitnessSigningIntent, err)
	}
	req.Header.Set("Accept", "application/json")
	response, err := s.client.Do(req)
	if err != nil {
		return nil, errors.Join(ErrWitnessSigningIntent, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: unexpected HTTP status %d", ErrWitnessSigningIntent, response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxWitnessBundleBytes+1))
	if err != nil || len(payload) == 0 || len(payload) > maxWitnessBundleBytes {
		return nil, ErrWitnessSigningIntent
	}
	return payload, nil
}
