//go:build amd64 || arm64

package pluginruntime

// getg returns the running goroutine's g pointer, an identity that is unique
// while the goroutine lives. Only the pointer is read, never a field of g.
func getg() uintptr
