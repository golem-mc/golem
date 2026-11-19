package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// env is one run: the server, its console, the observer and the bots.
type env struct {
	o   *options
	srv *serverProc
	rc  *rcon
	sum *summary

	observer *bot
	bots     []*bot

	mu      sync.Mutex
	samples []tickSample
	proc    []procSample
}

type tickSample struct {
	at   time.Time
	tick int64 // ns spent in the tick method
	full int64 // ns of the whole cycle
}

// profileTickWindow selects observer receipt times in (start, end]. For CPU
// requests this approximates the sampling interval, including HTTP overhead.
func profileTickWindow(samples []tickSample, start, end time.Time) []float64 {
	var ticks []float64
	for _, s := range samples {
		if s.at.After(start) && !s.at.After(end) {
			ticks = append(ticks, float64(s.tick)/1e6)
		}
	}
	return ticks
}

type summary struct {
	Server         string             `json:"server"`
	Scenario       string             `json:"scenario"`
	StartupSeconds float64            `json:"startup_s"`
	Windows        map[string]*window `json:"windows"`
	Extra          map[string]any     `json:"extra"`
	BotsLost       int                `json:"bots_lost"`
	BotErrors      []string           `json:"bot_errors,omitempty"`
	Error          string             `json:"error,omitempty"`
}

// window is what one measured interval saw.
type window struct {
	Seconds     float64        `json:"seconds"`
	Ticks       int            `json:"ticks"`
	TPS         float64        `json:"tps"`
	MSPT        stats          `json:"mspt"`
	TickQuery   []tickQuery    `json:"tick_query,omitempty"`
	CPUCores    float64        `json:"cpu_cores"` // CPU seconds per wall second
	RSSMiBMean  float64        `json:"rss_mib_mean"`
	RSSMiBMax   float64        `json:"rss_mib_max"`
	BytesIn     int64          `json:"bytes_in"` // wire bytes all clients received
	BytesPerSec float64        `json:"bytes_per_s"`
	Chunks      int64          `json:"chunks"` // chunk data packets all clients received
	ChunksPerS  float64        `json:"chunks_per_s"`
	ObserverPPS float64        `json:"observer_packets_per_s"`
	Extra       map[string]any `json:"extra,omitempty"`
}

type stats struct {
	Mean float64 `json:"mean"`
	P50  float64 `json:"p50"`
	P95  float64 `json:"p95"`
	P99  float64 `json:"p99"`
	Max  float64 `json:"max"`
}

type tickQuery struct {
	Mean, P50, P95, P99 float64
}

func statsOf(v []float64) stats {
	if len(v) == 0 {
		return stats{}
	}
	s := slices.Clone(v)
	slices.Sort(s)
	var sum float64
	for _, x := range s {
		sum += x
	}
	q := func(p float64) float64 { return s[min(len(s)-1, int(math.Ceil(p*float64(len(s))))-1)] }
	return stats{Mean: sum / float64(len(s)), P50: q(0.5), P95: q(0.95), P99: q(0.99), Max: s[len(s)-1]}
}

func (e *env) logf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s [%s %s] %s\n", time.Now().Format("15:04:05"), e.o.server, e.o.scenario, fmt.Sprintf(format, args...))
}

func (e *env) sampleProc() {
	for {
		ps, err := readProc(e.srv.pid())
		if err != nil {
			return
		}
		e.mu.Lock()
		e.proc = append(e.proc, ps)
		e.mu.Unlock()
		time.Sleep(time.Second)
	}
}

// waitIdle waits until the server has used under cores of CPU for 5 s, or
// limit, and returns how long it waited.
func (e *env) waitIdle(cores float64, limit time.Duration) time.Duration {
	start := time.Now()
	for time.Since(start) < limit {
		time.Sleep(5 * time.Second)
		e.mu.Lock()
		n := len(e.proc)
		var a, b procSample
		if n >= 6 {
			a, b = e.proc[n-6], e.proc[n-1]
		}
		e.mu.Unlock()
		if n >= 6 && (b.cpu-a.cpu)/b.wall.Sub(a.wall).Seconds() < cores {
			break
		}
	}
	return time.Since(start)
}

// cmd runs console commands, failing the run on a broken console.
func (e *env) cmd(commands ...string) {
	e.rc.MustRun(commands...)
}

// cmdOut runs one command and returns its reply.
func (e *env) cmdOut(command string) string {
	out, err := e.rc.Run(command)
	if err != nil {
		panic(err)
	}
	return out
}

// joinObserver joins the opped observer, which records every tick's time,
// and moves it to x, y, z unless x is NaN.
func (e *env) joinObserver(gamemode string, x, y, z float64) error {
	b, err := dialBot(e.o.gameAddr(), "Observer", int8(e.o.view), idle{})
	if err != nil {
		return err
	}
	b.idCounts = map[int32]int{}
	b.onSample = func(s []int64) {
		if len(s) < 2 {
			return
		}
		e.mu.Lock()
		e.samples = append(e.samples, tickSample{at: time.Now(), tick: s[1], full: s[0]})
		e.mu.Unlock()
	}
	go b.run()
	e.observer = b
	if err := b.waitPlaced(time.Minute); err != nil {
		return err
	}
	e.setup("op Observer", "gamemode "+gamemode+" Observer")
	if !math.IsNaN(x) {
		e.setup(fmt.Sprintf("tp Observer %g %g %g", x, y, z))
	}
	time.Sleep(time.Second)
	// The F3 TPS chart's samples, sent to operators only.
	b.send(protocol.IDPlayDebugSubscriptionRequest, func(w *protocol.Buffer) {
		w.PutVarInt(1)
		w.PutVarInt(protocol.DebugSubscriptionTickTime)
	})
	return nil
}

// spawnBots joins n bots at rate per second and returns them once all are in.
func (e *env) spawnBots(n int, prefix string, rate float64, view int8, behave func(i int) behaviour) ([]*bot, error) {
	out := make([]*bot, n)
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Go(func() {
			b, err := dialBot(e.o.gameAddr(), fmt.Sprintf("%s%d", prefix, i), view, behave(i))
			if err != nil {
				errs <- err
				return
			}
			out[i] = b
			go b.run()
			if err := b.waitPlaced(2 * time.Minute); err != nil {
				errs <- err
			}
		})
		time.Sleep(time.Duration(float64(time.Second) / rate))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return nil, err
	}
	e.bots = append(e.bots, out...)
	return out, nil
}

var (
	reMean = regexp.MustCompile(`Average time per tick: ([\d.]+) ?ms`)
	rePct  = regexp.MustCompile(`P50: ([\d.]+) ?ms,? P95: ([\d.]+) ?ms,? P99: ([\d.]+) ?ms`)
)

func parseTickQuery(s string) (tickQuery, bool) {
	m, p := reMean.FindStringSubmatch(s), rePct.FindStringSubmatch(s)
	if m == nil || p == nil {
		return tickQuery{}, false
	}
	f := func(x string) float64 { v, _ := strconv.ParseFloat(x, 64); return v }
	return tickQuery{Mean: f(m[1]), P50: f(p[1]), P95: f(p[2]), P99: f(p[3])}, true
}

// measureOpts are the side captures of a window.
type measureOpts struct {
	profile         bool          // pprof or JFR over the window's first 30 s
	profileDuration time.Duration // zero uses the first 30 s
	during          func(stop <-chan struct{})
}

// measure records a window of d under label.
func (e *env) measure(label string, d time.Duration, mo measureOpts) *window {
	e.logf("measure %s for %s", label, d)
	all := append([]*bot{}, e.bots...)
	if e.observer != nil {
		all = append(all, e.observer)
	}
	bytes0, chunks0 := make([]int64, len(all)), make([]int64, len(all))
	for i, b := range all {
		bytes0[i], chunks0[i] = b.bytesIn.Load(), b.chunksIn.Load()
	}
	var obs0 int64
	if e.observer != nil {
		obs0 = e.observer.packets.Load()
	}
	start := time.Now()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var queries []tickQuery
	wg.Go(func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if out, err := e.rc.Run("tick query"); err == nil {
					if q, ok := parseTickQuery(out); ok {
						queries = append(queries, q)
					} else if len(queries) == 0 {
						e.logf("unparsed tick query: %q", out)
					}
				}
			}
		}
	})
	var cpuStart, cpuEnd time.Time
	if mo.profile && e.o.profile {
		duration := min(d, 30*time.Second)
		if mo.profileDuration > 0 {
			duration = min(d, mo.profileDuration)
		}
		wg.Go(func() { cpuStart, cpuEnd = e.capture(label, duration) })
	}
	if mo.during != nil {
		wg.Go(func() { mo.during(stop) })
	}
	time.Sleep(d)
	close(stop)
	end := time.Now()
	// The clients' counters at the window's end, before the side work (a
	// round of teleports) that may outlast it finishes.
	bytes1, chunks1 := make([]int64, len(all)), make([]int64, len(all))
	for i, b := range all {
		bytes1[i], chunks1[i] = b.bytesIn.Load(), b.chunksIn.Load()
	}
	var obs1 int64
	if e.observer != nil {
		obs1 = e.observer.packets.Load()
	}
	wg.Wait()
	// The teleport round may outlast the measurement window and its CPU
	// profile. Capture live retention after that round has finished.
	if mo.profile && e.o.profile && e.srv.kind == "golem" {
		name := filepath.Join(e.o.out, "profile-"+label, "heap-final.pb.gz")
		if err := fetch("http://"+e.srv.pprof+"/debug/pprof/heap?gc=1", name); err != nil {
			e.logf("final heap profile: %v", err)
		}
	}

	w := &window{Seconds: end.Sub(start).Seconds(), TickQuery: queries, Extra: map[string]any{}}
	e.mu.Lock()
	ticks := profileTickWindow(e.samples, start, end)
	if !cpuStart.IsZero() {
		cpuTicks := profileTickWindow(e.samples, cpuStart, cpuEnd)
		w.Extra["cpu_profile_window"] = map[string]any{
			"start": cpuStart, "end": cpuEnd, "seconds": cpuEnd.Sub(cpuStart).Seconds(),
			"ticks": len(cpuTicks), "mspt": statsOf(cpuTicks),
		}
	}
	var procs []procSample
	for _, p := range e.proc {
		if !p.wall.Before(start) && !p.wall.After(end.Add(500*time.Millisecond)) {
			procs = append(procs, p)
		}
	}
	e.mu.Unlock()
	w.Ticks = len(ticks)
	w.TPS = float64(len(ticks)) / w.Seconds
	w.MSPT = statsOf(ticks)
	if len(procs) >= 2 {
		a, b := procs[0], procs[len(procs)-1]
		w.CPUCores = (b.cpu - a.cpu) / b.wall.Sub(a.wall).Seconds()
		var sum float64
		for _, p := range procs {
			sum += p.rss
			w.RSSMiBMax = max(w.RSSMiBMax, p.rss)
		}
		w.RSSMiBMean = sum / float64(len(procs))
	}
	for i := range all {
		w.BytesIn += bytes1[i] - bytes0[i]
		w.Chunks += chunks1[i] - chunks0[i]
	}
	w.BytesPerSec = float64(w.BytesIn) / w.Seconds
	w.ChunksPerS = float64(w.Chunks) / w.Seconds
	if e.observer != nil {
		w.ObserverPPS = float64(obs1-obs0) / w.Seconds
	}
	e.sum.Windows[label] = w
	e.logf("%s: tps=%.1f mspt mean=%.2f p50=%.2f p95=%.2f p99=%.2f cpu=%.2f rss=%.0fMiB in=%.1fMB/s chunks/s=%.0f",
		label, w.TPS, w.MSPT.Mean, w.MSPT.P50, w.MSPT.P95, w.MSPT.P99, w.CPUCores, w.RSSMiBMean, w.BytesPerSec/1e6, w.ChunksPerS)
	return w
}

// capture records the profiles of a window into the run's directory.
func (e *env) capture(label string, d time.Duration) (cpuStart, cpuEnd time.Time) {
	dir := filepath.Join(e.o.out, "profile-"+label)
	_ = os.MkdirAll(dir, 0o755)
	secs := int(d.Seconds())
	switch e.srv.kind {
	case "golem":
		base := "http://" + e.srv.pprof + "/debug/pprof/"
		_ = fetch(base+"heap", filepath.Join(dir, "heap0.pb.gz"))
		_ = fetch(base+"allocs", filepath.Join(dir, "allocs0.pb.gz"))
		_ = fetch(base+"block", filepath.Join(dir, "block0.pb.gz"))
		start := time.Now()
		err := fetch(fmt.Sprintf("%sprofile?seconds=%d", base, secs), filepath.Join(dir, "cpu.pb.gz"))
		end := time.Now()
		_ = fetch(base+"block", filepath.Join(dir, "block-window.pb.gz"))
		if err != nil {
			e.logf("cpu profile: %v", err)
		} else {
			cpuStart, cpuEnd = start, end
		}
		for _, p := range []string{"heap", "allocs", "mutex", "block"} {
			_ = fetch(base+p, filepath.Join(dir, p+".pb.gz"))
		}
		// Collect after CPU profiling so the forced GC cannot inflate its samples.
		_ = fetch(base+"heap?gc=1", filepath.Join(dir, "heap-live.pb.gz"))
		_ = fetch(base+"heap?debug=1", filepath.Join(dir, "heap.txt"))
		_ = fetch(base+"goroutine?debug=1", filepath.Join(dir, "goroutine.txt"))
	case "vanilla":
		pid := strconv.Itoa(e.srv.pid())
		file := filepath.Join(dir, "vanilla.jfr")
		abs, _ := filepath.Abs(file)
		out, err := exec.Command(e.srv.jcmd, pid, "JFR.start", "settings=profile", fmt.Sprintf("duration=%ds", secs), "filename="+abs).CombinedOutput()
		if err != nil {
			e.logf("jfr: %v %s", err, out)
			return
		}
		time.Sleep(d + 3*time.Second)
		heap, _ := exec.Command(e.srv.jcmd, pid, "GC.heap_info").CombinedOutput()
		_ = os.WriteFile(filepath.Join(dir, "heap_info.txt"), heap, 0o644)
	}
	return
}

// botHealth notes the bots that were kicked or dropped.
func (e *env) botHealth() {
	for _, b := range append(append([]*bot{}, e.bots...), e.observer) {
		if b == nil {
			continue
		}
		if err := b.Err(); err != nil {
			e.sum.BotsLost++
			if len(e.sum.BotErrors) < 10 {
				e.sum.BotErrors = append(e.sum.BotErrors, b.name+": "+err.Error())
			}
		}
	}
}
