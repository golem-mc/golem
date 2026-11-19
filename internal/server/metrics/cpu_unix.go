//go:build unix

package metrics

import (
	"runtime"
	"syscall"
)

// processCPU is the process's user plus system CPU time, in nanoseconds.
func processCPU() (int64, bool) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, false
	}
	return ru.Utime.Nano() + ru.Stime.Nano(), true
}

// numCPU is the cores a share of the machine's CPU is measured against.
func numCPU() int { return runtime.NumCPU() }
