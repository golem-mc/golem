package plugins_test

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// vanilla: ServerPlayer.giveExperiencePoints, PlayerXpUpdate in Player.getXpNeededForNextLevel
func TestPluginPlayerSetTotalExperience(t *testing.T) {
	srv, h, run, cs := statePlugin(t, "Xp")
	p := handle(t, h, "Xp")
	run(func() { p.SetLevel(30); p.SetTotalExperience(100) })
	// Level 7 costs 91 points in total; the other 9 are 9/21 of level 7's bar.
	if p.Level() != 7 || p.TotalExperience() != 100 || p.Experience() != 9 {
		t.Fatalf("level %d total %d points %d", p.Level(), p.TotalExperience(), p.Experience())
	}
	r := protocol.NewReader(lastPacket(cs[0], srv, protocol.IDPlaySetExperience))
	bar, level, total := r.ReadF32(), r.ReadVarInt(), r.ReadVarInt()
	if math.Abs(float64(bar)-9.0/21) > 1e-6 || level != 7 || total != 100 {
		t.Fatalf("experience packet %v %d %d", bar, level, total)
	}
	run(func() {
		if !p.SetExperience(3) || p.SetExperience(21) {
			t.Error("SetExperience accepts a full bar")
		}
	})
	if p.Experience() != 3 || p.Level() != 7 {
		t.Fatalf("points %d level %d", p.Experience(), p.Level())
	}
}

func TestPluginPlayerFallDistanceAndTarget(t *testing.T) {
	_, h, run, _ := statePlugin(t, "Faller")
	p := handle(t, h, "Faller")
	run(func() { p.SetFallDistance(12.5) })
	if p.FallDistance() != 12.5 {
		t.Fatalf("fall distance %v", p.FallDistance())
	}
	run(func() {
		loc := p.Location()
		loc.Pitch = 90
		p.Teleport(loc, plugin.TeleportPlugin)
	})
	var got plugin.Block
	var ok, none bool
	run(func() {
		got, ok = p.TargetBlock(10)
		_, none = p.TargetBlock(0)
	})
	if !ok || none || got.Pos.Y > p.Location().Pos.Block().Y-1 {
		t.Fatalf("target %v %v below %v", got.Pos, ok, p.Location().Pos)
	}
}

// vanilla: ClientboundSetDefaultSpawnPositionPacket
func TestPluginPlayerCompassTarget(t *testing.T) {
	srv, h, run, cs := statePlugin(t, "Seeker")
	p := handle(t, h, "Seeker")
	loc := plugin.Location{World: p.Location().World, Pos: plugin.Vec3{X: 100.5, Y: 70, Z: -20.5}}
	run(func() { p.SetCompassTarget(&loc) })
	if got := p.CompassTarget(); got.Pos.Block() != (plugin.BlockPos{X: 100, Y: 70, Z: -21}) {
		t.Fatalf("target %v", got.Pos)
	}
	r := protocol.NewReader(lastPacket(cs[0], srv, protocol.IDPlaySpawnPosition))
	r.ReadString(32767)
	if x, y, z := r.ReadPosition(); x != 100 || y != 70 || z != -21 {
		t.Fatalf("spawn packet %d %d %d", x, y, z)
	}
	run(func() { p.SetCompassTarget(nil) })
	r = protocol.NewReader(lastPacket(cs[0], srv, protocol.IDPlaySpawnPosition))
	r.ReadString(32767)
	if x, _, _ := r.ReadPosition(); x == 100 {
		t.Fatal("world spawn not restored")
	}
}

// vanilla: ClientboundSetTimePacket, ClientboundGameEventPacket
func TestPluginPlayerTimeAndWeather(t *testing.T) {
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

// Bukkit's getLineOfSight / getTargetBlock walk the cells from the eyes.
func TestPluginPlayerLineOfSight(t *testing.T) {
	_, h, run, _ := statePlugin(t, "Looker")
	p := handle(t, h, "Looker")
	run(func() {
		loc := p.Location()
		loc.Pitch = 90
		p.Teleport(loc, plugin.TeleportPlugin)
	})
	var all, air, none []plugin.Block
	var last plugin.Block
	run(func() {
		all = p.LineOfSight(5, func(plugin.BlockState) bool { return true })
		air = p.LineOfSight(60, nil)
		none = p.LineOfSight(5, func(plugin.BlockState) bool { return false })
		last = p.TargetBlockPassing(5, func(plugin.BlockState) bool { return true })
	})
	eye := p.EyePos().Block()
	if len(all) != 6 || all[0].Pos != eye || all[5].Pos.Y != eye.Y-5 || last.Pos != all[5].Pos {
		t.Fatalf("pass-all line %v, last %v, eye %v", all, last.Pos, eye)
	}
	if len(none) != 1 || none[0].Pos != eye {
		t.Fatalf("reject-all line %v", none)
	}
	if n := len(air); n < 2 || n > 61 || air[n-1].Pos.Y > p.Location().Pos.Block().Y-1 {
		t.Fatalf("air line ends at %v after %d blocks, feet %v", air[len(air)-1].Pos, n, p.Location().Pos)
	}
}
