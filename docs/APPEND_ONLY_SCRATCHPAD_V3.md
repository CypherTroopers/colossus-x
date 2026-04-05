# COLOSSUS-X Append-Only Scratchpad v3

This branch adds a GPU/AI-native scratchpad-oriented core without removing the existing v2 path.

## Goals

- Keep a single giant contiguous memory image.
- Grow by appending suffix cells instead of swapping full DAG images.
- Preserve Merkle-root-backed `ColossusXSolution` workflows.
- Bias the design toward GPU and AI-native unified/shared-memory hardware.
- Keep CPU participation possible, but structurally weaker.

## Core constants

- Base scratchpad size: 32 GiB
- Growth per cycle: 48 MiB
- Cycle length: 7200 blocks
- Growth window: last 300 blocks of the cycle
- Node/cell size: 256 B
- Tile size: 4096 B
- Reads per hash: 64
- Algorithm version: 3

## Files added

- `colossusx/scratchpad_v3.go`
  - append-only growth schedule helpers
  - in-memory Merkle sidecar helper
  - append-only scratchpad generator
- `colossusx/hash_scratchpad_v3.go`
  - matrix-oriented v3 scratchpad hash round
- `colossusx/solution_scratchpad_v3.go`
  - build / verify helpers for v3 solutions
- `colossusx/scratchpad_v3_test.go`
  - schedule, hash, proof tests

## Notes

This branch intentionally keeps the existing v2 code paths untouched. The new files provide the v3 core needed to wire the validator / node / GPU backends in a later integration step.
