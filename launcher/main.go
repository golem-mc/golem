// Command golem-launcher runs the server with the plugins launcher.json
// names. With none it runs the signed release binary; with some it builds the
// server with them, fetching a Go toolchain first when the host has none, and
// keeps the build until the list changes. docs/plugins.md describes it.
package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"runtime/debug"
)

// serverModule is the module the launcher builds and downloads releases of.
const serverModule = "github.com/golem-mc/golem"

// version is the release this launcher came from, set by tools/release with
// -ldflags -X; a build from source answers its module version instead.
var version string

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	// execute printed the error.
	err := execute(ctx, os.Args[1:], os.Stdout, os.Stderr, stop)
	var ue usageError
	switch {
	case errors.As(err, &ue):
		os.Exit(2)
	case err != nil:
		os.Exit(1)
	}
}

// ownVersion answers the version this launcher was built as, or "" when it
// has none (a build from a checkout).
func ownVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && isPinned(info.Main.Version) {
		return info.Main.Version
	}
	return ""
}
