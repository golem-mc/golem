package pluginruntime

import (
	"reflect"
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

// BenchmarkFireOneListener is the cost of firing an event to one no-op
// listener, dominated by the snapshot taken before the listener runs.
func BenchmarkFireOneListener(b *testing.B) {
	r := New([]plugin.Registration{{Name: "p", Plugin: enablePlugin{enable: func() error { return nil }}}},
		func(*Host) plugin.Server { return nil })
	r.Load()
	r.Enable()
	host := r.hosts[0]
	cases := []struct {
		name string
		ev   plugin.Event
	}{
		{"physics", &plugin.BlockPhysicsEvent{}},
		{"explode0", &plugin.BlockExplodeEvent{}},
		{"explode300", &plugin.BlockExplodeEvent{Blocks: make([]plugin.Block, 300)}},
	}
	r.bus.listeners = map[reflect.Type][]*pluginListener{}
	for _, c := range cases {
		t := reflect.TypeOf(c.ev)
		r.bus.listeners[t] = []*pluginListener{{host: host, t: t, fn: func(plugin.Event) {}}}
		r.bus.updateMask(t)
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				r.bus.fire(c.ev)
			}
		})
	}
}
