// Command reference downloads and runs the vanilla server and client of the
// version this repository implements, which the probe, the generators and the
// real-client checks are pointed at:
//
//	go run ./tools/reference setup         # server, client and decompiler jars
//	go run ./tools/reference run [ARGS]    # the server, on REFERENCE_PORT; ARGS such as --demo go to it
//	go run ./tools/reference reports       # its built-in registries, as JSON
//	go run ./tools/reference dump          # what a probe sees: registries.json
//	go run ./tools/reference client-setup  # the whole client, minus sounds
//	go run ./tools/reference client-run HOST:PORT [USERNAME]
//
// Everything lands in the reference directory (reference-server/, or
// $REFERENCE_DIR), which is not committed: the jars are Mojang's and the world is
// disposable. Downloads are resolved through Mojang's manifest and checked against
// its SHA-1. Use the client only with a copy of the game you own.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "reference:", err)
		os.Exit(1)
	}
}

// config is where the reference files live and which version they are.
type config struct {
	dir, version string
	port         int
}

func configFromEnv() (config, error) {
	c := config{dir: "reference-server", version: vanilla.VersionFromEnv(), port: 25599}
	if v := os.Getenv("REFERENCE_DIR"); v != "" {
		c.dir = v
	}
	if v := os.Getenv("REFERENCE_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return c, fmt.Errorf("REFERENCE_PORT %q is not a port", v)
		}
		c.port = p
	}
	return c, nil
}

func (c config) serverJar() string {
	return filepath.Join(c.dir, "minecraft_server."+c.version+".jar")
}

func (c config) clientJar() string {
	return filepath.Join(c.dir, "minecraft_client."+c.version+".jar")
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: reference setup|run|reports|dump|client-setup|client-run")
	}
	cfg, err := configFromEnv()
	if err != nil {
		return err
	}
	switch args[0] {
	case "setup":
		return setup(vanilla.NewFetcher(stderr), cfg, vanilla.CFRURL, vanilla.CFRSHA1, stdout)
	case "run":
		return runServer(cfg, args[1:], stdout, stderr)
	case "reports":
		return runReports(cfg, stdout, stderr)
	case "dump":
		return dump(cfg, stdout, stderr)
	case "client-setup":
		return clientSetup(vanilla.NewFetcher(stderr), cfg, stdout)
	case "client-run":
		if len(args) < 2 {
			return fmt.Errorf("usage: reference client-run HOST:PORT [USERNAME]")
		}
		username := "Tester"
		if len(args) > 2 {
			username = args[2]
		}
		return clientRun(cfg, args[1], username)
	}
	return fmt.Errorf("no command %q", args[0])
}
