//go:build !amd64 && !arm64

package pluginruntime

import "runtime"

// getg returns the running goroutine's number, parsed from its stack header.
func getg() uintptr {
	var buf [32]byte
	b := buf[:runtime.Stack(buf[:], false)]
	b = b[len("goroutine "):]
	var id uintptr
	for _, c := range b {
		if c < '0' || c > '9' {
			break
		}
		id = id*10 + uintptr(c-'0')
	}
	return id
}
