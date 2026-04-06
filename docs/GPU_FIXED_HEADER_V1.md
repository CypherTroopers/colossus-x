# GPU Fixed-Length Header Proposal (V1)

This proposal keeps the existing wire/block header unchanged and introduces a device-oriented fixed header encoding for future GPU/OpenCL/AI-native execution.

## Goals

- Remove variable-length `Coinbase` from the device hashing input.
- Make the mining header fixed-length and trivially mappable into device buffers.
- Keep the current network/header JSON and block encoding unchanged until a coordinated consensus switch.

## Format

`GPUFixedMiningHeaderV1` is exactly 256 bytes and contains:

- Version: 4 bytes
- AlgorithmVersion: 4 bytes
- Height: 8 bytes
- ParentHash: 32 bytes
- Timestamp: 8 bytes
- Target: 32 bytes
- CoinbaseHash: 32 bytes (`sha3-256(coinbase)`)
- EpochSeed: 32 bytes
- DAGSizeBytes: 8 bytes
- DAGMerkleRoot: 32 bytes
- TxRoot: 32 bytes
- StateRoot: 32 bytes

Total: 256 bytes.

Nonce is appended separately by the mining backend/device kernel, so kernels using this format need an input buffer of at least 264 bytes.

## Current status

The helper is added in `pkg/types/header_gpu_fixed.go` as a proposal only.
It is **not consensus-active yet**.

To activate it safely, all of the following must switch together:

1. CPU mining path
2. CPU validation/reference path
3. GPU/OpenCL device kernel path
4. Solution/proof verification expectations

Until that coordinated switch happens, `BlockHeader.EncodeForMining()` remains the canonical consensus input.
