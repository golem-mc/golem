package plugins_test

import (
	"bytes"
	"math"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

var trialPluginPos = plugin.BlockPos{X: 4, Y: 64, Z: 2}

func pluginTrial(t *testing.T, h plugin.Server, p plugin.BlockPos) plugin.TrialSpawner {
	t.Helper()
	b, ok := h.Overworld().BlockEntity(p)
	if !ok {
		t.Error("missing trial block entity")
		return nil
	}
	trial, ok := b.(plugin.TrialSpawner)
	if !ok {
		t.Errorf("trial handle %T lacks TrialSpawner", b)
		return nil
	}
	if _, ok := b.(plugin.Spawner); ok {
		t.Error("trial implements monster Spawner")
	}
	return trial
}

func pluginTrialTag(t *testing.T, srv *server.Server, p plugin.BlockPos) nbt.Compound {
	t.Helper()
	tag, ok := srv.BlockEntityTagAt(int32(p.X), int32(p.Y), int32(p.Z))
	if !ok {
		t.Fatal("missing saved trial tag")
	}
	return tag
}

func trialTagBytes(t *testing.T, tag nbt.Compound) []byte {
	t.Helper()
	data, err := nbt.EncodeNetwork(tag)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// vanilla: TrialSpawnerConfig.DIRECT_CODEC, TrialSpawner.FullConfig.
func TestPluginTrialSpawnerSettings(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	run(func() {
		p := trialPluginPos
		for _, x := range []int32{4, 5} {
			w.SetBlock(x, 64, 2, registry.BlockTrialSpawner.DefaultState())
			w.EnsureBlockEntityForBlock(x, 64, 2)
		}
		b := pluginTrial(t, h, p)
		if b == nil {
			return
		}
		pw := h.Overworld()
		chunk, _ := pw.Chunk(plugin.ChunkPos{})
		if b.World() != pw || b.Pos() != p || b.Type() != plugin.MinecraftKey("trial_spawner") || !b.IsValid() || chunk.BlockEntities()[0] != b {
			t.Error("lookup/enumeration identity")
		}
		defaults := plugin.TrialSpawnerConfig{SpawnRange: 4, TotalMobs: 6, SimultaneousMobs: 2, TotalMobsAddedPerPlayer: 2, SimultaneousMobsAddedPerPlayer: 1, TicksBetweenSpawn: 40}
		if b.Config(false) != defaults || b.Config(true) != defaults || b.NextSpawnAt() != 0 || b.RequiredPlayerRange() != 14 || b.EntityType() != -1 {
			t.Error("trial defaults")
		}
		normal := plugin.TrialSpawnerConfig{SpawnRange: 3, TotalMobs: 1.25, SimultaneousMobs: 4.75, TotalMobsAddedPerPlayer: .5, SimultaneousMobsAddedPerPlayer: .25, TicksBetweenSpawn: 7}
		ominous := plugin.TrialSpawnerConfig{SpawnRange: 8, TotalMobs: 9.5, SimultaneousMobs: 3.25, TotalMobsAddedPerPlayer: 1.75, SimultaneousMobsAddedPerPlayer: .75, TicksBetweenSpawn: 13}
		b.SetConfig(false, normal)
		if b.Config(true) != defaults {
			t.Error("normal edit changed ominous")
		}
		b.SetConfig(true, ominous)
		if b.Config(false) != normal || b.Config(true) != ominous || registry.StateBool(w.BlockAt(4, 64, 2), registry.PropOminous) {
			t.Error("independent fractional modes")
		}
		snapshot := b.Config(false)
		b.SetConfig(false, defaults)
		if snapshot != normal {
			t.Error("config is not a value snapshot")
		}
		b.SetConfig(false, normal)
		// Invalid input must reject all fields before writing any of them.
		before := pluginTrialTag(t, srv, p)
		beforeBytes := trialTagBytes(t, before)
		for field := range 6 {
			for _, v := range []float64{-1, math.Inf(1), math.Inf(-1), math.NaN()} {
				bad := normal
				bad.SpawnRange = 12
				switch field {
				case 0:
					bad.SpawnRange = 0
				case 1:
					bad.TotalMobs = float32(v)
				case 2:
					bad.SimultaneousMobs = float32(v)
				case 3:
					bad.TotalMobsAddedPerPlayer = float32(v)
				case 4:
					bad.SimultaneousMobsAddedPerPlayer = float32(v)
				case 5:
					bad.TicksBetweenSpawn = -1
				}
				b.SetConfig(false, bad)
			}
		}
		for _, bad := range []plugin.TrialSpawnerConfig{
			{SpawnRange: 129}, {SpawnRange: 1, TicksBetweenSpawn: 1 << 31},
		} {
			b.SetConfig(false, bad)
		}
		b.SetRequiredPlayerRange(0)
		b.SetRequiredPlayerRange(129)
		b.SetEntityType(-1)
		b.SetEntityType(registry.EntityType(len(registry.EntityTypes)))
		if !bytes.Equal(beforeBytes, trialTagBytes(t, pluginTrialTag(t, srv, p))) {
			t.Error("invalid edit wrote partial data")
		}
		for _, at := range []int64{math.MinInt64, -123, 0, math.MaxInt64} {
			b.SetNextSpawnAt(at)
			if b.NextSpawnAt() != at || pluginTrialTag(t, srv, p)["next_mob_spawns_at"] != nbt.Long(at) {
				t.Error("absolute signed timestamp")
			}
		}
		for _, distance := range []int{1, 128} {
			b.SetRequiredPlayerRange(distance)
			if b.RequiredPlayerRange() != distance {
				t.Error("range boundary")
			}
		}
		for _, rangeLimit := range []int{1, 128} {
			edge := plugin.TrialSpawnerConfig{SpawnRange: rangeLimit, TicksBetweenSpawn: math.MaxInt32, TotalMobs: math.MaxFloat32}
			b.SetConfig(false, edge)
			if b.Config(false) != edge {
				t.Error("valid codec boundary rejected")
			}
		}
		// Read only the saved selection, without choosing from potentials.
		named := nbt.String("minecraft:trial_chamber/melee/husk/ominous")
		w.SetBlockEntityTag(4, 64, 2, world.KindTrialSpawner.Name(), nbt.Compound{"normal_config": named, "ominous_config": named})
		w.SetBlockEntityTag(5, 64, 2, world.KindTrialSpawner.Name(), nbt.Compound{"normal_config": named, "ominous_config": named})
		other := pluginTrial(t, h, plugin.BlockPos{X: 5, Y: 64, Z: 2})
		selectedBefore := trialTagBytes(t, pluginTrialTag(t, srv, p))
		if b.EntityType() != -1 || b.EntityType() != -1 || !bytes.Equal(selectedBefore, trialTagBytes(t, pluginTrialTag(t, srv, p))) {
			t.Error("entity read selected or mutated data")
		}
		namedSettings := other.Config(false)
		b.SetConfig(false, normal)
		changed := pluginTrialTag(t, srv, p)
		materialized := mustCompound(t, changed, "normal_config")
		if changed["ominous_config"] != named || other.Config(false) != namedSettings || pluginTrialTag(t, srv, other.Pos())["normal_config"] != named {
			t.Error("named config leaked to another mode or spawner")
		}
		if materialized["spawn_potentials"] == nil || materialized["loot_tables_to_eject"] == nil {
			t.Error("named nested fields lost")
		}
		// Inline custom rules, equipment and unknown fields also survive edits.
		potentials := nbt.Copy(materialized["spawn_potentials"]).(nbt.List)
		customPotential, _ := nbt.CompoundView(potentials.Items[0])
		customData := mustCompound(t, customPotential, "data")
		customData["custom_spawn_rules"] = nbt.Compound{"sky_light_limit": nbt.Compound{"min_inclusive": nbt.Int(1), "max_inclusive": nbt.Int(12)}}
		nested := nbt.Compound{"spawn_potentials": potentials, "loot_tables_to_eject": materialized["loot_tables_to_eject"], "items_to_drop_when_ominous": nbt.String("test:items"), "custom": nbt.Compound{"value": nbt.Int(42)}}
		w.SetBlockEntityTag(4, 64, 2, world.KindTrialSpawner.Name(), nbt.Compound{"normal_config": nested, "spawn_data": nbt.Compound{"entity": nbt.Compound{"id": nbt.String("minecraft:cow")}}, "registered_players": nbt.List{Elem: nbt.TypeIntArray, Items: []nbt.Tag{nbt.IntArray{1, 2, 3, 4}}}, "next_mob_spawns_at": nbt.Long(1000), "total_mobs_spawned": nbt.Int(3)})
		old := pluginTrialTag(t, srv, p)
		oldBytes := trialTagBytes(t, old)
		b.SetConfig(false, normal)
		b.SetRequiredPlayerRange(9)
		now := pluginTrialTag(t, srv, p)
		cfg := mustCompound(t, now, "normal_config")
		for key, value := range nested {
			if !reflect.DeepEqual(cfg[key], value) {
				t.Errorf("lost nested %s", key)
			}
		}
		if b.EntityType() != registry.EntityTypeCow || b.NextSpawnAt() != 1000 || now["total_mobs_spawned"] != nbt.Int(3) || !reflect.DeepEqual(now["registered_players"], old["registered_players"]) {
			t.Error("settings reset runtime data")
		}
		if !bytes.Equal(oldBytes, trialTagBytes(t, old)) || !bytes.Equal(beforeBytes, trialTagBytes(t, before)) {
			t.Error("edit mutated retained snapshot")
		}
		for _, id := range []string{"test:unknown", ""} {
			w.SetCarriedField(4, 64, 2, "spawn_data", nbt.Compound{"entity": nbt.Compound{"id": nbt.String(id)}})
			if b.EntityType() != -1 {
				t.Error("unknown selected ID recognized")
			}
		}
		w.SetCarriedField(4, 64, 2, "normal_config", nbt.String("test:unknown"))
		if b.Config(false) != defaults {
			t.Error("unknown named config fallback")
		}
		w.SetCarriedField(4, 64, 2, "normal_config", nbt.Compound{"spawn_range": nbt.Int(12)})
		defaults.SpawnRange = 12
		if b.Config(false) != defaults {
			t.Error("missing numeric fields lack defaults")
		}

		// The override accepts registry entities even when the mob ticker cannot
		// spawn that kind; validation is against the entity registry, not mob kinds.
		b.SetEntityType(registry.EntityTypeItem)
		tag := pluginTrialTag(t, srv, p)
		for _, key := range []string{"normal_config", "ominous_config"} {
			potentials, _ := nbt.Get[nbt.List](mustCompound(t, tag, key), "spawn_potentials")
			potential, _ := nbt.CompoundView(potentials.Items[0])
			if mustCompound(t, mustCompound(t, potential, "data"), "entity")["id"] != nbt.String("minecraft:item") {
				t.Error("valid non-mob entity override rejected")
			}
		}
	})
}

// vanilla: TrialSpawnerStateData.Packed, TrialSpawnerBlockEntity.saveAdditional.
func TestPluginTrialSpawnerPersistence(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	pos := world.ChunkPos{X: -2, Z: 3}
	p := plugin.BlockPos{X: -27, Y: 80, Z: 53}
	w.Acquire(pos)
	defer w.Release(pos)
	normal := plugin.TrialSpawnerConfig{SpawnRange: 2, TotalMobs: 2.75, SimultaneousMobs: 1.25, TotalMobsAddedPerPlayer: .5, SimultaneousMobsAddedPerPlayer: .75, TicksBetweenSpawn: 17}
	ominous := normal
	ominous.TotalMobs = 4.5
	ominous.TicksBetweenSpawn = 31
	var b plugin.TrialSpawner
	run(func() {
		w.SetBlock(-27, 80, 53, registry.BlockTrialSpawner.DefaultState())
		w.EnsureBlockEntityForBlock(-27, 80, 53)
		b = pluginTrial(t, h, p)
		b.SetConfig(false, normal)
		b.SetConfig(true, ominous)
		b.SetNextSpawnAt(1 << 40)
		b.SetRequiredPlayerRange(7)
		b.Data().Set(plugin.Key{"test", "value"}, int64(42))
		// This is the replacement path the trial ticker uses every dirty tick.
		tag := pluginTrialTag(t, srv, p)
		tag["next_mob_spawns_at"] = nbt.Long(12345)
		w.ReplaceCarried(-27, 80, 53, tag)
		fresh := pluginTrial(t, h, p)
		if b != fresh || b.NextSpawnAt() != 12345 {
			t.Error("handle did not follow ticker replacement")
		}
		if value, _ := plugin.GetData[int64](b.Data(), plugin.Key{"test", "value"}); value != 42 {
			t.Error("ticker replacement lost Data")
		}
		h.Overworld().Save(true)
	})
	w.Release(pos)
	w.DrainSaves()
	w.Acquire(pos)
	run(func() {
		fresh := pluginTrial(t, h, p)
		if b.IsValid() || b == fresh || !fresh.IsValid() {
			t.Error("unload retained handle")
		}
		if fresh.Config(false) != normal || fresh.Config(true) != ominous || fresh.NextSpawnAt() != 12345 || fresh.RequiredPlayerRange() != 7 {
			t.Error("saved settings lost")
		}
		if value, _ := plugin.GetData[int64](fresh.Data(), plugin.Key{"test", "value"}); value != 42 {
			t.Error("saved Data lost")
		}
		assertStaleTrial(t, b, normal)
		if fresh.Config(false) != normal || fresh.NextSpawnAt() != 12345 || fresh.RequiredPlayerRange() != 7 || len(fresh.Data().Keys()) != 1 {
			t.Error("unloaded setter edited replacement")
		}
		w.RemoveBlockEntity(-27, 80, 53)
		w.EnsureBlockEntityForBlock(-27, 80, 53)
		replacement := pluginTrial(t, h, p)
		assertStaleTrial(t, fresh, normal)
		if replacement.Config(false).TotalMobs != 6 || replacement.NextSpawnAt() != 0 || replacement.RequiredPlayerRange() != 14 || len(replacement.Data().Keys()) != 0 {
			t.Error("removed setter edited replacement")
		}
	})
}

func assertStaleTrial(t *testing.T, b plugin.TrialSpawner, cfg plugin.TrialSpawnerConfig) {
	t.Helper()
	b.SetConfig(false, cfg)
	b.SetConfig(true, cfg)
	b.SetNextSpawnAt(8)
	b.SetRequiredPlayerRange(8)
	b.SetEntityType(registry.EntityTypePig)
	b.Data().Set(plugin.Key{"test", "stale"}, true)
	if b.IsValid() || b.Config(false) != (plugin.TrialSpawnerConfig{}) || b.Config(true) != (plugin.TrialSpawnerConfig{}) || b.NextSpawnAt() != 0 || b.RequiredPlayerRange() != 0 || b.EntityType() != -1 || len(b.Data().Keys()) != 0 {
		t.Error("stale trial getters")
	}
}

// vanilla: TrialSpawnerStateData.getUpdateTag, TrialSpawnerBlockEntity.setState.
func TestPluginTrialSpawnerPackets(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	c := servertest.DialServer(t, srv)
	c.Join("TrialPackets")
	servertest.HoldTicks(t, srv)
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	var b plugin.TrialSpawner
	run(func() {
		state, _ := registry.WithProp(registry.BlockTrialSpawner.DefaultState(), registry.PropTrialSpawnerState, "active")
		state, _ = registry.WithBool(state, registry.PropOminous, true)
		w.SetBlock(4, 64, 2, state)
		w.SetBlockEntityTag(4, 64, 2, world.KindTrialSpawner.Name(), nbt.Compound{"spawn_data": nbt.Compound{"entity": nbt.Compound{"id": nbt.String("minecraft:cow")}}, "current_mobs": nbt.List{Elem: nbt.TypeIntArray, Items: []nbt.Tag{nbt.IntArray{1, 2, 3, 4}}}, "registered_players": nbt.List{Elem: nbt.TypeIntArray, Items: []nbt.Tag{nbt.IntArray{4, 3, 2, 1}}}, "total_mobs_spawned": nbt.Int(3), "cooldown_ends_at": nbt.Long(100), "ejecting_loot_table": nbt.String("test:keep"), "custom": nbt.Int(42)})
		b = pluginTrial(t, h, trialPluginPos)
		b.Data().Set(plugin.Key{"test", "secret"}, "private")
	})
	for _, edit := range []func(){func() { b.SetNextSpawnAt(1234) }, func() { b.SetRequiredPlayerRange(9) }, func() { b.SetConfig(false, plugin.TrialSpawnerConfig{SpawnRange: 2, TotalMobs: 1.5}) }} {
		run(edit)
		x, y, z, kind, tag := c.AwaitBlockEntityData()
		if x != 4 || y != 64 || z != 2 || kind != world.BlockEntityTypeID(world.KindTrialSpawner.Name()) || len(tag) != 2 || tag["spawn_data"] == nil || tag["next_mob_spawns_at"] != nbt.Long(1234) {
			t.Errorf("active update leaked or omitted fields: %v", tag)
		}
	}
	// No immediate tag may precede the queued inactive state.
	run(func() { b.SetEntityType(registry.EntityTypePig) })
	blockSeen, tagSeen := false, false
	c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		for _, change := range servertest.BlockChangesIn(t, id, data) {
			if change.X == 4 && change.Y == 64 && change.Z == 2 {
				blockSeen = true
				if registry.StateValue(change.State, registry.PropTrialSpawnerState) != "inactive" || !registry.StateBool(change.State, registry.PropOminous) {
					t.Error("override sent wrong state")
				}
			}
		}
		if id == protocol.IDPlayBlockEntityData {
			r := protocol.NewReader(data)
			x, y, z := r.ReadPosition()
			r.ReadVarInt()
			if x == 4 && y == 64 && z == 2 {
				tagSeen = true
				if !blockSeen {
					t.Error("override sent tag before block")
				}
				tag, _, err := nbt.DecodeNetwork(r.Rest())
				if err != nil {
					t.Error(err)
				} else if compound, ok := nbt.CompoundView(tag); !ok || len(compound) != 0 {
					t.Errorf("override tag leaked data: %v", tag)
				}
			}
		}
		return false
	})
	if !blockSeen || !tagSeen {
		t.Error("override omitted block or entity tag")
	}
	run(func() {
		tag := pluginTrialTag(t, srv, trialPluginPos)
		for _, key := range []string{"spawn_data", "current_mobs", "registered_players", "next_mob_spawns_at", "cooldown_ends_at", "total_mobs_spawned"} {
			if value := tag[key]; value != nil && value != nbt.Long(0) && value != nbt.Int(0) { // lists are checked separately below
				if list, ok := value.(nbt.List); !ok || len(list.Items) != 0 {
					t.Errorf("override retained %s: %v", key, value)
				}
			}
		}
		if b.EntityType() != -1 || b.NextSpawnAt() != 0 || tag["ejecting_loot_table"] != nbt.String("test:keep") || tag["custom"] != nbt.Int(42) || !registry.StateBool(w.BlockAt(4, 64, 2), registry.PropOminous) || registry.StateValue(w.BlockAt(4, 64, 2), registry.PropTrialSpawnerState) != "inactive" {
			t.Error("override reset/state contract")
		}
		for _, key := range []string{"normal_config", "ominous_config"} {
			list, _ := nbt.Get[nbt.List](mustCompound(t, tag, key), "spawn_potentials")
			potential, _ := nbt.CompoundView(list.Items[0])
			entity := mustCompound(t, mustCompound(t, potential, "data"), "entity")
			if len(list.Items) != 1 || entity["id"] != nbt.String("minecraft:pig") {
				t.Error("override did not replace both potentials")
			}
		}
		if value, _ := plugin.GetData[string](b.Data(), plugin.Key{"test", "secret"}); value != "private" {
			t.Error("override lost Data")
		}
		b.SetNextSpawnAt(-1)
	})
	_, _, _, _, tag := c.AwaitBlockEntityData()
	if len(tag) != 0 {
		t.Errorf("inactive update exposed fields: %v", tag)
	}
	run(func() {
		w.SetCarriedField(4, 64, 2, "spawn_data", nbt.Compound{"entity": nbt.Compound{"id": nbt.String("minecraft:pig")}})
		b.SetRequiredPlayerRange(8)
	})
	_, _, _, _, tag = c.AwaitBlockEntityData()
	if len(tag) != 1 || tag["spawn_data"] == nil {
		t.Errorf("inactive selected update: %v", tag)
	}
	run(func() {
		b.SetConfig(false, plugin.TrialSpawnerConfig{})
		b.SetRequiredPlayerRange(129)
		b.SetEntityType(-1)
	})
	c.ExpectNoneWithinTicks(srv, 1, func(id int32, _ []byte) bool {
		return id == protocol.IDPlayBlockEntityData || id == protocol.IDPlayBlockUpdate || id == protocol.IDPlaySectionBlocks
	})
	run(func() {
		w.RemoveBlockEntity(4, 64, 2)
		w.EnsureBlockEntityForBlock(4, 64, 2)
		assertStaleTrial(t, b, plugin.TrialSpawnerConfig{SpawnRange: 2})
	})
	c.ExpectNoneWithinTicks(srv, 1, func(id int32, _ []byte) bool {
		return id == protocol.IDPlayBlockEntityData || id == protocol.IDPlayBlockUpdate || id == protocol.IDPlaySectionBlocks
	})
}

// vanilla: TrialSpawnerState.ACTIVE, TrialSpawnerConfig.calculateTargetTotalMobs,
// calculateTargetSimultaneousMobs, TrialSpawnerStateData.tryDetectPlayers.
func TestPluginTrialSpawnerGameplay(t *testing.T) {
	for _, ominous := range []bool{false, true} {
		name := "normal"
		if ominous {
			name = "ominous"
		}
		t.Run(name, func(t *testing.T) {
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, h, run := worldPlugin(t, cfg)
			servertest.FreezeUntilHeld(t, srv)
			servertest.ReleaseTicks(t, srv)
			c := servertest.DialServer(t, srv)
			c.Join("Trialist")
			second := servertest.DialServer(t, srv)
			second.Join("SecondTrialist")
			servertest.HoldTicks(t, srv)
			srv.SetTickFrozen(true)
			c.MoveToGround(2.5, 64, 5.5, true)
			second.MoveToGround(6.5, 64, 5.5, true)
			srv.SetPlayerGameMode("Trialist", 0)
			srv.SetPlayerGameMode("SecondTrialist", 0)
			w := srv.OverworldWorld()
			// The room and potentials are fixtures; every numeric setting is edited
			// through the test plugin, on its scheduled tick callback.
			for x := int32(-4); x <= 12; x++ {
				for z := int32(-6); z <= 10; z++ {
					w.SetBlock(x, 63, z, registry.BlockGrassBlock.DefaultState())
					for y := int32(64); y <= 67; y++ {
						w.SetBlock(x, y, z, registry.BlockAir.DefaultState())
					}
				}
			}
			potential := func(kind string) nbt.Compound {
				return nbt.Compound{
					"spawn_potentials": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"weight": nbt.Int(1), "data": nbt.Compound{"entity": nbt.Compound{"id": nbt.String(kind), "NoAI": nbt.Byte(1)}}}}},
				}
			}
			normal := plugin.TrialSpawnerConfig{SpawnRange: 3, TotalMobs: 1.25, SimultaneousMobs: .75, TotalMobsAddedPerPlayer: 1.75, SimultaneousMobsAddedPerPlayer: 1.25, TicksBetweenSpawn: 5}
			other := plugin.TrialSpawnerConfig{SpawnRange: 3, TotalMobs: 2.5, SimultaneousMobs: .5, TotalMobsAddedPerPlayer: 1.75, SimultaneousMobsAddedPerPlayer: .75, TicksBetweenSpawn: 11}
			selected := normal
			total, cap, kind := 3, 2, "minecraft:cow"
			if ominous {
				selected = other
				total, cap, kind = 4, 1, "minecraft:pig"
			}
			var b plugin.TrialSpawner
			run(func() {
				w.SetBlock(4, 64, 2, registry.BlockTrialSpawner.DefaultState())
				w.SetBlockEntityTag(4, 64, 2, world.KindTrialSpawner.Name(), nbt.Compound{"normal_config": potential("minecraft:cow"), "ominous_config": potential("minecraft:pig")})
				b = pluginTrial(t, h, trialPluginPos)
				b.SetConfig(false, normal)
				b.SetConfig(true, other)
				b.SetRequiredPlayerRange(1)
				b.SetNextSpawnAt(-100)
				b.Data().Set(plugin.Key{"test", "retained"}, int64(42))
			})
			srv.SetDifficulty(2)
			srv.SetTickFrozen(false)
			servertest.RunTicks(t, srv, 45)
			tag := pluginTrialTag(t, srv, trialPluginPos)
			players, _ := nbt.Get[nbt.List](tag, "registered_players")
			if len(players.Items) != 0 || srv.MobCount() != 0 || registry.StateValue(w.BlockAt(4, 64, 2), registry.PropTrialSpawnerState) != "waiting_for_players" {
				t.Fatal("out-of-range players activated trial or past timestamp forced activation")
			}
			srv.SetTickFrozen(true)
			run(func() { b.SetRequiredPlayerRange(14) })
			if ominous {
				srv.GiveEffect("Trialist", "minecraft:trial_omen", 10000, 0, false)
			}
			srv.SetTickFrozen(false)
			servertest.UntilTicks(t, srv, 60, "two registered survival players", func() bool {
				tag := pluginTrialTag(t, srv, trialPluginPos)
				players, _ := nbt.Get[nbt.List](tag, "registered_players")
				return len(players.Items) == 2 && registry.StateValue(w.BlockAt(4, 64, 2), registry.PropTrialSpawnerState) == "active" && registry.StateBool(w.BlockAt(4, 64, 2), registry.PropOminous) == ominous
			})
			srv.SetTickFrozen(true)
			now, _ := srv.Time()
			pending := now + 20
			run(func() {
				b.SetNextSpawnAt(pending)
				changed := selected
				changed.TicksBetweenSpawn = selected.TicksBetweenSpawn + 2
				b.SetConfig(ominous, changed)
				if b.NextSpawnAt() != pending {
					t.Error("SetConfig rescheduled pending spawn")
				}
				selected = changed
			})
			c.SettleAndCollect(func(int32, []byte) {})
			events, flames := 0, []int32{}
			seen := map[int32]bool{}
			spawnTimes := []int64{}
			lastTotal := 0
			recordSpawn := func() {
				tag := pluginTrialTag(t, srv, trialPluginPos)
				count, _ := nbt.IntValue(tag["total_mobs_spawned"])
				if int(count) > lastTotal {
					if int(count) != lastTotal+1 {
						t.Error("more than one spawn in a tick")
					}
					now, _ := srv.Time()
					spawnTimes = append(spawnTimes, now)
					next, _ := nbt.LongValue(tag["next_mob_spawns_at"])
					if next != now+int64(selected.TicksBetweenSpawn) {
						t.Errorf("scheduled at %d, spawn time %d + interval %d", next, now, selected.TicksBetweenSpawn)
					}
					if now < pending {
						t.Error("spawn preceded pending timestamp")
					}
					lastTotal = int(count)
				}
				mobs, _ := nbt.Get[nbt.List](tag, "current_mobs")
				if len(mobs.Items) > cap {
					t.Errorf("tracked %d mobs, cap %d", len(mobs.Items), cap)
				}
				for _, mob := range srv.Mobs() {
					if mob.Kind != kind {
						t.Errorf("spawned %s in %s mode", mob.Kind, name)
					}
					seen[mob.ID] = true
				}
			}
			step := func(n int) {
				for range n {
					c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
						if id == protocol.IDPlayWorldEvent {
							r := protocol.NewReader(data)
							if r.ReadI32() == 3011 {
								x, y, z := r.ReadPosition()
								flame := r.ReadI32()
								if x == 4 && y == 64 && z == 2 {
									events++
									flames = append(flames, flame)
								}
							}
						}
						return false
					})
					recordSpawn()
				}
			}
			srv.SetTickFrozen(false)
			for range 100 {
				if lastTotal >= cap {
					break
				}
				step(1)
			}
			if lastTotal != cap {
				t.Fatalf("spawned %d, want simultaneous cap %d", lastTotal, cap)
			}
			// Wait past the selected interval without killing: the cap must hold.
			step(2 * selected.TicksBetweenSpawn)
			if lastTotal != cap || len(seen) != cap {
				t.Fatal("simultaneous cap did not hold")
			}
			if cap == 2 && spawnTimes[1]-spawnTimes[0] < int64(selected.TicksBetweenSpawn) {
				t.Error("cadence shorter than fixed interval")
			}
			srv.RunLine("kill @e[type=" + kind + "]")
			for range 200 {
				step(1)
				if lastTotal == total {
					break
				}
				if len(seen) > cap {
					srv.RunLine("kill @e[type=" + kind + "]")
				}
			}
			if lastTotal != total || len(seen) != total {
				t.Fatalf("spawned %d distinct mobs, saved total %d, want floor target %d", len(seen), lastTotal, total)
			}
			// Leave the final mob alive: reaching the target must stop spawning even
			// after a slot is freed, until the reward transition resets statistics.
			step(2 * selected.TicksBetweenSpawn)
			if len(seen) != total || lastTotal != total {
				t.Error("extra spawns after target")
			}
			if events != total {
				t.Errorf("client received %d trial spawn events, want %d", events, total)
			}
			for _, flame := range flames {
				want := int32(0)
				if ominous {
					want = 1
				}
				if flame != want {
					t.Errorf("spawn flame %d, want %d", flame, want)
				}
			}
			if ominous {
				srv.RunLine("kill @e[type=" + kind + "]")
				step(35)
				if len(seen) != total || registry.StateValue(w.BlockAt(4, 64, 2), registry.PropTrialSpawnerState) != "waiting_for_reward_ejection" {
					t.Error("extra spawn or missing reward transition after final kill")
				}
			}
			// Old handles follow real ticker replacements, including private Data.
			srv.SetTickFrozen(true)
			var oldIDs []int32
			for _, mob := range srv.Mobs() {
				oldIDs = append(oldIDs, mob.ID)
			}
			run(func() {
				if pluginTrial(t, h, trialPluginPos) != b || b.Config(ominous) != selected {
					t.Error("ticker broke handle/settings identity")
				}
				if value, _ := plugin.GetData[int64](b.Data(), plugin.Key{"test", "retained"}); value != 42 {
					t.Error("ticker lost private Data")
				}
				b.SetEntityType(registry.EntityTypeChicken)
			})
			// Keep the trial disabled while ticking mobs, proving override doesn't
			// kill/discard any of the entities it previously tracked.
			srv.SetDifficulty(0)
			srv.SetTickFrozen(false)
			servertest.RunTicks(t, srv, 25)
			for _, id := range oldIDs {
				found := false
				for _, mob := range srv.Mobs() {
					found = found || mob.ID == id
				}
				if !found {
					t.Errorf("override discarded mob %d", id)
				}
			}
		})
	}
}
