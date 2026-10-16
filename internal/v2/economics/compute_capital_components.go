package economics

import (
	"bytes"
	"math"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

type ComputeCapitalComponentKind uint8

const (
	ComputeCapitalComponentUnknown ComputeCapitalComponentKind = iota
	ComputeCapitalComponentEscrow
	ComputeCapitalComponentCollateral
)

// ComputeCapitalComponent is a stable shadow identity for one economically
// distinct portion of native capital held by a compute job. Component IDs are
// deterministic across job-object version updates so later lineage accounting
// can keep owner escrow separate from provider collateral.
type ComputeCapitalComponent struct {
	ID       types.Hash
	Kind     ComputeCapitalComponentKind
	JobID    types.JobID
	Provider types.AccountID
	Amount   uint64
}

type ComputeCapitalComponentSet struct {
	JobID       types.JobID
	Escrow      ComputeCapitalComponent
	Collateral  []ComputeCapitalComponent
	TotalLocked uint64
}

func ComputeEscrowComponentID(jobID types.JobID) (types.Hash, error) {
	if types.IsZero32([32]byte(jobID)) {
		return types.Hash{}, ErrFinalizedEconomics
	}
	return types.HashBytes("economics/compute-capital/escrow/v1", jobID[:]), nil
}

func ComputeCollateralComponentID(jobID types.JobID, provider types.AccountID) (types.Hash, error) {
	if types.IsZero32([32]byte(jobID)) || types.IsZero32([32]byte(provider)) {
		return types.Hash{}, ErrFinalizedEconomics
	}
	payload := make([]byte, 0, len(jobID)+len(provider))
	payload = append(payload, jobID[:]...)
	payload = append(payload, provider[:]...)
	return types.HashBytes("economics/compute-capital/collateral/v1", payload), nil
}

// DeriveComputeCapitalComponents exposes the economic subcomponents carried by
// a compute job without mutating consensus state or IdleCapitalTracker.
//
// Escrow is always one stable component keyed by JobID. Each assignment adds a
// separate provider-collateral component keyed by JobID+provider. This avoids
// the incorrect shortcut of treating the whole job object's locked value as
// productive compute spend.
func DeriveComputeCapitalComponents(record compute.OnChainJob) (ComputeCapitalComponentSet, error) {
	if _, err := record.MarshalBinary(); err != nil || record.Escrow == 0 {
		return ComputeCapitalComponentSet{}, ErrFinalizedEconomics
	}
	escrowID, err := ComputeEscrowComponentID(record.ID)
	if err != nil {
		return ComputeCapitalComponentSet{}, err
	}
	out := ComputeCapitalComponentSet{
		JobID: record.ID,
		Escrow: ComputeCapitalComponent{
			ID:     escrowID,
			Kind:   ComputeCapitalComponentEscrow,
			JobID:  record.ID,
			Amount: record.Escrow,
		},
		TotalLocked: record.Escrow,
	}
	if record.Job.CollateralRequired == 0 {
		return out, nil
	}

	seenProviders := make(map[types.AccountID]struct{}, len(record.Assignments))
	out.Collateral = make([]ComputeCapitalComponent, 0, len(record.Assignments))
	for _, assignment := range record.Assignments {
		if _, duplicate := seenProviders[assignment.Provider]; duplicate {
			return ComputeCapitalComponentSet{}, ErrFinalizedEconomics
		}
		seenProviders[assignment.Provider] = struct{}{}
		componentID, err := ComputeCollateralComponentID(record.ID, assignment.Provider)
		if err != nil || math.MaxUint64-out.TotalLocked < record.Job.CollateralRequired {
			return ComputeCapitalComponentSet{}, ErrFinalizedEconomics
		}
		out.TotalLocked += record.Job.CollateralRequired
		out.Collateral = append(out.Collateral, ComputeCapitalComponent{
			ID:       componentID,
			Kind:     ComputeCapitalComponentCollateral,
			JobID:    record.ID,
			Provider: assignment.Provider,
			Amount:   record.Job.CollateralRequired,
		})
	}
	sort.Slice(out.Collateral, func(i, j int) bool {
		return bytes.Compare(out.Collateral[i].Provider[:], out.Collateral[j].Provider[:]) < 0
	})
	return out, nil
}
