//go:build linux || darwin

package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestChallengeAuthenticatedMultiOperatorHTTPSWitnessFloorRejectsReplayAndForgery(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-live-challenge", []byte("network")))
	root := types.HashBytes("witness-live-challenge", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 42, HeaderHash: types.Hash{7}}
	keys, witnessPrivates := witnessTestKeys()
	valid := signedWitnessTestBundle(t, cp, keys, witnessPrivates, 3)
	const revision uint64 = 11
	attested, operatorKeys, operatorPrivates := signedWitnessOperatorInventoryTestFixture(t, network, root, revision)
	floor, err := NewChallengeAuthenticatedMultiOperatorHTTPSWitnessFloor(
		attested, operatorKeys, keys, network, root, revision,
	)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	mode := "valid"
	priorSignature := make([][]byte, len(attested))
	seen := make([]map[string]bool, len(attested))
	servers := make([]*httptest.Server, 0, len(attested))
	for i := range attested {
		i := i
		seen[i] = make(map[string]bool)
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || r.URL.Path != "/v1/checkpoint" ||
				r.Header.Get("Cache-Control") != "no-store" || r.Header.Get("Pragma") != "no-cache" {
				t.Errorf("untrusted HTTPS request format: %s %s", r.Method, r.URL.String())
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			nonceHex := r.Header.Get(witnessHTTPSChallengeHeader)
			raw, err := hex.DecodeString(nonceHex)
			if err != nil || len(raw) != 32 || hex.EncodeToString(raw) != nonceHex {
				t.Errorf("invalid challenge %q: %v", nonceHex, err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var nonce [32]byte
			copy(nonce[:], raw)
			if isZeroWitnessFenceChallenge(nonce) {
				t.Error("zero challenge accepted")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			mu.Lock()
			currentMode := mode
			if seen[i][nonceHex] {
				t.Error("random challenge repeated")
			}
			seen[i][nonceHex] = true
			replayed := append([]byte(nil), priorSignature[i]...)
			mu.Unlock()
			if currentMode == "outage" && i == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			signingRevision := revision
			signingRoot := root
			signingID := attested[i].OperatorID
			if currentMode == "wrong-revision" && i == 1 {
				signingRevision++
			}
			if currentMode == "wrong-root" && i == 1 {
				signingRoot[0] ^= 1
			}
			if currentMode == "wrong-operator" && i == 1 {
				signingID += "-replay"
			}
			digest, err := WitnessHTTPSPublicationChallengeDigest(
				network, signingRoot, signingID, signingRevision, nonce, valid,
			)
			if err != nil {
				t.Errorf("fixture challenge digest: %v", err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			private := operatorPrivates[i]
			if currentMode == "foreign-key" && i == 1 {
				private = operatorPrivates[0]
			}
			sig := ed25519.Sign(private, digest[:])
			if currentMode == "replay-signature" && i == 1 {
				sig = replayed
			}
			mu.Lock()
			if currentMode == "valid" {
				priorSignature[i] = append([]byte(nil), sig...)
			}
			mu.Unlock()
			if currentMode == "duplicate-signature" && i == 1 {
				w.Header().Add(witnessHTTPSOperatorSignatureHeader, hex.EncodeToString(sig))
			}
			if currentMode != "missing-signature" || i != 1 {
				wireSignature := hex.EncodeToString(sig)
				if currentMode == "uppercase-signature" && i == 1 {
					wireSignature = strings.ToUpper(wireSignature)
				}
				w.Header().Add(witnessHTTPSOperatorSignatureHeader, wireSignature)
			}
			if currentMode == "bad-media" && i == 1 {
				w.Header().Set("Content-Type", "text/plain")
			} else {
				w.Header().Set("Content-Type", "application/json")
			}
			if currentMode == "modified-payload" && i == 1 {
				_, _ = w.Write([]byte("{}"))
			} else {
				_, _ = w.Write(valid)
			}
		}))
		servers = append(servers, server)
		quorum, ok := floor.sources[i].(*QuorumAuthenticatedWitnessFloor)
		if !ok {
			t.Fatal("missing authenticated quorum source")
		}
		source, ok := quorum.source.(*HTTPWitnessFloorSource)
		if !ok || source.challengePolicy == nil {
			t.Fatal("origin does not require a pinned operator challenge")
		}
		source.endpoint = server.URL + "/v1/checkpoint"
		source.client = server.Client()
		source.client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	defer func() {
		for _, server := range servers {
			server.Close()
		}
	}()
	latest, err := floor.AuthenticatedWitnessFloorContext(context.Background())
	if err != nil || latest != cp {
		t.Fatalf("three independently challenged signed sources rejected: %+v %v", latest, err)
	}
	for _, negative := range []string{
		"replay-signature", "missing-signature", "foreign-key",
		"wrong-revision", "wrong-root", "wrong-operator", "duplicate-signature",
		"uppercase-signature", "bad-media", "modified-payload", "outage",
	} {
		t.Run(negative, func(t *testing.T) {
			mu.Lock()
			mode = negative
			mu.Unlock()
			read, err := floor.AuthenticatedWitnessFloorContext(context.Background())
			if !errors.Is(err, ErrWitnessSigningIntent) {
				t.Fatalf("%s incorrectly authorized checkpoint %+v: %v", negative, read, err)
			}
		})
	}
	mu.Lock()
	mode = "valid"
	mu.Unlock()
	latest, err = floor.AuthenticatedWitnessFloorContext(context.Background())
	if err != nil || latest != cp {
		t.Fatalf("valid live operator signatures failed after rejected responses: %+v %v", latest, err)
	}
}

func TestWitnessHTTPSPublicationChallengeDigestBindsAllFields(t *testing.T) {
	n := types.NetworkID{1}
	r := types.Hash{2}
	nonce := [32]byte{3}
	payload := []byte("signed-quorum-payload")
	digest, err := WitnessHTTPSPublicationChallengeDigest(n, r, "operator-one", 7, nonce, payload)
	if err != nil {
		t.Fatal(err)
	}
	changes := [][32]byte{}
	for _, variant := range []struct {
		network types.NetworkID
		root    types.Hash
		id      string
		rev     uint64
		nonce   [32]byte
		body    []byte
	}{
		{types.NetworkID{4}, r, "operator-one", 7, nonce, payload},
		{n, types.Hash{4}, "operator-one", 7, nonce, payload},
		{n, r, "operator-two", 7, nonce, payload},
		{n, r, "operator-one", 8, nonce, payload},
		{n, r, "operator-one", 7, [32]byte{4}, payload},
		{n, r, "operator-one", 7, nonce, []byte("changed")},
	} {
		got, err := WitnessHTTPSPublicationChallengeDigest(
			variant.network, variant.root, variant.id, variant.rev, variant.nonce, variant.body,
		)
		if err != nil || bytes.Equal(got[:], digest[:]) {
			t.Fatalf("replay across challenge identity accepted: digest=%x err=%v", got, err)
		}
		changes = append(changes, got)
	}
	if len(changes) != 6 {
		t.Fatal("missing challenge bindings")
	}
	for _, invalid := range []struct {
		id      string
		rev     uint64
		nonce   [32]byte
		payload []byte
	}{
		{"", 7, nonce, payload},
		{"operator-one", 0, nonce, payload},
		{"operator-one", 7, [32]byte{}, payload},
		{"operator-one", 7, nonce, nil},
		{"operator-one", 7, nonce, make([]byte, maxWitnessBundleBytes+1)},
	} {
		if _, err := WitnessHTTPSPublicationChallengeDigest(n, r, invalid.id, invalid.rev, invalid.nonce, invalid.payload); !errors.Is(err, ErrWitnessSigningIntent) {
			t.Fatalf("invalid unsigned challenge parameters accepted: %+v", invalid)
		}
	}
}
