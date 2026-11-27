// Command loadtest drives a server with bot clients and measures it, to
// compare golem with the vanilla server under the same load.
//
//	loadtest run -server golem -golem ./golem -scenario players-50 -out results/...
//	loadtest run -server vanilla -jar server.jar -java .../bin/java -scenario players-50 -out ...
//	loadtest scenarios
//
// A run starts the server in a fresh directory pinned to -server-cpus (the
// bots and this process are pinned by whoever starts it), joins an opped
// observer that subscribes to the F3 TPS chart's samples (one per tick, the
// same source on both servers), builds the scenario through RCON, warms up,
// then measures: tick time, the server's CPU and RSS, and the bytes the
// clients receive. -profile also captures pprof (golem) or a JFR
// recording (vanilla) during the first 30 s of the scenario's main window.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type options struct {
	server, scenario, out, workdir string
	golem, jar, java, heap         string
	serverCPUs                     string
	port, rconPort, pprofPort      int // 0 until run picks a free one
	seed                           string
	view, sim                      int
	warmup, measure                time.Duration
	profile                        bool
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: loadtest run|scenarios [flags]")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "scenarios":
		names := make([]string, 0, len(scenarios))
		for n := range scenarios {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Println(n)
		}
	case "run":
		var o options
		fs := flag.NewFlagSet("run", flag.ExitOnError)
		fs.StringVar(&o.server, "server", "golem", "golem or vanilla")
		fs.StringVar(&o.scenario, "scenario", "idle", "scenario name (loadtest scenarios)")
		fs.StringVar(&o.out, "out", "results", "directory for this run's results")
		fs.StringVar(&o.workdir, "workdir", "", "server directory, wiped before and after (default a fresh temporary directory)")
		fs.IntVar(&o.port, "port", 0, "game port (default a free one)")
		fs.IntVar(&o.rconPort, "rcon-port", 0, "RCON port (default a free one)")
		fs.StringVar(&o.golem, "golem", "golem", "golem binary")
		fs.StringVar(&o.jar, "jar", "minecraft_server.jar", "vanilla server jar")
		fs.StringVar(&o.java, "java", "java", "java binary (its jcmd is used for JFR)")
		fs.StringVar(&o.heap, "heap", "4G", "vanilla heap, -Xms and -Xmx")
		fs.StringVar(&o.serverCPUs, "server-cpus", "0-7", "taskset CPU list for the server")
		fs.StringVar(&o.seed, "seed", "8675309", "level seed")
		fs.IntVar(&o.view, "view", 10, "view distance, server and bots")
		fs.IntVar(&o.sim, "sim", 10, "simulation distance")
		fs.DurationVar(&o.warmup, "warmup", 60*time.Second, "warm-up under the scenario's load")
		fs.DurationVar(&o.measure, "measure", 60*time.Second, "measured window")
		fs.BoolVar(&o.profile, "profile", false, "capture pprof or JFR")
		_ = fs.Parse(os.Args[2:])
		if err := run(&o); err != nil {
			fmt.Fprintln(os.Stderr, "loadtest:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "unknown command", os.Args[1])
		os.Exit(2)
	}
}

func run(o *options) error {
	sc, ok := scenarios[o.scenario]
	if !ok {
		return fmt.Errorf("unknown scenario %q", o.scenario)
	}
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return err
	}
	if err := o.pickPorts(); err != nil {
		return err
	}
	if o.workdir == "" {
		dir, err := os.MkdirTemp("", "loadtest-world-")
		if err != nil {
			return err
		}
		o.workdir = dir
	}
	srv, err := startServer(o, sc.levelType, o.out)
	if err != nil {
		return err
	}
	e := &env{o: o, srv: srv, sum: &summary{Server: o.server, Scenario: o.scenario, Windows: map[string]*window{}, Extra: map[string]any{}}}
	defer func() {
		for _, b := range e.bots {
			b.close()
		}
		srv.stop(e.rc)
		_ = os.RemoveAll(o.workdir)
	}()
	rc, err := srv.waitRCON(10 * time.Minute)
	if err != nil {
		return err
	}
	e.rc = rc
	e.sum.StartupSeconds = time.Since(srv.start).Seconds()
	e.logf("server up in %.1fs", e.sum.StartupSeconds)
	go e.sampleProc()
	runErr := sc.run(e)
	if runErr != nil {
		e.sum.Error = runErr.Error()
	}
	e.botHealth()
	if b := e.observer; b != nil {
		b.mu.Lock()
		ids := map[string]int{}
		for id, n := range b.idCounts {
			ids[fmt.Sprintf("0x%02X", id)] = n
		}
		b.mu.Unlock()
		e.sum.Extra["observer_packets_by_id"] = ids
	}
	raw, _ := json.MarshalIndent(e.sum, "", "  ")
	if err := os.WriteFile(filepath.Join(o.out, "summary.json"), raw, 0o644); err != nil {
		return err
	}
	e.logf("done: %s", filepath.Join(o.out, "summary.json"))
	return runErr
}

// pickPorts fills the unset game, RCON and pprof ports from one freePorts
// call, so no two are the same and none equals a port set by flag.
func (o *options) pickPorts() error {
	for {
		ports, err := freePorts(3)
		if err != nil {
			return err
		}
		clash := false
		for _, p := range ports {
			clash = clash || p == o.port || p == o.rconPort
		}
		if clash {
			continue
		}
		for i, p := range []*int{&o.port, &o.rconPort, &o.pprofPort} {
			if *p == 0 {
				*p = ports[i]
			}
		}
		return nil
	}
}
