//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// logLine is the start of a console log line.
var logLine = regexp.MustCompile(`^\[\d\d:\d\d:\d\d\] \[Server thread/(INFO|WARN|ERROR)\]: `)

// TestMain runs the server itself when a test starts this binary as one.
func TestMain(m *testing.M) {
	if os.Getenv("GOLEM_TEST_MAIN") == "1" {
		// The server parses the command line, so it gets the test's
		// options rather than -test.run.
		os.Args = append(os.Args[:1], strings.Fields(os.Getenv("GOLEM_TEST_ARGS"))...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// openPTY opens a pseudo-terminal, answering its master and its slave.
func openPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	conn, err := master.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var n int
	var ioctlErr error
	if err := conn.Control(func(fd uintptr) {
		if ioctlErr = unix.IoctlSetPointerInt(int(fd), unix.TIOCSPTLCK, 0); ioctlErr == nil {
			n, ioctlErr = unix.IoctlGetInt(int(fd), unix.TIOCGPTN)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if ioctlErr != nil {
		t.Fatal(ioctlErr)
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = slave.Close() })
	return master, slave
}

// termios is the slave's terminal settings.
func termios(t *testing.T, slave *os.File) unix.Termios {
	t.Helper()
	tios, err := unix.IoctlGetTermios(int(slave.Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	return *tios
}

// failToListen runs the server on a port another listener holds, with the
// input on the terminal, the output where the test puts it and NO_COLOR set
// to noColor, and answers the exit code.
func failToListen(t *testing.T, stdin, stdout *os.File, noColor string) int {
	t.Helper()
	taken, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	dir := t.TempDir()
	properties := fmt.Sprintf("server-port=%d\n", taken.Addr().(*net.TCPAddr).Port)
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(properties), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOLEM_TEST_MAIN=1", "LOG_LEVEL=", "NO_COLOR="+noColor)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stdout
	err = cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("the server ended with %v, want an exit status", err)
	}
	return exit.ExitCode()
}

// captured collects what the reader carries until it ends.
func captured(r io.Reader) (stop func() string) {
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, r)
		close(done)
	}()
	return func() string {
		<-done
		return buf.String()
	}
}

// TestPipedOutputIsPlain runs the server with its input on a terminal and
// its output piped, and holds that the output carries no prompt and no
// escape codes and that the terminal is not touched.
func TestPipedOutputIsPlain(t *testing.T) {
	_, slave := openPTY(t)
	before := termios(t, slave)
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	output := captured(r)

	code := failToListen(t, slave, w, "")
	_ = w.Close()
	if code != 1 {
		t.Errorf("exit code %d, want 1", code)
	}
	if after := termios(t, slave); after != before {
		t.Error("the terminal was left changed")
	}
	out := output()
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\r") || strings.Contains(out, "> ") {
		t.Errorf("piped output carries terminal drawing: %q", out)
	}
	if !strings.Contains(out, "[Server thread/ERROR]: ") {
		t.Errorf("output %q lacks the error", out)
	}
	// The lines before the console exists log in the same format as the rest.
	for line := range strings.Lines(out) {
		if !logLine.MatchString(line) {
			t.Errorf("line %q is not in the log format", line)
		}
	}
}

// sgrCode is an escape code that sets a colour or a decoration.
var sgrCode = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// writeServerProperties gives dir a server.properties on a free port with no
// online check, so the server runs without a client.
func writeServerProperties(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "server.properties"),
		[]byte("server-port=0\nonline-mode=false\nwhite-list=false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runServer starts this test binary as the server in dir, feeds input on its
// standard input, and answers what the run wrote to the standard output and
// the standard error.
func runServer(t *testing.T, dir, input string) (stdout, stderr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOLEM_TEST_MAIN=1", "LOG_LEVEL=", "NO_COLOR=1")
	cmd.Stdin = strings.NewReader(input)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("server: %v\nstdout:\n%s\nstderr:\n%s", err, out.String(), errOut.String())
	}
	return out.String(), errOut.String()
}

// syncBuffer is a writer exec's copy goroutine and the test can share: its
// Write runs on that goroutine while String reads on the test's.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *syncBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *syncBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

// startServer runs this test binary as the server in dir, answering what
// it writes to the combined output and a stop function that ends the run;
// the standard input is a pipe stop writes the /stop line to. A test can
// probe the server while the run is up; the run is also stopped by the
// cleanup, so a stop of the test's own leaves nothing running. Each call
// must wait for the previous run to have ended.
func startServer(t *testing.T, dir string) (log func() string, stop func()) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOLEM_TEST_MAIN=1", "LOG_LEVEL=", "NO_COLOR=1")
	pipe, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out := &syncBuffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the server: %v", err)
	}
	var once sync.Once
	stop = func() {
		once.Do(func() {
			_, _ = pipe.Write([]byte("stop\n"))
			_ = pipe.Close()
			// stop is also the cleanup, which runs after the pipe of a
			// crashed run is gone; a kill is what ends that one.
			done := make(chan struct{})
			go func() { _ = cmd.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(time.Minute):
				_ = cmd.Process.Kill()
			}
		})
	}
	t.Cleanup(stop)
	return func() string { return out.String() }, stop
}

// waitLog polls the output a running server writes until it contains the
// given text, failing the test if it does not within the wait.
func waitLog(t *testing.T, log func() string, text string) {
	t.Helper()
	deadline := time.Now().Add(time.Minute)
	for time.Now().Before(deadline) {
		if strings.Contains(log(), text) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("the server log never said %q, so far:\n%s", text, log())
}

// TestQueryListenerRunsFromTheConfig runs the server from its properties
// with enable-query on and asks it for a stat over UDP on the query.port it
// names, keeping the level id separate from its universe path, and then a
// server with the switch off, which answers nothing.
// vanilla: Main.main, QueryThreadGs4, DedicatedServer.getLevelIdName
func TestQueryListenerRunsFromTheConfig(t *testing.T) {
	t.Setenv("GOLEM_TEST_ARGS", "--universe u --world w")
	reserved, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a query port: %v", err)
	}
	port := reserved.LocalAddr().(*net.UDPAddr).Port
	if err := reserved.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	properties := fmt.Sprintf("level-type=minecraft:flat\nserver-port=0\nonline-mode=false\nwhite-list=false\nenable-query=true\nquery.port=%d\n", port)
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(properties), 0o644); err != nil {
		t.Fatal(err)
	}

	log, stop := startServer(t, dir)
	waitLog(t, log, "query running")
	if !strings.Contains(log(), `Preparing level "w"`) {
		t.Errorf("startup log does not name the level id w")
	}

	query, err := net.DialUDP("udp",
		&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)},
		&net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		t.Fatalf("dial the query port: %v", err)
	}
	defer query.Close()

	// A handshake answers the token a stat then spends.
	session := [4]byte{4, 4, 4, 4}
	if _, err := query.Write(append([]byte{0xFE, 0xFD, 0x09}, session[:]...)); err != nil {
		t.Fatalf("send a handshake: %v", err)
	}
	handshake, err := readDatagram(query)
	if err != nil {
		t.Fatalf("read the handshake answer: %v", err)
	}
	if handshake[0] != 0x09 || !bytes.Equal(handshake[1:5], session[:]) {
		t.Fatalf("handshake answer = %q, want the type and the session echoed", handshake)
	}
	token, err := strconv.Atoi(string(handshake[5 : len(handshake)-1]))
	if err != nil {
		t.Fatalf("challenge token %q is not decimal: %v", handshake[5:len(handshake)-1], err)
	}

	var stat [11]byte
	stat[0], stat[1], stat[2] = 0xFE, 0xFD, 0x00
	copy(stat[3:7], session[:])
	binary.BigEndian.PutUint32(stat[7:11], uint32(token))
	if _, err := query.Write(stat[:]); err != nil {
		t.Fatalf("send a stat: %v", err)
	}
	answer, err := readDatagram(query)
	if err != nil {
		t.Fatalf("read the stat answer: %v", err)
	}
	rest := answer[5:]
	for _, want := range []string{"A Go Minecraft Server", "SMP", "w", "0", "20"} {
		end := bytes.IndexByte(rest, 0)
		if end < 0 {
			t.Fatalf("stat answer ran out of fields: %q", answer)
		}
		if got := string(rest[:end]); got != want {
			t.Errorf("stat field = %q, want %q", got, want)
		}
		rest = rest[end+1:]
	}
	if _, err := query.Write(append(stat[:], 0, 0, 0, 0)); err != nil {
		t.Fatalf("send a full stat: %v", err)
	}
	full, err := readDatagram(query)
	if err != nil {
		t.Fatalf("read the full stat answer: %v", err)
	}
	if !bytes.Contains(full, []byte("\x00map\x00w\x00")) {
		t.Errorf("full stat does not report map w: %q", full)
	}
	stop()
	if _, err := os.Stat(filepath.Join(dir, "u", "w", "level.dat")); err != nil {
		t.Errorf("world under u/w has no level.dat: %v", err)
	}
	for _, name := range []string{"w", "world"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("unexpected world folder %s: %v", name, err)
		}
	}

	// A server with the switch off answers nothing on the port.
	properties = "level-type=minecraft:flat\nserver-port=0\nonline-mode=false\nwhite-list=false\n"
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(properties), 0o644); err != nil {
		t.Fatal(err)
	}
	log, stop = startServer(t, dir)
	waitLog(t, log, "Done (")
	if _, err := query.Write(append([]byte{0xFE, 0xFD, 0x09}, session[:]...)); err != nil {
		t.Fatalf("send a handshake: %v", err)
	}
	if n, err := readDatagram(query); err == nil {
		t.Errorf("a server with the switch off answered: %q", n)
	}
	stop()
}

// readDatagram reads one datagram within a second, the answer a query gets.
func readDatagram(conn *net.UDPConn) ([]byte, error) {
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		return nil, err
	}
	var buf [2048]byte
	n, err := conn.Read(buf[:])
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// TestServerRotatesTheLogOnRestart starts the server twice in the same
// directory: the second start gzips the first run's latest.log away and opens
// a fresh one.
func TestServerRotatesTheLogOnRestart(t *testing.T) {
	dir := t.TempDir()
	writeServerProperties(t, dir)
	runServer(t, dir, "stop\n")
	runServer(t, dir, "stop\n")

	entries, err := os.ReadDir(filepath.Join(dir, "logs"))
	if err != nil {
		t.Fatal(err)
	}
	rolled := false
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".log.gz") {
			rolled = true
		}
	}
	if !rolled {
		t.Errorf("the second start left no rolled log: %v", entries)
	}
}

// optionsServer is the server in a temp dir on a flat world, cheap to
// generate, run with args as its command line.
func optionsServer(t *testing.T, ctx context.Context, args string) (cmd *exec.Cmd, dir string, out *syncBuffer) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte("level-type=minecraft:flat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd = exec.CommandContext(ctx, os.Args[0], "-test.run=^$")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOLEM_TEST_MAIN=1", "LOG_LEVEL=", "NO_COLOR=1", "GOLEM_TEST_ARGS="+args)
	out = &syncBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	return cmd, dir, out
}

// exitCode is how cmd's run ended, failing the test if it did not exit.
func exitCode(t *testing.T, err error, out *syncBuffer) int {
	t.Helper()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	}
	t.Fatalf("the server ended with %v:\n%s", err, out.String())
	return -1
}

// TestPortAndWorldOptionsOverrideTheProperties runs the server with --port,
// --world and --pidFile, and a bare nogui before them: it listens on the
// port, writes its PID and opens the named world, while server.properties
// keeps its own values. vanilla: Main.main, DedicatedServer.initServer
func TestPortAndWorldOptionsOverrideTheProperties(t *testing.T) {
	free, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := free.Addr().(*net.TCPAddr).Port
	_ = free.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd, dir, out := optionsServer(t, ctx, fmt.Sprintf("nogui --port %d --world other --pidFile pid.txt", port))
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	for {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			_ = conn.Close()
			break
		}
		select {
		case err := <-done:
			t.Fatalf("the server ended (%v) before listening on %s:\n%s", err, addr, out.String())
		case <-time.After(20 * time.Millisecond):
		}
	}

	if pid, err := os.ReadFile(filepath.Join(dir, "pid.txt")); err != nil {
		t.Errorf("read pid.txt: %v", err)
	} else if string(pid) != strconv.Itoa(cmd.Process.Pid) {
		t.Errorf("pid.txt holds %q, want %d", pid, cmd.Process.Pid)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if code := exitCode(t, <-done, out); code != 0 {
		t.Fatalf("exit code %d, want 0:\n%s", code, out.String())
	}

	if _, err := os.Stat(filepath.Join(dir, "other", "level.dat")); err != nil {
		t.Errorf("the world named by --world has no level.dat: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "world")); err == nil {
		t.Error("the server created the level-name world as well")
	}
	props, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"level-name=world\n", "server-port=25565\n"} {
		if !strings.Contains(string(props), want) {
			t.Errorf("server.properties lacks %q, so an option was saved to it:\n%s", want, props)
		}
	}
}

// TestInitSettingsWritesThePropertiesAndQuits holds that --initSettings
// fills in server.properties and exits without opening a world.
func TestInitSettingsWritesThePropertiesAndQuits(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd, dir, out := optionsServer(t, ctx, "--initSettings")
	if code := exitCode(t, cmd.Run(), out); code != 0 {
		t.Fatalf("exit code %d, want 0:\n%s", code, out.String())
	}
	props, err := os.ReadFile(filepath.Join(dir, "server.properties"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(props), "level-name=world") {
		t.Errorf("server.properties was not filled in:\n%s", props)
	}
	if _, err := os.Stat(filepath.Join(dir, "world")); err == nil {
		t.Error("--initSettings created the world")
	}
	if !strings.Contains(out.String(), "Initialized '"+filepath.Join(dir, "server.properties")+"'") {
		t.Errorf("output lacks the Initialized line:\n%s", out.String())
	}
}

// TestAnUnknownOptionFailsTheStart holds that an option the server does not
// know ends the run with status 1 and vanilla's start failure line.
func TestAnUnknownOptionFailsTheStart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd, dir, out := optionsServer(t, ctx, "--nope")
	if code := exitCode(t, cmd.Run(), out); code != 1 {
		t.Errorf("exit code %d, want 1:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "Failed to start the minecraft server") {
		t.Errorf("output lacks the start failure:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "world")); err == nil {
		t.Error("a failed start created the world")
	}
}
