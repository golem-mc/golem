package pluginruntime

import (
	"testing"

	"github.com/golem-mc/golem/plugin"
)

func benchRuntime() *Runtime {
	return New(nil, func(*Host) plugin.Server { return nil })
}

// BenchmarkStartTickIdle is a tick's start with nothing queued, every tick
// of a server whose connections fire no events.
func BenchmarkStartTickIdle(b *testing.B) {
	r := benchRuntime()
	for b.Loop() {
		r.StartTick()
	}
}

// BenchmarkStartTickPosted is a tick's start running one posted callback,
// which no connection waits on.
func BenchmarkStartTickPosted(b *testing.B) {
	r := benchRuntime()
	for b.Loop() {
		r.Post(func() {})
		r.StartTick()
	}
}

// BenchmarkOnTick is the tick-goroutine check hot paths make per event.
func BenchmarkOnTick(b *testing.B) {
	r := benchRuntime()
	r.BindTick()
	for b.Loop() {
		r.OnTick()
	}
}
