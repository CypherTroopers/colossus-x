### 1-1. Clone

```powershell
git clone https://github.com/CypherTroopers/colossus-x.git
Set-Location .\colossus-x
git fetch --all
git checkout Miner-Validator-Hybrid
```
```powershell
# Download dependencies
go mod download

# Create output directory
New-Item -ItemType Directory -Force -Path .\bin | Out-Null

# Build subcommand CLI binary (mine/daemon/verify)
go build -o .\bin\colossusx.exe .\cmd\colossusx
```
#### `-node-role=miner`

```powershell
go run .\cmd\colossusx daemon `
  -mode colossusx `
  -network devnet `
  -genesis-file .\configs\devnet\genesis.json `
  -node-role miner `
  -workers 16 `
  -max-nonces 500000 `
  -block-time 500ms `
  -datadir .\data `
  -listen :30333 `
  -bootnodes 203.0.113.10:30333,203.0.113.11:30333 `
  -node-id node-01 `
  -miner-backend auto `
  -miner-dag-alloc auto
```

#### `-node-role=full`

```powershell
go run .\cmd\colossusx daemon `
  -mode colossusx `
  -network devnet `
  -genesis-file .\configs\devnet\genesis.json `
  -node-role full `
  -workers 16 `
  -max-nonces 500000 `
  -block-time 500ms `
  -datadir .\data `
  -listen :30333 `
  -bootnodes 203.0.113.10:30333,203.0.113.11:30333 `
  -node-id node-01 `
  -miner-backend auto `
  -miner-dag-alloc auto
```

#### `-node-role=light`

```powershell
go run .\cmd\colossusx daemon `
  -mode colossusx `
  -network devnet `
  -genesis-file .\configs\devnet\genesis.json `
  -node-role light `
  -workers 16 `
  -max-nonces 500000 `
  -block-time 500ms `
  -datadir .\data `
  -listen :30333 `
  -bootnodes 203.0.113.10:30333,203.0.113.11:30333 `
  -node-id node-01 `
  -miner-backend auto `
  -miner-dag-alloc auto
```
