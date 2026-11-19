package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

// setup downloads the server, the client jar the generators read (without its
// libraries or assets: client-setup fetches those) and the decompiler, and
// writes the files that let a probe log in to the server: it runs offline and
// unauthenticated, and is never meant to face the network.
func setup(f *vanilla.Fetcher, cfg config, cfrURL, cfrSHA1 string, stdout io.Writer) error {
	if err := os.MkdirAll(cfg.dir, 0o755); err != nil {
		return err
	}
	meta, err := f.VersionMeta(cfg.version)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Minecraft %s needs Java %d\n", cfg.version, meta.JavaVersion.MajorVersion)
	for _, d := range []struct {
		what string
		dl   vanilla.Download
		path string
	}{
		{"server jar", meta.Downloads.Server, cfg.serverJar()},
		{"client jar", meta.Downloads.Client, cfg.clientJar()},
		{"decompiler", vanilla.Download{URL: cfrURL, SHA1: cfrSHA1}, vanilla.Ref{Dir: cfg.dir}.CFRJar()},
	} {
		fmt.Fprintf(stdout, "%s: %s\n", d.what, d.path)
		if err := f.Fetch(d.dl.URL, d.dl.SHA1, d.path); err != nil {
			return fmt.Errorf("%s: %w", d.what, err)
		}
	}
	if err := os.WriteFile(filepath.Join(cfg.dir, "eula.txt"), []byte("eula=true\n"), 0o644); err != nil {
		return err
	}
	properties := fmt.Sprintf("server-port=%d\nonline-mode=false\nwhite-list=false\nsync-chunk-writes=false\nlevel-type=minecraft\\:flat\nspawn-protection=0\n", cfg.port)
	if err := os.WriteFile(filepath.Join(cfg.dir, "server.properties"), []byte(properties), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "reference server ready in %s (port %d)\n", cfg.dir, cfg.port)
	return nil
}

// javaIn runs java in the reference directory with the terminal attached.
func javaIn(cfg config, stdout, stderr io.Writer, args ...string) error {
	if _, err := os.Stat(cfg.serverJar()); err != nil {
		return fmt.Errorf("%s is missing, run `mise run reference:setup` first", cfg.serverJar())
	}
	cmd := exec.Command("java", args...)
	cmd.Dir = cfg.dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	return runForwarding(cmd)
}

// runForwarding runs cmd and passes SIGINT and SIGTERM on to it. The JVM is
// the last of mise, go run, this tool and java, so a signal sent to any of the
// first three (a CI cancel, `kill`) would otherwise leave it running with its
// port taken.
func runForwarding(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	forwardSignals(cmd.Process, done)
	err := cmd.Wait()
	close(done)
	return err
}

// forwardSignals sends the interrupts and terminations this process gets to
// p until done is closed.
func forwardSignals(p *os.Process, done <-chan struct{}) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer signal.Stop(signals)
		for {
			select {
			case sig := <-signals:
				_ = p.Signal(sig)
			case <-done:
				return
			}
		}
	}()
}

// runServer runs the server. The port is passed on the command line as well as
// written to server.properties, so REFERENCE_PORT picks it at run time too:
// several worktrees can each run their own reference server without editing
// the server.properties setup wrote. extra goes to the server as it is.
func runServer(cfg config, extra []string, stdout, stderr io.Writer) error {
	args := append([]string{"-Xmx1G", "-jar", filepath.Base(cfg.serverJar()), "--port", strconv.Itoa(cfg.port)}, extra...)
	return javaIn(cfg, stdout, stderr, append(args, "nogui")...)
}

// runReports runs the data generator built into the server jar, which writes
// out the registries the client keeps to itself (entity types above all) and
// every packet ID. The bundler unpacks the server's own jar and libraries on
// the way, which is what the generators read.
func runReports(cfg config, stdout, stderr io.Writer) error {
	return javaIn(cfg, stdout, stderr, "-DbundlerMainClass=net.minecraft.data.Main", "-jar", filepath.Base(cfg.serverJar()), "--reports")
}

// dump starts the server, has the probe join it and save the registries and
// tags the server sends, and stops it: the input of `gen registries`, written
// to <dir>/registries.json. It has to come from a running server, not the data
// pack in the jar (see tools/gen/registries.go).
func dump(cfg config, stdout, stderr io.Writer) error {
	if _, err := os.Stat(cfg.serverJar()); err != nil {
		return fmt.Errorf("%s is missing, run `mise run reference:setup` first", cfg.serverJar())
	}
	root, err := vanilla.RepoRoot()
	if err != nil {
		return err
	}
	out, err := filepath.Abs(filepath.Join(cfg.dir, "registries.json"))
	if err != nil {
		return err
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.port))
	// waitForPort would take any listener for the server it starts, and the
	// probe would dump that one while this one dies on "address already in use".
	if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		c.Close()
		return fmt.Errorf("something already listens on %s: stop it, or set REFERENCE_PORT to a free port", addr)
	}
	logFile, err := os.Create(filepath.Join(cfg.dir, "dump-server.log"))
	if err != nil {
		return err
	}
	defer logFile.Close()

	server := exec.Command("java", "-Xmx1G", "-jar", filepath.Base(cfg.serverJar()), "--port", strconv.Itoa(cfg.port), "nogui")
	server.Dir = cfg.dir
	server.Stdout, server.Stderr = logFile, logFile
	stdin, err := server.StdinPipe()
	if err != nil {
		return err
	}
	if err := server.Start(); err != nil {
		return err
	}
	// exited closes when the server is gone, so that waitForPort and the
	// cleanup below can both see it; exitErr is valid after that.
	exited := make(chan struct{})
	var exitErr error
	go func() { exitErr = server.Wait(); close(exited) }()
	forwardSignals(server.Process, exited)
	defer func() {
		// The console's stop saves the world and exits; kill it if it will not.
		_, _ = io.WriteString(stdin, "stop\n")
		select {
		case <-exited:
		case <-time.After(60 * time.Second):
			_ = server.Process.Kill()
			<-exited
		}
		if exitErr != nil {
			fmt.Fprintln(stderr, "the reference server stopped with:", exitErr)
		}
	}()

	fmt.Fprintf(stdout, "waiting for the server on %s (log: %s)\n", addr, logFile.Name())
	if err := waitForPort(addr, 5*time.Minute, exited, &exitErr); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// The probe writes nothing when it saw no registries, so a file left by an
	// earlier run must not pass for this run's.
	if err := os.Remove(out); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	probe := exec.CommandContext(ctx, "go", "run", "./tools/probe", addr)
	probe.Dir = root
	refDir, err := filepath.Abs(cfg.dir)
	if err != nil {
		return err
	}
	probe.Env = append(os.Environ(), "DUMP="+out, "CONFIG_ONLY=1", "REFERENCE_DIR="+refDir, "REFERENCE_VERSION="+cfg.version)
	probe.Stdout, probe.Stderr = stdout, stderr
	if err := probe.Run(); err != nil {
		return fmt.Errorf("the probe failed: %w", err)
	}
	if _, err := os.Stat(out); err != nil {
		return fmt.Errorf("the probe wrote no dump to %s: the server sent it no registries", out)
	}
	fmt.Fprintf(stdout, "wrote %s\n", out)
	return nil
}

// waitForPort polls until addr accepts a connection, the server exits (an
// error, with its exit status read from exitErr) or the time runs out.
func waitForPort(addr string, limit time.Duration, exited <-chan struct{}, exitErr *error) error {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			return fmt.Errorf("the server exited before it was ready: %v", *exitErr)
		default:
		}
		if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
			c.Close()
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("the server did not come up on %s within %s", addr, limit)
}
