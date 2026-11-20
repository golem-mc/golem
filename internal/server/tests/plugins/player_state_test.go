package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// statePlugin joins the named players on a held loop and returns their handles.
func statePlugin(t *testing.T, names ...string) (*server.Server, plugin.Server, func(func()), []*servertest.Client) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	var host plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "state", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	var clients []*servertest.Client
	for _, name := range names {
		c := servertest.DialServer(t, srv)
		c.ViewAsk = config.MinDistance
		c.Join(name)
		clients = append(clients, c)
	}
	servertest.HoldTicks(t, srv)
	run := func(f func()) {
		t.Helper()
		ran := false
		host.Scheduler().RunTask(func() { f(); ran = true })
		servertest.RunTicks(t, srv, 1)
		if !ran {
			t.Fatal("plugin task did not complete")
		}
	}
	for _, c := range clients {
		c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
	}
	return srv, host, run, clients
}

// lastPacket is the newest packet of id the client was sent so far.
func lastPacket(c *servertest.Client, srv *server.Server, id int32) (got []byte) {
	c.ReadTicks(srv, 0, func(gotID int32, data []byte) bool {
		if gotID == id {
			got = data
		}
		return false
	})
	return got
}

func handle(t *testing.T, h plugin.Server, name string) plugin.Player {
	t.Helper()
	p, ok := h.PlayerNamed(name)
	if !ok {
		t.Fatalf("player %s missing", name)
	}
	return p
}

// vanilla: ClientboundSetHealthPacket, ClientboundSetExperiencePacket
func TestPluginPlayerFoodAndExperience(t *testing.T) {
	t.Parallel()
	srv, h, run, cs := statePlugin(t, "Eater")
	c, p := cs[0], handle(t, h, "Eater")
	run(func() { p.SetFood(7); p.SetSaturation(3); p.SetExhaustion(1.5) })
	r := protocol.NewReader(lastPacket(c, srv, protocol.IDPlaySetHealth))
	if r.ReadF32() <= 0 || r.ReadVarInt() != 7 || r.ReadF32() != 3 {
		t.Fatal("health packet food or saturation")
	}
	if p.Food() != 7 || p.Saturation() != 3 || p.Exhaustion() != 1.5 {
		t.Fatalf("food %d saturation %v exhaustion %v", p.Food(), p.Saturation(), p.Exhaustion())
	}
	run(func() { p.SetLevel(5) })
	r = protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySetExperience))
	if r.ReadF32() != 0 || r.ReadVarInt() != 5 {
		t.Fatal("experience packet level")
	}
	run(func() { p.SetProgress(0.5) })
	r = protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySetExperience))
	if r.ReadF32() != 0.5 || p.Progress() != 0.5 || p.Level() != 5 {
		t.Fatal("experience progress")
	}
	before := p.TotalExperience()
	run(func() { p.GiveExperience(10) })
	r = protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySetExperience))
	r.ReadF32()
	r.ReadVarInt()
	if total := r.ReadVarInt(); int(total) != before+10 || p.TotalExperience() != before+10 {
		t.Fatalf("total experience = %d, want %d", total, before+10)
	}
}

// vanilla: ClientboundPlayerAbilitiesPacket, ServerPlayer.setGameMode
func TestPluginPlayerAbilitiesAndGameMode(t *testing.T) {
	t.Parallel()
	srv, h, run, cs := statePlugin(t, "Flyer", "Watcher")
	c, w, p := cs[0], cs[1], handle(t, h, "Flyer")
	run(func() { p.SetGameMode(plugin.Survival) })
	if p.GameMode() != plugin.Survival || p.AllowFlight() || p.Flying() {
		t.Fatal("survival player able to fly")
	}
	c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	abilities := func() (flags uint8, fly, walk float32) {
		r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayAbilities))
		return r.ReadU8(), r.ReadF32(), r.ReadF32()
	}
	run(func() { p.SetFlying(true) })
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayAbilities))
	run(func() { p.SetAllowFlight(true) })
	if flags, _, _ := abilities(); flags&protocol.AbilityAllowFlying == 0 || !p.AllowFlight() {
		t.Fatalf("allow flight flags %x", flags)
	}
	// A new player answers Bukkit's defaults, twice vanilla's ability values.
	if p.FlySpeed() != 0.1 || p.WalkSpeed() != 0.2 {
		t.Fatalf("new player fly %v walk %v, want 0.1 and 0.2", p.FlySpeed(), p.WalkSpeed())
	}
	// Setting the defaults back changes nothing on the wire or the attribute.
	run(func() { p.SetFlySpeed(0.1); p.SetWalkSpeed(0.2) })
	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if id == protocol.IDPlayAbilities {
			r := protocol.NewReader(data)
			r.ReadU8()
			if fly, walk := r.ReadF32(), r.ReadF32(); fly != 0.05 || walk != 0.1 {
				t.Fatalf("defaults sent as fly %v walk %v, want vanilla's 0.05 and 0.1", fly, walk)
			}
		}
		return false
	})
	var base float64
	run(func() {
		if a, ok := p.Attribute(registry.AttributeMovementSpeed); ok {
			base = a.Base()
		}
	})
	if base != float64(float32(0.1)) {
		t.Fatalf("movement speed base %v after SetWalkSpeed(0.2), want 0.1", base)
	}
	run(func() { p.SetFlying(true); p.SetFlySpeed(0.4); p.SetWalkSpeed(0.6) })
	var flags uint8
	var fly, walk float32
	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if id == protocol.IDPlayAbilities {
			r := protocol.NewReader(data)
			flags, fly, walk = r.ReadU8(), r.ReadF32(), r.ReadF32()
		}
		return false
	})
	if flags&protocol.AbilityFlying == 0 || fly != 0.2 || walk != 0.3 || !p.Flying() || p.FlySpeed() != 0.4 || p.WalkSpeed() != 0.6 {
		t.Fatalf("abilities flags %x fly %v walk %v", flags, fly, walk)
	}
	run(func() { p.SetAllowFlight(false) })
	if flags, _, _ := abilities(); flags&(protocol.AbilityAllowFlying|protocol.AbilityFlying) != 0 || p.Flying() {
		t.Fatalf("revoked flight flags %x", flags)
	}

	w.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
	run(func() { p.SetGameMode(plugin.Creative) })
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayGameEvent))
	if r.ReadU8() != protocol.GameEventChangeGameMode || r.ReadF32() != float32(plugin.Creative) || p.GameMode() != plugin.Creative {
		t.Fatal("game event")
	}
	if flags, _, _ := abilities(); flags&protocol.AbilityAllowFlying == 0 {
		t.Fatalf("creative flags %x", flags)
	}
	w.ExpectWithinTicks(srv, 0, protocol.IDPlayPlayerInfoUpdate)
	if p.Sneaking() || p.Sprinting() {
		t.Fatal("idle player sneaking or sprinting")
	}
}

// vanilla: ServerGamePacketListenerImpl.handleChat, handleChatCommand
func TestPluginPlayerChat(t *testing.T) {
	t.Parallel()
	srv, h, run, cs := statePlugin(t, "Talker", "Hearer")
	hearer, p := cs[1], handle(t, h, "Talker")
	run(func() { p.Chat("hello there") })
	r := protocol.NewReader(hearer.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat))
	text, ok := protocol.ReadText(r)
	if !ok || text.Translate != "chat.type.text" || len(text.With) != 2 || text.With[1].Plain() != "hello there" {
		t.Fatalf("chat = %+v", text)
	}
	run(func() { p.Chat("/me waves") })
	hearer.ExpectWithinTicks(srv, 0, protocol.IDPlayDisguisedChat)
}

// vanilla: ServerStatsCounter.setValue, PlayerAdvancements.award, revoke
func TestPluginPlayerStatisticsAndAdvancements(t *testing.T) {
	t.Parallel()
	srv, h, run, cs := statePlugin(t, "Scorer")
	c, p := cs[0], handle(t, h, "Scorer")
	jump, ok := h.Registries().Statistic(plugin.MinecraftKey("jump"))
	if !ok {
		t.Fatal("jump statistic")
	}
	run(func() { p.SetStatistic(jump, 5) })
	if p.Statistic(jump) != 5 {
		t.Fatalf("statistic = %d", p.Statistic(jump))
	}
	run(func() { p.SetStatistic(jump, 2) })
	if p.Statistic(jump) != 2 {
		t.Fatalf("statistic = %d", p.Statistic(jump))
	}
	run(func() { p.SetStatistic(jump, 0) })
	if p.Statistic(jump) != 0 {
		t.Fatal("statistic not reset")
	}

	adv, ok := h.Advancement(plugin.MinecraftKey("story/mine_stone"))
	if !ok {
		t.Fatal("advancement")
	}
	progress := p.Advancements(adv)
	c.WatchAdvancements = true
	criterion := adv.Criteria()[0]
	if progress.Done() || len(progress.Awarded()) != 0 || len(progress.Remaining()) != len(adv.Criteria()) {
		t.Fatal("fresh progress")
	}
	c.ReadTicks(srv, 1, func(int32, []byte) bool { return false })
	run(func() {
		if !progress.Award(criterion) || progress.Award(criterion) {
			t.Error("award result")
		}
	})
	if !progress.Done() || len(progress.Awarded()) != 1 || len(progress.Remaining()) != 0 {
		t.Fatal("progress after award")
	}
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayUpdateAdvancements)
	run(func() {
		if !progress.Revoke(criterion) || progress.Revoke(criterion) {
			t.Error("revoke result")
		}
	})
	if progress.Done() {
		t.Fatal("progress after revoke")
	}
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayUpdateAdvancements)
}

// vanilla: PlayerAdvancements.award, revoke
func TestPluginSetAwardedIsSilent(t *testing.T) {
	t.Parallel()
	srv, h, run, cs := statePlugin(t, "Silent")
	c, p := cs[0], handle(t, h, "Silent")
	keys := map[string]bool{}
	for _, a := range h.Advancements() {
		keys[a.Key().String()] = true
	}
	if !keys["minecraft:story/root"] || !keys["minecraft:story/mine_stone"] {
		t.Fatal("Advancements misses story advancements")
	}
	adv, _ := h.Advancement(plugin.MinecraftKey("story/mine_stone"))
	progress := p.Advancements(adv)
	run(func() { progress.SetAwarded([]string{"get_stone"}) })
	if !progress.Done() {
		t.Fatal("not done after SetAwarded")
	}
	run(func() { progress.SetAwarded(nil) })
	if progress.Done() {
		t.Fatal("done after SetAwarded(nil)")
	}
	run(func() { progress.SetAwarded([]string{"get_stone"}) })
	if !progress.Done() {
		t.Fatal("not done again")
	}
	c.ExpectNoneWithinTicks(srv, 5, func(id int32, _ []byte) bool {
		return id == protocol.IDPlaySystemChat || id == protocol.IDPlaySetExperience
	})
}

// vanilla: ServerPlayer.setCamera
func TestPluginPlayerSpectating(t *testing.T) {
	t.Parallel()
	srv, h, run, cs := statePlugin(t, "Viewer", "Target")
	c, p, target := cs[0], handle(t, h, "Viewer"), handle(t, h, "Target")
	if _, ok := p.Spectating(); ok {
		t.Fatal("spectating from the start")
	}
	run(func() { p.SetSpectating(target) })
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySetCamera))
	run(func() { p.SetGameMode(plugin.Spectator); p.SetSpectating(target) })
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySetCamera))
	if r.ReadVarInt() != target.ID() {
		t.Fatal("camera target")
	}
	if got, ok := p.Spectating(); !ok || got.UUID() != target.UUID() {
		t.Fatalf("Spectating = %v, %v", got, ok)
	}
	run(func() { p.SetSpectating(nil) })
	r = protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySetCamera))
	if r.ReadVarInt() != p.ID() {
		t.Fatal("camera not returned to the player")
	}
	if _, ok := p.Spectating(); ok {
		t.Fatal("still spectating")
	}
}

// vanilla: ChunkMap.TrackedEntity.removePlayer/updatePlayer,
// ClientboundPlayerInfoRemovePacket
func TestPluginPlayerHideShow(t *testing.T) {
	t.Parallel()
	srv, h, run, cs := statePlugin(t, "Looker", "Hidden")
	looker, hidden := cs[0], cs[1]
	p, target := handle(t, h, "Looker"), handle(t, h, "Hidden")
	if !p.CanSee(target) || !target.CanSee(p) {
		t.Fatal("players start hidden from each other")
	}
	run(func() { p.Hide(target) })
	if p.CanSee(target) || !target.CanSee(p) {
		t.Fatal("CanSee after Hide")
	}
	var removed, unlisted bool
	looker.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayRemoveEntities:
			removed = r.ReadVarInt() == 1 && r.ReadVarInt() == target.ID()
		case protocol.IDPlayPlayerInfoRemove:
			unlisted = r.ReadVarInt() == 1 && r.ReadUUID() == [16]byte(target.UUID())
		}
		return removed && unlisted
	})
	if !removed || !unlisted {
		t.Fatalf("hide sent removal %v, tab removal %v", removed, unlisted)
	}
	// The tracker must not bring the body back, and the hidden player's own view is untouched.
	looker.ExpectNoneWithinTicks(srv, 30, servertest.PacketAbout(target.ID(), protocol.IDPlaySpawnEntity))
	hidden.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayRemoveEntities, protocol.IDPlayPlayerInfoRemove))

	run(func() { p.Show(target) })
	looker.ExpectWithinTicks(srv, 0, protocol.IDPlayPlayerInfoUpdate)
	if !looker.ReadTicks(srv, 5, func(id int32, data []byte) bool {
		return id == protocol.IDPlaySpawnEntity && protocol.NewReader(data).ReadVarInt() == target.ID()
	}) {
		t.Fatal("shown player was not drawn again")
	}
	if !p.CanSee(target) {
		t.Fatal("CanSee after Show")
	}
}
