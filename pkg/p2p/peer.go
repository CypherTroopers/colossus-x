package p2p

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"colossusx/pkg/types"
)

const (
	defaultMaxMessageSize = 4 << 20
	defaultReadTimeout    = 90 * time.Second
	defaultWriteTimeout   = 15 * time.Second
)

type Peer struct {
	ID          string
	Addr        string
	Conn        net.Conn
	Inbound     bool
	ConnectedAt time.Time
	LastSeen    time.Time
	Hello       HelloMessage
	Status      types.PeerStatus
	mu          sync.Mutex
}

func (p *Peer) Send(msg Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(body) > defaultMaxMessageSize {
		return fmt.Errorf("message too large: %d", len(body))
	}
	frame := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(body)))
	copy(frame[4:], body)
	_ = p.Conn.SetWriteDeadline(time.Now().Add(defaultWriteTimeout))
	_, err = p.Conn.Write(frame)
	return err
}

type PeerSet struct {
	mu    sync.RWMutex
	peers map[string]*Peer
}

func NewPeerSet() *PeerSet { return &PeerSet{peers: make(map[string]*Peer)} }

func (ps *PeerSet) Add(peer *Peer) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.peers[peerKey(peer)] = peer
}

func (ps *PeerSet) Remove(peer *Peer) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	delete(ps.peers, peerKey(peer))
}

func (ps *PeerSet) List() []*Peer {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	out := make([]*Peer, 0, len(ps.peers))
	for _, peer := range ps.peers {
		out = append(out, peer)
	}
	return out
}

func (ps *PeerSet) Broadcast(msg Message) {
	for _, peer := range ps.List() {
		_ = peer.Send(msg)
	}
}

func (ps *PeerSet) HasPeerID(id string, exclude *Peer) bool {
	if id == "" {
		return false
	}
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	for _, peer := range ps.peers {
		if peer == exclude {
			continue
		}
		if peer.ID == id {
			return true
		}
	}
	return false
}

func peerKey(peer *Peer) string {
	if peer.ID != "" {
		return peer.ID
	}
	return fmt.Sprintf("%s/%t", peer.Addr, peer.Inbound)
}

func readMessage(r io.Reader) (Message, error) {
	var sizeBuf [4]byte
	if _, err := io.ReadFull(r, sizeBuf[:]); err != nil {
		return Message{}, err
	}
	size := binary.BigEndian.Uint32(sizeBuf[:])
	if size == 0 || size > defaultMaxMessageSize {
		return Message{}, fmt.Errorf("invalid message size %d", size)
	}
	payload := make([]byte, size)
	if _, err := io.ReadFull(r, payload); err != nil {
		return Message{}, err
	}
	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		return Message{}, err
	}
	return msg, nil
}
