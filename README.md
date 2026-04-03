# Colossus-X Testnet P2P Sync Commands (genesis profile driven)

In this repository, `colossusx daemon` does **not** read a genesis profile JSON directly.
Instead, it receives chain settings via CLI flags.
Use `configs/genesis.p2p.example.json` as the source of truth and pass those values to each node command.

## 0) Build

```bash
mkdir -p bin
go build -o ./bin/colossusx ./cmd/colossusx
```

## 1) Export shared values from the genesis profile

```bash
GENESIS_JSON=./configs/genesis.p2p.example.json

NETWORK_ID="$(jq -r '.chain.network_id' "$GENESIS_JSON")"
TARGET_HEX="$(jq -r '.genesis.target' "$GENESIS_JSON")"
INITIAL_DAG_MIB="$(jq -r '.spec.initial_dag_mib' "$GENESIS_JSON")"
DAG_GROWTH_MIB="$(jq -r '.spec.dag_growth_mib_per_epoch' "$GENESIS_JSON")"
BOOTNODES_CSV="$(jq -r '.p2p.bootnodes | join(",")' "$GENESIS_JSON")"
GENESIS_MESSAGE="$(jq -r '.genesis.message' "$GENESIS_JSON")"

mkdir -p ./data
```

## 2) Initialize fresh datadirs (optional)

If you are not distributing prebuilt chain data, run `init` once per node datadir.

```bash
./bin/colossusx init \
  -genesis-json "$GENESIS_JSON" \
  -datadir ./data/hybrid-01

./bin/colossusx init \
  -genesis-json "$GENESIS_JSON" \
  -datadir ./data/miner-01

./bin/colossusx init \
  -genesis-json "$GENESIS_JSON" \
  -datadir ./data/validator-01
```

> Re-running `init` on an already initialized datadir fails with `already initialized`.

## 3) Hybrid node

```bash
./bin/colossusx daemon \
  -mode colossusx \
  -testnet-preset \
  -node-role hybrid \
  -network "$NETWORK_ID" \
  -genesis-message "$GENESIS_MESSAGE" \
  -target "$TARGET_HEX" \
  -initial-dag-mib "$INITIAL_DAG_MIB" \
  -dag-growth-mib-per-epoch "$DAG_GROWTH_MIB" \
  -node-id hybrid-01 \
  -listen :30335 \
  -bootnodes "$BOOTNODES_CSV" \
  -datadir ./data/hybrid-01
```

## 4) Miner node

```bash
./bin/colossusx daemon \
  -mode colossusx \
  -testnet-preset \
  -node-role miner \
  -mine \
  -network "$NETWORK_ID" \
  -genesis-message "$GENESIS_MESSAGE" \
  -target "$TARGET_HEX" \
  -initial-dag-mib "$INITIAL_DAG_MIB" \
  -dag-growth-mib-per-epoch "$DAG_GROWTH_MIB" \
  -node-id miner-01 \
  -listen :30334 \
  -bootnodes "$BOOTNODES_CSV" \
  -datadir ./data/miner-01
```

## 5) Validator node

```bash
./bin/colossusx daemon \
  -mode colossusx \
  -testnet-preset \
  -node-role validator \
  -no-mine \
  -network "$NETWORK_ID" \
  -genesis-message "$GENESIS_MESSAGE" \
  -target "$TARGET_HEX" \
  -initial-dag-mib "$INITIAL_DAG_MIB" \
  -dag-growth-mib-per-epoch "$DAG_GROWTH_MIB" \
  -fixed-validator-set validator-01,validator-02,validator-03 \
  -node-id validator-01 \
  -listen :30333 \
  -bootnodes "$BOOTNODES_CSV" \
  -datadir ./data/validator-01
```

## 6) Important notes

- `./deploy/genesis.testnet.json` and `bootstrap-datadir.tar.gz` do not exist in this repository.
  Use `configs/genesis.p2p.example.json` or provide your own deployment artifacts.
- `daemon` can create genesis on startup even with an empty datadir, but explicit `init` reduces operational mistakes.
- `-testnet-preset` overrides defaults (`network=testnet`, etc.), so keep explicit flags like `-network "$NETWORK_ID"` to stay aligned with your profile.
