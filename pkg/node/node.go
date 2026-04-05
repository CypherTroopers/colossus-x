package node

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"strings"
	"sync"
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
	Mine               bool
	MaxNonces          uint64
	BlockTime          time.Duration
	Logf               func(string, ...any)
	NodeID             string
	ListenAddr         string
	Bootnodes          []string
	NodeIDAlias        string
	MinerBackend       string
	MinerDAGAlloc      string
	ResolvedDAGAlloc   string
	RuntimeInitStatus  string
	MinerExecutionPath string
	HTTPAddr           string
	Coinbase           string
	MaxTxsPerBlock     int
}

type Node struct {
	cfg       Config
	store     chain.Store
	validator *consensus.Validator
	p2p       *p2p.Server
	mu        sync.RWMutex
	prewarmMu sync.Mutex
	prewarmed map[uint64]struct{}

	mempoolMu sync.Mutex
	mempool   []types.Transaction
}

const syncBatchLimit uint64 = 128
const daemonEpochPrewarmLeadBlocks uint64 = 128

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
	if cfg.Coinbase == "" {
		cfg.Coinbase = cfg.NodeID
	}
	if cfg.MaxTxsPerBlock <= 0 {
		cfg.MaxTxsPerBlock = 256
	}
	cfg.Chain.Economics = cfg.Chain.Economics.Normalized()

	n := &Node{
		cfg:       cfg,
		validator: validator,
		store:     store,
		prewarmed: make(map[uint64]struct{}),
	}

	cfg.Logf(
		"node mining configured backend=%s dag_alloc=%s resolved_alloc=%s runtime_init=%s execution=%s",
		cfg.MinerBackend,
		cfg.MinerDAGAlloc,
		cfg.ResolvedDAGAlloc,
		cfg.RuntimeInitStatus,
		cfg.MinerExecutionPath,
	)

	n.p2p = p2p.NewServer(p2p.Config{
		NodeID:        cfg.NodeID,
		Network:       cfg.Chain.NetworkID,
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
			OnSyncRequest:      n.onSyncRequest,
			OnSyncResponse:     n.onSyncResponse,
		},
	})

	return n, nil
}

func (n *Node) Store() chain.Store { return n.store }

func (n *Node) InitGenesis() (types.Block, error) {
	if tip, _, err := n.store.CurrentTip(); err == nil {
		return tip, nil
	}

	n.cfg.Genesis.Economics = n.cfg.Chain.Economics
	genesis := types.NewGenesisBlock(n.cfg.Genesis)
	work := consensus.CalcBlockWork(genesis.Header.Target)

	if err := n.store.StoreBlock(genesis, work); err != nil {
		return types.Block{}, err
	}
	if err := n.store.SetCurrentTip(genesis.BlockHash()); err != nil {
		return types.Block{}, err
	}

	n.cfg.Logf("genesis initialized (deterministic) hash=%s nonce=%d", genesis.BlockHash().String(), genesis.Header.Nonce)
	return genesis, nil
}

func (n *Node) Run(ctx context.Context) error {
	if _, err := n.InitGenesis(); err != nil {
		return err
	}
	if n.cfg.HTTPAddr != "" {
		if err := n.startHTTP(ctx); err != nil {
			return err
		}
	}
	if !n.cfg.Mine {
		if err := n.ensureStartupDAGReady(); err != nil {
			return err
		}
	}
	if err := n.p2p.Start(ctx); err != nil {
		return err
	}
	if err := n.waitForInitialSync(ctx); err != nil {
		return err
	}
	if n.cfg.Mine {
		if err := n.ensureStartupDAGReady(); err != nil {
			return err
		}
	}
	n.scheduleStartupPrewarm()

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

		_, becameTip, err := n.validator.InsertBlock(n.store, block)
		if err != nil {
			return err
		}
		n.pruneMempool(block.Transactions)

		n.cfg.Logf(
			"mined block height=%d hash=%s nonce=%d hashes=%d hashrate=%.2fH/s became_tip=%t",
			block.Header.Height,
			block.BlockHash().String(),
			block.Header.Nonce,
			res.Hashes,
			res.HashRate,
			becameTip,
		)

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

func (n *Node) waitForInitialSync(ctx context.Context) error {
	if !n.cfg.Mine {
		return nil
	}

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		tip, work, err := n.store.CurrentTip()
		if err != nil {
			return fmt.Errorf("initial sync tip lookup failed: %w", err)
		}

		ready, remoteBest, peersWithStatus := initialSyncReady(tip.Header.Height, work, n.p2p.Peers())
		if ready {
			n.cfg.Logf("initial sync complete local_height=%d remote_best=%d peers_with_status=%d", tip.Header.Height, remoteBest, peersWithStatus)
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func initialSyncReady(localTip uint64, localWork *big.Int, peers []*p2p.Peer) (ready bool, remoteBest uint64, peersWithStatus int) {
	if len(peers) == 0 {
		return true, localTip, 0
	}

	remoteBest = localTip
	bestRemoteWork := new(big.Int)
	if localWork != nil {
		bestRemoteWork.Set(localWork)
	}

	for _, peer := range peers {
		if peer.Status.PeerID == "" {
			continue
		}
		peersWithStatus++
		if peer.Status.BestHeight > remoteBest {
			remoteBest = peer.Status.BestHeight
		}
		remoteWork := consensus.ParseWorkString(peer.Status.TotalWork)
		if remoteWork.Cmp(bestRemoteWork) > 0 {
			bestRemoteWork = remoteWork
		}
	}

	if peersWithStatus == 0 {
		return false, remoteBest, 0
	}
	if localWork == nil {
		return localTip >= remoteBest, remoteBest, peersWithStatus
	}
	return localTip >= remoteBest && localWork.Cmp(bestRemoteWork) >= 0, remoteBest, peersWithStatus
}

func (n *Node) ensureStartupDAGReady() error {
	if n.validator.LightValidationEnabled() {
		return nil
	}

	tip, _, err := n.store.CurrentTip()
	if err != nil {
		return fmt.Errorf("startup dag prepare tip lookup failed: %w", err)
	}

	nextHeight := tip.Header.Height + 1
	if err := n.validator.PrewarmMiningDAGAtHeight(nextHeight); err != nil {
		return fmt.Errorf("startup dag prepare failed height=%d: %w", nextHeight, err)
	}

	n.cfg.Logf("startup dag ready height=%d", nextHeight)
	return nil
}

func (n *Node) mineNextBlock() (types.Block, cx.MineResult, error) {
	tip, _, err := n.store.CurrentTip()
	if err != nil {
		return types.Block{}, cx.MineResult{}, err
	}

	nextHeight := tip.Header.Height + 1
	resolved := n.cfg.Chain.Spec.ResolvedForHeight(nextHeight)
	target, err := consensus.ExpectedTargetForChild(n.store, n.cfg.Chain, tip)
	if err != nil {
		return types.Block{}, cx.MineResult{}, err
	}

	txs := n.mempoolSnapshot()
	if len(txs) > n.cfg.MaxTxsPerBlock {
		txs = txs[:n.cfg.MaxTxsPerBlock]
	}
	state, txs := n.applyTransactionsForTemplate(tip.State, txs)

	header := types.BlockHeader{
		Version:          1,
		AlgorithmVersion: resolved.AlgorithmVersion,
		Height:           nextHeight,
		ParentHash:       tip.BlockHash(),
		Timestamp:        max(time.Now().Unix(), tip.Header.Timestamp+1),
		Target:           target,
		Coinbase:         n.cfg.Coinbase,
		EpochSeed:        types.EpochSeedForHeight(resolved, nextHeight),
		DAGSizeBytes:     resolved.DAGSizeBytes,
		TxRoot:           types.ComputeTxRoot(txs),
		StateRoot:        types.ComputeStateRoot(state),
	}

	n.scheduleNextEpochPrewarm(nextHeight)
	return n.validator.SealBlock(types.Block{
		Header:       header,
		Transactions: txs,
		State:        state,
	}, n.cfg.MaxNonces)
}

func (n *Node) applyTransactionsForTemplate(parent map[string]types.AccountState, txs []types.Transaction) (map[string]types.AccountState, []types.Transaction) {
	state := types.CloneState(parent)
	accepted := make([]types.Transaction, 0, len(txs))

	for _, tx := range txs {
		next, err := types.ApplyTransactions(state, []types.Transaction{tx}, "", 0)
		if err != nil {
			continue
		}
		state = next
		accepted = append(accepted, tx)
	}

	state, _ = types.ApplyTransactions(state, nil, n.cfg.Coinbase, n.cfg.Chain.Economics.BlockReward)
	return state, accepted
}

func nextEpochStartHeight(fromHeight uint64, epochBlocks uint64) (uint64, bool) {
	if epochBlocks == 0 {
		return 0, false
	}
	return ((fromHeight / epochBlocks) + 1) * epochBlocks, true
}

func epochStartHeight(height uint64, epochBlocks uint64) uint64 {
	if epochBlocks == 0 {
		return height
	}
	return (height / epochBlocks) * epochBlocks
}

func (n *Node) scheduleStartupPrewarm() {
	if n.cfg.Mine || n.validator.LightValidationEnabled() {
		return
	}

	tip, _, err := n.store.CurrentTip()
	if err != nil {
		n.cfg.Logf("dag prewarm skipped: tip lookup failed: %v", err)
		return
	}

	nextHeight := tip.Header.Height + 1
	n.scheduleEpochPrewarm(nextHeight)
	n.scheduleNextEpochPrewarm(nextHeight)
}

func (n *Node) scheduleEpochPrewarm(height uint64) {
	key := epochStartHeight(height, n.cfg.Chain.Spec.EpochBlocks)

	n.prewarmMu.Lock()
	if _, ok := n.prewarmed[key]; ok {
		n.prewarmMu.Unlock()
		return
	}
	n.prewarmed[key] = struct{}{}
	n.prewarmMu.Unlock()

	go func(height uint64, key uint64) {
		if err := n.validator.PrewarmMiningDAGAtHeight(height); err != nil {
			n.cfg.Logf("dag prewarm failed height=%d err=%v", height, err)
			n.prewarmMu.Lock()
			delete(n.prewarmed, key)
			n.prewarmMu.Unlock()
			return
		}
		n.cfg.Logf("dag prewarm ready height=%d", height)
	}(height, key)
}

func (n *Node) scheduleNextEpochPrewarm(fromHeight uint64) {
	if nextEpoch, ok := shouldPrewarmNextEpoch(fromHeight, n.cfg.Chain.Spec.EpochBlocks, daemonEpochPrewarmLeadBlocks); ok {
		n.scheduleEpochPrewarm(nextEpoch)
	}
}

func shouldPrewarmNextEpoch(fromHeight uint64, epochBlocks uint64, leadBlocks uint64) (uint64, bool) {
	nextEpoch, ok := nextEpochStartHeight(fromHeight, epochBlocks)
	if !ok || leadBlocks == 0 {
		return 0, false
	}
	if epochBlocks > 1 && leadBlocks >= epochBlocks {
		leadBlocks = epochBlocks - 1
	}
	if nextEpoch-fromHeight > leadBlocks {
		return 0, false
	}
	return nextEpoch, true
}

func (n *Node) onPeerConnected(peer *p2p.Peer) {
	n.cfg.Logf("peer connected addr=%s inbound=%t", peer.Addr, peer.Inbound)
}

func (n *Node) onPeerDisconnected(peer *p2p.Peer) {
	n.cfg.Logf("peer disconnected addr=%s id=%s", peer.Addr, peer.ID)
}

func (n *Node) onHello(peer *p2p.Peer, msg p2p.HelloMessage) {
	n.cfg.Logf("hello received peer=%s addr=%s version=%s listen=%s", msg.NodeID, peer.Addr, msg.Version, msg.Listen)

	go n.sendStatus(peer)
	go func() {
		_ = peer.Send(p2p.Message{
			Type: p2p.MessagePing,
			Body: p2p.PingMessage{Timestamp: time.Now().Unix()},
		})
	}()
}

func (n *Node) onStatus(peer *p2p.Peer, msg p2p.StatusMessage) {
	n.cfg.Logf("status received peer=%s height=%d hash=%s total_work=%s", msg.Status.PeerID, msg.Status.BestHeight, msg.Status.BestHash.String(), msg.Status.TotalWork)

	tip, _, err := n.store.CurrentTip()
	if err != nil {
		n.cfg.Logf("status local tip lookup failed: %v", err)
		return
	}
	if msg.Status.BestHeight <= tip.Header.Height {
		return
	}

	n.requestSync(peer, syncStartHeight(tip, msg.Status))
}

func syncStartHeight(localTip types.Block, remote types.PeerStatus) uint64 {
	if remote.BestHeight <= localTip.Header.Height {
		return localTip.Header.Height
	}
	return localTip.Header.Height + 1
}

func (n *Node) onPing(peer *p2p.Peer, msg p2p.PingMessage) {
	n.cfg.Logf("ping received peer=%s ts=%d", peer.ID, msg.Timestamp)
}

func (n *Node) onPong(peer *p2p.Peer, msg p2p.PongMessage) {
	n.cfg.Logf("pong received peer=%s ts=%d", peer.ID, msg.Timestamp)
}

func (n *Node) onNewBlock(peer *p2p.Peer, msg p2p.NewBlockMessage) {
	h := msg.Block.BlockHash()
	if h == (types.Hash{}) || n.store.HasBlock(h) {
		return
	}

	_, becameTip, err := n.validator.InsertBlock(n.store, msg.Block)
	if err != nil {
		n.cfg.Logf("newblock rejected peer=%s err=%v", peer.ID, err)
		return
	}

	n.cfg.Logf("newblock accepted peer=%s height=%d hash=%s became_tip=%t", peer.ID, msg.Block.Header.Height, h.String(), becameTip)
	if becameTip {
		go n.broadcastStatus()
	}
}

func (n *Node) onSyncRequest(peer *p2p.Peer, msg p2p.SyncRequestMessage) {
	limit := msg.Limit
	if limit == 0 || limit > syncBatchLimit {
		limit = syncBatchLimit
	}

	blocks := n.collectBlocks(msg.FromHeight, limit)
	if err := peer.Send(p2p.Message{
		Type: p2p.MessageSyncRs,
		Body: p2p.SyncResponseMessage{Blocks: blocks},
	}); err != nil {
		n.cfg.Logf("sync response send failed peer=%s err=%v", peer.ID, err)
	}
}

func (n *Node) onSyncResponse(peer *p2p.Peer, msg p2p.SyncResponseMessage) {
	if len(msg.Blocks) == 0 {
		return
	}

	tip, _, err := n.store.CurrentTip()
	if err != nil {
		n.cfg.Logf("sync tip lookup failed: %v", err)
		return
	}

	applied := n.applySyncBlocks(peer.ID, msg.Blocks)
	if applied == 0 && peer.Status.BestHeight <= tip.Header.Height {
		return
	}

	tip, _, err = n.store.CurrentTip()
	if err != nil {
		n.cfg.Logf("sync tip lookup failed: %v", err)
		return
	}
	if peer.Status.BestHeight > tip.Header.Height {
		n.requestSync(peer, tip.Header.Height+1)
	}
}

func (n *Node) requestSync(peer *p2p.Peer, fromHeight uint64) {
	if err := peer.Send(p2p.Message{
		Type: p2p.MessageSyncRq,
		Body: p2p.SyncRequestMessage{
			FromHeight: fromHeight,
			Limit:      syncBatchLimit,
		},
	}); err != nil {
		n.cfg.Logf("sync request send failed peer=%s from=%d err=%v", peer.ID, fromHeight, err)
	}
}

func (n *Node) collectBlocks(fromHeight, limit uint64) []types.Block {
	if limit == 0 {
		return nil
	}

	blocks := make([]types.Block, 0, limit)
	for i := uint64(0); i < limit; i++ {
		if block, err := n.store.GetBlockByHeight(fromHeight + i); err == nil {
			blocks = append(blocks, block)
		} else {
			break
		}
	}
	return blocks
}

func (n *Node) applySyncBlocks(peerID string, blocks []types.Block) int {
	applied := 0
	for _, block := range blocks {
		h := block.BlockHash()
		if h == (types.Hash{}) || n.store.HasBlock(h) {
			continue
		}

		_, becameTip, err := n.validator.InsertBlock(n.store, block)
		if err != nil {
			n.cfg.Logf("sync block rejected peer=%s height=%d hash=%s err=%v", peerID, block.Header.Height, h.String(), err)
			continue
		}

		applied++
		n.cfg.Logf("sync block accepted peer=%s height=%d hash=%s became_tip=%t", peerID, block.Header.Height, h.String(), becameTip)
	}

	if applied > 0 {
		go n.broadcastStatus()
	}
	return applied
}

func (n *Node) sendStatus(peer *p2p.Peer) {
	if status, err := n.localStatus(); err == nil {
		_ = peer.Send(p2p.Message{
			Type: p2p.MessageStatus,
			Body: p2p.StatusMessage{Status: status},
		})
	} else {
		n.cfg.Logf("status build failed: %v", err)
	}
}

func (n *Node) broadcastStatus() {
	if status, err := n.localStatus(); err == nil {
		n.p2p.Broadcast(p2p.Message{
			Type: p2p.MessageStatus,
			Body: p2p.StatusMessage{Status: status},
		})
	}
}

func (n *Node) broadcastNewBlock(block types.Block) {
	n.p2p.Broadcast(p2p.Message{
		Type: p2p.MessageNewBlk,
		Body: p2p.NewBlockMessage{Block: block},
	})
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
		TotalWork:   FormatWork(work),
		ConnectedAt: time.Now().Unix(),
	}, nil
}

func ParseBootnodes(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
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

func (n *Node) SubmitTransaction(tx types.Transaction) error {
	n.mempoolMu.Lock()
	defer n.mempoolMu.Unlock()

	if tx.From == "" || tx.To == "" {
		return fmt.Errorf("transaction requires from/to")
	}
	n.mempool = append(n.mempool, tx)
	return nil
}

func (n *Node) mempoolSnapshot() []types.Transaction {
	n.mempoolMu.Lock()
	defer n.mempoolMu.Unlock()

	out := make([]types.Transaction, len(n.mempool))
	copy(out, n.mempool)
	return out
}

func (n *Node) pruneMempool(committed []types.Transaction) {
	if len(committed) == 0 {
		return
	}

	seen := make(map[string]struct{}, len(committed))
	for _, tx := range committed {
		seen[fmt.Sprintf("%s|%s|%d|%d|%s", tx.From, tx.To, tx.Value, tx.Nonce, tx.Data)] = struct{}{}
	}

	n.mempoolMu.Lock()
	defer n.mempoolMu.Unlock()

	keep := n.mempool[:0]
	for _, tx := range n.mempool {
		key := fmt.Sprintf("%s|%s|%d|%d|%s", tx.From, tx.To, tx.Value, tx.Nonce, tx.Data)
		if _, ok := seen[key]; !ok {
			keep = append(keep, tx)
		}
	}
	n.mempool = keep
}

func (n *Node) startHTTP(ctx context.Context) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "network": n.cfg.Chain.NetworkID})
	})

	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		status, err := n.localStatus()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]any{
			"status":    status,
			"backend":   n.cfg.MinerBackend,
			"dag_alloc": n.cfg.ResolvedDAGAlloc,
		})
	})

	mux.HandleFunc("/mempool", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"transactions": n.mempoolSnapshot()})
	})

	mux.HandleFunc("/block", func(w http.ResponseWriter, r *http.Request) {
		h := strings.TrimSpace(r.URL.Query().Get("height"))
		if h == "" {
			http.Error(w, "height is required", http.StatusBadRequest)
			return
		}

		var height uint64
		if _, err := fmt.Sscan(h, &height); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		block, err := n.store.GetBlockByHeight(height)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, block)
	})

	mux.HandleFunc("/tx", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var tx types.Transaction
		if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := n.SubmitTransaction(tx); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]any{"accepted": true})
	})

	srv := &http.Server{Addr: n.cfg.HTTPAddr, Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			n.cfg.Logf("http server error: %v", err)
		}
	}()

	return nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
