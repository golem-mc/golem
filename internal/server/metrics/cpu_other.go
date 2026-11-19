//go:build !unix && !windows

package metrics

import "runtime"

// processCPU has no source outside Unix here.
func processCPU() (int64, bool) { return 0, false }

func numCPU() int { return runtime.NumCPU() }
