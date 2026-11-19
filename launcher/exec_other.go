//go:build !unix

package main

import (
	"errors"
	"os"
	"os/exec"
)

// runServer runs the server as a child, with the launcher's console, and
// exits with its status: there is no exec to replace the process with.
func runServer(bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		os.Exit(exit.ExitCode())
	}
	return err
}
