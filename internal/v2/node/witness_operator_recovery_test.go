package node

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestOperatorSeparatedWitnessFloorPolicyRejectsFakeRedundancy(t *testing.T) {
	network := types.NetworkID(types.HashBytes("operator-separated", []byte("net")))
	root := types.HashBytes("operator-separated", []byte("root"))
	keys, _ := witnessTestKeys()
	valid := []WitnessPublisherPolicy{
		{Endpoint: "https://witness-a.example.org/checkpoint", Keys: keys},
		{Endpoint: "https://witness-b.example.org/checkpoint", Keys: keys},
	}
	if _, err := NewOperatorSeparatedWitnessFloor(valid, network, root); err != nil {
		t.Fatalf("two separately configured HTTPS DNS names rejected: %v", err)
	}
	for _, second := range []string{
		"https://witness-a.example.org:8443/checkpoint",
		"https://WITNESS-A.EXAMPLE.ORG./checkpoint",
		"http://witness-b.example.org/checkpoint",
		"https://user:pass@witness-b.example.org/checkpoint",
		"https://127.0.0.1/checkpoint",
		"https://[::1]/checkpoint",
		"https://witness-b.example.org/checkpoint?foo=1",
	} {
		policies := append([]WitnessPublisherPolicy(nil), valid...)
		policies[1].Endpoint = second
		if _, err := NewOperatorSeparatedWitnessFloor(policies, network, root); !errors.Is(err, ErrWitnessSigningIntent) {
			t.Fatalf("insecure or repeated operator endpoint %q accepted: %v", second, err)
		}
	}
	if _, err := NewOperatorSeparatedWitnessFloor(valid[:1], network, root); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("single publisher accepted: %v", err)
	}
	policies := append([]WitnessPublisherPolicy(nil), valid...)
	policies[1].Keys = nil
	if _, err := NewOperatorSeparatedWitnessFloor(policies, network, root); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("missing second publisher trust keys accepted: %v", err)
	}
}

func TestOperatorSeparatedWitnessFloorRequiresCanonicalPublicDNSLabels(t *testing.T) {
	for _, host := range []string{"witness-a.example.org", "operator-123.witness.example.net", "witness-a.example.org."} {
		canonical := host
		if len(canonical) > 0 && canonical[len(canonical)-1] == '.' {
			canonical = canonical[:len(canonical)-1]
		}
		if !validWitnessPublisherDNSName(canonical) {
			t.Fatalf("valid DNS name %q rejected", canonical)
		}
	}
	for _, host := range []string{
		"localhost",
		"witness.local",
		"node.localhost",
		"witness",
		"witness..example.org",
		"-witness.example.org",
		"witness-.example.org",
		"witness.example.org.",
		"witness_example.org",
		"witness.éxample.org",
		"witness.example.a",
	} {
		if validWitnessPublisherDNSName(host) {
			t.Fatalf("noncanonical DNS name %q accepted", host)
		}
	}
	network := types.NetworkID(types.HashBytes("operator-separated", []byte("strict-dns")))
	root := types.HashBytes("operator-separated", []byte("strict-dns-root"))
	keys, _ := witnessTestKeys()
	for _, endpoint := range []string{
		"https://localhost/checkpoint",
		"https://witness.local/checkpoint",
		"https://witness..example.org/checkpoint",
		"https://witness_example.org/checkpoint",
		"https://-witness.example.org/checkpoint",
	} {
		policies := []WitnessPublisherPolicy{
			{Endpoint: "https://valid-witness.example.net/checkpoint", Keys: keys},
			{Endpoint: endpoint, Keys: keys},
		}
		if _, err := NewOperatorSeparatedWitnessFloor(policies, network, root); !errors.Is(err, ErrWitnessSigningIntent) {
			t.Fatalf("unsafe publisher endpoint %q accepted: %v", endpoint, err)
		}
	}
}
