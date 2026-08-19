package p2p

import (
	"context"
	"errors"
	"fmt"

	pubsub "github.com/libp2p/go-libp2p-pubsub"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const DefaultMaxGossipBytes = 4 << 20

var (
	ErrGossipConfig  = errors.New("invalid Zephyr GossipSub configuration")
	ErrGossipPayload = errors.New("Zephyr GossipSub payload exceeds limit")
)

type Gossip struct {
	ps         *pubsub.PubSub
	network    types.NetworkID
	maxPayload int
}

type ShardSubscription struct {
	Shard uint32
	Tx    *pubsub.Subscription
	DA    *pubsub.Subscription
	tx    *pubsub.Topic
	da    *pubsub.Topic
}

func NewGossip(ctx context.Context, node *Node, maxPayload int) (*Gossip, error) {
	if node == nil || node.host == nil || types.IsZero32([32]byte(node.networkID)) {
		return nil, ErrGossipConfig
	}
	if maxPayload <= 0 {
		maxPayload = DefaultMaxGossipBytes
	}
	ps, err := pubsub.NewGossipSub(ctx, node.host, pubsub.WithMaxMessageSize(maxPayload), pubsub.WithFloodPublish(true))
	if err != nil {
		return nil, err
	}
	return &Gossip{ps: ps, network: node.networkID, maxPayload: maxPayload}, nil
}

func (g *Gossip) JoinShard(shard uint32) (*ShardSubscription, error) {
	if g == nil || g.ps == nil {
		return nil, ErrGossipConfig
	}
	txTopic, err := g.ps.Join(g.topic(shard, "tx"))
	if err != nil {
		return nil, err
	}
	daTopic, err := g.ps.Join(g.topic(shard, "da"))
	if err != nil {
		_ = txTopic.Close()
		return nil, err
	}
	txSub, err := txTopic.Subscribe()
	if err != nil {
		_ = txTopic.Close()
		_ = daTopic.Close()
		return nil, err
	}
	daSub, err := daTopic.Subscribe()
	if err != nil {
		txSub.Cancel()
		_ = txTopic.Close()
		_ = daTopic.Close()
		return nil, err
	}
	return &ShardSubscription{Shard: shard, Tx: txSub, DA: daSub, tx: txTopic, da: daTopic}, nil
}

func (s *ShardSubscription) Close() error {
	if s == nil {
		return nil
	}
	if s.Tx != nil {
		s.Tx.Cancel()
	}
	if s.DA != nil {
		s.DA.Cancel()
	}
	var first error
	if s.tx != nil {
		first = s.tx.Close()
	}
	if s.da != nil {
		if err := s.da.Close(); first == nil {
			first = err
		}
	}
	return first
}

func (g *Gossip) PublishTransaction(ctx context.Context, subscription *ShardSubscription, payload []byte) error {
	if subscription == nil || subscription.tx == nil {
		return ErrGossipConfig
	}
	return g.publish(ctx, subscription.tx, payload)
}

func (g *Gossip) PublishDA(ctx context.Context, subscription *ShardSubscription, payload []byte) error {
	if subscription == nil || subscription.da == nil {
		return ErrGossipConfig
	}
	return g.publish(ctx, subscription.da, payload)
}

func (g *Gossip) NextTransaction(ctx context.Context, subscription *ShardSubscription) ([]byte, error) {
	if subscription == nil || subscription.Tx == nil {
		return nil, ErrGossipConfig
	}
	return g.next(ctx, subscription.Tx)
}

func (g *Gossip) NextDA(ctx context.Context, subscription *ShardSubscription) ([]byte, error) {
	if subscription == nil || subscription.DA == nil {
		return nil, ErrGossipConfig
	}
	return g.next(ctx, subscription.DA)
}

func (g *Gossip) publish(ctx context.Context, topic *pubsub.Topic, payload []byte) error {
	if len(payload) > g.maxPayload {
		return ErrGossipPayload
	}
	return topic.Publish(ctx, payload)
}

func (g *Gossip) next(ctx context.Context, subscription *pubsub.Subscription) ([]byte, error) {
	message, err := subscription.Next(ctx)
	if err != nil {
		return nil, err
	}
	if len(message.Data) > g.maxPayload {
		return nil, ErrGossipPayload
	}
	return append([]byte(nil), message.Data...), nil
}

func (g *Gossip) topic(shard uint32, kind string) string {
	return fmt.Sprintf("/zephyr/%s/v2/shard/%d/%s", g.network.String(), shard, kind)
}

func GossipTopic(network types.NetworkID, shard uint32, kind string) (string, error) {
	if types.IsZero32([32]byte(network)) || (kind != "tx" && kind != "da") {
		return "", ErrGossipConfig
	}
	return fmt.Sprintf("/zephyr/%s/v2/shard/%d/%s", network.String(), shard, kind), nil
}
