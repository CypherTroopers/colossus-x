# COLOSSUS-X

COLOSSUS-X is a Go codebase that currently contains **three related layers**:

1. the core memory-hard hashing/mining library in `colossusx/`,
2. the miner package at the repository root, which exposes backend selection, DAG allocation strategy selection, and CLI parsing helpers, and
3. executable entrypoints under `cmd/` for the standalone miner, the dev node/daemon, and a placeholder control tool.

After reviewing the current repository layout, the most important practical takeaway is:

- `go run .` from the repository root is **not** the correct way to run the project,
- the actual runnable binaries live under `cmd/colossusx`, `cmd/colossusd`, and `cmd/colossusxctl`, and
- small local runs should generally use **research mode** because strict mode uses a dynamic DAG profile (8 GiB initial, +512 MiB per epoch).

## Repository layout

### Executables

- `cmd/colossusx`: standalone miner CLI wrapper around `miner.Main`.
- `cmd/colossusd`: dev node/daemon that wires together chain storage, consensus validation, mining, and P2P networking.
- `cmd/colossusxctl`: placeholder control CLI for a later RPC phase.

### Root miner package

- `main.go`: flag parsing, backend selection, DAG allocation strategy selection, DAG generation, mining/benchmark execution, and formatted console output.
- `backend_*.go`: unified, CPU, and GPU backend adapters.
- `gpu_backend_*.go`: CUDA/OpenCL runtime handling and GPU dispatch planning.
- `memory_strategy.go`: DAG allocation strategy selection (`auto`, `go-heap`, `pinned-host`, `cuda-managed`, `opencl-svm`).
- `dag_helpers.go`: convenience wrappers for DAG construction and generation.
- `shared_dag_hash.go`: host-reference hashing path for the canonical contiguous DAG allocation.

### Core packages

- `colossusx/`: protocol constants, spec validation, DAG generation, nonce abstraction, lattice hash, miner loop, and target comparison.
- `pkg/consensus`: block validation, sealing, DAG caching, and chain-work selection.
- `pkg/chain`: in-memory and disk-backed chain stores.
- `pkg/node`: node runtime that initializes genesis, mines blocks, and announces status/blocks over P2P.
- `pkg/p2p`: TCP-based peer server/message transport.
- `pkg/types`: block/header/hash/config types and mining header encoding helpers.

## What is runnable today

## 1. Standalone miner CLI

Run the miner CLI help:

```bash
go run ./cmd/colossusx -h
```

Build the miner binary:

```bash
go build -o ./bin/colossusx ./cmd/colossusx
```

Run the built miner binary:

```bash
./bin/colossusx -h
```

## 2. Dev node / daemon

Run the daemon help:

```bash
go run ./cmd/colossusd -h
```

Build the daemon binary:

```bash
go build -o ./bin/colossusd ./cmd/colossusd
```

Run the built daemon binary:

```bash
./bin/colossusd -h
```

## 3. Control CLI placeholder

Run the control CLI placeholder:

```bash
go run ./cmd/colossusxctl
```

Build it explicitly:

```bash
go build -o ./bin/colossusxctl ./cmd/colossusxctl
```

## Core algorithm summary

The active hashing path is implemented in `colossusx/hash.go` and works as follows:

1. `sha3-512(header || nonce_bytes)` derives a 64-byte seed.
2. The first 32 bytes become the initial mix.
3. For each round in `READS_PER_H`:
   - compute `fnv1a64(mix || little_endian(round)) % node_count`,
   - read one 64-byte DAG node,
   - XOR the 32-byte mix with both 32-byte halves of the DAG node,
   - hash the resulting 64-byte buffer with `blake3`.
4. Finalize with `sha3-512(seed || mix)`.

The proof-of-work comparison uses the first 32 bytes of the final digest and compares them against the configured target in big-endian order.

## DAG generation

DAG generation is deterministic and fills each node with:

```text
dag[i] = keccak512(epoch_seed || little_endian(i))
```

Generation is parallelized across worker goroutines.

## Modes

### `strict`

Strict mode is protocol-locked to:

- DAG size: `80 * 1024 * 1024 * 1024` bytes
- node size: `64` bytes
- reads per hash: `512`
- epoch blocks: `8000`

When `-mode strict` is selected, CLI overrides for DAG size, reads per hash, and epoch length are rejected.

### `research`

Research mode preserves the same algorithm but allows smaller DAGs and alternate read/epoch settings so the code can run on normal development machines.

For nearly all local development and CI-style checks, `research` is the right choice.

## Backends and memory behavior

### `unified`

- Hashes directly against the DAG allocation's underlying byte slice.
- Observes in-place DAG mutations after `Prepare`.
- Works naturally with `go-heap` and is designed for shared/unified allocation strategies.

### `cpu`

- Copies DAG nodes into a CPU-side node table during `Prepare`.
- Produces the same hashes as `unified` for the same DAG contents.
- Does **not** observe DAG mutations after preparation because it uses the copied representation.

### `gpu`

- Exists as a real selectable backend.
- Uses OpenCL dispatch planning in normal builds and a CUDA-specific backend in `cuda` builds.
- Keeps the contiguous DAG allocation as the canonical representation.
- Only reports `device-kernel` when a real accelerator dispatch succeeds.
- Otherwise falls back to the validated host-reference/shared-host execution path.

## DAG allocation strategies

Supported CLI names:

- `auto`
- `go-heap`
- `pinned-host`
- `cuda-managed`
- `opencl-svm`

Current practical behavior:

- `auto` tries `cuda-managed`, then `opencl-svm`, then falls back to `go-heap`.
- `go-heap` works in standard Go builds.
- `pinned-host` currently works as an in-memory allocation in non-CUDA builds and uses CUDA pinned host memory in `cgo && cuda` builds.
- `cuda-managed` requires an initialized CUDA runtime/device.
- `opencl-svm` requires a live OpenCL context/device with SVM capability.

## Miner CLI flags that matter most

The standalone miner in `cmd/colossusx` exposes these important patterns:

- `-mode strict|research`
- `-backend unified|cpu|gpu`
- `-dag-alloc auto|go-heap|pinned-host|cuda-managed|opencl-svm`
- `-initial-dag-mib <MiB>`
- `-dag-growth-mib-per-epoch <MiB>`
- `-dag-mib <MiB>` (deprecated alias for `-initial-dag-mib`)
- `-reads <count>`
- `-epoch-blocks <count>`
- `-workers <count>`
- `-bench`
- `-start-nonce <n>`
- `-max-nonces <n>`
- `-header <hex>`
- `-epoch-seed <hex>`
- `-target <32-byte hex>`

## Recommended standalone miner command patterns

The commands below are updated to match the **actual runnable entrypoint**: `./cmd/colossusx`.

### Basic help / smoke check

```bash
go run ./cmd/colossusx -h
```
### DAG growth profile (`--initial-dag-mib`, `--dag-growth-mib-per-epoch`)

DAG sizing is dynamic and based on epoch height.

- `--initial-dag-mib` sets the initial epoch-0 DAG size.
- `--dag-growth-mib-per-epoch` sets growth applied at each epoch boundary.
- `--dag-mib` is retained as a deprecated alias for `--initial-dag-mib`.

Examples:
- `--mode research --initial-dag-mib 1024 --dag-growth-mib-per-epoch 64`
- `--mode strict --initial-dag-mib 8192 --dag-growth-mib-per-epoch 512`

Note:
- In `research` mode, initial size and growth are configurable.
- In `strict` mode, defaults are 8 GiB initial and +512 MiB per epoch.
### Small research benchmark: unified backend

```bash
go run ./cmd/colossusx \
  -mode research \
  -bench \
  -backend unified \
  -dag-mib 1 \
  -reads 8 \
  -workers 2 \
  -max-nonces 1000
```

### Small research benchmark: CPU backend

```bash
go run ./cmd/colossusx \
  -mode research \
  -bench \
  -backend cpu \
  -dag-mib 1 \
  -reads 8 \
  -workers 2 \
  -max-nonces 1000
```

### Small research benchmark: GPU backend path

```bash
go run ./cmd/colossusx \
  -mode research \
  -bench \
  -backend gpu \
  -dag-mib 1 \
  -reads 8 \
  -workers 2 \
  -max-nonces 1000
```

### Same benchmark with an explicit DAG allocation override

```bash
go run ./cmd/colossusx \
  -mode research \
  -bench \
  -backend unified \
  -dag-alloc go-heap \
  -dag-mib 8 \
  -reads 32 \
  -workers 4 \
  -max-nonces 50000
```

### Easy-target mining smoke test

```bash
go run ./cmd/colossusx \
  -mode research \
  -backend unified \
  -dag-mib 1 \
  -reads 8 \
  -workers 2 \
  -max-nonces 10 \
  -target ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff
```

### Custom header / epoch seed / nonce range example

```bash
go run ./cmd/colossusx \
  -mode research \
  -backend unified \
  -dag-mib 1 \
  -reads 8 \
  -workers 2 \
  -start-nonce 1000 \
  -max-nonces 100 \
  -header 00112233445566778899aabbccddeeff \
  -epoch-seed 000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f \
  -target ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff
```

### Strict-mode example

```bash
go run ./cmd/colossusx -mode strict -bench -backend unified
```

> Warning: strict mode starts at 8 GiB and grows every epoch; it may still be too large for ordinary laptops/CI containers.

## Dev node (`colossusd`) command patterns

The daemon defaults to **research mode**, an 8 MiB DAG, 32 reads per hash, local mining enabled, a disk-backed store in `./data`, and a TCP listener on `:30333`.

### Pattern 1: single-node local devnet miner

```bash
go run ./cmd/colossusd \
  -network devnet \
  -mode research \
  -dag-mib 8 \
  -reads 32 \
  -epoch-blocks 32 \
  -mine \
  -workers 2 \
  -max-nonces 500000 \
  -datadir ./data/node1 \
  -listen :30333 \
  -node-id node1 \
  -miner-backend unified \
  -miner-dag-alloc go-heap
```

### Pattern 2: second mining node joining the first node

```bash
go run ./cmd/colossusd \
  -network devnet \
  -mode research \
  -dag-mib 8 \
  -reads 32 \
  -epoch-blocks 32 \
  -mine \
  -workers 2 \
  -max-nonces 500000 \
  -datadir ./data/node2 \
  -listen :30334 \
  -bootnodes 127.0.0.1:30333 \
  -node-id node2 \
  -miner-backend cpu \
  -miner-dag-alloc go-heap
```

### Pattern 3: observer node with mining disabled

```bash
go run ./cmd/colossusd \
  -network devnet \
  -mode research \
  -dag-mib 8 \
  -reads 32 \
  -epoch-blocks 32 \
  -no-mine \
  -datadir ./data/observer \
  -listen :30335 \
  -bootnodes 127.0.0.1:30333,127.0.0.1:30334 \
  -node-id observer
```

### Pattern 4: single-node run with the GPU backend request

```bash
go run ./cmd/colossusd \
  -network devnet \
  -mode research \
  -dag-mib 8 \
  -reads 32 \
  -epoch-blocks 32 \
  -mine \
  -workers 2 \
  -datadir ./data/gpu-node \
  -listen :30336 \
  -node-id gpu-node \
  -miner-backend gpu \
  -miner-dag-alloc auto
```

### Pattern 5: strict-profile daemon run

```bash
go run ./cmd/colossusd \
  -network strictnet \
  -mode strict \
  -mine \
  -workers 4 \
  -datadir ./data/strict \
  -listen :30333 \
  -node-id strict-node \
  -miner-backend unified \
  -miner-dag-alloc auto
```

> Warning: this uses the strict dynamic DAG profile (8 GiB initial, +512 MiB/epoch).

## What `colossusd` flags control

Useful daemon flags include:

- `-mode`
- `-network`
- `-initial-dag-mib`
- `-dag-growth-mib-per-epoch`
- `-dag-mib` (deprecated alias for initial DAG size)
- `-reads`
- `-epoch-blocks`
- `-mine` / `-no-mine`
- `-workers`
- `-max-nonces`
- `-block-time`
- `-genesis-message`
- `-datadir`
- `-listen`
- `-bootnodes`
- `-node-id`
- `-target`
- `-miner-backend`
- `-miner-dag-alloc`

## Makefile status

The Makefile exists, but it is **not fully aligned** with the current runnable entrypoints.

Current state:

- `make deps` is still useful.
- `make build` currently tries to build `.` instead of `./cmd/colossusx` or `./cmd/colossusd`.
- `make run-help` still points at `go run . -h`, which is not the correct executable path.
- `make bench-small`, `make bench-cpu`, and `make mine-easy` also point at the root package and therefore do not reflect the actual command patterns you should use.

For now, prefer the explicit `go run ./cmd/...` commands in this README.

## Testing

### Full test suite

```bash
go test ./... -count=1
```

### Core algorithm package only

```bash
go test ./colossusx -count=1
```

### Root miner package only

```bash
go test . -count=1
```

### Daemon package tests

```bash
go test ./cmd/colossusd -count=1
```

### Node / chain / consensus package tests

```bash
go test ./pkg/... -count=1
```

### Single test examples

```bash
go test . -run TestRunInitializesBackendRuntimeBeforeResolvingAllocator -count=1
go test ./colossusx -run TestLatticeHashDeterministic -count=1
go test ./cmd/colossusd -run TestInitializeMiningUnifiedGoHeap -count=1
```

### Optional extra validation

```bash
go test ./... -count=1 -race
go build ./cmd/colossusx ./cmd/colossusd ./cmd/colossusxctl
```
LOG
```bash
mkdir -p /tmp/colossusx-test
RUNLOG=/tmp/colossusx-test/run-1024.log

(
  while true; do
    PID=$(pgrep -af 'colossusd.*node1' | awk 'NR==1{print $1}')
    if [ -n "$PID" ] && [ -r "/proc/$PID/status" ]; then
      echo "[MEM $(date '+%H:%M:%S')] RSS=$(awk '/VmRSS/ {print $2" kB"}' /proc/$PID/status) SWAP=$(awk '/VmSwap/ {print $2" kB"}' /proc/$PID/status) AVAIL=$(awk '/MemAvailable/ {print $2" kB"}' /proc/meminfo)"
    else
      echo "[MEM $(date '+%H:%M:%S')] waiting for colossusd..."
    fi
    sleep 5
  done
) &
MONPID=$!

./bin/colossusd \
  --datadir ./data/node1 \
  --listen :30333 \
  --node-id node1 \
  --network devnet \
  --mode research \
  --dag-mib 1024 \
  --reads 32 \
  --epoch-blocks 32 \
  --miner-backend cpu \
  --miner-dag-alloc auto \
  --mine \
  --workers "$(nproc)" \
  --max-nonces 500000 \
  --block-time 1s 2>&1 | tee "$RUNLOG"

kill "$MONPID" 2>/dev/null
wait "$MONPID" 2>/dev/null
```
## Build requirements

- Go `1.23.0`
- Go modules from `go.mod`
- optional CUDA/OpenCL toolchains only when intentionally building the tagged runtime/allocation files

## Current status summary

Today this repository is best understood as:

- a working COLOSSUS-X hashing/mining library,
- a standalone miner binary at `cmd/colossusx`,
- a runnable dev node/daemon at `cmd/colossusd`, and
- a control CLI placeholder at `cmd/colossusxctl`.

If you want to actually run the codebase, start from `cmd/colossusx` for local miner experiments or `cmd/colossusd` for multi-node/devnet experiments.
