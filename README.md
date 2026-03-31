# Colossus-X

Colossus-X は Go で実装された PoW マイナー／ノード実装です。CLI は主に次の 3 サブコマンドを持ちます。  
- `mine`（デフォルト）: マイニング実行  
- `daemon`（`node` エイリアス）: ノード本体を起動  
- `verify`: ヘッダー／ブロック JSON の PoW 検証

---

## 1. セットアップ（`git clone` から実行まで）

### 1-1. 取得

```bash
git clone https://github.com/CypherTroopers/colossus-x.git
cd colossus-x
```

### 1-2. 必須ツール

- Go **1.23.x**（`go.mod` の `go 1.23.0` に準拠）
- `make`（任意: 便利ターゲット利用時）

確認例:

```bash
go version
make --version
```

### 1-3. 依存解決とビルド

```bash
# 依存取得
go mod download

# バイナリビルド
mkdir -p bin
go build -o bin/colossusx .
```

`make` を使う場合:

```bash
make deps
make build
```

### 1-4. 実行ファイル

- ルートの `main.go` はマイナー CLI 実装（`colossusx [mine flags]`）
- `cmd/colossusx/main.go` はサブコマンド対応 CLI 実装（`mine/daemon/verify`）

実運用（ノード）では **`daemon` サブコマンドを使う実行形** を利用してください。

---

## 2. 本番ノード起動コマンド（`daemon`）

最小例:

```bash
go run ./cmd/colossusx daemon \
  -mode strict \
  -network mainnet \
  -datadir ./data \
  -listen :30333 \
  -node-id node-01 \
  -bootnodes 203.0.113.10:30333,203.0.113.11:30333 \
  -mine=true \
  -workers 16 \
  -miner-backend opencl \
  -miner-dag-alloc auto
```

ビルド済みバイナリ利用例:

```bash
./bin/colossusx daemon -mode strict -network mainnet -datadir ./data -listen :30333
```

> 注: `mode` は現状 `strict` のみサポートです。

---

## 3. CLI フラグ一覧（詳細）

## 3-1. `daemon` フラグ

| フラグ | デフォルト | 説明 |
|---|---:|---|
| `-mode` | `strict` | チェーンモード。現状 strict のみ。 |
| `-network` | `devnet` | ネットワーク識別子（チェーンID相当）。 |
| `-initial-dag-mib` | `1024` | 初期 DAG サイズ (MiB)。 |
| `-dag-mib` | `0` | `-initial-dag-mib` の非推奨エイリアス。0 以外なら上書き。 |
| `-dag-growth-mib-per-epoch` | `8` | エポックごとの DAG 増加量 (MiB)。 |
| `-mine` | `true` | ローカルマイニング有効化。 |
| `-no-mine` | `false` | ローカルマイニング無効化（指定時は `-mine=false` 扱い）。 |
| `-workers` | `runtime.NumCPU()` | マイニング worker 数。 |
| `-max-nonces` | `500000` | 1 ブロックテンプレートあたり探索 nonce 上限。 |
| `-block-time` | `500ms` | 採掘ブロック間隔。 |
| `-genesis-message` | `colossusx devnet genesis` | Genesis メッセージ文字列。 |
| `-datadir` | `./data` | ノード永続データディレクトリ。 |
| `-listen` | `:30333` | TCP リッスンアドレス。 |
| `-bootnodes` | `""` | カンマ区切りブートノード。 |
| `-node-id` | `""` | 安定ノード識別子。 |
| `-target` | `0fffffffff...ffff` | マイニングターゲット（hex）。 |
| `-miner-backend` | `opencl` | `cuda/opencl/metal/cpu/unified/gpu`。 |
| `-miner-dag-alloc` | `auto` | `auto/go-heap/pinned-host/cuda-managed/opencl-svm/metal-shared`。 |

`strict` 本番相当では `backend` と `dag-alloc` の組み合わせに制約があります（無効組み合わせはエラー）。

## 3-2. `mine` フラグ（デフォルトコマンド）

| フラグ | デフォルト | 説明 |
|---|---:|---|
| `-mode` | `strict` | 動作モード（strict のみ）。 |
| `-backend` | `opencl` | `cuda/opencl/metal/cpu/unified/gpu`。 |
| `-dag-alloc` | `auto` | DAG アロケーション戦略。 |
| `-initial-dag-mib` | `1024` | 初期 DAG サイズ (MiB)。 |
| `-dag-mib` | `0` | `-initial-dag-mib` 非推奨エイリアス。 |
| `-dag-growth-mib-per-epoch` | `8` | DAG 増分 (MiB/epoch)。 |
| `-workers` | `runtime.NumCPU()` | worker 数。 |
| `-header` | 固定テスト値 | マイニング対象ヘッダー hex。 |
| `-epoch-seed` | 固定テスト値 | エポックシード hex。 |
| `-target` | `00ffff...ffff` | 32-byte big-endian target。 |
| `-start-nonce` | `0` | 探索開始 nonce。 |
| `-max-nonces` | `200000` | 0 なら無制限。 |
| `-bench` | `false` | true でベンチモード（採掘結果判定なし）。 |

## 3-3. `verify` フラグ

| フラグ | デフォルト | 説明 |
|---|---:|---|
| `-mode` | `strict` | 検証モード（strict のみ）。 |
| `-header` | `""` | `types.BlockHeader` JSON パス。 |
| `-block` | `""` | `types.Block` JSON パス。 |
| `-initial-dag-mib` | `1024` | DAG 初期サイズ。 |
| `-dag-mib` | `0` | `-initial-dag-mib` 非推奨エイリアス。 |
| `-dag-growth-mib-per-epoch` | `8` | DAG 増分。 |

`verify` は `--header` と `--block` の同時指定不可、どちらか片方を必須とします。

---

## 4. GPU/アクセラレータ関連の補助情報

バックエンド別の実行条件（要点）:

- `opencl` / `gpu`
  - OpenCL 実行系を使用。
  - `cgo && opencl` ビルドでは `-lOpenCL` リンク。
- `cuda`
  - `cuda` ビルドタグ有効時の CUDA 実装を利用。
  - `cgo && cuda` パスでは `-lcudart` リンク。
- `metal`
  - Metal 用バックエンド。strict では `metal-shared` DAG 要求。
- `unified`, `cpu`
  - CPU / 共有メモリ寄りのパス。

環境依存のため、まずは `-backend cpu` または `-backend unified` で動作確認し、次に GPU バックエンドへ移行するのが安全です。

---

## 5. テスト用コマンド一覧

### 5-1. 全体テスト

```bash
go test ./...
```

### 5-2. 主要パッケージ個別

```bash
go test ./colossusx -v
go test ./pkg/node -v
go test ./pkg/consensus -v
go test ./pkg/chain -v
go test ./pkg/types -v
```

### 5-3. CLI の軽量実行確認

```bash
# ヘルプ
./bin/colossusx -h

# 小規模ベンチ（unified）
go run . -bench -backend unified -dag-mib 1 -max-nonces 1000 -workers 2

# 小規模ベンチ（cpu）
go run . -bench -backend cpu -dag-mib 1 -max-nonces 1000 -workers 2

# 低難易度マイニング動作確認

go run . -backend unified -dag-mib 1 -workers 2 -max-nonces 10 -target ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff
```

### 5-4. Makefile ターゲット経由

```bash
make run-help
make bench-small
make bench-cpu
make mine-easy
```

---

## 6. 運用メモ

- 本番運用前に `daemon` 起動で `runtime_init` / `execution` 表示を確認し、期待したバックエンドで動作しているか検証してください。
- strict モードは不正な DAG 戦略組み合わせを拒否するため、`-miner-backend` と `-miner-dag-alloc` を必ず整合させてください。
