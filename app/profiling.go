package app

import (
	"log/slog"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof/ on http.DefaultServeMux
	"os"
	"runtime"
	"time"
)

// startProfiling serves net/http/pprof on PPROF_ADDR (e.g. localhost:6060)
// when it is set; unset, nothing listens. The pprof handlers expose the
// process, so bind to localhost unless the port is otherwise firewalled.
func startProfiling() {
	addr := os.Getenv("PPROF_ADDR")
	if addr == "" {
		return
	}
	// Sampled, so cheap enough to leave on while profiling: about one
	// contended lock in 10, and goroutines blocked for 1ms on average.
	runtime.SetMutexProfileFraction(10)
	runtime.SetBlockProfileRate(int(time.Millisecond))
	go func() {
		slog.Info("serving pprof", "addr", "http://"+addr+"/debug/pprof/")
		if err := http.ListenAndServe(addr, nil); err != nil {
			slog.Warn("pprof stopped", "error", err)
		}
	}()
}
