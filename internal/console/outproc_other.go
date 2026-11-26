//go:build !linux && !darwin && !dragonfly && !freebsd && !netbsd && !openbsd

package console

// keepOutputProcessing does nothing: there is no output processing to restore.
func keepOutputProcessing(int) {}
