package p2p

import "colossusx/pkg/types"

type Message struct {
	Type string `json:"type"`
	Body any    `json:"body,omitempty"`
}

const (
	MessageHello      = "hello"
	MessageStatus     = "status"
	MessagePing       = "ping"
	MessagePong       = "pong"
	MessageNewBlk     = "newblock"
	MessageGetHeaders = "getheaders"
	MessageHeaders    = "headers"
	MessageGetBlocks  = "getblocks"
	MessageBlocks     = "blocks"
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

type GetHeadersMessage struct {
	FromHeight uint64 `json:"from_height"`
	Limit      uint64 `json:"limit"`
}

type HeadersMessage struct {
	Headers []types.BlockHeader `json:"headers"`
}

type GetBlocksMessage struct {
	Hashes []types.Hash `json:"hashes"`
}

type BlocksMessage struct {
	Blocks []types.Block `json:"blocks"`
}
