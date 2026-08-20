package p2p

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

func (n *Node) ComputeProtocol() protocol.ID {
	return protocol.ID(fmt.Sprintf("/zephyr/%s/v2/compute", n.networkID.String()))
}

func (n *Node) SetComputeHandler(handler Handler) {
	n.install(n.ComputeProtocol(), handler)
}

func (n *Node) SendCompute(ctx context.Context, remote peer.ID, payload []byte) ([]byte, error) {
	return n.request(ctx, remote, n.ComputeProtocol(), payload)
}
