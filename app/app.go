// Package app runs the server: the command line, logging, the console and
// the plugins built in with it. The repository's main package and the
// launcher's custom builds both call Main.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/console"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/plugin"
)

// Main runs the server until it stops, then exits the process on failure.
func Main() {
	// Opened first, so the earliest lines reach latest.log too.
	logs, logErr := console.OpenLogFile("logs")
	// Log lines and command replies all go to stdout, coloured only on a
	// terminal.
	setupLogging(os.Stdout, console.Colourful(os.Stdout), logs)
	if logs != nil {
		defer logs.Close()
	}
	if logErr != nil {
		slog.Warn("could not open the log files", "error", logErr)
	}
	startProfiling()
	pending, clean := crashOutput(".")
	defer logPanic(pending)
	testCrashHook()
	err := run(logs)
	clean()
	if err != nil {
		if !errors.Is(err, errStartFailed) {
			slog.Error(err.Error())
		}
		os.Exit(1)
	}
}

// run runs the server until it stops. Errors are returned rather than fatal
// so the console's deferred Close restores the terminal before exit.
func run(logs *console.LogFile) error {
	opts, err := parseOptions(os.Args[1:])
	if err != nil {
		slog.Error("Failed to start the minecraft server", "error", err)
		return errStartFailed
	}
	if opts.help {
		opts.flags.PrintDefaults()
		return nil
	}
	if opts.pidFile != "" {
		// vanilla: Main.writePidFile, the decimal PID with no newline
		if err := os.WriteFile(opts.pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
			return fmt.Errorf("write the pid file: %w", err)
		}
	}

	cfg, err := config.Load(config.FileName)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if opts.initSettings {
		// This server keeps no eula.txt, so only the properties are named.
		path, err := filepath.Abs(config.FileName)
		if err != nil {
			path = config.FileName
		}
		slog.Info("Initialized '" + path + "'")
		return nil
	}
	// After Load has written server.properties, so the overrides are never
	// saved to it.
	opts.apply(cfg)

	srv, err := server.New(cfg, plugin.Registered()...)
	if err != nil {
		return fmt.Errorf("create server: %w", err)
	}

	// Log lines go through the console's writer so that on a terminal they
	// land above the line being typed; everything goes to stdout, where
	// vanilla's SYSTEM_OUT appender puts it.
	con := console.New(os.Stdin, os.Stdout, os.Stdout, srv)
	setupLogging(con.LogWriter(), con.LogColour(), logs)
	// latest.log holds the replies as well as the log lines.
	if logs != nil {
		con.SetLogFile(logs)
	}
	// The terminal leaves raw mode on the way out, whatever ends the run.
	defer con.Close()

	// Signals are caught so a stopping server saves the players still in
	// the world; a player is otherwise written only on leaving.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	failed := make(chan error, 1)
	go func() { failed <- srv.Listen() }()
	// os.Exit skips the deferred Close, so the terminal is restored first.
	srv.StartWatchdog(".", con.LogWriter(), func(code int) { con.Close(); os.Exit(code) })

	go con.Serve()

	select {
	case err := <-failed:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		// A server stopping on its own (/stop from the console) shuts
		// down the same way.
	case <-ctx.Done():
	}
	if srv.Crashed() {
		// A tick that panicked holding a lock leaves it held, so the save
		// may never finish. 60s: vanilla's max-tick-time default
		// (DedicatedServerProperties.maxTickTime).
		go func() {
			time.Sleep(60 * time.Second)
			slog.Error("the crashed server did not finish saving in time")
			// os.Exit skips the deferred Close.
			con.Close()
			os.Exit(1)
		}()
		srv.Shutdown()
		return errors.New("server crashed")
	}
	srv.Shutdown()
	return nil
}

// setupLogging honours LOG_LEVEL; at debug every packet is logged, which is
// how a client that disconnects mid-join is tracked down.
func setupLogging(w io.Writer, colour bool, logs *console.LogFile) {
	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(strings.ToUpper(os.Getenv("LOG_LEVEL")))); err != nil {
		level = slog.LevelInfo
	}
	h := console.NewLogHandler(w, level, colour)
	if logs != nil {
		h = h.WithFile(logs)
	}
	slog.SetDefault(slog.New(h))
}
