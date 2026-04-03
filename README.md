# Colossus-X Testnet P2P Sync Commands (Genesis Profile Based)

Use one shared genesis profile file for all nodes:

```bash
GENESIS_JSON=./deploy/genesis.testnet.json

NETWORK_ID="$(jq -r '.chain.network_id' "$GENESIS_JSON")"
TARGET_HEX="$(jq -r '.genesis.target' "$GENESIS_JSON")"
INITIAL_DAG_MIB="$(jq -r '.spec.initial_dag_mib' "$GENESIS_JSON")"
DAG_GROWTH_MIB="$(jq -r '.spec.dag_growth_mib_per_epoch' "$GENESIS_JSON")"
BOOTNODES_CSV="$(jq -r '.p2p.bootnodes | join(",")' "$GENESIS_JSON")"

mkdir -p ./data
tar -xzf bootstrap-datadir.tar.gz -C ./data
```

## Hybrid node

```bash
cp -a ./data/bootstrap ./data/hybrid-01

./bin/colossusx daemon \
  -mode colossusx \
  -testnet-preset \
  -node-role hybrid \
  -network "$NETWORK_ID" \
  -target "$TARGET_HEX" \
  -initial-dag-mib "$INITIAL_DAG_MIB" \
  -dag-growth-mib-per-epoch "$DAG_GROWTH_MIB" \
  -node-id hybrid-01 \
  -listen :30335 \
  -bootnodes "$BOOTNODES_CSV" \
  -datadir ./data/hybrid-01
```

## Miner node

```bash
cp -a ./data/bootstrap ./data/miner-01

./bin/colossusx daemon \
  -mode colossusx \
  -testnet-preset \
  -node-role miner \
  -mine \
  -network "$NETWORK_ID" \
  -target "$TARGET_HEX" \
  -initial-dag-mib "$INITIAL_DAG_MIB" \
  -dag-growth-mib-per-epoch "$DAG_GROWTH_MIB" \
  -node-id miner-01 \
  -listen :30334 \
  -bootnodes "$BOOTNODES_CSV" \
  -datadir ./data/miner-01
```

## Validator node

```bash
cp -a ./data/bootstrap ./data/validator-01

./bin/colossusx daemon \
  -mode colossusx \
  -testnet-preset \
  -node-role validator \
  -no-mine \
  -network "$NETWORK_ID" \
  -target "$TARGET_HEX" \
  -initial-dag-mib "$INITIAL_DAG_MIB" \
  -dag-growth-mib-per-epoch "$DAG_GROWTH_MIB" \
  -fixed-validator-set validator-01,validator-02,validator-03 \
  -node-id validator-01 \
  -listen :30333 \
  -bootnodes "$BOOTNODES_CSV" \
  -datadir ./data/validator-01
```
