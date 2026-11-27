package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// serverProc is a server under test, started in a fresh directory and
// pinned to its CPUs.
type serverProc struct {
	kind     string // "golem" or "vanilla"
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	dir      string
	out      string
	pprof    string // golem's pprof address, empty when not profiling
	jcmd     string
	rconAddr string
	start    time.Time
	exit     chan struct{} // closed when the process has exited
}

// properties both servers start with; the scenario adds its world type.
func properties(levelType string, o *options) map[string]string {
	// Vanilla's flat preset places villages, which golem does not:
	// off, so the flat scenarios hold only what they build.
	structures := strconv.FormatBool(levelType != flat)
	return map[string]string{
		"server-port":                   strconv.Itoa(o.port),
		"online-mode":                   "false",
		"white-list":                    "false",
		"level-seed":                    o.seed,
		"level-type":                    levelType,
		"view-distance":                 strconv.Itoa(o.view),
		"simulation-distance":           strconv.Itoa(o.sim),
		"difficulty":                    "normal",
		"gamemode":                      "survival",
		"enable-rcon":                   "true",
		"rcon.port":                     strconv.Itoa(o.rconPort),
		"rcon.password":                 "bench",
		"pause-when-empty-seconds":      "0",
		"max-players":                   "300",
		"spawn-protection":              "0",
		"allow-flight":                  "true",
		"max-tick-time":                 "-1",
		"sync-chunk-writes":             "true",
		"network-compression-threshold": "256",
		"generate-structures":           structures,
		"enforce-secure-profile":        "false",
		"motd":                          "loadtest",
	}
}

// portsFree waits until nothing answers on the game and RCON ports, so a
// run never talks to the server of the run before it.
func portsFree(o *options, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		busy := false
		for _, addr := range []string{o.gameAddr(), o.rconAddr()} {
			if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
				c.Close()
				busy = true
			}
		}
		if !busy {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("a server is still listening on %d or %d", o.port, o.rconPort)
}

// freePorts returns n distinct TCP ports nothing listens on at the moment. All
// listeners stay open until every port is read, so none repeats; another
// process may still take one before the server binds it.
func freePorts(n int) ([]int, error) {
	ports := make([]int, 0, n)
	for len(ports) < n {
		ls := make([]net.Listener, 0, n)
		var err error
		for range n {
			var l net.Listener
			if l, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
				break
			}
			ls = append(ls, l)
		}
		for _, l := range ls {
			ports = append(ports, l.Addr().(*net.TCPAddr).Port)
			l.Close()
		}
		if err != nil {
			return nil, err
		}
	}
	return ports[:n], nil
}

func (o *options) gameAddr() string { return "127.0.0.1:" + strconv.Itoa(o.port) }
func (o *options) rconAddr() string { return "127.0.0.1:" + strconv.Itoa(o.rconPort) }

func startServer(o *options, levelType, out string) (*serverProc, error) {
	if err := portsFree(o, 2*time.Minute); err != nil {
		return nil, err
	}
	dir := o.workdir
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var props strings.Builder
	for k, v := range properties(levelType, o) {
		fmt.Fprintf(&props, "%s=%s\n", k, v)
	}
	if err := os.WriteFile(filepath.Join(dir, "server.properties"), []byte(props.String()), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "eula.txt"), []byte("eula=true\n"), 0o644); err != nil {
		return nil, err
	}
	args := []string{"-c", o.serverCPUs}
	s := &serverProc{kind: o.server, rconAddr: o.rconAddr(), dir: dir, out: out, exit: make(chan struct{})}
	switch o.server {
	case "golem":
		args = append(args, o.golem)
	case "vanilla":
		args = append(args, o.java, "-Xms"+o.heap, "-Xmx"+o.heap)
		if o.profile {
			args = append(args, "-XX:+FlightRecorder")
		}
		args = append(args, "-jar", o.jar, "nogui")
		s.jcmd = filepath.Join(filepath.Dir(o.java), "jcmd")
	default:
		return nil, fmt.Errorf("unknown server %q", o.server)
	}
	cmd := exec.Command("taskset", args...)
	cmd.Dir = dir
	if o.server == "golem" && o.profile {
		s.pprof = "127.0.0.1:" + strconv.Itoa(o.pprofPort)
		cmd.Env = append(os.Environ(), "PPROF_ADDR="+s.pprof)
	}
	logf, err := os.Create(filepath.Join(out, "server.log"))
	if err != nil {
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = logf, logf
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	s.cmd, s.stdin = cmd, stdin
	s.start = time.Now()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() { _ = cmd.Wait(); logf.Close(); close(s.exit) }()
	return s, nil
}

// waitRCON polls until the remote console answers, which both servers open
// once the world is loaded.
func (s *serverProc) waitRCON(timeout time.Duration) (*rcon, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-s.exit:
			return nil, fmt.Errorf("server exited: %v", s.cmd.ProcessState)
		default:
		}
		if c, err := dialRCON(s.rconAddr, "bench"); err == nil {
			return c, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return nil, fmt.Errorf("no rcon within %s", timeout)
}

func (s *serverProc) stop(rc *rcon) {
	if rc != nil {
		go func() { _, _ = rc.Run("stop") }()
	}
	select {
	case <-s.exit:
	case <-time.After(90 * time.Second):
		_ = s.cmd.Process.Kill()
		<-s.exit
	}
}

func (s *serverProc) pid() int { return s.cmd.Process.Pid }

// procSample is the server process's CPU time and resident memory.
type procSample struct {
	wall time.Time
	cpu  float64 // seconds of user and system time
	rss  float64 // MiB
}

func readProc(pid int) (procSample, error) {
	ps := procSample{wall: time.Now()}
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ps, err
	}
	// Fields after the parenthesised command; utime and stime are 14 and 15.
	rest := string(stat[strings.LastIndexByte(string(stat), ')')+2:])
	f := strings.Fields(rest)
	ut, _ := strconv.ParseFloat(f[11], 64)
	st, _ := strconv.ParseFloat(f[12], 64)
	ps.cpu = (ut + st) / 100 // CLK_TCK
	status, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return ps, err
	}
	defer status.Close()
	sc := bufio.NewScanner(status)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "VmRSS:"); ok {
			kb, _ := strconv.ParseFloat(strings.Fields(v)[0], 64)
			ps.rss = kb / 1024
		}
	}
	return ps, nil
}

// fetch saves a pprof endpoint's response to path.
func fetch(url, path string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("fetch %s: %s", url, resp.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}
