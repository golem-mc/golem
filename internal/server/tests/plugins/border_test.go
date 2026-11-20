package plugins_test

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// TestPluginBorderDamageBuffer checks a survival player one block past the
// edge is hurt with buffer 0 and not with the default 5.
func TestPluginBorderDamageBuffer(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.SurvivalTestConfig())
	c := servertest.DialServer(t, srv)
	c.Join("Edge")
	run(func() {
		b := h.Overworld().Border()
		if b.DamageBuffer() != 5 {
			t.Errorf("default buffer=%v", b.DamageBuffer())
		}
		b.SetSize(10, 0)
	})
	c.MoveToGround(6.5, 64, 0.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 40)
	if got := srv.PlayerHealth("Edge"); got != 20 {
		t.Fatalf("health inside the default buffer = %v, want 20", got)
	}
	run(func() {
		b := h.Overworld().Border()
		b.SetDamageBuffer(0)
		if b.DamageBuffer() != 0 {
			t.Errorf("buffer=%v", b.DamageBuffer())
		}
	})
	servertest.RunTicks(t, srv, 2)
	if got := srv.PlayerHealth("Edge"); got >= 20 {
		t.Errorf("health with buffer 0 = %v, want hurt", got)
	}
}

// TestPluginBorderWarningTime checks the packet and the save of the buffer
// and the warning time.
func TestPluginBorderWarningTime(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, h, run := worldPlugin(t, cfg)
	c := servertest.DialServer(t, srv)
	c.Join("Warn")
	run(func() {
		b := h.Overworld().Border()
		b.SetWarningTime(30 * time.Second)
		b.SetDamageBuffer(2.5)
		if b.WarningTime() != 30*time.Second {
			t.Errorf("warning time=%v", b.WarningTime())
		}
		h.Overworld().Save(true)
	})
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlaySetBorderWarningDelay))
	if got := r.ReadVarInt(); got != 600 {
		t.Errorf("warning delay=%d ticks, want 600", got)
	}
	srv.Shutdown()
	_, h, run = worldPlugin(t, cfg)
	run(func() {
		var b plugin.WorldBorder = h.Overworld().Border()
		if b.WarningTime() != 30*time.Second || b.DamageBuffer() != 2.5 {
			t.Errorf("reloaded warning=%v buffer=%v", b.WarningTime(), b.DamageBuffer())
		}
		b.SetDamageBuffer(5)
		h.Overworld().Save(false)
	})
}
