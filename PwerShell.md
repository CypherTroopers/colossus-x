# Colossus-X PowerShell Guide

This file is for the `testnet20260405` branch and uses **PowerShell + go run only**.

Main command style:

```powershell
go run .\cmd\colossusx daemon ...
```

---

## 1. Checkout

```powershell
git clone https://github.com/CypherTroopers/colossus-x.git
Set-Location .\colossus-x
git fetch --all
git checkout testnet20260405

go mod download
```

Quick checks:

```powershell
go version
go run .\cmd\colossusx -h
go run .\cmd\colossusx daemon -h
```

---

## 2. Recommended genesis file

Use the same `-genesis-file` on every node.

Example `configs\devnet\genesis.json`:

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

Important:

- if `datadir` already contains a different genesis, daemon exits with a genesis mismatch error
- `alloc` becomes the initial on-chain state
- economics are loaded from the genesis file on this branch

---

## 3. Daemon startup commands

### Miner node

```powershell
go run .\cmd\colossusx daemon `
  -mode colossusx `
  -network devnet `
  -genesis-file .\configs\devnet\genesis.json `
  -node-role miner `
  -workers 16 `
  -max-nonces 500000 `
  -block-time 18000ms `
  -datadir .\data\miner-01 `
  -listen :30333 `
  -bootnodes 161.97.184.220:30333 `
  -node-id miner-01 `
  -coinbase miner-01 `
  -miner-backend auto `
  -miner-dag-alloc auto `
  -http :8080 `
  -max-txs-per-block 256
```

### Full node

```powershell
go run .\cmd\colossusx daemon `
  -mode colossusx `
  -network devnet `
  -genesis-file .\configs\devnet\genesis.json `
  -node-role full `
  -workers 16 `
  -max-nonces 500000 `
  -block-time 18000ms `
  -datadir .\data\full-01 `
  -listen :30334 `
  -bootnodes 161.97.184.220:30333 `
  -node-id full-01 `
  -miner-backend auto `
  -miner-dag-alloc auto `
  -http :8081
```

### Light node

```powershell
go run .\cmd\colossusx daemon `
  -mode colossusx `
  -network devnet `
  -genesis-file .\configs\devnet\genesis.json `
  -node-role light `
  -datadir .\data\light-01 `
  -listen :30335 `
  -bootnodes 161.97.184.220:30333 `
  -node-id light-01 `
  -http :8082
```

Notes:

- `-node-role miner` enables mining
- `-node-role full` does not mine
- `-node-role light` enables light validation mode
- do not combine `-node-role` with legacy `-mine` or `-no-mine`

---

## 4. HTTP API

### Health

```powershell
Invoke-RestMethod http://127.0.0.1:8080/health
```

### Status

```powershell
Invoke-RestMethod http://127.0.0.1:8080/status
```

### Mempool

```powershell
Invoke-RestMethod http://127.0.0.1:8080/mempool
```

### Submit transaction

```powershell
$body = @{
  from  = "alice"
  to    = "bob"
  value = 10
  nonce = 0
  data  = "hello"
} | ConvertTo-Json

Invoke-RestMethod `
  -Method Post `
  -Uri http://127.0.0.1:8080/tx `
  -ContentType 'application/json' `
  -Body $body
```

### Block by height

```powershell
Invoke-RestMethod "http://127.0.0.1:8080/block?height=1"
```

---

## 5. Test commands

```powershell
go test ./pkg/node -v
go test ./pkg/consensus -v
go test ./pkg/chain -v
go test ./pkg/types -v
go test ./pkg/p2p -v
```

Full suite:

```powershell
go test ./...
```

---

## 6. Minimal one-node devnet command

```powershell
go run .\cmd\colossusx daemon `
  -mode colossusx `
  -network devnet `
  -node-role miner `
  -datadir .\data\devnet-01 `
  -listen :30333 `
  -node-id devnet-01 `
  -coinbase devnet-01 `
  -miner-backend cpu `
  -miner-dag-alloc go-heap `
  -http :8080
```
