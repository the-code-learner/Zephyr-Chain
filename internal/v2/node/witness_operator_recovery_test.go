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
