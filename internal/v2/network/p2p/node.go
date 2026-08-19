package p2p

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	p2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	DefaultMaxMessageBytes = 4 << 20
	DefaultStreamTimeout   = 10 * time.Second
)

var (
	ErrConfig        = errors.New("invalid Zephyr p2p configuration")
	ErrFrameTooLarge = errors.New("Zephyr p2p frame exceeds limit")
	ErrNoHandler     = errors.New("Zephyr p2p protocol handler unavailable")
)

type Handler func(context.Context, peer.ID, []byte) ([]byte, error)

type Handlers struct {
	Consensus   Handler
	Transaction Handler
	LightProof  Handler
}

type Config struct {
	Network         types.NetworkID
	Identity        p2pcrypto.PrivKey
	ListenAddrs     []string
	MaxMessageBytes uint32
	StreamTimeout   time.Duration
	Handlers        Handlers
}

type Node struct {
	host        host.Host
	networkID   types.NetworkID
	maxMessage  uint32
	timeout     time.Duration
	consensus   protocol.ID
	transaction protocol.ID
	lightProof  protocol.ID
}

func New(cfg Config) (*Node, error) {
	if types.IsZero32([32]byte(cfg.Network)) {
		return nil, ErrConfig
	}
	identity := cfg.Identity
	if identity == nil {
		var err error
		identity, _, err = p2pcrypto.GenerateEd25519Key(rand.Reader)
		if err != nil {
			return nil, err
		}
	}
	if len(cfg.ListenAddrs) == 0 {
		cfg.ListenAddrs = []string{"/ip4/0.0.0.0/udp/0/quic-v1", "/ip6/::/udp/0/quic-v1"}
	}
	if cfg.MaxMessageBytes == 0 {
		cfg.MaxMessageBytes = DefaultMaxMessageBytes
	}
	if cfg.StreamTimeout <= 0 {
		cfg.StreamTimeout = DefaultStreamTimeout
	}

	h, err := libp2p.New(libp2p.Identity(identity), libp2p.ListenAddrStrings(cfg.ListenAddrs...))
	if err != nil {
		return nil, err
	}
	prefix := fmt.Sprintf("/zephyr/%s/v2", cfg.Network.String())
	n := &Node{
		host: h, networkID: cfg.Network, maxMessage: cfg.MaxMessageBytes, timeout: cfg.StreamTimeout,
		consensus: protocol.ID(prefix + "/consensus"), transaction: protocol.ID(prefix + "/tx"), lightProof: protocol.ID(prefix + "/light-proof"),
	}
	n.install(n.consensus, cfg.Handlers.Consensus)
	n.install(n.transaction, cfg.Handlers.Transaction)
	n.install(n.lightProof, cfg.Handlers.LightProof)
	return n, nil
}

func (n *Node) Close() error                     { return n.host.Close() }
func (n *Node) Host() host.Host                  { return n.host }
func (n *Node) ID() peer.ID                      { return n.host.ID() }
func (n *Node) Addrs() []ma.Multiaddr            { return append([]ma.Multiaddr(nil), n.host.Addrs()...) }
func (n *Node) ConsensusProtocol() protocol.ID   { return n.consensus }
func (n *Node) TransactionProtocol() protocol.ID { return n.transaction }
func (n *Node) LightProofProtocol() protocol.ID  { return n.lightProof }

func (n *Node) AddrInfo() peer.AddrInfo {
	return peer.AddrInfo{ID: n.host.ID(), Addrs: n.Addrs()}
}
func (n *Node) Connect(ctx context.Context, remote peer.AddrInfo) error {
	return n.host.Connect(ctx, remote)
}
func (n *Node) SendConsensus(ctx context.Context, remote peer.ID, payload []byte) ([]byte, error) {
	return n.request(ctx, remote, n.consensus, payload)
}
func (n *Node) SendTransaction(ctx context.Context, remote peer.ID, payload []byte) ([]byte, error) {
	return n.request(ctx, remote, n.transaction, payload)
}
func (n *Node) FetchLightProof(ctx context.Context, remote peer.ID, payload []byte) ([]byte, error) {
	return n.request(ctx, remote, n.lightProof, payload)
}

func (n *Node) install(id protocol.ID, handler Handler) {
	n.host.SetStreamHandler(id, func(stream network.Stream) {
		if handler == nil {
			_ = stream.Reset()
			return
		}
		defer stream.Close()
		_ = stream.SetDeadline(time.Now().Add(n.timeout))
		payload, err := readFrame(stream, n.maxMessage)
		if err != nil {
			_ = stream.Reset()
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), n.timeout)
		defer cancel()
		response, err := handler(ctx, stream.Conn().RemotePeer(), payload)
		if err != nil {
			_ = stream.Reset()
			return
		}
		if response == nil {
			response = []byte{}
		}
		if err := writeFrame(stream, response, n.maxMessage); err != nil {
			_ = stream.Reset()
		}
	})
}

func (n *Node) request(ctx context.Context, remote peer.ID, id protocol.ID, payload []byte) ([]byte, error) {
	if len(payload) > int(n.maxMessage) {
		return nil, ErrFrameTooLarge
	}
	ctx, cancel := context.WithTimeout(ctx, n.timeout)
	defer cancel()
	stream, err := n.host.NewStream(ctx, remote, id)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(n.timeout))
	if err := writeFrame(stream, payload, n.maxMessage); err != nil {
		_ = stream.Reset()
		return nil, err
	}
	response, err := readFrame(stream, n.maxMessage)
	if err != nil {
		_ = stream.Reset()
		return nil, err
	}
	return response, nil
}

func writeFrame(w io.Writer, payload []byte, max uint32) error {
	if len(payload) > int(max) {
		return ErrFrameTooLarge
	}
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(payload)))
	writer := bufio.NewWriter(w)
	if _, err := writer.Write(length[:]); err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	return writer.Flush()
}

func readFrame(r io.Reader, max uint32) ([]byte, error) {
	var length [4]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(length[:])
	if size > max {
		return nil, ErrFrameTooLarge
	}
	payload := make([]byte, int(size))
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}
