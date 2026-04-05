# Colossus-X

This README is intentionally **daemon-focused** for the `testnet20260405` branch.

The codebase still contains `mine` and `verify`, but this document is written for operators who only run:

```bash
go run ./cmd/colossusx daemon ...
```

---

## 1. Checkout and setup

```bash
git clone https://github.com/CypherTroopers/colossus-x.git
cd colossus-x
git fetch --all
git checkout testnet20260405

go mod download
```

Requirements:

- Go `1.23.x`
- optional: `make`

Quick checks:

```bash
go version
go run ./cmd/colossusx -h
```

---

## 2. What `daemon` does on this branch

`daemon` starts the node runtime implemented in `cmd/colossusx/main.go` and `pkg/node/node.go`.

Main behaviors on this branch:

- loads or creates deterministic genesis in `datadir`
- keeps canonical chain data on disk
- preserves competing side branches in store
- syncs blocks over the built-in P2P layer
- can run as `miner`, `full`, or `light`
- exposes optional HTTP endpoints when `-http` is set
- supports mempool submission with `POST /tx`
- uses `coinbase` for block rewards on mining nodes

---

## 3. Recommended genesis file

For multi-node testnet operation, use the same `-genesis-file` on every node.

Example `configs/devnet/genesis.json`:

```json
{
  "chain_id": "devnet",
  "message": "colossusx devnet genesis",
  "timestamp": 1710000000,
  "target": "0fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
  "mode": "colossusx",
  "initial_dag_mib": 32768,
  "dag_growth_mib_per_epoch": 256,
  "alloc": {
    "alice": 1000000,
    "bob": 1000000
  },
  "block_reward": 50,
  "target_block_time_millis": 18000,
  "retarget_interval": 4
}
```

Supported JSON fields on this branch:

- `chain_id`
- `message`
- `timestamp`
- `target`
- `mode`
- `initial_dag_mib`
- `dag_growth_mib_per_epoch`
- `extra_data`
- `alloc`
- `block_reward`
- `target_block_time_millis`
- `retarget_interval`

Important:

- if `datadir` already contains a different genesis, daemon exits with a genesis mismatch error
- `alloc` becomes the initial on-chain account state
- `block_reward`, `target_block_time_millis`, and `retarget_interval` are loaded into chain economics

---

## 4. Daemon startup commands

### 4-1. Miner node

```bash
go run ./cmd/colossusx daemon \
  -mode colossusx \
  -network devnet \
  -genesis-file ./configs/devnet/genesis.json \
  -node-role miner \
  -workers 16 \
  -max-nonces 500000 \
  -block-time 18000ms \
  -datadir ./data/miner-01 \
  -listen :30333 \
  -bootnodes 161.97.184.220:30333 \
  -node-id miner-01 \
  -coinbase miner-01 \
  -miner-backend auto \
  -miner-dag-alloc auto \
  -http :8080 \
  -max-txs-per-block 256
```

### 4-2. Full node

```bash
go run ./cmd/colossusx daemon \
  -mode colossusx \
  -network devnet \
  -genesis-file ./configs/devnet/genesis.json \
  -node-role full \
  -workers 16 \
  -max-nonces 500000 \
  -block-time 18000ms \
  -datadir ./data/full-01 \
  -listen :30334 \
  -bootnodes 161.97.184.220:30333 \
  -node-id full-01 \
  -miner-backend auto \
  -miner-dag-alloc auto \
  -http :8081
```

### 4-3. Light node

```bash
go run ./cmd/colossusx daemon \
  -mode colossusx \
  -network devnet \
  -genesis-file ./configs/devnet/genesis.json \
  -node-role light \
  -datadir ./data/light-01 \
  -listen :30335 \
  -bootnodes 161.97.184.220:30333 \
  -node-id light-01 \
  -http :8082
```

Notes:

- `-node-role miner` enables mining
- `-node-role full` does not mine, but still runs the full node runtime
- `-node-role light` skips mining runtime initialization and enables light validation mode
- do **not** combine `-node-role` with legacy `-mine` or `-no-mine`

---

## 5. Daemon flags actually used on this branch

| Flag | Default | Meaning |
|---|---:|---|
| `-mode` | `colossusx` | Only `colossusx` is supported here. |
| `-network` | `devnet` | Network / chain identifier. |
| `-initial-dag-mib` | `32768` | Initial DAG size in MiB. |
| `-dag-mib` | `0` | Deprecated alias for `-initial-dag-mib`. |
| `-dag-growth-mib-per-epoch` | `256` | DAG growth in MiB per epoch. |
| `-node-role` | `full` | `full`, `miner`, or `light`. |
| `-mine` | `true` | Legacy behavior. Prefer `-node-role`. |
| `-no-mine` | `false` | Legacy behavior. Prefer `-node-role`. |
| `-workers` | `runtime.NumCPU()` | Worker count used by validator/miner runtime. |
| `-max-nonces` | `500000` | Nonce search limit per block template. |
| `-block-time` | `500ms` | Delay between locally mined blocks. |
| `-genesis-message` | `colossusx devnet genesis` | Used only when `-genesis-file` is not provided. |
| `-genesis-file` | `""` | Shared genesis JSON. Recommended. |
| `-datadir` | `./data` | Persistent chain data directory. |
| `-listen` | `:30333` | P2P TCP listen address. |
| `-bootnodes` | `""` | Comma-separated peers. |
| `-node-id` | `""` | Stable node identifier. |
| `-target` | `0fffffffff...` | Genesis / initial target when not using `-genesis-file`. |
| `-miner-backend` | `opencl` | `auto`, `cuda`, `opencl`, `metal`, `cpu`, `unified`, `gpu`. |
| `-miner-dag-alloc` | `auto` | `auto`, `go-heap`, `pinned-host`, `cuda-managed`, `opencl-svm`, `metal-shared`. |
| `-http` | `""` | Optional HTTP API listen address. |
| `-coinbase` | `""` | Reward address label. Defaults to `node-id` when empty. |
| `-max-txs-per-block` | `256` | Maximum number of accepted mempool txs per mined block. |

---

## 6. HTTP API

HTTP server starts only when `-http` is set.

### 6-1. Health

```bash
curl http://127.0.0.1:8080/health
```

### 6-2. Node status

```bash
curl http://127.0.0.1:8080/status
```

### 6-3. Current mempool

```bash
curl http://127.0.0.1:8080/mempool
```

### 6-4. Submit transaction

```bash
curl -X POST http://127.0.0.1:8080/tx \
  -H 'Content-Type: application/json' \
  -d '{
    "from": "alice",
    "to": "bob",
    "value": 10,
    "nonce": 0,
    "data": "hello"
  }'
```

### 6-5. Query block by height

```bash
curl "http://127.0.0.1:8080/block?height=1"
```

HTTP endpoints on this branch:

- `GET /health`
- `GET /status`
- `GET /mempool`
- `POST /tx`
- `GET /block?height=<n>`

---

## 7. P2P and sync behavior

The built-in P2P runtime does the following:

- exchanges `hello`, `status`, `ping`, `pong`
- requests missing blocks with `sync request`
- responds with canonical blocks from local store
- broadcasts accepted new tip blocks
- compares peer `total_work` during initial sync readiness

Operational notes:

- bootnodes are plain `host:port` entries separated by commas
- if no peers are connected, a miner node starts immediately
- if peers are connected but have no status yet, miner waits for sync metadata
- frame size checks and duplicate/self-peer rejection are enabled in P2P code on this branch

---

## 8. Test and validation commands

Run the packages most relevant to daemon operation:

```bash
go test ./pkg/node -v
go test ./pkg/consensus -v
go test ./pkg/chain -v
go test ./pkg/types -v
go test ./pkg/p2p -v
```

Full suite:

```bash
go test ./...
```

CLI smoke checks:

```bash
go run ./cmd/colossusx -h
go run ./cmd/colossusx daemon -h
```

---

## 9. Practical notes for this branch

- Use `go run ./cmd/colossusx`, not the repository root `main.go`, for daemon operation.
- For multi-node testnet, keep `-genesis-file` identical on every node.
- `coinbase` controls where block reward is credited in block state.
- Transactions are accepted into mempool through `/tx` and packed up to `-max-txs-per-block`.
- `full` nodes do not mine, but they still participate in validation, sync, and HTTP serving.
- `light` nodes are the lightest daemon path on this branch.

---

## 10. Minimal one-node devnet command

```bash
go run ./cmd/colossusx daemon \
  -mode colossusx \
  -network devnet \
  -node-role miner \
  -datadir ./data/devnet-01 \
  -listen :30333 \
  -node-id devnet-01 \
  -coinbase devnet-01 \
  -miner-backend cpu \
  -miner-dag-alloc go-heap \
  -http :8080
```

This is the shortest daemon-only startup path on this branch.
