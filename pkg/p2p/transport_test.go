package p2p

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestHandleConnRejectsNonHelloFirstMessage(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	disconnected := make(chan struct{}, 1)
	connected := make(chan struct{}, 1)

	s := NewServer(Config{
		NodeID:  "server",
		Network: "devnet",
		Handlers: Handlers{
			OnPeerConnected: func(*Peer) {
				connected <- struct{}{}
			},
			OnPeerDisconnected: func(*Peer) {
				disconnected <- struct{}{}
			},
		},
	})

	go s.handleConn(ctx, serverConn, true)

	msg, err := readMessage(clientConn)
	if err != nil {
		t.Fatalf("read server hello: %v", err)
	}
	if msg.Type != MessageHello {
		t.Fatalf("first outbound message = %q, want %q", msg.Type, MessageHello)
	}

	if err := writeTestMessage(clientConn, Message{
		Type: MessagePing,
		Body: PingMessage{Timestamp: 1},
	}); err != nil {
		t.Fatalf("write ping before hello: %v", err)
	}

	select {
	case <-disconnected:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for disconnect")
	}

	select {
	case <-connected:
		t.Fatal("OnPeerConnected fired before hello completed")
	default:
	}

	if got := len(s.Peers()); got != 0 {
		t.Fatalf("registered peers = %d, want 0", got)
	}
}

func TestHandleConnRegistersPeerOnlyAfterHello(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()

	var mu sync.Mutex
	events := make([]string, 0, 2)
	connected := make(chan struct{}, 1)
	helloSeen := make(chan struct{}, 1)

	s := NewServer(Config{
		NodeID:  "server",
		Network: "devnet",
		Handlers: Handlers{
			OnPeerConnected: func(peer *Peer) {
				mu.Lock()
				events = append(events, "connected")
				mu.Unlock()
				connected <- struct{}{}
			},
			OnHello: func(peer *Peer, msg HelloMessage) {
				mu.Lock()
				events = append(events, "hello")
				mu.Unlock()
				helloSeen <- struct{}{}
			},
		},
	})

	go s.handleConn(ctx, serverConn, true)

	msg, err := readMessage(clientConn)
	if err != nil {
		t.Fatalf("read server hello: %v", err)
	}
	if msg.Type != MessageHello {
		t.Fatalf("first outbound message = %q, want %q", msg.Type, MessageHello)
	}

	select {
	case <-connected:
		t.Fatal("OnPeerConnected fired before remote hello")
	default:
	}

	if got := len(s.Peers()); got != 0 {
		t.Fatalf("registered peers before hello = %d, want 0", got)
	}

	if err := writeTestMessage(clientConn, Message{
		Type: MessageHello,
		Body: HelloMessage{
			NodeID:  "client",
			Network: "devnet",
			Version: "colossusx/0.1",
			Listen:  ":30333",
		},
	}); err != nil {
		t.Fatalf("write remote hello: %v", err)
	}

	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for OnPeerConnected")
	}

	select {
	case <-helloSeen:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for OnHello")
	}

	if got := len(s.Peers()); got != 1 {
		t.Fatalf("registered peers after hello = %d, want 1", got)
	}
	if got := s.Peers()[0].ID; got != "client" {
		t.Fatalf("registered peer id = %q, want %q", got, "client")
	}

	mu.Lock()
	gotEvents := append([]string(nil), events...)
	mu.Unlock()

	wantEvents := []string{"connected", "hello"}
	if !reflect.DeepEqual(gotEvents, wantEvents) {
		t.Fatalf("events = %v, want %v", gotEvents, wantEvents)
	}
}

func writeTestMessage(conn net.Conn, msg Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	frame := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(frame[:4], uint32(len(body)))
	copy(frame[4:], body)

	_, err = conn.Write(frame)
	return err
}
