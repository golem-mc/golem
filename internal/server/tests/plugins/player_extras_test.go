package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// vanilla: ClientboundSetTimePacket, ClientboundGameEventPacket
func TestPluginPlayerTimeAndWeather(t *testing.T) {
	t.Parallel()
	srv, h, run, cs := statePlugin(t, "Sky")
	p := handle(t, h, "Sky")
	run(func() { p.SetPlayerTime(6000, false) })
	r := protocol.NewReader(lastPacket(cs[0], srv, protocol.IDPlayUpdateTime))
	r.ReadI64()
	if r.ReadVarInt() != 1 {
		t.Fatal("one clock expected")
	}
	r.ReadVarInt()
	if total := r.ReadVarLong(); total != 6000 {
		t.Fatalf("clock %d", total)
	}
	r.ReadF32()
	if rate := r.ReadF32(); rate != 0 {
		t.Fatalf("fixed clock rate %v", rate)
	}
	if ticks, fixed, over := p.PlayerTime(); ticks != 6000 || !fixed || !over {
		t.Fatal("PlayerTime")
	}
	run(func() { p.ResetPlayerTime() })
	if _, _, over := p.PlayerTime(); over {
		t.Fatal("time override stays")
	}

	run(func() { p.SetPlayerWeather(true) })
	var events []byte
	cs[0].ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if id == protocol.IDPlayGameEvent {
			events = append(events, data[0])
		}
		return false
	})
	if len(events) != 3 || events[0] != protocol.GameEventBeginRaining {
		t.Fatalf("weather events %v", events)
	}
	if rain, over := p.PlayerWeather(); !rain || !over {
		t.Fatal("PlayerWeather")
	}
	run(func() { p.ResetPlayerWeather() })
	if _, over := p.PlayerWeather(); over {
		t.Fatal("weather override stays")
	}
}

// vanilla: ServerStatsCounter, UserNameToIdResolver
func TestPluginOfflineStatisticAndNameLookup(t *testing.T) {
	t.Parallel()
	srv, h, run, cs := statePlugin(t, "Leaver")
	p := handle(t, h, "Leaver")
	jump, ok := h.Registries().Statistic(plugin.MinecraftKey("jump"))
	if !ok {
		t.Fatal("jump statistic")
	}
	run(func() { p.SetStatistic(jump, 7) })
	id := p.UUID()
	cs[0].Conn.Close()
	cs[0].AwaitServed()
	servertest.RunTicks(t, srv, 1)
	if p.IsValid() {
		t.Fatal("handle valid after quit")
	}
	off, ok := h.OfflinePlayerNamed("lEAVER")
	if !ok || off.UUID() != id || off.Name() != "Leaver" {
		t.Fatalf("lookup %v %v", off, ok)
	}
	if got := off.Statistic(jump); got != 7 {
		t.Fatalf("offline statistic %d", got)
	}
	if profile, ok := h.ProfileByName("leaver"); !ok || profile.ID != id {
		t.Fatal("profile lookup")
	}
	if _, ok := h.OfflinePlayerNamed("Nobody"); ok {
		t.Fatal("unknown name found")
	}
}
