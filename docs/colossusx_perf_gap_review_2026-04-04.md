# ColossusX v2 パフォーマンス改善チェック（2026-04-04）

このメモは、提示された 4 つの懸念点と 4 つの改善優先事項が現行コードに実装済みかを確認した結果です。

## 結論（要約）

- **「全て改善済み」ではありません。**
- 4 つの優先事項のうち、
  - **実装済み**: 1) per-round SHA3-512 廃止（FNV fold化 + 最終1回SHA3-512）、2) prior epoch 中の DAG/Merkle 非同期プリビルド、3) audit cell 数削減（32→16）
  - **部分対応**: 4) SHA3→BLAKE3 への広範囲置換

## 1) Merkle tree 構築ボトルネック

- `dagMerkleRootStreaming` があり、全葉配列を保持しない形でルート構築する実装です。
- daemon 側では `scheduleNextEpochPrewarm` と `PrewarmMiningDAGAtHeight` により、前 epoch 採掘中に次 epoch の DAG/Merkle をバックグラウンド構築する経路があります。

判定: **改善済み（ストリーミング + 非同期プリビルド経路あり）**

## 2) 128 ラウンドで毎回 SHA3-512

- `colossusXRoundFold` で 16 ワード FNV fold のみを 128 ラウンド実行し、ループ後に `sha3.Sum512` を 1 回だけ実行。
- `ColossusXTraceHash` / `VerifyColossusXSolution` ともに同じ手順へ揃えています。

判定: **改善済み（FNV foldのみ + ループ後に単発SHA3-512）**

## 3) audit cells 32 個の後段遅延

- 定数 `ColossusXAuditCellCount = 16` へ変更済みです。
- solution 構築・検証の双方でこの定数を前提に audit proof を処理しています。

判定: **改善済み（16 へ削減）**

## 4) DAG 生成時の Blake3 keyed expansion

- `colossusXNodeInto` 内で `blake3.NewKeyed(mix[:32])` を使い、`Digest().Read(out)` で node 出力（nodeSize 分）を展開しています。
- 提示された「64→256 バイト展開の追加計算コスト」に相当する構造は残っています（実際の出力長は `spec.NodeSize` に依存）。

判定: **部分対応（DAG生成の主要反復はBLAKE3化、keyed expansion自体は継続）**

## 優先改善項目との対応表

1. per-round SHA3-512 をやめ FNV fold + 最終 1 回 SHA3-512:
   - **実装済み**
2. Merkle tree を前 epoch 採掘中に非同期構築:
   - **実装済み（daemon prewarm）**
3. audit cells を 32 から 8–16 へ削減:
   - **実装済み（16）**
4. seed 以外 SHA3-512 を BLAKE3 に置換:
   - **部分対応（DAG生成系はBLAKE3化。ハッシュ本体の最終圧縮などはSHA3を維持）**

## 補足

- 既存の `BuildMerkleMultiProofFromAccessor` / compact solution など、proof 伝送量や計算重複を下げる工夫はあります。
- ただし、提示された 4 優先事項を「完了」と判定できる状態ではありません。

## 今回変更に対する完了判定（質問への直接回答）

- **(2) per-round SHA3-512 問題**: **完了**  
  128 ラウンド中は FNV fold のみで、SHA3-512 はループ後 1 回です。
- **(3) audit cells 32 問題**: **完了**  
  監査セル数は `16` へ削減済みです。
- **(4) DAG 生成時の Blake3 keyed expansion 追加計算問題**: **未完了（部分対応）**  
  DAG 生成内の反復 `SHA3-512` は BLAKE3 化しましたが、`blake3.NewKeyed(...).Digest().Read(out)` の keyed expansion 自体は残っています。

### 優先修正項目ごとの状態

- Replace per-round SHA3-512 with FNV-fold + single final SHA3-512: **完了**
- Pre-build the Merkle tree asynchronously during prior epoch mining: **完了（daemon prewarm 経路）**
- Reduce audit cells from 32 to 8–16: **完了（16）**
- Swap SHA3-512 to Blake3 except seed derivation / keep SHA3-256 where needed: **部分対応**
