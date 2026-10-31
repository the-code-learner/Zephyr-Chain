package node

import (
	"net"
	"net/url"
	"strings"

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
		if host == "" || strings.Contains(host, "%") || net.ParseIP(host) != nil {
			// Require canonical DNS names, not raw addresses or IPv6 zones.
			// DNS still requires independently audited operator separation.
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
	return NewMultiSourceWitnessFloor(sources...)
}
