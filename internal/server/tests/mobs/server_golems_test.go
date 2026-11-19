package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The golem builds, black box: a carved pumpkin set down over the vanilla
// patterns — the snow column, the iron T — clears its blocks and stands the
// mob up, the way CarvedPumpkinBlock.onPlace's trySpawnGolem does. The
// pattern decisions themselves live in internal/server/rules/golems.go, whose unit
// tests cover the shapes' other orientations and broken forms; these are
// the builds a player lays with a held item.

// golemScene joins two creative players into the flat world (stone up to
// y=63): a builder holding one of the pumpkin items a golem head takes, and
// a watcher standing clear to hear the clear's announcements. mob_griefing is
// off so the built snow golem lays no trail: on its first tick it would put a
// snow layer in its own foot cell, which is a pattern cell the tests assert
// cleared.
func golemScene(t *testing.T, pumpkin string) (*server.Server, *servertest.Client, *servertest.Client) {
	t.Helper()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, connect := servertest.Start(t, cfg)
	srv.SetGameRule(world.RuleMobGriefing.String(), false)
	watcher := connect().Join("Watcher")
	builder := connect().Join("Builder")
	watcher.ExpectPlayerAdded("Builder")
	srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, pumpkin), Count: 1})
	builder.Settle()
	watcher.Settle()
	return srv, builder, watcher
}

// snowPattern is the column: two snow blocks and the pumpkin over them, the
// pumpkin's cell first in the walk the clear takes downward.
func snowPattern() [][3]int32 {
	return [][3]int32{{2, 66, 3}, {2, 65, 3}, {2, 64, 3}}
}

// ironTPattern is the T: four iron blocks — the body under the pumpkin, the
// two arms beside it, the legs under the body — with the pumpkin closing
// it, listed in the order the clear walks the shape: the +axis arm first,
// the head's column, the −axis arm last. The corner cells the shape
// demands empty are the flat world's air.
func ironTPattern() [][3]int32 {
	return [][3]int32{{3, 65, 3}, {2, 66, 3}, {2, 65, 3}, {2, 64, 3}, {1, 65, 3}}
}

// assertGolemBuild checks one build the whole way: the pattern's cells gone
// from the world, the golem standing in the given foot cell at the half-block
// with vanilla's twentieth of a lift, carrying the health and the pace its
// kind creates with, its own flags byte where the kind carries one away from
// the client's defaults, and the pattern's removal told to everyone as one
// 2001 event per block that left, in the clear's own walk order.
func assertGolemBuild(t *testing.T, srv *server.Server, c *servertest.Client, watcher *servertest.Client,
	wantType int32, wantHealth float32, wantSpeed float64, pattern [][3]int32, feet [3]int32, wantFlags uint8) {
	t.Helper()
	golem := c.ExpectMobSpawn(wantType)
	if golem.X != float64(feet[0])+0.5 || golem.Y != float64(feet[1])+0.05 || golem.Z != float64(feet[2])+0.5 {
		t.Errorf("the golem spawned at (%v, %v, %v), want the pattern's foot cell (%d.5, %d.05, %d.5)",
			golem.X, golem.Y, golem.Z, feet[0], feet[1], feet[2])
	}
	if golem.Health != wantHealth {
		t.Errorf("the golem's health = %v, want %v", golem.Health, wantHealth)
	}
	if golem.SpeedAttribute != wantSpeed {
		t.Errorf("the golem's movement speed = %v, want %v", golem.SpeedAttribute, wantSpeed)
	}
	var defaultFlags uint8
	if wantType == protocol.EntityTypeSnowGolem {
		defaultFlags = 0x10
	}
	if wantFlags == defaultFlags {
		if golem.HasGolemFlags {
			t.Errorf("the golem carries the flags byte %#x, want the client's own default left out", golem.GolemFlags)
		}
	} else if !golem.HasGolemFlags || golem.GolemFlags != wantFlags {
		t.Errorf("the golem's flags byte = %#x (carried %v), want %#x", golem.GolemFlags, golem.HasGolemFlags, wantFlags)
	}
	for _, cell := range pattern {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != 0 {
			name, _, _ := registry.BlockStateName(got)
			t.Errorf("the pattern cell (%d, %d, %d) still holds %s, want it cleared", cell[0], cell[1], cell[2], name)
		}
	}
	if got := srv.MobCount(); got != 1 {
		t.Errorf("mob count = %d, want the one golem", got)
	}
	if heard := servertest.HearTool(t, watcher); !slices.Equal(heard.Events[2001], pattern) {
		t.Errorf("the watcher heard the block-break events %v, want one per cleared cell in the clear's own order %v",
			heard.Events[2001], pattern)
	}
}

// TestACarvedPumpkinOnSnowBuildsASnowGolem: the two snow blocks of the
// column and the pumpkin over them are the whole of vanilla's snow golem
// pattern; the build clears all three and stands the golem at the column's
// foot. vanilla: CarvedPumpkinBlock.trySpawnGolem
func TestACarvedPumpkinOnSnowBuildsASnowGolem(t *testing.T) {
	t.Parallel()
	srv, c, watcher := golemScene(t, "minecraft:carved_pumpkin")
	snow := servertest.MustState(t, "minecraft:snow_block", nil)
	srv.SetBlock(2, 64, 3, snow)
	srv.SetBlock(2, 65, 3, snow)
	c.PlaceBlock(2, 65, 3, protocol.FaceTop)
	// SnowGolem.createAttributes' 0.2f, the widened float the wire carries.
	assertGolemBuild(t, srv, c, watcher, protocol.EntityTypeSnowGolem, 4, float64(float32(0.2)), snowPattern(), [3]int32{2, 64, 3}, 0x10)
	c.ExpectBlockAck()
}

// TestACarvedPumpkinOnIronBuildsAnIronGolem: the T of four iron blocks under
// the pumpkin, its arms' corner cells left empty; the built golem carries the
// player-created bit vanilla's trySpawnGolem sets on it.
// vanilla: CarvedPumpkinBlock.trySpawnGolem, IronGolem.setPlayerCreated
func TestACarvedPumpkinOnIronBuildsAnIronGolem(t *testing.T) {
	t.Parallel()
	srv, c, watcher := golemScene(t, "minecraft:carved_pumpkin")
	iron := servertest.MustState(t, "minecraft:iron_block", nil)
	srv.SetBlock(2, 64, 3, iron)
	srv.SetBlock(1, 65, 3, iron)
	srv.SetBlock(2, 65, 3, iron)
	srv.SetBlock(3, 65, 3, iron)
	c.PlaceBlock(2, 65, 3, protocol.FaceTop)
	assertGolemBuild(t, srv, c, watcher, protocol.EntityTypeIronGolem, 100, 0.25, ironTPattern(), [3]int32{2, 64, 3}, 0x01)
	c.ExpectBlockAck()
}

// TestAJackOLanternBuildsASnowGolem: the lit pumpkin is the same
// CarvedPumpkinBlock whose onPlace runs, and the pattern's head cell reads
// either face of it. vanilla: CarvedPumpkinBlock.PUMPKINS_PREDICATE
func TestAJackOLanternBuildsASnowGolem(t *testing.T) {
	t.Parallel()
	srv, c, watcher := golemScene(t, "minecraft:jack_o_lantern")
	snow := servertest.MustState(t, "minecraft:snow_block", nil)
	srv.SetBlock(2, 64, 3, snow)
	srv.SetBlock(2, 65, 3, snow)
	c.PlaceBlock(2, 65, 3, protocol.FaceTop)
	assertGolemBuild(t, srv, c, watcher, protocol.EntityTypeSnowGolem, 4, float64(float32(0.2)), snowPattern(), [3]int32{2, 64, 3}, 0x10)
	c.ExpectBlockAck()
}

// TestAPatternMissingItsGolemBuildsNothing: a column one snow short and a T
// without an arm are no pattern at all — the pumpkin stands where it was
// placed, nothing spawns, and nobody hears a block break.
func TestAPatternMissingItsGolemBuildsNothing(t *testing.T) {
	t.Parallel()
	iron := servertest.MustState(t, "minecraft:iron_block", nil)
	for _, tc := range []struct {
		name   string
		setup  func(srv *server.Server)
		click  [3]int32
		placed [3]int32
	}{
		{name: "a column one snow short", setup: func(srv *server.Server) {
			srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:snow_block", nil))
		}, click: [3]int32{2, 64, 3}, placed: [3]int32{2, 65, 3}},
		{name: "a T without its east arm", setup: func(srv *server.Server) {
			srv.SetBlock(2, 64, 3, iron)
			srv.SetBlock(1, 65, 3, iron)
			srv.SetBlock(2, 65, 3, iron)
		}, click: [3]int32{2, 65, 3}, placed: [3]int32{2, 66, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c, watcher := golemScene(t, "minecraft:carved_pumpkin")
			tc.setup(srv)
			c.PlaceBlock(tc.click[0], tc.click[1], tc.click[2], protocol.FaceTop)
			c.ExpectBlockAck()
			c.Settle()
			watcher.Settle()

			if state := srv.BlockAt(tc.placed[0], tc.placed[1], tc.placed[2]); state == 0 {
				t.Error("the pattern never completed, but the pumpkin is gone from where it was placed")
			} else if name, _, _ := registry.BlockStateName(state); name != "minecraft:carved_pumpkin" {
				t.Errorf("the placed cell = %s, want the carved pumpkin standing", name)
			}
			if n := srv.MobCount(); n != 0 {
				t.Errorf("mob count = %d, want none", n)
			}
			if heard := servertest.HearTool(t, watcher); len(heard.Events[2001]) != 0 {
				t.Errorf("the watcher heard %d block-break events, want none: no build ran", len(heard.Events[2001]))
			}
		})
	}
}

// TestACarvedPumpkinBuildsASnowGolemInTheNetherToo: the onPlace of the
// carved pumpkin runs wherever the change lands, so a pattern built in the
// nether stands its golem up in the nether's herd — a world that is not the
// overworld's is still the one whose blocks leave and whose mob appears.
func TestACarvedPumpkinBuildsASnowGolemInTheNetherToo(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Builder")
	srv.MovePlayerToDimension("Builder", world.NetherName, 0.5, 64, 0.5)
	// Hold the arrival chunk for the test: its send is asynchronous, and a
	// mob planted in a chunk the player does not hold yet is saved away by
	// the next mob tick and stands back up under a new ID.
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, 0))
	c.Settle()
	// The nether melts a snow golem (SnowGolemMelts), which dies within a few
	// seconds of free ticks; the loop is held so the herd is read before any
	// melt tick can kill it.
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Builder", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:carved_pumpkin"), Count: 1})

	snow := servertest.MustState(t, "minecraft:snow_block", nil)
	srv.SetBlockInDimension(world.NetherName, 2, 64, 3, snow)
	srv.SetBlockInDimension(world.NetherName, 2, 65, 3, snow)
	// The nether's own rock may rise through the cell the pumpkin lands in;
	// the pattern's air corners the nether's blocks would break are cleared
	// with the rest of the column's surroundings.
	for _, cell := range [][3]int32{{1, 66, 3}, {3, 66, 3}, {1, 63, 3}, {3, 63, 3}, {2, 66, 3}} {
		srv.SetBlockInDimension(world.NetherName, cell[0], cell[1], cell[2], 0)
	}
	c.PlaceBlock(2, 65, 3, protocol.FaceTop)
	golem := c.ExpectMobSpawn(protocol.EntityTypeSnowGolem)
	// The ack goes out at the tick's end; one melt tick leaves the golem
	// standing.
	servertest.RunTicks(t, srv, 1)
	c.ExpectBlockAck()
	if golem.X != 2.5 || golem.Y != 64.05 || golem.Z != 3.5 {
		t.Errorf("the nether golem spawned at (%v, %v, %v), want the foot cell (2.5, 64.05, 3.5)", golem.X, golem.Y, golem.Z)
	}
	if state := srv.BlockInDimension(world.NetherName, 2, 66, 3); state != 0 {
		name, _, _ := registry.BlockStateName(state)
		t.Errorf("the nether pattern's head cell still holds %s, want it cleared", name)
	}
	if mobs := srv.Mobs(); len(mobs) != 1 || mobs[0].Kind != "minecraft:snow_golem" {
		t.Errorf("mobs = %+v, want the one snow golem of the nether", mobs)
	}
}

// TestASnowGolemDoesNotFreezeInItsOwnElement: the snow golem is on the
// freeze_immune_entity_types tag — of the powder snow it
// is built of — so the snow it is stood in has no hold on it.
// vanilla: Entity.canFreeze, the freeze_immune_entity_types tag
func TestASnowGolemDoesNotFreezeInItsOwnElement(t *testing.T) {
	t.Parallel()
	srv, c, _ := golemScene(t, "minecraft:carved_pumpkin")
	snow := servertest.MustState(t, "minecraft:snow_block", nil)
	srv.SetBlock(2, 64, 3, snow)
	srv.SetBlock(2, 65, 3, snow)
	c.PlaceBlock(2, 65, 3, protocol.FaceTop)
	golem := c.ExpectMobSpawn(protocol.EntityTypeSnowGolem)
	if !srv.SetMobNoAI(golem.EntityID) {
		t.Fatal("the built golem is not in the herd")
	}
	c.ExpectBlockAck()
	servertest.HoldTicks(t, srv)

	// Stand it in powder snow past the fully-frozen mark: a body the snow
	// can hold would count its ticks, the golem counts none.
	srv.SetBlock(2, 64, 3, servertest.MustState(t, "minecraft:powder_snow", nil))
	servertest.AwaitTicks(t, srv, uint64(rules.FullyFrozenTicks+20))
	mobs := srv.Mobs()
	if len(mobs) != 1 {
		t.Fatalf("mobs = %+v, want the one golem", mobs)
	}
	if frozen := mobs[0].Frozen; frozen != 0 {
		t.Errorf("the snow golem counted %d frozen ticks in powder snow, want the kind the snow has no hold on", frozen)
	}
}

// TestABuiltIronGolemKeepsItsPlayerCreatedBitAcrossAReload: the build's
// player-created bit is written into the golem's saved file and read back,
// so a chunk unload and reload leaves the golem the player's.
// vanilla: IronGolem.addAdditionalSaveData, readAdditionalSaveData
func TestABuiltIronGolemKeepsItsPlayerCreatedBitAcrossAReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Keeper")
	srv.GiveSlot("Keeper", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:carved_pumpkin"), Count: 1})
	c.Settle()
	iron := servertest.MustState(t, "minecraft:iron_block", nil)
	srv.SetBlock(2, 64, 3, iron)
	srv.SetBlock(1, 65, 3, iron)
	srv.SetBlock(2, 65, 3, iron)
	srv.SetBlock(3, 65, 3, iron)
	c.PlaceBlock(2, 65, 3, protocol.FaceTop)
	c.ExpectMobSpawn(protocol.EntityTypeIronGolem)
	c.ExpectBlockAck()

	// The only holder leaves; the chunk unloads and the next tick writes
	// the golem into its entity file.
	c.Disconnect()
	servertest.UntilTicks(t, srv, 100, "condition", func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	second := connect()
	second.Login("Keeper")
	second.Configure()
	returned, back := second.EnterWorldSeeingMob("Keeper", protocol.EntityTypeIronGolem)
	if !back {
		t.Fatal("the iron golem was not restored with its chunk")
	}
	if !returned.HasGolemFlags || returned.GolemFlags != 0x01 {
		t.Errorf("the reloaded golem's flags = %#x (carried %v), want the player-created bit 0x01", returned.GolemFlags, returned.HasGolemFlags)
	}
}

// golemSounds records the names of the sound effects among the packets a
// reader sees.
func golemSounds(t *testing.T, sounds map[string]int) func(id int32, data []byte) bool {
	return func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySoundEffect:
			name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data)
			sounds[name]++
		case protocol.IDPlayEntitySoundEffect:
			name, _, _, _, _ := servertest.ReadEntitySound(t, data)
			sounds[name]++
		}
		return false
	}
}

func golemInteractionScene(t *testing.T, name string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), name)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.SetSpawning(false)
	servertest.HoldTicks(t, srv)
	return srv, c
}

// TestASnowGolemThrowsSnowballsAtAnEnemy: a zombie in sight within the throw
// radius is targeted and pelted.
// vanilla: SnowGolem.performRangedAttack, RangedAttackGoal
func TestASnowGolemThrowsSnowballsAtAnEnemy(t *testing.T) {
	t.Parallel()
	srv, c := golemInteractionScene(t, "Pelter")
	golem := srv.SpawnMob("minecraft:snow_golem", 4.5, 64, 0.5)
	zombie := srv.SpawnMob("minecraft:zombie", 10.5, 64, 0.5)
	srv.SetMobNoAI(zombie)
	sounds := map[string]int{}
	thrown := false
	c.ReadTicks(srv, 100, func(id int32, data []byte) bool {
		golemSounds(t, sounds)(id, data)
		for _, it := range srv.ThrownInDimension("minecraft:overworld") {
			thrown = thrown || it.Kind == "minecraft:snowball"
		}
		return thrown && sounds["minecraft:entity.snow_golem.shoot"] > 0
	})
	if srv.MobTargetID(golem) != zombie {
		t.Errorf("the golem targets %d, want the zombie %d", srv.MobTargetID(golem), zombie)
	}
	if !thrown {
		t.Error("no snowball flew within 100 ticks")
	}
	if sounds["minecraft:entity.snow_golem.shoot"] == 0 {
		t.Error("entity.snow_golem.shoot was not sent")
	}
}

// TestShearingASnowGolemTakesItsPumpkin: shears drop a carved pumpkin once,
// tell the client the pumpkin bit is gone and keep it gone over a reload of
// the mob's data.
// vanilla: SnowGolem.shear
func TestShearingASnowGolemTakesItsPumpkin(t *testing.T) {
	t.Parallel()
	srv, c := golemInteractionScene(t, "Shearer")
	id := srv.SpawnMob("minecraft:snow_golem", 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	giveHand(t, srv, "Shearer", "minecraft:shears", 1)
	pumpkin := servertest.MustItemID(t, "minecraft:carved_pumpkin")

	sounds := map[string]int{}
	cleared := false
	sawSounds := golemSounds(t, sounds)
	c.UseOn(id)
	c.ReadTicks(srv, 3, func(pid int32, data []byte) bool {
		sawSounds(pid, data)
		if pid == protocol.IDPlayEntityMetadata {
			r := protocol.NewReader(data)
			if r.ReadVarInt() == id {
				// Byte entries only: the NoAI flag precedes the golem's.
				for index := r.ReadU8(); index != 0xff && r.ReadVarInt() == protocol.MetadataByte; index = r.ReadU8() {
					if v := r.ReadU8(); index == protocol.MetadataIndexGolemFlags {
						cleared = v == 0
					}
				}
			}
		}
		return false
	})
	if sounds["minecraft:entity.snow_golem.shear"] != 1 {
		t.Errorf("entity.snow_golem.shear sent %d times, want once", sounds["minecraft:entity.snow_golem.shear"])
	}
	if !cleared {
		t.Error("no metadata packet cleared the pumpkin bit")
	}
	items := srv.ItemsInDimension("minecraft:overworld")
	if len(items) != 1 || items[0].Stack.ItemID != pumpkin {
		t.Fatalf("shearing dropped %+v, want one carved pumpkin", items)
	}
	for _, m := range srv.Mobs() {
		if m.ID == id && !m.Sheared {
			t.Error("the golem kept its pumpkin")
		}
	}

	c.UseOn(id)
	c.Settle()
	if got := len(srv.ItemsInDimension("minecraft:overworld")); got != 1 {
		t.Errorf("a second shearing left %d items, want the one", got)
	}
}

// TestIronIngotsRepairAnIronGolem: an ingot heals 25 and is spent; at full
// health the click passes.
// vanilla: IronGolem.mobInteract
func TestIronIngotsRepairAnIronGolem(t *testing.T) {
	t.Parallel()
	srv, c := golemInteractionScene(t, "Smith")
	id := srv.SpawnMob("minecraft:iron_golem", 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.SetMobHealth(id, 40)
	giveHand(t, srv, "Smith", "minecraft:iron_ingot", 2)

	sounds := map[string]int{}
	c.UseOn(id)
	c.ReadTicks(srv, 2, golemSounds(t, sounds))
	if hp, _ := srv.MobHealth(id); hp != 65 {
		t.Errorf("health after an ingot = %v, want 65", hp)
	}
	if sounds["minecraft:entity.iron_golem.repair"] != 1 {
		t.Errorf("entity.iron_golem.repair sent %d times, want once", sounds["minecraft:entity.iron_golem.repair"])
	}
	if left := srv.InventorySlot("Smith", player.HotbarSlot).Count; left != 1 {
		t.Errorf("ingots left = %d, want 1", left)
	}

	srv.SetMobHealth(id, 100)
	c.UseOn(id)
	c.Settle()
	if left := srv.InventorySlot("Smith", player.HotbarSlot).Count; left != 1 {
		t.Errorf("ingots left = %d at full health, want 1", left)
	}
}

// TestAnIronGolemCracksAudibly: a blow that moves the golem to another crack
// level plays entity.iron_golem.damage once.
// vanilla: IronGolem.hurtServer
func TestAnIronGolemCracksAudibly(t *testing.T) {
	t.Parallel()
	srv, c := golemInteractionScene(t, "Breaker")
	id := srv.SpawnMob("minecraft:iron_golem", 1.5, 64, 0.5)
	srv.SetMobNoAI(id)
	sounds := map[string]int{}
	srv.HurtMobByWorld(id, 30, "minecraft:generic")
	c.ReadTicks(srv, 3, golemSounds(t, sounds))
	if sounds["minecraft:entity.iron_golem.damage"] != 1 {
		t.Errorf("entity.iron_golem.damage sent %d times, want once", sounds["minecraft:entity.iron_golem.damage"])
	}
}
