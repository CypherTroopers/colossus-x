package node

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"math/big"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cx "colossusx/colossusx"
	"colossusx/pkg/chain"
	"colossusx/pkg/consensus"
	"colossusx/pkg/p2p"
	"colossusx/pkg/types"
)

type Config struct {
	Chain              types.ChainConfig
	Genesis            types.GenesisConfig
	Role               string
	Mine               bool
	MaxNonces          uint64
	BlockTime          time.Duration
	BlockReward        uint64
	Logf               func(string, ...any)
	NodeID             string
	ListenAddr         string
	Bootnodes          []string
	FixedValidatorSet  []string
	MinerBackend       string
	MinerDAGAlloc      string
	ResolvedDAGAlloc   string
	RuntimeInitStatus  string
	MinerExecutionPath string
}

type Node struct {
	cfg        Config
	store      chain.Store
	validator  *consensus.Validator
	p2p        *p2p.Server
	mu         sync.RWMutex
	validators map[string]struct{}
	rewardsMu  sync.Mutex
	rewards    map[string]uint64

	syncing   atomic.Bool
	syncMu    sync.Mutex
	headerSub map[string]chan p2p.HeadersMessage
	blockSub  map[string]chan p2p.BlocksMessage
}

const (
	RoleHybrid    = "hybrid"
	RoleMiner     = "miner"
	RoleValidator = "validator"
)

func New(cfg Config, validator *consensus.Validator, store chain.Store) (*Node, error) {
	if store == nil {
		store = chain.NewMemoryStore()
	}
	if validator == nil {
		return nil, fmt.Errorf("validator is required")
	}
	if cfg.BlockTime <= 0 {
		cfg.BlockTime = time.Second
	}
	if cfg.Logf == nil {
		cfg.Logf = log.Printf
	}
	if cfg.NodeID == "" {
		cfg.NodeID = fmt.Sprintf("node-%d", time.Now().UnixNano())
	}
	if cfg.Role == "" {
		cfg.Role = RoleHybrid
	}
	n := &Node{
		cfg:        cfg,
		validator:  validator,
		store:      store,
		headerSub:  make(map[string]chan p2p.HeadersMessage),
		blockSub:   make(map[string]chan p2p.BlocksMessage),
		validators: make(map[string]struct{}),
		rewards:    make(map[string]uint64),
	}
	for _, id := range cfg.FixedValidatorSet {
		n.validators[id] = struct{}{}
	}
	cfg.Logf("node mining configured backend=%s dag_alloc=%s resolved_alloc=%s runtime_init=%s execution=%s", cfg.MinerBackend, cfg.MinerDAGAlloc, cfg.ResolvedDAGAlloc, cfg.RuntimeInitStatus, cfg.MinerExecutionPath)
	n.p2p = p2p.NewServer(p2p.Config{
		NodeID:        cfg.NodeID,
		Network:       cfg.Chain.NetworkID,
		Role:          cfg.Role,
		ListenAddr:    cfg.ListenAddr,
		AdvertiseAddr: cfg.ListenAddr,
		Bootnodes:     cfg.Bootnodes,
		Handlers: p2p.Handlers{
			OnPeerConnected:    n.onPeerConnected,
			OnPeerDisconnected: n.onPeerDisconnected,
			OnHello:            n.onHello,
			OnStatus:           n.onStatus,
			OnPing:             n.onPing,
			OnPong:             n.onPong,
			OnNewBlock:         n.onNewBlock,
			OnPoWSubmit:        n.onPoWSubmit,
			OnReward:           n.onReward,
			OnGetHeaders:       n.onGetHeaders,
			OnHeaders:          n.onHeaders,
			OnGetBlocks:        n.onGetBlocks,
			OnBlocks:           n.onBlocks,
		},
	})
	return n, nil
}

func (n *Node) Store() chain.Store { return n.store }

func (n *Node) InitGenesis() (types.Block, error) {
	if tip, _, err := n.store.CurrentTip(); err == nil {
		return tip, nil
	}
	genesis := types.NewGenesisBlock(n.cfg.Genesis)
	sealed, res, err := n.validator.SealBlock(genesis, n.cfg.MaxNonces)
	if err != nil {
		return types.Block{}, err
	}
	work := consensus.CalcBlockWork(sealed.Header.Target)
	if err := n.store.StoreBlock(sealed, work); err != nil {
		return types.Block{}, err
	}
	if err := n.store.SetCurrentTip(sealed.BlockHash()); err != nil {
		return types.Block{}, err
	}
	n.cfg.Logf("genesis initialized hash=%s nonce=%d hashes=%d", sealed.BlockHash().String(), sealed.Header.Nonce, res.Hashes)
	return sealed, nil
}

func (n *Node) Run(ctx context.Context) error {
	if _, err := n.InitGenesis(); err != nil {
		return err
	}
	if err := n.p2p.Start(ctx); err != nil {
		return err
	}
	if !n.cfg.Mine {
		<-ctx.Done()
		return ctx.Err()
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		block, res, err := n.mineNextBlock()
		if err != nil {
			return err
		}
		if n.cfg.Role == RoleMiner {
			n.submitPoW(block)
			n.cfg.Logf("submitted pow height=%d hash=%s nonce=%d hashes=%d hashrate=%.2fH/s", block.Header.Height, block.BlockHash().String(), block.Header.Nonce, res.Hashes, res.HashRate)
			timer := time.NewTimer(n.cfg.BlockTime)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		_, becameTip, err := n.validator.InsertBlock(n.store, block)
		if err != nil {
			return err
		}
		n.cfg.Logf("mined block height=%d hash=%s nonce=%d hashes=%d hashrate=%.2fH/s became_tip=%t", block.Header.Height, block.BlockHash().String(), block.Header.Nonce, res.Hashes, res.HashRate, becameTip)
		if becameTip {
			n.broadcastNewBlock(block)
			go n.broadcastStatus()
		}
		timer := time.NewTimer(n.cfg.BlockTime)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (n *Node) mineNextBlock() (types.Block, cx.MineResult, error) {
	tip, _, err := n.store.CurrentTip()
	if err != nil {
		return types.Block{}, cx.MineResult{}, err
	}
	nextHeight := tip.Header.Height + 1
	resolvedSpec := n.cfg.Chain.Spec.ResolvedForHeight(nextHeight)
	header := types.BlockHeader{
		Version:          1,
		AlgorithmVersion: resolvedSpec.AlgorithmVersion,
		Height:           nextHeight,
		ParentHash:       tip.BlockHash(),
		Timestamp:        max(time.Now().Unix(), tip.Header.Timestamp+1),
		Target:           n.cfg.Genesis.Bits,
		EpochSeed:        types.EpochSeedForHeight(resolvedSpec, nextHeight),
		DAGSizeBytes:     resolvedSpec.DAGSizeBytes,
		TxRoot:           sha256.Sum256([]byte(fmt.Sprintf("height:%d", nextHeight))),
		StateRoot:        sha256.Sum256([]byte(tip.BlockHash().String())),
	}
	block := types.Block{Header: header}
	if n.cfg.BlockReward > 0 {
		block.Transactions = []string{fmt.Sprintf("coinbase:%s:%d", n.cfg.NodeID, n.cfg.BlockReward)}
	}
	sealed, res, err := n.validator.SealBlock(block, n.cfg.MaxNonces)
	if err != nil {
		return types.Block{}, cx.MineResult{}, err
	}
	return sealed, res, nil
}

func (n *Node) onPeerConnected(peer *p2p.Peer) {
	n.cfg.Logf("peer connected addr=%s inbound=%t", peer.Addr, peer.Inbound)
	go n.sendStatus(peer)
	go func() {
		_ = peer.Send(p2p.Message{Type: p2p.MessagePing, Body: p2p.PingMessage{Timestamp: time.Now().Unix()}})
	}()
}

func (n *Node) onPeerDisconnected(peer *p2p.Peer) {
	n.cfg.Logf("peer disconnected addr=%s id=%s", peer.Addr, peer.ID)
}

func (n *Node) onHello(peer *p2p.Peer, msg p2p.HelloMessage) {
	if msg.Network != n.cfg.Chain.NetworkID {
		n.cfg.Logf("peer network mismatch addr=%s peer_network=%s local_network=%s", peer.Addr, msg.Network, n.cfg.Chain.NetworkID)
		_ = peer.Conn.Close()
		return
	}
	if msg.Role != RoleMiner && !n.isAllowedValidator(msg.NodeID) {
		n.cfg.Logf("peer validator mismatch addr=%s peer_id=%s", peer.Addr, msg.NodeID)
		_ = peer.Conn.Close()
		return
	}
	n.cfg.Logf("hello received peer=%s addr=%s version=%s listen=%s", msg.NodeID, peer.Addr, msg.Version, msg.Listen)
	go n.sendStatus(peer)
}

func (n *Node) isAllowedValidator(nodeID string) bool {
	if len(n.validators) == 0 {
		return true
	}
	_, ok := n.validators[nodeID]
	return ok
}

func (n *Node) onStatus(peer *p2p.Peer, msg p2p.StatusMessage) {
	n.cfg.Logf("status received peer=%s height=%d hash=%s total_work=%s", msg.Status.PeerID, msg.Status.BestHeight, msg.Status.BestHash.String(), msg.Status.TotalWork)
	n.maybeStartSync(peer, msg.Status)
}

func (n *Node) onPing(peer *p2p.Peer, msg p2p.PingMessage) {
	n.cfg.Logf("ping received peer=%s ts=%d", peer.ID, msg.Timestamp)
}

func (n *Node) onPong(peer *p2p.Peer, msg p2p.PongMessage) {
	n.cfg.Logf("pong received peer=%s ts=%d", peer.ID, msg.Timestamp)
}

func (n *Node) onNewBlock(peer *p2p.Peer, msg p2p.NewBlockMessage) {
	if msg.Block.BlockHash() == (types.Hash{}) {
		return
	}
	if n.store.HasBlock(msg.Block.BlockHash()) {
		return
	}
	_, becameTip, err := n.validator.InsertBlock(n.store, msg.Block)
	if err != nil {
		n.cfg.Logf("newblock rejected peer=%s err=%v", peer.ID, err)
		return
	}
	n.cfg.Logf("newblock accepted peer=%s height=%d hash=%s became_tip=%t", peer.ID, msg.Block.Header.Height, msg.Block.BlockHash().String(), becameTip)
	if becameTip {
		go n.broadcastStatus()
	}
}

func (n *Node) onPoWSubmit(peer *p2p.Peer, msg p2p.PoWSubmitMessage) {
	if n.cfg.Role == RoleMiner {
		return
	}
	if msg.MinerID == "" || msg.Block.BlockHash() == (types.Hash{}) {
		return
	}
	_, becameTip, err := n.validator.InsertBlock(n.store, msg.Block)
	if err != nil {
		n.cfg.Logf("pow submit rejected miner=%s err=%v", msg.MinerID, err)
		return
	}
	n.cfg.Logf("pow submit accepted miner=%s height=%d hash=%s became_tip=%t", msg.MinerID, msg.Block.Header.Height, msg.Block.BlockHash().String(), becameTip)
	if n.cfg.BlockReward > 0 {
		n.creditReward(msg.MinerID, n.cfg.BlockReward)
		_ = peer.Send(p2p.Message{
			Type: p2p.MessageReward,
			Body: p2p.RewardMessage{
				ValidatorID: n.cfg.NodeID,
				MinerID:     msg.MinerID,
				Amount:      n.cfg.BlockReward,
				Height:      msg.Block.Header.Height,
				BlockHash:   msg.Block.BlockHash().String(),
			},
		})
	}
	if becameTip {
		n.broadcastNewBlock(msg.Block)
		go n.broadcastStatus()
	}
}

func (n *Node) onReward(peer *p2p.Peer, msg p2p.RewardMessage) {
	if msg.MinerID != n.cfg.NodeID || msg.Amount == 0 {
		return
	}
	n.creditReward(msg.MinerID, msg.Amount)
	n.cfg.Logf("reward received validator=%s amount=%d height=%d block=%s", msg.ValidatorID, msg.Amount, msg.Height, msg.BlockHash)
}

func (n *Node) submitPoW(block types.Block) {
	peers := n.p2p.Peers()
	for _, peer := range peers {
		if peer == nil {
			continue
		}
		_ = peer.Send(p2p.Message{
			Type: p2p.MessagePoWSubmit,
			Body: p2p.PoWSubmitMessage{
				MinerID: n.cfg.NodeID,
				Block:   block,
			},
		})
	}
}

func (n *Node) creditReward(minerID string, amount uint64) {
	n.rewardsMu.Lock()
	defer n.rewardsMu.Unlock()
	n.rewards[minerID] += amount
}

func (n *Node) onGetHeaders(peer *p2p.Peer, msg p2p.GetHeadersMessage) {
	limit := msg.Limit
	if limit == 0 {
		limit = 1
	}
	if limit > 256 {
		limit = 256
	}
	headers := make([]types.BlockHeader, 0, limit)
	for i := uint64(0); i < limit; i++ {
		block, err := n.store.GetBlockByHeight(msg.FromHeight + i)
		if err != nil {
			break
		}
		headers = append(headers, block.Header)
	}
	_ = peer.Send(p2p.Message{Type: p2p.MessageHeaders, Body: p2p.HeadersMessage{Headers: headers}})
}

func (n *Node) onHeaders(peer *p2p.Peer, msg p2p.HeadersMessage) {
	key := peerSyncKey(peer)
	n.syncMu.Lock()
	ch := n.headerSub[key]
	n.syncMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}

func (n *Node) onGetBlocks(peer *p2p.Peer, msg p2p.GetBlocksMessage) {
	if len(msg.Hashes) == 0 {
		_ = peer.Send(p2p.Message{Type: p2p.MessageBlocks, Body: p2p.BlocksMessage{Blocks: nil}})
		return
	}
	const maxBlocksPerResponse = 256
	blocks := make([]types.Block, 0, minInt(len(msg.Hashes), maxBlocksPerResponse))
	for i, hash := range msg.Hashes {
		if i >= maxBlocksPerResponse {
			break
		}
		block, err := n.store.GetBlock(hash)
		if err != nil {
			continue
		}
		blocks = append(blocks, block)
	}
	_ = peer.Send(p2p.Message{Type: p2p.MessageBlocks, Body: p2p.BlocksMessage{Blocks: blocks}})
}

func (n *Node) onBlocks(peer *p2p.Peer, msg p2p.BlocksMessage) {
	key := peerSyncKey(peer)
	n.syncMu.Lock()
	ch := n.blockSub[key]
	n.syncMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}

func (n *Node) sendStatus(peer *p2p.Peer) {
	status, err := n.localStatus()
	if err != nil {
		n.cfg.Logf("status build failed: %v", err)
		return
	}
	if err := peer.Send(p2p.Message{Type: p2p.MessageStatus, Body: p2p.StatusMessage{Status: status}}); err != nil {
		n.cfg.Logf("status send failed peer=%s err=%v", peer.Addr, err)
	}
}

func (n *Node) broadcastStatus() {
	status, err := n.localStatus()
	if err != nil {
		n.cfg.Logf("status build failed: %v", err)
		return
	}
	n.p2p.Broadcast(p2p.Message{Type: p2p.MessageStatus, Body: p2p.StatusMessage{Status: status}})
}

func (n *Node) broadcastNewBlock(block types.Block) {
	n.p2p.Broadcast(p2p.Message{Type: p2p.MessageNewBlk, Body: p2p.NewBlockMessage{Block: block}})
}

func (n *Node) maybeStartSync(peer *p2p.Peer, remote types.PeerStatus) {
	local, _, err := n.store.CurrentTip()
	if err != nil {
		return
	}
	localHeight := local.Header.Height
	if remote.BestHeight <= localHeight {
		return
	}
	if !n.syncing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer n.syncing.Store(false)
		if err := n.syncRange(peer, localHeight+1, remote.BestHeight); err != nil {
			n.cfg.Logf("sync failed peer=%s from=%d to=%d err=%v", peer.ID, localHeight+1, remote.BestHeight, err)
			return
		}
		n.cfg.Logf("sync completed peer=%s from=%d to=%d", peer.ID, localHeight+1, remote.BestHeight)
	}()
}

func (n *Node) syncRange(peer *p2p.Peer, fromHeight, toHeight uint64) error {
	if fromHeight > toHeight {
		return nil
	}
	key := peerSyncKey(peer)
	headerCh := make(chan p2p.HeadersMessage, 2)
	blockCh := make(chan p2p.BlocksMessage, 2)
	n.syncMu.Lock()
	n.headerSub[key] = headerCh
	n.blockSub[key] = blockCh
	n.syncMu.Unlock()
	defer func() {
		n.syncMu.Lock()
		delete(n.headerSub, key)
		delete(n.blockSub, key)
		n.syncMu.Unlock()
	}()

	const headerBatch = uint64(128)
	current := fromHeight
	for current <= toHeight {
		limit := headerBatch
		remaining := (toHeight - current) + 1
		if remaining < limit {
			limit = remaining
		}
		if err := peer.Send(p2p.Message{
			Type: p2p.MessageGetHeaders,
			Body: p2p.GetHeadersMessage{FromHeight: current, Limit: limit},
		}); err != nil {
			return err
		}

		var headersMsg p2p.HeadersMessage
		select {
		case headersMsg = <-headerCh:
		case <-time.After(5 * time.Second):
			return fmt.Errorf("headers timeout at height %d", current)
		}
		if len(headersMsg.Headers) == 0 {
			return fmt.Errorf("empty headers response at height %d", current)
		}
		needed := make([]types.Hash, 0, len(headersMsg.Headers))
		for i, header := range headersMsg.Headers {
			expectedHeight := current + uint64(i)
			if header.Height != expectedHeight {
				return fmt.Errorf("unexpected header height: got %d want %d", header.Height, expectedHeight)
			}
			hash := header.HeaderHash()
			if n.store.HasBlock(hash) {
				continue
			}
			needed = append(needed, hash)
		}
		if len(needed) > 0 {
			if err := peer.Send(p2p.Message{
				Type: p2p.MessageGetBlocks,
				Body: p2p.GetBlocksMessage{Hashes: needed},
			}); err != nil {
				return err
			}
			var blocksMsg p2p.BlocksMessage
			select {
			case blocksMsg = <-blockCh:
			case <-time.After(5 * time.Second):
				return fmt.Errorf("blocks timeout at height %d", current)
			}
			if err := n.insertBlocksInRequestedOrder(needed, blocksMsg.Blocks); err != nil {
				return err
			}
		}
		last := headersMsg.Headers[len(headersMsg.Headers)-1]
		current = last.Height + 1
	}
	return nil
}

func (n *Node) insertBlocksInRequestedOrder(needed []types.Hash, blocks []types.Block) error {
	byHash := make(map[types.Hash]types.Block, len(blocks))
	for _, block := range blocks {
		byHash[block.BlockHash()] = block
	}
	for _, hash := range needed {
		block, ok := byHash[hash]
		if !ok {
			return fmt.Errorf("missing requested block %s", hash.String())
		}
		if n.store.HasBlock(hash) {
			continue
		}
		if _, _, err := n.validator.InsertBlock(n.store, block); err != nil {
			return err
		}
	}
	return nil
}

func (n *Node) localStatus() (types.PeerStatus, error) {
	tip, work, err := n.store.CurrentTip()
	if err != nil {
		return types.PeerStatus{}, err
	}
	return types.PeerStatus{
		PeerID:      n.cfg.NodeID,
		BestHash:    tip.BlockHash(),
		BestHeight:  tip.Header.Height,
		TotalWork:   work.String(),
		ConnectedAt: time.Now().Unix(),
	}, nil
}

func ParseBootnodes(raw string) []string {
	return ParseNodeIDs(raw)
}

func ParseNodeIDs(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func FormatWork(work *big.Int) string {
	if work == nil {
		return "0"
	}
	return work.String()
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func peerSyncKey(peer *p2p.Peer) string {
	if peer == nil {
		return ""
	}
	if peer.ID != "" {
		return peer.ID
	}
	return peer.Addr
}
