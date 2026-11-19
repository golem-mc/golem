package plugins_test

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func TestPluginTypedBlockEntityKinds(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	cases := []struct {
		block   registry.Block
		kind    world.BlockEntityKind
		accepts func(plugin.BlockEntity) bool
	}{
		{registry.BlockOakSign, world.KindSign, func(b plugin.BlockEntity) bool { _, ok := b.(plugin.Sign); return ok }},
		{registry.BlockOakHangingSign, world.KindHangingSign, func(b plugin.BlockEntity) bool { _, ok := b.(plugin.Sign); return ok }},
		{registry.BlockSpawner, world.KindSpawner, func(b plugin.BlockEntity) bool { _, ok := b.(plugin.Spawner); return ok }},
		{registry.BlockCommandBlock, world.KindCommandBlock, func(b plugin.BlockEntity) bool { _, ok := b.(plugin.CommandBlock); return ok }},
		{registry.BlockChainCommandBlock, world.KindCommandBlock, func(b plugin.BlockEntity) bool { _, ok := b.(plugin.CommandBlock); return ok }},
		{registry.BlockRepeatingCommandBlock, world.KindCommandBlock, func(b plugin.BlockEntity) bool { _, ok := b.(plugin.CommandBlock); return ok }},
		{registry.BlockBeacon, world.KindBeacon, func(b plugin.BlockEntity) bool { _, ok := b.(plugin.Beacon); return ok }},
	}
	for i, tc := range cases {
		w.SetBlock(int32(i), 80, 0, tc.block.DefaultState())
		w.EnsureBlockEntity(int32(i), 80, 0, tc.kind)
	}
	run(func() {
		pw := h.Overworld()
		chunk, _ := pw.Chunk(plugin.ChunkPos{})
		for i, tc := range cases {
			b, ok := pw.BlockEntity(plugin.BlockPos{X: i, Y: 80})
			if !ok || !tc.accepts(b) {
				t.Errorf("%s: live handle does not implement its declared kind", tc.block.Name())
				continue
			}
			found := false
			for _, cb := range chunk.BlockEntities() {
				if cb == b {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: chunk handle differs from world handle", tc.block.Name())
			}
		}
	})
}

// vanilla: SignBlockEntity.setText, BaseSpawner.setEntityId,
// BaseCommandBlock.setCommand, BeaconBlockEntity.filterEffect.
func TestPluginBlockEntityEditsLiveAndSaveReload(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	srv, h, run := worldPlugin(t, cfg)
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	pos := world.ChunkPos{X: -2, Z: 3}
	w.Acquire(pos)
	defer w.Release(pos)
	blocks := []registry.Block{registry.BlockOakSign, registry.BlockSpawner, registry.BlockCommandBlock, registry.BlockBeacon}
	kinds := []world.BlockEntityKind{world.KindSign, world.KindSpawner, world.KindCommandBlock, world.KindBeacon}
	entities := make([]*world.BlockEntity, len(blocks))
	for i, block := range blocks {
		w.SetBlock(-27+int32(i), 80, 53, block.DefaultState())
		entities[i] = w.EnsureBlockEntity(-27+int32(i), 80, 53, kinds[i])
	}
	entities[0].Use(func(e *world.BlockEntity) {
		lines := nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("Old filtered"), nbt.String(""), nbt.String(""), nbt.String("")}}
		e.LoadBlockEntityData(e.TypeID(), nbt.Compound{"front_text": nbt.Compound{"messages": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("Old unfiltered"), nbt.String(""), nbt.String(""), nbt.String("")}}, "filtered_messages": lines}})
	})
	entities[1].Use(func(e *world.BlockEntity) {
		e.SpawnData = nbt.Compound{"entity": nbt.Compound{"id": nbt.String("minecraft:pig"), "CustomName": nbt.String("Keep me")}}
	})
	entities[2].Use(func(e *world.BlockEntity) { e.SuccessCount = 9 })
	entities[3].Use(func(e *world.BlockEntity) { e.BeaconLevels = 3 })
	run(func() { h.Overworld().Save(true) })
	var handles [4]plugin.BlockEntity
	var sign plugin.Sign
	var spawner plugin.Spawner
	var command plugin.CommandBlock
	var beacon plugin.Beacon
	run(func() {
		for i := range handles {
			handles[i], _ = h.Overworld().BlockEntity(plugin.BlockPos{X: -27 + i, Y: 80, Z: 53})
		}
		sign, _ = handles[0].(plugin.Sign)
		spawner, _ = handles[1].(plugin.Spawner)
		command, _ = handles[2].(plugin.CommandBlock)
		beacon, _ = handles[3].(plugin.Beacon)
	})
	if sign == nil || spawner == nil || command == nil || beacon == nil {
		t.Fatal("typed handles missing")
	}
	front := [4]plugin.Text{plugin.Plain("Front").Color("red").Append(plugin.Plain(" child")), plugin.Translate("block.minecraft.stone"), plugin.Plain("third"), plugin.Plain("fourth")}
	back := [4]plugin.Text{plugin.Plain("Back").Color("blue")}
	name := plugin.Plain("Live name").Color("gold").Append(plugin.Plain(" child"))
	speed := plugin.Effect(registry.MustMobEffect("speed"))
	regen := plugin.Effect(registry.MustMobEffect("regeneration"))
	run(func() {
		if spawner.Delay() != 20 || spawner.MinDelay() != 200 || spawner.MaxDelay() != 800 || spawner.SpawnCount() != 4 || spawner.RequiredPlayerRange() != 16 {
			t.Error("spawner defaults")
		}
		if command.SuccessCount() != 9 || command.CustomName() != nil || beacon.Levels() != 3 || beacon.CustomName() != nil {
			t.Error("live command/beacon defaults")
		}
		if p, s := beacon.Effects(); p != -1 || s != -1 {
			t.Error("beacon default effects")
		}
		sign.SetColor(true, plugin.Red)
		sign.SetColor(false, plugin.Blue)
		sign.SetGlowing(true, true)
		sign.SetGlowing(false, false)
		sign.SetWaxed(true)
		sign.SetLines(true, front)
		sign.SetLines(false, back)
		sign.SetColor(true, plugin.DyeColor(255))
		spawner.SetEntityType(registry.EntityTypeSkeleton)
		spawner.SetEntityType(registry.EntityType(-1))
		spawner.SetDelays(40, 80)
		spawner.SetDelay(60)
		spawner.SetSpawnCount(2)
		spawner.SetRequiredPlayerRange(12)
		command.SetCommand("say Live command")
		command.SetCustomName(&name)
		beacon.SetCustomName(&name)
		beacon.SetEffects(plugin.Effect(registry.MustMobEffect("poison")), plugin.Effect(10000))
		if p, s := beacon.Effects(); p != -1 || s != -1 {
			t.Error("beacon accepted invalid effects")
		}
		beacon.SetEffects(speed, regen)
		for _, be := range handles {
			be.Data().Set(plugin.Key{"test", "value"}, int64(42))
		}
		if !reflect.DeepEqual(sign.Lines(true), front) || !reflect.DeepEqual(sign.Lines(false), back) || sign.Color(true) != plugin.Red || sign.Color(false) != plugin.Blue || !sign.Glowing(true) || sign.Glowing(false) || !sign.Waxed() {
			t.Error("sign fields")
		}
		if spawner.EntityType() != registry.EntityTypeSkeleton || spawner.Delay() != 60 || spawner.MinDelay() != 40 || spawner.MaxDelay() != 80 || spawner.SpawnCount() != 2 || spawner.RequiredPlayerRange() != 12 {
			t.Error("spawner fields")
		}
		if command.Command() != "say Live command" || command.SuccessCount() != 0 {
			t.Error("command did not reset success")
		}
		for _, be := range []plugin.Nameable{command, beacon} {
			if !reflect.DeepEqual(be.CustomName(), &name) {
				t.Errorf("custom name=%+v", be.CustomName())
			}
			returned := be.CustomName()
			returned.Children[0].Literal = "Changed copy"
			if !reflect.DeepEqual(be.CustomName(), &name) {
				t.Error("name read aliases live state")
			}
		}
		lines := sign.Lines(true)
		lines[0].Children[0].Literal = "Changed copy"
		if !reflect.DeepEqual(sign.Lines(true), front) {
			t.Error("sign read aliases live state")
		}
		if p, s := beacon.Effects(); p != speed || s != regen {
			t.Error("beacon effects")
		}
		h.Overworld().Save(true)
	})
	// Read the disk while the original server is still running: its shutdown
	// save must not conceal missing dirty marks on plugin edits.
	saved, err := world.NewWorld(cfg.LevelName, world.NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer saved.Close()
	saved.Acquire(pos)
	defer saved.Release(pos)
	for i := range handles {
		be := saved.BlockEntity(-27+int32(i), 80, 53)
		if be == nil {
			t.Fatal("saved block entity missing")
		}
		be.Use(func(e *world.BlockEntity) {
			tag := e.EncodeNBT()
			data, _ := nbt.Get[nbt.Compound](tag, "plugins")
			if data["test:value"] != nbt.Long(42) {
				t.Errorf("saved plugin data=%v", data)
			}
			switch i {
			case 0:
				text, ok := registry.SignTextOf(mustCompound(t, tag, "front_text"))
				if !ok || text.Filtered != nil || text.Dye != int32(plugin.Red) || !text.Glowing || !e.Waxed || e.SignBack.Color != "blue" {
					t.Error("saved sign")
				}
				if got, _ := protocol.TextOf(text.Messages[0]); got.Plain() != "Front child" {
					t.Error("saved sign component")
				}
			case 1:
				if e.SpawnEntityID() != "minecraft:skeleton" || e.Delay != 60 || e.MinSpawnDelay != 40 || e.MaxSpawnDelay != 80 || e.SpawnCount != 2 || e.RequiredPlayerRange != 12 {
					t.Error("saved spawner")
				}
				spawn, _ := nbt.Get[nbt.Compound](e.SpawnData.Entries(), "entity")
				if spawn["CustomName"] != nbt.String("Keep me") {
					t.Error("changing entity type lost spawn data")
				}
			case 2:
				if e.Command != "say Live command" || e.SuccessCount != 0 || e.CustomName == nil || e.CustomName.Plain() != "Live name child" {
					t.Error("saved command block")
				}
			case 3:
				if e.BeaconPrimary != int32(speed) || e.BeaconSecondary != int32(regen) || e.CustomName == nil || e.CustomName.Plain() != "Live name child" {
					t.Error("saved beacon")
				}
			}
		})
	}
	// A second lookup observes changes made through the original handles, and
	// original handles observe gameplay writes rather than retaining a snapshot.
	entities[1].Use(func(e *world.BlockEntity) { e.Delay = 17 })
	run(func() {
		b, _ := h.Overworld().BlockEntity(spawner.Pos())
		if b.(plugin.Spawner).Delay() != 17 || spawner.Delay() != 17 {
			t.Error("spawner is a snapshot")
		}
		command.SetCustomName(nil)
		beacon.SetCustomName(nil)
		if command.CustomName() != nil || beacon.CustomName() != nil {
			t.Error("name not cleared")
		}
		sign.SetWaxed(false)
		sign.SetGlowing(true, false)
		if sign.Waxed() || sign.Glowing(true) {
			t.Error("sign flags not cleared")
		}
	})
	w.Release(pos)
	w.DrainSaves()
	w.Acquire(pos)
	run(func() {
		for _, old := range handles {
			fresh, ok := h.Overworld().BlockEntity(old.Pos())
			if old.IsValid() || !ok || !fresh.IsValid() || old == fresh {
				t.Error("handle did not expire across chunk reload")
				continue
			}
			if value, ok := plugin.GetData[int64](fresh.Data(), plugin.Key{"test", "value"}); !ok || value != 42 {
				t.Error("reloaded handle lost plugin data")
			}
		}
		if !reflect.DeepEqual(sign.Lines(true), [4]plugin.Text{}) {
			t.Error("unloaded sign returned live lines")
		}
		if spawner.Delay() != 0 || command.Command() != "" || command.CustomName() != nil || beacon.Levels() != 0 {
			t.Error("unloaded handles returned live fields")
		}
		sign.SetLines(true, [4]plugin.Text{plugin.Plain("Unloaded")})
		fresh, _ := h.Overworld().BlockEntity(sign.Pos())
		if !reflect.DeepEqual(fresh.(plugin.Sign).Lines(true), front) {
			t.Error("unloaded sign handle changed the reloaded sign")
		}
	})
}

func mustCompound(t *testing.T, tag nbt.Compound, key string) nbt.Compound {
	t.Helper()
	c, ok := nbt.Get[nbt.Compound](tag, key)
	if !ok {
		t.Errorf("missing compound %s", key)
	}
	return c
}

// vanilla: CommandBlock.tick, BaseSpawner.serverTick, BeaconBlockEntity.tick.
func TestPluginBlockEntityEditsAffectGameplay(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	c := servertest.DialServer(t, srv)
	c.Join("PluginObserver")
	servertest.HoldTicks(t, srv)
	srv.SetTickFrozen(true)
	for _, line := range []string{
		"setblock 2 64 0 command_block",
		"fill 7 63 0 9 63 2 iron_block",
		"setblock 8 64 1 beacon",
		`setblock 4 64 2 spawner{SpawnData:{entity:{id:"minecraft:pig",Pos:[4.5d,64d,4.5d],NoAI:1b},custom_spawn_rules:{block_light_limit:[0,15],sky_light_limit:[0,15]}}}`,
	} {
		srv.RunLine(line)
	}
	var command plugin.CommandBlock
	var beacon plugin.Beacon
	run(func() {
		pw := h.Overworld()
		cb, _ := pw.BlockEntity(plugin.BlockPos{X: 2, Y: 64})
		bb, _ := pw.BlockEntity(plugin.BlockPos{X: 8, Y: 64, Z: 1})
		sb, _ := pw.BlockEntity(plugin.BlockPos{X: 4, Y: 64, Z: 2})
		command, _ = cb.(plugin.CommandBlock)
		beacon, _ = bb.(plugin.Beacon)
		spawner, ok := sb.(plugin.Spawner)
		if command == nil || beacon == nil || !ok {
			t.Error("typed handles missing")
			return
		}
		name := plugin.Plain("Plugin command")
		command.SetCustomName(&name)
		command.SetCommand("say Running")
		beacon.SetEffects(plugin.Effect(registry.MustMobEffect("speed")), -1)
		spawner.SetEntityType(registry.EntityTypeCow)
		spawner.SetDelay(0)
		spawner.SetDelays(200, 200)
		spawner.SetSpawnCount(1)
		spawner.SetRequiredPlayerRange(16)
	})
	if command == nil || beacon == nil {
		t.Fatal("typed handles missing")
	}
	srv.RunLine("setblock 2 65 0 redstone_block")
	srv.SetTickFrozen(false)
	servertest.RunTicks(t, srv, 80)
	c.ExpectSayLine(t, "Plugin command", "Running")
	effect := c.AwaitMobEffect()
	if effect.Entity != c.PlayerEntityID || effect.Effect != registry.MustMobEffect("speed") || effect.Amplifier != 0 {
		t.Errorf("beacon effect=%+v", effect)
	}
	spawned := false
	for _, mob := range srv.Mobs() {
		if mob.Kind == "minecraft:cow" {
			spawned = true
		}
	}
	if !spawned {
		t.Error("plugin-configured spawner did not spawn a cow")
	}
	run(func() {
		if command.SuccessCount() != 1 {
			t.Errorf("command success=%d", command.SuccessCount())
		}
		if beacon.Levels() != 1 {
			t.Errorf("beacon levels=%d", beacon.Levels())
		}
	})
}
