package node

import (
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// WitnessPublisherPolicy binds an operator-provisioned HTTPS publisher to
// independently pinned checkpoint-signing keys. Endpoint strings and signing
// keys MUST be obtained through an authenticated configuration channel.
type WitnessPublisherPolicy struct {
	Endpoint string
	Keys     [][]byte
}

// NewOperatorSeparatedWitnessFloor wires HTTPS retrieval, per-publisher
// signature verification, and strict multi-source agreement. It rejects
// repeated URL authorities and hostnames (including different ports) to
// avoid accidentally counting the same service twice.
//
// Distinct hostnames are only a configuration sanity check, NOT proof of
// separate physical operators, infrastructure, ASNs or fault domains.
// Deployment approval requires independent evidence of those properties.
func NewOperatorSeparatedWitnessFloor(policies []WitnessPublisherPolicy, network types.NetworkID, root types.Hash) (*MultiSourceWitnessFloor, error) {
	if len(policies) < 2 || len(policies) > 8 {
		return nil, ErrWitnessSigningIntent
	}
	seenHosts := make(map[string]struct{}, len(policies))
	sources := make([]WitnessExternalFloorVerifier, 0, len(policies))
	for _, policy := range policies {
		u, err := url.Parse(policy.Endpoint)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" ||
			u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" {
			return nil, ErrWitnessSigningIntent
		}
		host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
		if net.ParseIP(host) != nil || !validWitnessPublisherDNSName(host) {
			// Reject IP literals, local-only names, non-canonical DNS labels
			// and deceptive repeated separators. DNS validity alone does
			// NOT establish independent infrastructure or key custody.
			return nil, ErrWitnessSigningIntent
		}
		if _, duplicate := seenHosts[host]; duplicate {
			return nil, ErrWitnessSigningIntent
		}
		seenHosts[host] = struct{}{}
		transport, err := NewHTTPWitnessFloorSource(policy.Endpoint)
		if err != nil {
			return nil, err
		}
		verified, err := NewQuorumAuthenticatedWitnessFloor(transport, policy.Keys, network, root)
		if err != nil {
			return nil, err
		}
		sources = append(sources, verified)
	}
	floor, err := NewMultiSourceWitnessFloor(sources...)
	if err != nil {
		return nil, err
	}
	// These independently configured HTTPS sources are read sequentially.
	// Apply one aggregate five-second budget rather than permitting each
	// unresponsive publisher to consume its own five-second client timeout.
	// The strict 3+ operator attested constructor has the same fail-closed
	// budget; this legacy constructor still does NOT prove independence.
	floor.readTimeout = 5 * time.Second
	return floor, nil
}

// validWitnessPublisherDNSName accepts only fully qualified ASCII DNS
// hostnames. It does not resolve DNS or attempt to prove separate operators;
// independent infrastructure and publisher key custody remain release gates.
func validWitnessPublisherDNSName(host string) bool {
	if len(host) < 4 || len(host) > 253 || !strings.Contains(host, ".") ||
		net.ParseIP(host) != nil ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") {
		return false
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return len(labels[len(labels)-1]) >= 2
}
