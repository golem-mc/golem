package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

// vanilla: ServerBossEvent, CustomBossEvent, ClientboundBossEventPacket
func TestPluginBossBars(t *testing.T) {
	t.Parallel()
	srv, host := pluginAdminServer(t)
	c := pluginJoin(t, srv, "Viewer")
	other := pluginJoin(t, srv, "Other")
	servertest.HoldTicks(t, srv)
	p, _ := host.PlayerNamed("Viewer")
	run := func(line string) []protocol.Text {
		servertest.ReleaseTicks(t, srv)
		out := srv.RunLine(line)
		servertest.HoldTicks(t, srv)
		return out
	}
	bar := host.CreateBossBar(plugin.Plain("Private"), plugin.BarBlue, plugin.BarNotched6)
	if bar.Title().PlainString() != "Private" || bar.Color() != plugin.BarBlue || bar.Style() != plugin.BarNotched6 || !bar.Visible() || bar.Progress() != 1 {
		t.Fatal("initial boss bar")
	}
	var wire uuid.UUID
	read := func(action int32) *protocol.Reader {
		t.Helper()
		r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayBossEvent))
		id := r.ReadUUID()
		if wire == uuid.Nil {
			wire = id
		}
		if id != wire || r.ReadVarInt() != action {
			t.Fatal("boss event identity/action")
		}
		return r
	}
	add := func(title string, progress float32, color, style int32, flags uint8) {
		t.Helper()
		r := read(protocol.BossEventAdd)
		text, ok := protocol.ReadText(r)
		if !ok || text.Plain() != title || r.ReadF32() != progress || r.ReadVarInt() != color || r.ReadVarInt() != style || r.ReadU8() != flags || r.Err() != nil || r.Remaining() != 0 {
			t.Fatal("boss add fields")
		}
	}
	bar.AddPlayer(p)
	bar.AddPlayer(p)
	add("Private", 1, 1, 1, 0)
	if len(bar.Players()) != 1 || bar.Players()[0].UUID() != p.UUID() {
		t.Fatal("boss players")
	}
	bar.SetProgress(.12345678)
	if bar.Progress() != .12345678 || read(protocol.BossEventProgress).ReadF32() != .12345678 {
		t.Fatal("boss progress precision")
	}
	bar.SetTitle(plugin.Plain("Changed"))
	if text, ok := protocol.ReadText(read(protocol.BossEventName)); !ok || text.Plain() != "Changed" {
		t.Fatal("boss title")
	}
	bar.SetColor(plugin.BarRed)
	r := read(protocol.BossEventStyle)
	if r.ReadVarInt() != 2 || r.ReadVarInt() != 1 {
		t.Fatal("boss color")
	}
	bar.SetStyle(plugin.BarNotched10)
	r = read(protocol.BossEventStyle)
	if r.ReadVarInt() != 2 || r.ReadVarInt() != 2 {
		t.Fatal("boss style")
	}
	bar.SetFlags(true, true, true)
	if read(protocol.BossEventProperties).ReadU8() != 7 {
		t.Fatal("boss flags packet")
	}
	if dark, music, fog := bar.Flags(); !dark || !music || !fog {
		t.Fatal("boss flags getters")
	}
	bar.SetVisible(false)
	read(protocol.BossEventRemove)
	bar.SetTitle(plugin.Plain("Hidden"))
	bar.SetProgress(.5)
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayBossEvent))
	bar.SetVisible(true)
	add("Hidden", .5, 2, 2, 7)
	bar.RemovePlayer(p)
	read(protocol.BossEventRemove)
	bar.AddPlayer(p)
	add("Hidden", .5, 2, 2, 7)
	other.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayBossEvent))
	if len(host.BossBars()) != 0 {
		t.Fatal("private bar is command-visible")
	}
	run("bossbar add test:saved \"Saved\"")
	saved := host.BossBars()[plugin.Key{Namespace: "test", Path: "saved"}]
	if saved == nil {
		t.Fatal("command bar absent from plugin")
	}
	saved.AddPlayer(p)
	wire = uuid.Nil
	add("Saved", 0, 6, 0, 0)
	saved.SetProgress(.25)
	if read(protocol.BossEventProgress).ReadF32() != .25 {
		t.Fatal("saved bar progress")
	}
	saved.SetProgress(.7)
	if got := saved.Progress(); got != .7 {
		t.Fatalf("saved bar progress = %v, want .7", got)
	}
	if got := read(protocol.BossEventProgress).ReadF32(); got != .7 {
		t.Fatalf("saved bar progress packet = %v, want .7", got)
	}
	if out := run("bossbar get test:saved value"); len(out) != 1 || out[0].Translate != "commands.bossbar.get.value" || len(out[0].With) != 2 || out[0].With[1].Plain() != "70" {
		t.Fatalf("saved bar command value = %+v, want 70", out)
	}
	saved.SetFlags(true, false, true)
	if read(protocol.BossEventProperties).ReadU8() != 5 {
		t.Fatal("saved bar flags")
	}
	run("bossbar set test:saved max 2147483647")
	read(protocol.BossEventProgress)
	saved.SetProgress(1)
	if read(protocol.BossEventProgress).ReadF32() != 1 {
		t.Fatal("progress overflow at maximum integer max")
	}
	run("bossbar set test:saved max 100")
	run("bossbar set test:saved value 75")
	if read(protocol.BossEventProgress).ReadF32() != .75 || saved.Progress() != .75 {
		t.Fatal("command and plugin do not share bar")
	}
	servertest.ReleaseTicks(t, srv)
	srv.SaveAll(true)
	servertest.HoldTicks(t, srv)
	loaded := world.NewBossBars(srv.Config.LevelName, func(_ nbt.Tag) bool { return true })
	if len(loaded.All()) != 1 || loaded.IDs()[0] != "test:saved" {
		t.Fatal("private bar reached saved data")
	}
	if b, _ := loaded.Get("test:saved"); b.Fill() != .75 || b.Flags() != 5 {
		t.Fatal("command bar changes not persisted")
	}
	saved.Remove()
	read(protocol.BossEventRemove)
	run("bossbar add test:saved \"Replacement\"")
	saved.SetTitle(plugin.Plain("Stale"))
	if host.BossBars()[plugin.Key{Namespace: "test", Path: "saved"}].Title().PlainString() != "Replacement" {
		t.Fatal("stale bar handle changed replacement")
	}
	servertest.ReleaseTicks(t, srv)
	c.Disconnect()
	other.ExpectPlay(protocol.IDPlayPlayerInfoRemove)
	other.ExpectAnnouncement("multiplayer.player.left", "Viewer")
	c = pluginJoin(t, srv, "Viewer")
	servertest.HoldTicks(t, srv)
	bar.SetProgress(.75)
	if len(bar.Players()) != 0 {
		t.Fatal("private membership followed a rejoin")
	}
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayBossEvent))
	p, _ = host.PlayerNamed("Viewer")
	bar.AddPlayer(p)
	wire = uuid.Nil
	add("Hidden", .75, 2, 2, 7)
	wire = uuid.Nil
	bar.Remove()
	read(protocol.BossEventRemove)
	bar.AddPlayer(p)
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayBossEvent))
	owned := host.CreateBossBar(plugin.Plain("Cleanup"), plugin.BarWhite, plugin.BarProgress)
	owned.AddPlayer(p)
	wire = uuid.Nil
	add("Cleanup", 1, 6, 0, 0)
	servertest.ReleaseTicks(t, srv)
	srv.Shutdown()
	if owned.Visible() || len(owned.Players()) != 0 {
		t.Fatal("disabled plugin retained its bar")
	}
}
