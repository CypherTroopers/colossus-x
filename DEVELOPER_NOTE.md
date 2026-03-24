# Strict mode production note

## Guarantees
- strict mode uses algorithm version 2.
- strict mode requires unified/shared DAG allocation.
- strict mode rejects CPU, legacy `gpu`, and legacy `unified` mining paths.
- strict mode requires accelerator-backed device execution and fails loudly if a backend would fall back.

## Supported production allocators
- `cuda-managed`
- `opencl-svm`
- `metal-shared`
- `auto` only when it resolves to one of the allocators above

## Backend matrix
- CUDA: explicit production backend, strict allocator must be `cuda-managed`
- OpenCL: explicit production backend, strict allocator must be `opencl-svm`
- Metal: explicit production backend for Apple Silicon shared-memory deployments

## Remaining TODOs
- replace the placeholder Metal runtime/allocation shims with native MTLDevice/MTLBuffer bindings on darwin builds
- wire strict-v2 kernels into real CUDA/OpenCL/Metal kernels for production acceleration beyond the deterministic reference path used by tests
