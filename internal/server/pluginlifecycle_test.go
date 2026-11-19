package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
)

func TestPluginLifecycleNoListenerAllocations(t *testing.T) {
	c := &client{srv: &Server{plugins: pluginruntime.New(nil, nil)}}
	if allocations := testing.AllocsPerRun(100, func() {
		if c.pluginJoin("old") || c.queuePluginKick(protocol.Text{}) {
			t.Fatal("listener-free gate handled an event")
		}
		c.pluginConnecting.Store(true)
		if c.queuePluginKick(protocol.Text{}) {
			t.Fatal("listener-free configuration gate handled an event")
		}
		c.pluginConnecting.Store(false)
		if _, handled := c.pluginQuit(); handled {
			t.Fatal("listener-free quit handled an event")
		}
	}); allocations != 0 {
		t.Fatalf("listener-free hooks allocated %g times", allocations)
	}
}
