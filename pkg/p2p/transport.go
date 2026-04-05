package p2p

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

type Handlers struct {
	OnPeerConnected    func(*Peer)
	OnPeerDisconnected func(*Peer)
	OnHello            func(*Peer, HelloMessage)
	OnStatus           func(*Peer, StatusMessage)
	OnPing             func(*Peer, PingMessage)
	OnPong             func(*Peer, PongMessage)
	OnNewBlock         func(*Peer, NewBlockMessage)
	OnSyncRequest      func(*Peer, SyncRequestMessage)
	OnSyncResponse     func(*Peer, SyncResponseMessage)
}

type Config struct {
	NodeID        string
	Network       string
	ListenAddr    string
	AdvertiseAddr string
	Bootnodes     []string
	Version       string
	Handlers      Handlers
}

type Server struct {
	cfg      Config
	listener net.Listener
	peers    *PeerSet
	wg       sync.WaitGroup
}

func NewServer(cfg Config) *Server {
	if cfg.Version == "" {
		cfg.Version = "colossusx/0.1"
	}
	return &Server{cfg: cfg, peers: NewPeerSet()}
}

func (s *Server) Peers() []*Peer { return s.peers.List() }

func (s *Server) Broadcast(msg Message) { s.peers.Broadcast(msg) }

func (s *Server) Start(ctx context.Context) error {
	if s.cfg.ListenAddr != "" {
		ln, err := net.Listen("tcp", s.cfg.ListenAddr)
		if err != nil {
			return err
		}
		s.listener = ln
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.acceptLoop(ctx)
		}()
	}

	for _, bootnode := range s.cfg.Bootnodes {
		bootnode = strings.TrimSpace(bootnode)
		if bootnode == "" {
			continue
		}
		s.wg.Add(1)
		go func(addr string) {
			defer s.wg.Done()
			s.dialLoop(ctx, addr)
		}(bootnode)
	}

	go func() {
		<-ctx.Done()
		if s.listener != nil {
			_ = s.listener.Close()
		}
		for _, peer := range s.Peers() {
			_ = peer.Conn.Close()
		}
	}()

	return nil
}

func (s *Server) acceptLoop(ctx context.Context) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(ctx, conn, true)
		}()
	}
}

func (s *Server) dialLoop(ctx context.Context, addr string) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}

		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			s.handleConn(ctx, conn, false)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Server) handleConn(ctx context.Context, conn net.Conn, inbound bool) {
	peer := &Peer{
		Addr:        conn.RemoteAddr().String(),
		Conn:        conn,
		Inbound:     inbound,
		ConnectedAt: time.Now(),
		LastSeen:    time.Now(),
	}

	defer func() {
		s.peers.Remove(peer)
		_ = conn.Close()
		if s.cfg.Handlers.OnPeerDisconnected != nil {
			s.cfg.Handlers.OnPeerDisconnected(peer)
		}
	}()

	if err := peer.Send(Message{
		Type: MessageHello,
		Body: HelloMessage{
			NodeID:  s.cfg.NodeID,
			Network: s.cfg.Network,
			Version: s.cfg.Version,
			Listen:  s.cfg.AdvertiseAddr,
		},
	}); err != nil {
		return
	}

	for {
		_ = conn.SetReadDeadline(time.Now().Add(defaultReadTimeout))

		msg, err := readMessage(conn)
		if err != nil {
			return
		}
		peer.LastSeen = time.Now()

		if peer.ID == "" && msg.Type != MessageHello {
			return
		}

		if err := s.dispatch(peer, msg); err != nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
		}
	}
}

func (s *Server) dispatch(peer *Peer, msg Message) error {
	payload, err := json.Marshal(msg.Body)
	if err != nil {
		return err
	}

	switch msg.Type {
	case MessageHello:
		if peer.ID != "" {
			return fmt.Errorf("duplicate hello from %q", peer.ID)
		}

		var body HelloMessage
		if err := json.Unmarshal(payload, &body); err != nil {
			return err
		}

		if strings.TrimSpace(body.NodeID) == "" {
			return fmt.Errorf("peer hello missing node id")
		}
		if strings.TrimSpace(body.Network) == "" {
			return fmt.Errorf("peer hello missing network")
		}
		if body.Network != s.cfg.Network {
			return fmt.Errorf("peer network mismatch peer=%s local=%s", body.Network, s.cfg.Network)
		}
		if body.NodeID == s.cfg.NodeID {
			return fmt.Errorf("self connection rejected")
		}

		peer.ID = body.NodeID
		peer.Hello = body

		if err := s.peers.AddIfNoPeerID(peer); err != nil {
			peer.ID = ""
			peer.Hello = HelloMessage{}
			return err
		}

		if s.cfg.Handlers.OnPeerConnected != nil {
			s.cfg.Handlers.OnPeerConnected(peer)
		}
		if s.cfg.Handlers.OnHello != nil {
			s.cfg.Handlers.OnHello(peer, body)
		}
		return nil

	case MessageStatus:
		var body StatusMessage
		if err := json.Unmarshal(payload, &body); err != nil {
			return err
		}
		peer.Status = body.Status
		if s.cfg.Handlers.OnStatus != nil {
			s.cfg.Handlers.OnStatus(peer, body)
		}
		return nil

	case MessagePing:
		var body PingMessage
		if err := json.Unmarshal(payload, &body); err != nil {
			return err
		}
		if s.cfg.Handlers.OnPing != nil {
			s.cfg.Handlers.OnPing(peer, body)
		}
		return peer.Send(Message{
			Type: MessagePong,
			Body: PongMessage{Timestamp: time.Now().Unix()},
		})

	case MessagePong:
		var body PongMessage
		if err := json.Unmarshal(payload, &body); err != nil {
			return err
		}
		if s.cfg.Handlers.OnPong != nil {
			s.cfg.Handlers.OnPong(peer, body)
		}
		return nil

	case MessageNewBlk:
		var body NewBlockMessage
		if err := json.Unmarshal(payload, &body); err != nil {
			return err
		}
		if s.cfg.Handlers.OnNewBlock != nil {
			s.cfg.Handlers.OnNewBlock(peer, body)
		}
		return nil

	case MessageSyncRq:
		var body SyncRequestMessage
		if err := json.Unmarshal(payload, &body); err != nil {
			return err
		}
		if s.cfg.Handlers.OnSyncRequest != nil {
			s.cfg.Handlers.OnSyncRequest(peer, body)
		}
		return nil

	case MessageSyncRs:
		var body SyncResponseMessage
		if err := json.Unmarshal(payload, &body); err != nil {
			return err
		}
		if s.cfg.Handlers.OnSyncResponse != nil {
			s.cfg.Handlers.OnSyncResponse(peer, body)
		}
		return nil

	default:
		return fmt.Errorf("unknown message type %q", msg.Type)
	}
}
