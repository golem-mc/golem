package plugins_test

import (
	"bytes"
	"reflect"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// vanilla: ClientboundSystemChatPacket, ClientboundTransferPacket
func TestPluginPlayerMessaging(t *testing.T) {
	t.Parallel()
	var host plugin.Server
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "players", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Messenger")
	servertest.HoldTicks(t, srv)
	p, ok := host.PlayerNamed("mEsSeNgEr")
	if !ok || !p.IsValid() {
		t.Fatal("online player missing")
	}
	p.SendMessage(plugin.Plain("hello"))
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat))
	text, ok := protocol.ReadText(r)
	if !ok || text.Plain() != "hello" || r.ReadBool() || r.Err() != nil {
		t.Fatalf("chat = %+v, %v", text, r.Err())
	}
	p.Transfer("example.org", 25566)
	r = protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayTransfer))
	if r.ReadString(32767) != "example.org" || r.ReadVarInt() != 25566 || r.Err() != nil {
		t.Fatal("transfer fields")
	}
}

// vanilla: ClientboundSetTitlesAnimationPacket, ClientboundTabListPacket,
// ClientboundPlayerInfoUpdatePacket.Action.UPDATE_DISPLAY_NAME
func TestPluginPlayerControlPackets(t *testing.T) {
	t.Parallel()
	var host plugin.Server
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "control", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Control")
	servertest.HoldTicks(t, srv)
	p, _ := host.PlayerNamed("Control")
	check := func(id int32, call func(), put func(*protocol.Buffer)) {
		t.Helper()
		call()
		var want protocol.Buffer
		put(&want)
		got := c.ExpectWithinTicks(srv, 0, id)
		if !bytes.Equal(got, want.Bytes()) {
			t.Fatalf("packet %x = %x, want %x", id, got, want.Bytes())
		}
	}
	text := plugin.Plain("styled").Bold().Append(plugin.Plain("child").NotItalic())
	putText := func(b *protocol.Buffer) { b.PutText(protocol.Text{Raw: nbt.Compound{"text": nbt.String("line")}}) }
	check(protocol.IDPlayActionBarText, func() { p.SendActionBar(plugin.Plain("line")) }, putText)
	p.SendTitle(plugin.Plain("title"), plugin.Plain("sub"), 2, 30, 4)
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayTitlesAnimation))
	if r.ReadI32() != 2 || r.ReadI32() != 30 || r.ReadI32() != 4 || r.Err() != nil {
		t.Fatal("title timings")
	}
	for _, v := range []struct {
		id   int32
		text string
	}{{protocol.IDPlaySubtitleText, "sub"}, {protocol.IDPlayTitleText, "title"}} {
		actual, ok := protocol.ReadText(protocol.NewReader(c.ExpectWithinTicks(srv, 0, v.id)))
		if !ok || actual.Plain() != v.text {
			t.Fatalf("title = %+v", actual)
		}
	}
	p.SendTitle(plugin.Plain("t2"), plugin.Plain("s2"), -1, -1, -1)
	for _, v := range []struct {
		id   int32
		text string
	}{{protocol.IDPlaySubtitleText, "s2"}, {protocol.IDPlayTitleText, "t2"}} {
		actual, ok := protocol.ReadText(protocol.NewReader(c.ExpectWithinTicks(srv, 0, v.id)))
		if !ok || actual.Plain() != v.text {
			t.Fatalf("title = %+v", actual)
		}
	}
	c.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlayTitlesAnimation))
	check(protocol.IDPlayClearTitles, p.ClearTitle, func(b *protocol.Buffer) { b.PutBool(false) })
	check(protocol.IDPlayTabList, func() { p.SetListHeaderFooter(plugin.Plain("head"), plugin.Plain("foot")) }, func(b *protocol.Buffer) {
		b.PutText(protocol.Text{Raw: nbt.Compound{"text": nbt.String("head")}})
		b.PutText(protocol.Text{Raw: nbt.Compound{"text": nbt.String("foot")}})
	})
	if p.ListName() != nil {
		t.Fatal("default list name")
	}
	check(protocol.IDPlayPlayerInfoUpdate, func() { p.SetListName(&text) }, func(b *protocol.Buffer) {
		b.PutU8(0x20)
		b.PutVarInt(1)
		b.PutUUID(uuid.UUID(p.UUID()))
		b.PutBool(true)
		b.PutText(protocol.Text{Raw: nbt.Compound{"text": nbt.String("styled"), "bold": nbt.Byte(1), "extra": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"text": nbt.String("child"), "italic": nbt.Byte(0)}}}}})
	})
	if !reflect.DeepEqual(p.ListName(), &text) {
		t.Fatal("list name roundtrip")
	}
	check(protocol.IDPlayPlayerInfoUpdate, func() { p.SetListName(nil) }, func(b *protocol.Buffer) {
		b.PutU8(0x20)
		b.PutVarInt(1)
		b.PutUUID(uuid.UUID(p.UUID()))
		b.PutBool(false)
	})
	p.SetDisplayName(text)
	if !reflect.DeepEqual(p.DisplayName(), text) {
		t.Fatal("display name roundtrip")
	}
	host.Broadcast(plugin.Plain("broadcast"))
	actual, ok := protocol.ReadText(protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)))
	if !ok || actual.Plain() != "broadcast" {
		t.Fatal("broadcast")
	}
	check(protocol.IDPlayShowDialog, func() { p.ShowDialog(plugin.MinecraftKey("custom_options")) }, func(b *protocol.Buffer) {
		reg, _ := registry.Get(registry.DialogRegistry)
		for i, key := range reg.Entries {
			if key == "minecraft:custom_options" {
				b.PutVarInt(int32(i + 1))
				return
			}
		}
		t.Fatal("missing dialog")
	})
	sound, _ := host.Registries().Sound(plugin.MinecraftKey("block.note_block.harp"))
	p.PlaySound(plugin.Vec3{X: 1, Y: 64, Z: -2}, sound, plugin.SoundPlayers, .5, 1.25)
	r = protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySoundEffect))
	if r.ReadVarInt() != int32(sound) || r.ReadVarInt() != 7 || r.ReadI32() != 8 || r.ReadI32() != 512 || r.ReadI32() != -16 || r.ReadF32() != .5 || r.ReadF32() != 1.25 {
		t.Fatal("sound fields")
	}
	r.ReadI64()
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	check(protocol.IDPlayStopSound, func() { p.StopSound(sound, plugin.SoundPlayers) }, func(b *protocol.Buffer) { b.PutU8(3); b.PutVarInt(7); b.PutString("minecraft:block.note_block.harp") })
	state := plugin.BlockState(registry.BlockStone.DefaultState())
	check(protocol.IDPlayBlockUpdate, func() { p.SendBlockChange(plugin.BlockPos{X: 3, Y: 64, Z: 4}, state) }, func(b *protocol.Buffer) { b.PutPosition(3, 64, 4); b.PutVarInt(int32(state)) })
	if srv.BlockAt(3, 64, 4) == uint16(state) {
		t.Fatal("fake block changed world")
	}
	check(protocol.IDPlayPluginMessage, func() { p.SendPluginMessage(plugin.Key{Namespace: "test", Path: "data"}, []byte{0, 255}) }, func(b *protocol.Buffer) { b.PutString("test:data"); b.PutRaw([]byte{0, 255}) })
	check(protocol.IDPlayStoreCookie, func() { p.StoreCookie(plugin.Key{Namespace: "test", Path: "cookie"}, []byte{1, 2}) }, func(b *protocol.Buffer) { b.PutString("test:cookie"); b.PutBytes([]byte{1, 2}) })

	p.SetListName(&text)
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayPlayerInfoUpdate)
	servertest.ReleaseTicks(t, srv)
	viewer := servertest.DialServer(t, srv)
	viewer.ViewAsk = config.MinDistance
	viewer.Login("Viewer")
	viewer.Configure()
	sawListName := false
	viewer.ReadJoin(func(id int32, data []byte) {
		if id != protocol.IDPlayPlayerInfoUpdate {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadU8() != 0x20 {
			return
		}
		if r.ReadVarInt() != 1 || r.ReadUUID() != uuid.UUID(p.UUID()) || !r.ReadBool() {
			t.Fatal("late join list update")
		}
		name, ok := protocol.ReadText(r)
		sawListName = ok && name.Plain() == "styledchild"
	})
	servertest.HoldTicks(t, srv)
	if !sawListName {
		t.Fatal("late join lost list name")
	}
	p.Kick(plugin.Plain("bye"))
	actual, ok = protocol.ReadText(protocol.NewReader(c.AwaitPacket(protocol.IDPlayDisconnect)))
	if !ok || actual.Plain() != "bye" {
		t.Fatal("kick reason")
	}
	c.AwaitServed()
}

func TestPluginPlayerLookupsAndStaleHandles(t *testing.T) {
	t.Parallel()
	var host plugin.Server
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	cfg.Universe = t.TempDir()
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "lookup", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	srv.SetOpsFile(cfg.Universe + "/ops.json")
	c := servertest.DialServer(t, srv)
	c.ViewAsk = 3
	c.Join("Lookup")
	servertest.HoldTicks(t, srv)
	p, ok := host.PlayerNamed("LOOKUP")
	if !ok {
		t.Fatal("name lookup")
	}
	byID, ok := host.Player(p.UUID())
	if !ok || byID.Name() != p.Name() || len(host.Players()) != 1 {
		t.Fatal("UUID/list lookup")
	}
	if _, ok := host.Player(plugin.UUID{}); ok {
		t.Fatal("unknown online UUID")
	}
	if _, ok := host.OfflinePlayer(plugin.UUID{}); ok {
		t.Fatal("unknown offline UUID")
	}
	if profile := p.Profile(); profile.ID != p.UUID() || profile.Name != "Lookup" {
		t.Fatal(profile)
	}
	if p.Address() == nil || p.Address().Network() != "tcp" || p.Locale() != "en_GB" || p.ViewDistance() != 3 || p.Ping() < 0 || p.OpLevel() != 0 || p.HasPermission("anything") {
		t.Fatal("client info or permissions")
	}
	c.Send(protocol.IDPlayPluginMessageIn, func(b *protocol.Buffer) { b.PutString("minecraft:brand"); b.PutString("test-brand") })
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayDisconnect))
	if p.Brand() != "test-brand" {
		t.Fatal("client brand")
	}
	if first := p.FirstPlayed(); first.IsZero() || first.After(time.Now()) {
		t.Fatal(first)
	}
	if _, ok := p.RespawnPoint(); ok {
		t.Fatal("unexpected respawn")
	}
	srv.RunLine("spawnpoint Lookup 2 65 3 45 0")
	loc, ok := p.RespawnPoint()
	if !ok || loc.Pos != (plugin.Vec3{X: 2, Y: 65, Z: 3}) {
		t.Fatal("respawn point")
	}
	callback := false
	p.RequestCookie(plugin.MinecraftKey("pending"), func([]byte, bool) { callback = true })
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayCookieRequest)
	c.Send(protocol.IDPlayCookieResponse, func(b *protocol.Buffer) { b.PutString("minecraft:pending"); b.PutBool(false) })
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayDisconnect))
	srv.RunLine("op Lookup")
	if p.OpLevel() != 4 || !p.HasPermission("anything") {
		t.Fatal("operator fallback")
	}
	first := p.FirstPlayed().UnixMilli()
	id, name := p.UUID(), p.Name()
	c.Conn.Close()
	c.AwaitServed()
	servertest.RunTicks(t, srv, 1)
	if callback {
		t.Fatal("callback ran after disconnect")
	}
	if p.IsValid() {
		t.Fatal("disconnected handle valid")
	}
	if _, ok := p.Online(); ok {
		t.Fatal("stale handle online")
	}
	offline, ok := host.OfflinePlayer(id)
	if !ok || offline.Name() != name || offline.FirstPlayed().UnixMilli() != first || offline.LastSeen().Before(offline.FirstPlayed()) {
		t.Fatal("offline metadata")
	}
	if loc, ok := offline.RespawnPoint(); !ok || loc.Pos.X != 2 {
		t.Fatal("offline spawn")
	}
	servertest.ReleaseTicks(t, srv)
	next := servertest.DialServer(t, srv)
	next.ViewAsk = config.MinDistance
	next.Join("Lookup")
	servertest.HoldTicks(t, srv)
	if p.Name() != "" || p.UUID() != (plugin.UUID{}) || p.Address() != nil || p.Locale() != "" || p.Brand() != "" || p.Ping() != 0 || p.OpLevel() != 0 || p.ViewDistance() != 0 || p.ListName() != nil || !p.FirstPlayed().IsZero() || !p.LastSeen().IsZero() {
		t.Fatal("stale reads are not zero")
	}
	if p.IsValid() {
		t.Fatal("reconnect revived old handle")
	}
	if online, ok := offline.Online(); !ok || !online.IsValid() {
		t.Fatal("offline lookup does not find new session")
	}
	p.SendMessage(plugin.Plain("stale"))
	p.Transfer("bad.invalid", 1)
	p.SetDisplayName(plugin.Plain("stale"))
	p.SetListName(nil)
	p.SetListHeaderFooter(plugin.Plain("stale"), plugin.Plain("stale"))
	p.SendActionBar(plugin.Plain("stale"))
	p.SendTitle(plugin.Text{}, plugin.Text{}, 1, 1, 1)
	p.ClearTitle()
	p.ShowDialog(plugin.MinecraftKey("custom_options"))
	p.PlaySound(plugin.Vec3{}, registry.SoundBlockNoteBlockHarp, plugin.SoundMaster, 1, 1)
	p.StopSound(0, plugin.SoundMaster)
	p.SendBlockChange(plugin.BlockPos{}, 0)
	p.SendPluginMessage(plugin.MinecraftKey("brand"), nil)
	p.SendResourcePack(plugin.UUID{}, "", "", false, nil)
	p.RemoveResourcePack(plugin.UUID{})
	p.StoreCookie(plugin.MinecraftKey("cookie"), nil)
	p.RequestCookie(plugin.MinecraftKey("cookie"), func([]byte, bool) { t.Error("stale callback") })
	p.SpawnParticles(plugin.Particle{}, plugin.Vec3{}, 1, plugin.Vec3{}, 0)
	p.Kick(plugin.Plain("stale"))
	next.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlayDisconnect, protocol.IDPlaySystemChat, protocol.IDPlayTransfer, protocol.IDPlayTabList, protocol.IDPlayCookieRequest, protocol.IDPlayPlayerInfoUpdate, protocol.IDPlayBlockUpdate, protocol.IDPlayActionBarText, protocol.IDPlayTitleText, protocol.IDPlayShowDialog, protocol.IDPlaySoundEffect, protocol.IDPlayStopSound, protocol.IDPlayLevelParticles, protocol.IDPlayPluginMessage, protocol.IDPlayResourcePackPush, protocol.IDPlayResourcePackPop, protocol.IDPlayStoreCookie))
}

// vanilla: ClientboundResourcePackPushPacket, ClientboundResourcePackPopPacket
func TestPluginPlayerResourcePacks(t *testing.T) {
	t.Parallel()
	var host plugin.Server
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "packs", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Pack")
	servertest.HoldTicks(t, srv)
	p, _ := host.PlayerNamed("Pack")
	id := plugin.UUID{1, 2, 3}
	prompt := plugin.Plain("pack prompt").NotItalic()
	p.SendResourcePack(id, "https://example.org/pack.zip", "hash", false, &prompt)
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayResourcePackPush))
	if r.ReadUUID() != uuid.UUID(id) || r.ReadString(32767) != "https://example.org/pack.zip" || r.ReadString(40) != "hash" || r.ReadBool() || !r.ReadBool() {
		t.Fatal("pack fields")
	}
	text, ok := protocol.ReadText(r)
	if !ok || text.Plain() != "pack prompt" || r.Err() != nil {
		t.Fatal("pack prompt")
	}
	p.RemoveResourcePack(id)
	r = protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayResourcePackPop))
	if !r.ReadBool() || r.ReadUUID() != uuid.UUID(id) || r.Err() != nil {
		t.Fatal("pack pop")
	}
	for _, required := range []bool{false, true} {
		p.SendResourcePack(id, "https://example.org/pack.zip", "", required, nil)
		r = protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayResourcePackPush))
		r.ReadUUID()
		r.ReadString(32767)
		r.ReadString(40)
		if r.ReadBool() != required || r.ReadBool() || r.Err() != nil {
			t.Fatal("pack flags")
		}
		c.Send(protocol.IDPlayResourcePackIn, func(b *protocol.Buffer) { b.PutUUID(uuid.UUID(id)); b.PutVarInt(1) })
		if !required {
			c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayDisconnect))
		}
	}
	text, ok = protocol.ReadText(protocol.NewReader(c.AwaitPacket(protocol.IDPlayDisconnect)))
	if !ok || text.Translate != "multiplayer.requiredTexturePrompt.disconnect" {
		t.Fatal("required decline")
	}
}
