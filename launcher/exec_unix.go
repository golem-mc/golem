//go:build unix

package main

import (
	"os"
	"syscall"
)

// runServer replaces the launcher with the server, which keeps its process
// ID, its terminal and the signals sent to it.
func runServer(bin string, args []string) error {
	return syscall.Exec(bin, append([]string{bin}, args...), os.Environ())
}
