//go:build windows

package metrics

import (
	"runtime"
	"syscall"
)

// processCPU is the process's user plus system CPU time, in nanoseconds.
func processCPU() (int64, bool) {
	h, err := syscall.GetCurrentProcess()
	if err != nil {
		return 0, false
	}
	var created, exited, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return 0, false
	}
	return filetimeNanos(kernel) + filetimeNanos(user), true
}

// filetimeNanos converts a 100ns-unit duration to nanoseconds.
// Filetime.Nanoseconds rebases onto the Unix epoch, which overflows here.
func filetimeNanos(ft syscall.Filetime) int64 {
	return (int64(ft.HighDateTime)<<32 + int64(ft.LowDateTime)) * 100
}

// numCPU is the cores a share of the machine's CPU is measured against.
func numCPU() int { return runtime.NumCPU() }
