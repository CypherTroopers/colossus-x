package miner

import (
	"fmt"
	"strings"

	cx "colossusx/colossusx"
)

func ValidateStrictProductionConfig(mode cx.Mode, backend BackendMode, dagAlloc string) error {
	if mode != cx.ModeStrict {
		return nil
	}
	switch backend {
	case BackendCUDA, BackendOpenCL, BackendMetal:
	default:
		return fmt.Errorf("strict mode requires explicit accelerator backend (cuda, opencl, or metal), got %q", backend)
	}
	switch strings.ToLower(strings.TrimSpace(dagAlloc)) {
	case "", "auto", "cuda-managed", "opencl-svm", "metal-shared":
		return nil
	default:
		return fmt.Errorf("strict mode requires unified/shared DAG allocation, got %q", dagAlloc)
	}
}
