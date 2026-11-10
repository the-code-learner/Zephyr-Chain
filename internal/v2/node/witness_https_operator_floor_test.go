//go:build linux || darwin

package node

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func witnessOperatorFloorTestProvisioning() []WitnessHTTPSPublication {
	return []WitnessHTTPSPublication{
		{OperatorID: "operator-north", Endpoint: "https://witness.north.example.org/v1/floor"},
		{OperatorID: "operator-east", Endpoint: "https://witness.east.example.org/v1/floor"},
		{OperatorID: "operator-west", Endpoint: "https://witness.west.example.org/v1/floor"},
	}
}

func TestProvisionedMultiOperatorHTTPSWitnessFloorRejectsAmbiguousProvisioning(t *testing.T) {
	keys, _ := witnessTestKeys()
	network := types.NetworkID(types.HashBytes("operator-https-floor", []byte("network")))
	root := types.HashBytes("operator-https-floor", []byte("validator-root"))
	good := witnessOperatorFloorTestProvisioning()
	valid, err := NewProvisionedMultiOperatorHTTPSWitnessFloor(good, keys, network, root)
	if err != nil || len(valid.sources) != 3 {
		t.Fatalf("strict three-origin configuration rejected: sources=%d err=%v", len(valid.sources), err)
	}
	for index, source := range valid.sources {
		q, ok := source.(*QuorumAuthenticatedWitnessFloor)
		if !ok {
			t.Fatalf("origin %d not locally quorum-verified: %T", index, source)
		}
		httpSource, ok := q.source.(*HTTPWitnessFloorSource)
		if !ok || httpSource == nil || httpSource.client == nil {
			t.Fatalf("origin %d not backed by context-aware HTTPS: %T", index, q.source)
		}
		if httpSource.client.Timeout != 5*time.Second {
			t.Fatalf("origin %d no five-second bounded HTTP request", index)
		}
		transport, ok := httpSource.client.Transport.(*http.Transport)
		if !ok || transport.Proxy != nil || httpSource.client.CheckRedirect == nil {
			t.Fatalf("origin %d lacks proxy/redirect isolation", index)
		}
		if _, contextual := interface{}(httpSource).(interface {
			SignedWitnessFloorContext(context.Context) ([]byte, error)
		}); !contextual {
			t.Fatalf("origin %d not cancelable on actual source", index)
		}
	}
	// Constructor copies URLs and independently pinned quorum keys.
	good[0].Endpoint = "https://attacker.example.org/changed"
	keys[0][0] ^= 0xff
	first := valid.sources[0].(*QuorumAuthenticatedWitnessFloor)
	if first.source.(*HTTPWitnessFloorSource).endpoint != "https://witness.north.example.org/v1/floor" ||
		first.trustedKeys[0][0] == keys[0][0] {
		t.Fatal("operator config or pinned keys aliased mutable caller input")
	}

	tests := []struct {
		name string
		edit func([]WitnessHTTPSPublication) []WitnessHTTPSPublication
	}{
		{"missing operator", func(p []WitnessHTTPSPublication) []WitnessHTTPSPublication { return p[:2] }},
		{"duplicate operator", func(p []WitnessHTTPSPublication) []WitnessHTTPSPublication {
			p[1].OperatorID = p[0].OperatorID
			return p
		}},
		{"same DNS different path", func(p []WitnessHTTPSPublication) []WitnessHTTPSPublication {
			p[1].Endpoint = "https://witness.north.example.org/different"
			return p
		}},
		{"same DNS different port", func(p []WitnessHTTPSPublication) []WitnessHTTPSPublication {
			p[1].Endpoint = "https://witness.north.example.org:8443/other"
			return p
		}},
		{"duplicate endpoint", func(p []WitnessHTTPSPublication) []WitnessHTTPSPublication { p[1].Endpoint = p[0].Endpoint; return p }},
		{"HTTP origin", func(p []WitnessHTTPSPublication) []WitnessHTTPSPublication {
			p[0].Endpoint = "http://witness.north.example.org/v1/floor"
			return p
		}},
		{"encoded path alias", func(p []WitnessHTTPSPublication) []WitnessHTTPSPublication {
			p[0].Endpoint = "https://witness.north.example.org/v1/%66loor"
			return p
		}},
		{"local IP literal", func(p []WitnessHTTPSPublication) []WitnessHTTPSPublication {
			p[0].Endpoint = "https://127.0.0.1/v1/floor"
			return p
		}},
		{"empty operator ID", func(p []WitnessHTTPSPublication) []WitnessHTTPSPublication { p[0].OperatorID = ""; return p }},
		{"case alias", func(p []WitnessHTTPSPublication) []WitnessHTTPSPublication {
			p[0].OperatorID = "Operator-north"
			return p
		}},
		{"padded operator ID", func(p []WitnessHTTPSPublication) []WitnessHTTPSPublication {
			p[0].OperatorID = "operator-north "
			return p
		}},
		{"trailing hyphen", func(p []WitnessHTTPSPublication) []WitnessHTTPSPublication {
			p[0].OperatorID = "operator-north-"
			return p
		}},
		{"leading digit", func(p []WitnessHTTPSPublication) []WitnessHTTPSPublication { p[0].OperatorID = "9north"; return p }},
		{"operator ID too long", func(p []WitnessHTTPSPublication) []WitnessHTTPSPublication {
			p[0].OperatorID = "a" + strings.Repeat("b", 63)
			return p
		}},
		{"oversized source count", func(p []WitnessHTTPSPublication) []WitnessHTTPSPublication {
			for i := 0; i < 6; i++ {
				p = append(p, WitnessHTTPSPublication{OperatorID: "extra" + string(rune('a'+i)), Endpoint: "https://extra" + string(rune('a'+i)) + ".example.org/v1/floor"})
			}
			return p
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.edit(witnessOperatorFloorTestProvisioning())
			trusted, _ := witnessTestKeys()
			if _, err := NewProvisionedMultiOperatorHTTPSWitnessFloor(p, trusted, network, root); !errors.Is(err, ErrWitnessSigningIntent) {
				t.Fatalf("accepted ambiguous operator HTTPS inventory: err=%v", err)
			}
		})
	}
	policy := witnessOperatorFloorTestProvisioning()
	trusted, _ := witnessTestKeys()
	if _, err := NewProvisionedMultiOperatorHTTPSWitnessFloor(policy, trusted[:3], network, root); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("accepted fewer than four independently pinned witness keys: %v", err)
	}
	trusted[1] = trusted[0]
	if _, err := NewProvisionedMultiOperatorHTTPSWitnessFloor(policy, trusted, network, root); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("accepted duplicate pinned witness keys: %v", err)
	}
}

func TestProvisionedMultiOperatorHTTPSWitnessFloorQuorumAndAvailability(t *testing.T) {
	keys, privates := witnessTestKeys()
	network := types.NetworkID(types.HashBytes("operator-https-bundles", []byte("network")))
	root := types.HashBytes("operator-https-bundles", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 35, HeaderHash: types.Hash{6}}
	pubs := witnessOperatorFloorTestProvisioning()
	floor, err := NewProvisionedMultiOperatorHTTPSWitnessFloor(pubs, keys, network, root)
	if err != nil {
		t.Fatal(err)
	}
	var requests [3]atomic.Int32
	var bodies [3]atomic.Value
	var status [3]atomic.Int32
	setAll := func(checkpoint FinalityCheckpoint) {
		bundle := signedWitnessTestBundle(t, checkpoint, keys, privates, 3)
		for i := range bodies {
			bodies[i].Store(bundle)
			status[i].Store(http.StatusOK)
		}
	}
	setAll(cp)
	for i := 0; i < 3; i++ {
		index := i
		// Test-only endpoints are deliberately loopback TLS with per-test
		// injected trust CAs. Production constructors still require DNS.
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests[index].Add(1)
			if r.Method != http.MethodGet {
				t.Errorf("origin %d accepted non-GET request", index)
			}
			w.WriteHeader(int(status[index].Load()))
			_, _ = w.Write(bodies[index].Load().([]byte))
		}))
		t.Cleanup(server.Close)
		quorum := floor.sources[i].(*QuorumAuthenticatedWitnessFloor)
		source := quorum.source.(*HTTPWitnessFloorSource)
		source.endpoint = server.URL + "/v1/floor"
		source.client = server.Client()
		source.client.Timeout = 5 * time.Second
		source.client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	}
	got, err := floor.AuthenticatedWitnessFloorContext(context.Background())
	if err != nil || got != cp {
		t.Fatalf("three signed HTTPS origins did not agree on authentic checkpoint: %+v %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := [3]int32{requests[0].Load(), requests[1].Load(), requests[2].Load()}
	if _, err := floor.AuthenticatedWitnessFloorContext(ctx); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("already canceled read was accepted: %v", err)
	}
	for i := range requests {
		if requests[i].Load() != before[i] {
			t.Fatalf("canceled floor read contacted origin %d", i)
		}
	}
	// One unavailable operator must block authorization, even when the
	// other two produce the valid, signed quorum response.
	status[1].Store(http.StatusServiceUnavailable)
	if _, err := floor.AuthenticatedWitnessFloorContext(context.Background()); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("two of three HTTP origins authorized unavailable witness: %v", err)
	}
	status[1].Store(http.StatusOK)
	conflict := cp
	conflict.HeaderHash[0]++
	bodies[2].Store(signedWitnessTestBundle(t, conflict, keys, privates, 3))
	if _, err := floor.AuthenticatedWitnessFloorContext(context.Background()); !errors.Is(err, ErrWitnessEquivocation) {
		t.Fatalf("conflicting valid signed publications accepted: %v", err)
	}
	next := cp
	next.Height++
	next.HeaderHash = types.Hash{8}
	setAll(next)
	if got, err = floor.AuthenticatedWitnessFloorContext(context.Background()); err != nil || got != next {
		t.Fatalf("fresh three-origin signed floor not accepted: %+v %v", got, err)
	}
	// A locally observed higher high-water mark cannot be lowered by
	// three agreeing but outdated signed publications.
	setAll(cp)
	if _, err := floor.AuthenticatedWitnessFloorContext(context.Background()); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("agreed but rolled-back signed floor was accepted: %v", err)
	}
}
