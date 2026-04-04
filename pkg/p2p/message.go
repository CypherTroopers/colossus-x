package p2p

import "colossusx/pkg/types"

type Message struct {
	Type string `json:"type"`
	Body any    `json:"body,omitempty"`
}

const (
	MessageHello  = "hello"
	MessageStatus = "status"
	MessagePing   = "ping"
	MessagePong   = "pong"
	MessageNewBlk = "newblock"
	MessageSyncRq = "sync_request"
	MessageSyncRs = "sync_response"
)

type HelloMessage struct {
	NodeID  string `json:"node_id"`
	Network string `json:"network"`
	Version string `json:"version"`
	Listen  string `json:"listen"`
}

type StatusMessage struct {
	Status types.PeerStatus `json:"status"`
}

type PingMessage struct {
	Timestamp int64 `json:"timestamp"`
}

type PongMessage struct {
	Timestamp int64 `json:"timestamp"`
}

type NewBlockMessage struct {
	Block types.Block `json:"block"`
}

type SyncRequestMessage struct {
	FromHeight uint64 `json:"from_height"`
	Limit      uint64 `json:"limit"`
}

type SyncResponseMessage struct {
	Blocks []types.Block `json:"blocks"`
}
