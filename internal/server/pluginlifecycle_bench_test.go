package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
)

// BenchmarkLifecycleDisconnect measures the mailbox path with no hook listeners.
func BenchmarkLifecycleDisconnect(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "no-plugins"
		if enabled {
			name = "no-listeners"
		}
		b.Run(name, func(b *testing.B) {
			c, _ := mailboxClient()
			if enabled {
				c.srv.plugins = pluginruntime.New(nil, nil)
			}
			message := protocol.Text{Translate: "multiplayer.disconnect.kicked"}
			b.ReportAllocs()
			for b.Loop() {
				c.Disconnect(message)
				// Reuse the mailbox without a writer so scheduling and network
				// I/O cannot hide the cost of deciding and queueing a disconnect.
				c.out.mu.Lock()
				c.out.queue = c.out.queue[:0]
				c.out.queued = 0
				c.out.closing = false
				c.out.mu.Unlock()
				<-c.out.wake
			}
		})
	}
}
