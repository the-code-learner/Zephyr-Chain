package transport

import (
	"context"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

type Role uint8

const (
	RoleCitizen Role = iota + 1
	RoleFullNode
	RoleValidator
	RoleArchive
	RoleComputeProvider
)

type PeerIdentity struct {
	NodeID      types.NodeID
	ValidatorID *types.ValidatorID
	Roles       []Role
}

type ConsensusTransport interface {
	BroadcastProposal(ctx context.Context, payload []byte) error
	BroadcastVote(ctx context.Context, payload []byte) error
	FetchCertifiedBlock(ctx context.Context, height uint64) ([]byte, error)
}

type TransactionTransport interface {
	Submit(ctx context.Context, transaction tx.Transaction) error
	Relay(ctx context.Context, transaction tx.Transaction, shardID uint32) error
}

type LightTransport interface {
	FetchFinalizedHeader(ctx context.Context, height uint64) (sharding.GlobalHeader, error)
	FetchShardCommitment(ctx context.Context, height uint64, shardID uint32) (sharding.Commitment, []byte, error)
	FetchObjectProof(ctx context.Context, root types.Hash, id types.ObjectID) ([]byte, error)
}
