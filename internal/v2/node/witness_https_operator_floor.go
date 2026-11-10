package node

import (
	"net/url"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// WitnessHTTPSPublication is an operator-provisioned public HTTPS origin and
// a locally assigned operator identifier. Identifiers are inventory labels,
// NOT cryptographic evidence that operators or failure domains are independent.
// Independent ownership, keys, infrastructure and signed freshness must be
// separately attested and audited before any production mainnet approval.
type WitnessHTTPSPublication struct {
	OperatorID string
	Endpoint   string
}

// NewProvisionedMultiOperatorHTTPSWitnessFloor is the strict, non-signing
// construction path for externally published, signed witness floors. In
// contrast to NewMultiSourceWitnessFloor (which remains available for
// legacy/test adapters), it rejects:
//   - fewer than three separately provisioned operator IDs or DNS hosts;
//   - fewer than four independently pinned witness public keys;
//   - duplicate operator IDs, duplicate DNS hosts (even with other ports,
//     paths or schemes), and malformed/untrusted HTTPS endpoints.
//
// Each origin is read by a context-aware HTTPS client with an independent
// transport that ignores ambient proxies, rejects redirects and uses a
// five-second deadline. Each bundle is authenticated independently against
// the SAME pinned Ed25519 quorum keys, then all origins must agree exactly.
// This does NOT verify actual operator separation, quorum signer custody,
// globally monotonic publication or freshness across a process restart.
func NewProvisionedMultiOperatorHTTPSWitnessFloor(
	publications []WitnessHTTPSPublication,
	trustedKeys [][]byte,
	network types.NetworkID,
	validatorRoot types.Hash,
) (*MultiSourceWitnessFloor, error) {
	if len(publications) < 3 || len(publications) > 8 ||
		len(trustedKeys) < 4 || len(trustedKeys) > maxWitnessKeys {
		return nil, ErrWitnessSigningIntent
	}
	seenOperators := make(map[string]struct{}, len(publications))
	seenHosts := make(map[string]struct{}, len(publications))
	sources := make([]WitnessExternalFloorVerifier, 0, len(publications))
	for _, publication := range publications {
		if !validWitnessPublicationOperatorID(publication.OperatorID) {
			return nil, ErrWitnessSigningIntent
		}
		if _, duplicate := seenOperators[publication.OperatorID]; duplicate {
			return nil, ErrWitnessSigningIntent
		}
		source, err := NewHTTPWitnessFloorSource(publication.Endpoint)
		if err != nil {
			return nil, err
		}
		u, err := url.Parse(source.endpoint)
		if err != nil || u.Hostname() == "" {
			return nil, ErrWitnessSigningIntent
		}
		// Distinct port/path on the SAME DNS identity is not independent.
		// DNS aliases and shared operator infrastructure still require
		// out-of-band provisioning review; strings cannot certify that.
		host := u.Hostname()
		if _, duplicate := seenHosts[host]; duplicate {
			return nil, ErrWitnessSigningIntent
		}
		verified, err := NewQuorumAuthenticatedWitnessFloor(source, trustedKeys, network, validatorRoot)
		if err != nil {
			return nil, err
		}
		seenOperators[publication.OperatorID] = struct{}{}
		seenHosts[host] = struct{}{}
		sources = append(sources, verified)
	}
	floor, err := NewMultiSourceWitnessFloor(sources...)
	if err != nil {
		return nil, err
	}
	// A single five-second budget covers the entire multi-origin read, not
	// five seconds per origin. Do not relax fail-closed all-origin agreement.
	floor.readTimeout = 5 * time.Second
	return floor, nil
}

func validWitnessPublicationOperatorID(id string) bool {
	if len(id) == 0 || len(id) > 63 || id[0] < 'a' || id[0] > 'z' ||
		id[len(id)-1] == '-' {
		return false
	}
	for i := 1; i < len(id); i++ {
		c := id[i]
		if c != '-' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}
