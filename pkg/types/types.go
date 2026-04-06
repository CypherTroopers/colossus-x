package types

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	cx "colossusx/colossusx"
	"golang.org/x/crypto/sha3"
)

type Hash [32]byte

type EconomicConfig struct {
	BlockReward           uint64    `json:"block_reward,omitempty"`
	TargetBlockTimeMillis uint64    `json:"target_block_time_millis,omitempty"`
	RetargetInterval      uint64    `json:"retarget_interval,omitempty"`
	MaxTarget             cx.Target `json:"max_target,omitempty"`
}

func (c EconomicConfig) Normalized() EconomicConfig {
	if c.BlockReward == 0 {
		c.BlockReward = 50
	}
	if c.TargetBlockTimeMillis == 0 {
		c.TargetBlockTimeMillis = 18_000
	}
	if c.RetargetInterval == 0 {
		c.RetargetInterval = 20
	}
	return c
}

func (c EconomicConfig) TargetBlockTime() time.Duration {
	cfg := c.Normalized()
	return time.Duration(cfg.TargetBlockTimeMillis) * time.Millisecond
}

type Transaction struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Value uint64 `json:"value"`
	Nonce uint64 `json:"nonce"`
	Data  string `json:"data,omitempty"`
}

type AccountState struct {
	Balance uint64 `json:"balance"`
	Nonce   uint64 `json:"nonce"`
}

type BlockHeader struct {
	Version          uint32    `json:"version"`
	AlgorithmVersion uint32    `json:"algorithm_version"`
	Height           uint64    `json:"height"`
	ParentHash       Hash      `json:"parent_hash"`
	Timestamp        int64     `json:"timestamp"`
	Target           cx.Target `json:"target"`
	Nonce            uint64    `json:"nonce"`
	Coinbase         string    `json:"coinbase,omitempty"`
	EpochSeed        Hash      `json:"epoch_seed"`
	DAGSizeBytes     uint64    `json:"dag_size_bytes"`
	DAGMerkleRoot    Hash      `json:"dag_merkle_root"`
	TxRoot           Hash      `json:"tx_root"`
	StateRoot        Hash      `json:"state_root"`
}

type Block struct {
	Header                   BlockHeader                  `json:"header"`
	Transactions             []Transaction                `json:"transactions,omitempty"`
	State                    map[string]AccountState      `json:"state,omitempty"`
	ColossusXSolution        *cx.ColossusXSolution        `json:"colossusx_solution,omitempty"`
	ColossusXSolutionCompact *cx.ColossusXSolutionCompact `json:"colossusx_solution_compact,omitempty"`
}

type GenesisConfig struct {
	ChainID   string            `json:"chain_id"`
	Message   string            `json:"message,omitempty"`
	Timestamp int64             `json:"timestamp"`
	Bits      cx.Target         `json:"target"`
	Spec      cx.Spec           `json:"spec"`
	ExtraData string            `json:"extra_data,omitempty"`
	Alloc     map[string]uint64 `json:"alloc,omitempty"`
	Economics EconomicConfig    `json:"economics,omitempty"`
}

type ChainConfig struct {
	NetworkID string         `json:"network_id"`
	Spec      cx.Spec        `json:"spec"`
	Economics EconomicConfig `json:"economics,omitempty"`
}

type PeerStatus struct {
	PeerID      string `json:"peer_id"`
	BestHash    Hash   `json:"best_hash"`
	BestHeight  uint64 `json:"best_height"`
	TotalWork   string `json:"total_work"`
	ConnectedAt int64  `json:"connected_at"`
}

type MiningTemplate struct {
	Parent     Hash        `json:"parent"`
	Height     uint64      `json:"height"`
	Target     cx.Target   `json:"target"`
	EpochSeed  Hash        `json:"epoch_seed"`
	CreatedAt  time.Time   `json:"created_at"`
	Header     BlockHeader `json:"header"`
}

func (h Hash) String() string { return hex.EncodeToString(h[:]) }

func (h Hash) MarshalJSON() ([]byte, error) {
	return json.Marshal(h.String())
}

func (h *Hash) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	decoded, err := hex.DecodeString(s)
	if err != nil {
		return err
	}
	if len(decoded) != len(h) {
		return fmt.Errorf("expected %d bytes, got %d", len(h), len(decoded))
	}
	copy(h[:], decoded)
	return nil
}

func (h BlockHeader) EncodeForMining() []byte {
	fixed := h.EncodeForMiningGPUFixedV1()
	return fixed[:]
}

func (h BlockHeader) Encode() []byte {
	buf := append([]byte{}, h.EncodeForMining()...)
	buf = binary.BigEndian.AppendUint64(buf, h.Nonce)
	return buf
}

func (h BlockHeader) HeaderHash() Hash { return sha256.Sum256(h.Encode()) }
func (b Block) BlockHash() Hash        { return b.Header.HeaderHash() }

func NewGenesisBlock(cfg GenesisConfig) Block {
	resolved := cfg.Spec.ResolvedForHeight(0)
	state := make(map[string]AccountState, len(cfg.Alloc))
	for addr, balance := range cfg.Alloc {
		state[addr] = AccountState{Balance: balance}
	}
	return Block{
		Header: BlockHeader{
			Version:          1,
			AlgorithmVersion: resolved.AlgorithmVersion,
			Height:           0,
			ParentHash:       Hash{},
			Timestamp:        cfg.Timestamp,
			Target:           cfg.Bits,
			Nonce:            0,
			Coinbase:         "",
			EpochSeed:        EpochSeedForHeight(resolved, 0),
			DAGSizeBytes:     resolved.DAGSizeBytes,
			DAGMerkleRoot:    Hash{},
			TxRoot:           ComputeTxRoot(nil),
			StateRoot:        ComputeStateRoot(state),
		},
		Transactions: nil,
		State:        state,
	}
}

func EpochSeedForHeight(spec cx.Spec, height uint64) Hash {
	if spec.IsAppendOnlyScratchpad() {
		return Hash(cx.CycleSeedForHeight(spec, height))
	}
	var seedMaterial [40]byte
	epoch := uint64(0)
	if spec.EpochBlocks != 0 {
		epoch = height / spec.EpochBlocks
	}
	binary.BigEndian.PutUint64(seedMaterial[:8], epoch)
	copy(seedMaterial[8:], spec.GenesisHash[:])
	return sha3.Sum256(seedMaterial[:])
}

func CloneState(in map[string]AccountState) map[string]AccountState {
	if len(in) == 0 {
		return map[string]AccountState{}
	}
	out := make(map[string]AccountState, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func ComputeTxRoot(txs []Transaction) Hash {
	if len(txs) == 0 {
		return sha256.Sum256(nil)
	}
	payload, _ := json.Marshal(txs)
	return sha256.Sum256(payload)
}

func ComputeStateRoot(state map[string]AccountState) Hash {
	if len(state) == 0 {
		return sha256.Sum256(nil)
	}
	keys := make([]string, 0, len(state))
	for k := range state {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf := make([]byte, 0, len(keys)*64)
	for _, key := range keys {
		acct := state[key]
		buf = binary.BigEndian.AppendUint32(buf, uint32(len(key)))
		buf = append(buf, []byte(key)...)
		buf = binary.BigEndian.AppendUint64(buf, acct.Balance)
		buf = binary.BigEndian.AppendUint64(buf, acct.Nonce)
	}
	return sha256.Sum256(buf)
}

func ApplyTransactions(parent map[string]AccountState, txs []Transaction, coinbase string, reward uint64) (map[string]AccountState, error) {
	state := CloneState(parent)
	for i, tx := range txs {
		if tx.To == "" {
			return nil, fmt.Errorf("transaction %d: to is required", i)
		}
		if tx.From == "" {
			return nil, fmt.Errorf("transaction %d: from is required", i)
		}
		from := state[tx.From]
		if from.Nonce != tx.Nonce {
			return nil, fmt.Errorf("transaction %d: nonce mismatch got=%d want=%d", i, tx.Nonce, from.Nonce)
		}
		if from.Balance < tx.Value {
			return nil, fmt.Errorf("transaction %d: insufficient balance", i)
		}
		from.Balance -= tx.Value
		from.Nonce++
		if from.Balance == 0 && from.Nonce == 0 {
			delete(state, tx.From)
		} else {
			state[tx.From] = from
		}
		to := state[tx.To]
		to.Balance += tx.Value
		state[tx.To] = to
	}
	if coinbase != "" && reward > 0 {
		acct := state[coinbase]
		acct.Balance += reward
		state[coinbase] = acct
	}
	return state, nil
}
