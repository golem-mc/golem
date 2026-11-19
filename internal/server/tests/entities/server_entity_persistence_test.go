package entities

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The persistence of the saved kinds that are not mobs: the item entities,
// the arrows, the experience orbs, the primed TNT and the falling blocks the
// mobs' chunk entity files keep beside them. The tests drive real connections the
// way the rest of the package's do — a thing put into the world, the world
// saved or the player gone, a restart or a reload, and the thing back with
// the state it left with — and, where the wire cannot say it, the entity
// file read back the way the game reads it.

// TestAnItemSurvivesARestart checks the item entities' persistence: a stack
// thrown on the ground is written into its chunk's entity file the vanilla
// way — the stack it holds, the age it has lived, the pickup delay it waits
// behind — and a restarted server stands it back up with all of it.
func TestAnItemSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the item registry has no diamond")
	}

	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Keeper")
	c.Configure()
	c.Inventory()
	c.EnterWorld("Keeper")

	srv.GiveSlot("Keeper", player.HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 2})
	c.DropKey(true)

	// The throw's own arrival, with the identity the file is to keep. The
	// player steps away from it at once, out of reach of the pickup its
	// two-second delay ends — the item is this test's to keep on the
	// ground.
	dropped := c.AwaitSpawn(protocol.EntityTypeItem)
	c.MoveTo(20.5, 64, 20.5, 0, 0)
	c.Settle()

	srv.FlushMobs()
	srv.Shutdown()

	// On disk the compound is the vanilla one: an id, the stack under the
	// Item key the stack codec reads, and the age and the pickup delay as
	// the shorts the game keeps them as.
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:item", func(compound nbt.Compound) {
		identity, ok := nbt.Get[nbt.IntArray](compound, "UUID")
		if !ok {
			t.Error("the saved item has no UUID")
		} else if len(identity) != 4 {
			t.Errorf("saved UUID = %v, want four integers", identity)
		} else {
			want := servertest.UuidWords(dropped.Uuid)
			for i := range want {
				if identity[i] != want[i] {
					t.Errorf("saved UUID = %v, want %v (the thrown item's)", identity, want)
					break
				}
			}
		}
		item, ok := nbt.Get[nbt.Compound](compound, "Item")
		if !ok {
			t.Fatal("the saved item has no Item compound")
		}
		if id, _ := nbt.Get[nbt.String](item, "id"); string(id) != "minecraft:diamond" {
			t.Errorf("Item.id = %v, want minecraft:diamond", id)
		}
		if count, _ := nbt.Get[nbt.Int](item, "count"); count != 2 {
			t.Errorf("Item.count = %v, want 2", count)
		}
		age, ok := nbt.Get[nbt.Short](compound, "Age")
		if !ok {
			t.Error("the saved item has no Age")
		} else if age < 0 {
			t.Errorf("Age = %d, want a count of the ticks it has lived", age)
		}
		delay, ok := nbt.Get[nbt.Short](compound, "PickupDelay")
		if !ok {
			t.Error("the saved item has no PickupDelay")
		} else if delay < 0 || delay > 60 {
			t.Errorf("PickupDelay = %d, want the throw's two seconds winding down", delay)
		}
		if health, ok := nbt.Get[nbt.Short](compound, "Health"); !ok || health != 5 {
			t.Errorf("Health = %v, %v; want 5, the vanilla default", health, ok)
		}
		if _, ok := nbt.Get[nbt.Short](compound, "Fire"); !ok {
			t.Error("the saved item has no Fire")
		}
		if cooldown, ok := nbt.Get[nbt.Int](compound, "PortalCooldown"); !ok || cooldown != 0 {
			t.Errorf("PortalCooldown = %v, %v; want 0", cooldown, ok)
		}
	})

	// And a restarted server stands it back up, stack and all.
	restarted := *cfg
	againSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	shown := again.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypeItem: "item",
	})
	back := servertest.CapturedOr(t, again, shown, "item", protocol.EntityTypeItem)
	servertest.ExpectUUID(t, back.Uuid, dropped.Uuid, "item")
	if back.MetaBad {
		t.Fatal("the restored item's metadata could not be read")
	}
	for _, entry := range back.Meta {
		if entry.Index != protocol.MetadataIndexItemSlot {
			t.Errorf("item metadata carries field %d, want only the stack", entry.Index)
			continue
		}
		if entry.Typ != protocol.MetadataSlot {
			t.Errorf("item stack field type = %d, want %d (a slot)", entry.Typ, protocol.MetadataSlot)
		}
		if got := entry.SlotVal; got.ItemID != diamond || got.Count != 2 {
			t.Errorf("the restored item holds %+v, want two diamonds", got)
		}
	}
	if got := againSrv.ItemCount(); got != 1 {
		t.Errorf("the restarted server holds %d items, want the one that was saved", got)
	}
}

// TestAStuckArrowSurvivesARestart checks the arrows': a shaft stood in the
// ground is written into its chunk's entity file — the life it counts, the
// shake of its arrival, who may pick it up, the damage it carries — and a
// restarted server stands it back up in the ground where it was.
func TestAStuckArrowSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Keeper")

	const (
		arrowX = 14.5
		arrowY = 64.0
		arrowZ = 14.5
	)
	srv.SpawnStuckArrow("allowed", arrowX, arrowY, arrowZ)
	stuck := c.AwaitSpawn(protocol.EntityTypeArrow)

	srv.FlushMobs()
	srv.Shutdown()

	// On disk the compound is the vanilla one: in the ground, its arrival
	// long settled, the pickup rule and the base damage the shot carries.
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:arrow", func(compound nbt.Compound) {
		if inGround, ok := nbt.Get[nbt.Byte](compound, "inGround"); !ok || inGround == 0 {
			t.Errorf("inGround = %v, %v; want 1", inGround, ok)
		}
		if pickup, ok := nbt.Get[nbt.Byte](compound, "pickup"); !ok || pickup != 1 {
			t.Errorf("pickup = %v, %v; want 1 (allowed)", pickup, ok)
		}
		if life, ok := nbt.Get[nbt.Short](compound, "life"); !ok || life < 0 {
			t.Errorf("life = %v, %v; want the stuck one's count", life, ok)
		}
		if shake, ok := nbt.Get[nbt.Byte](compound, "shake"); !ok || shake != 0 {
			t.Errorf("shake = %v, %v; want 0, the arrival long settled", shake, ok)
		}
		if damage, ok := nbt.Get[nbt.Double](compound, "damage"); !ok || float64(damage) != 2.0 {
			t.Errorf("damage = %v, %v; want the base 2.0", damage, ok)
		}
		if crit, ok := nbt.Get[nbt.Byte](compound, "crit"); !ok || crit != 0 {
			t.Errorf("crit = %v, %v; want 0", crit, ok)
		}
		// The block it hit, vanilla's lastState: the ground the shaft
		// stands out of, not the air its own position is in.
		inBlock := compound["inBlockState"]
		if name, _ := registry.BlockStateOf(inBlock); name != "minecraft:stone" {
			t.Errorf("inBlockState = %v, want the stone it stands in", inBlock)
		}
		pos, ok := nbt.Get[nbt.List](compound, "Pos")
		if !ok || len(pos.Items) != 3 {
			t.Fatalf("Pos = %v, %v; want three doubles", pos, ok)
		}
		if got := pos.Items[0].(nbt.Double); float64(got) != arrowX {
			t.Errorf("Pos.x = %v, want %v", got, arrowX)
		}
		if got := pos.Items[2].(nbt.Double); float64(got) != arrowZ {
			t.Errorf("Pos.z = %v, want %v", got, arrowZ)
		}
	})

	// And the restarted server stands it back up, in the ground where it
	// was, pickable by the rule it was saved with.
	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	shown := again.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypeArrow: "arrow",
	})
	back := servertest.CapturedOr(t, again, shown, "arrow", protocol.EntityTypeArrow)
	servertest.ExpectUUID(t, back.Uuid, stuck.Uuid, "arrow")
	if back.X != arrowX || back.Y != arrowY || back.Z != arrowZ {
		t.Errorf("the arrow came back at (%g, %g, %g), want (%g, %g, %g)",
			back.X, back.Y, back.Z, arrowX, arrowY, arrowZ)
	}
	if back.MetaBad {
		t.Fatal("the restored arrow's metadata could not be read")
	}
	if entry := servertest.MetaOf(back.Meta, protocol.MetadataIndexArrowInGround); entry == nil || !entry.FlagVal {
		t.Error("the restored arrow was not shown in the ground")
	}
}

// TestAnOrbSurvivesARestart checks the experience orbs': an orb lying in
// the world is written into its chunk's entity file — the worth of one of
// its pickups and the count of the pickups still in it, the age of its life
// — and a restarted server stands it back up worth what it was.
func TestAnOrbSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Keeper")

	// Far enough from the player that the orb's own follow never reaches
	// them, near enough that the chunk they load keeps it standing.
	srv.SpawnOrb(-14.5, 65, 14.5, 7)
	float := c.AwaitSpawn(protocol.EntityTypeExperienceOrb)

	srv.FlushMobs()
	srv.Shutdown()

	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: -1, Z: 0}, "minecraft:experience_orb", func(compound nbt.Compound) {
		if value, ok := nbt.Get[nbt.Short](compound, "Value"); !ok || value != 7 {
			t.Errorf("Value = %v, %v; want 7", value, ok)
		}
		if count, ok := nbt.Get[nbt.Int](compound, "Count"); !ok || count != 1 {
			t.Errorf("Count = %v, %v; want 1", count, ok)
		}
		if health, ok := nbt.Get[nbt.Short](compound, "Health"); !ok || health != 5 {
			t.Errorf("Health = %v, %v; want 5, the vanilla default", health, ok)
		}
	})

	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	shown := again.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypeExperienceOrb: "orb",
	})
	back := servertest.CapturedOr(t, again, shown, "orb", protocol.EntityTypeExperienceOrb)
	servertest.ExpectUUID(t, back.Uuid, float.Uuid, "orb")
	if back.MetaBad {
		t.Fatal("the restored orb's metadata could not be read")
	}
	entry := servertest.MetaOf(back.Meta, protocol.MetadataIndexOrbValue)
	if entry == nil {
		t.Fatal("the restored orb carries no value")
	}
	if entry.Typ != protocol.MetadataVarInt {
		t.Errorf("orb value field type = %d, want %d (a varint)", entry.Typ, protocol.MetadataVarInt)
	}
	if entry.VarintVal != 7 {
		t.Errorf("the restored orb is worth %d, want 7", entry.VarintVal)
	}
}

// TestPrimedTNTSurvivesARestart checks the primed TNT's: a lit block is
// written into its chunk's entity file with the fuse it has left to burn,
// and a restarted server stands it back up mid-burn — the fuse the file
// said, and the blast at the end of it. Both servers run on a step clock, so
// the fuse burns down only where the test steps it: a busy machine lagging
// the save or the join cannot burn the ticks out from under either one.
func TestPrimedTNTSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()

	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, cfg, "Keeper", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})

	// A hundred ticks of fuse, twenty-five of them spent before the save: the
	// fuse the file is written with is the one the step clock left, never one
	// a slow machine let pass. Twenty-five rather than twenty leaves a fuse
	// off the default of eighty, which the spawn's metadata leaves out.
	srv.SpawnTNT(20.5, 64, 20.5, 100)
	burn := c.AwaitSpawn(protocol.EntityTypeTnt)
	servertest.StepTicks(t, srv, clock, 25)

	srv.FlushMobs()
	srv.Shutdown()

	var savedFuse int16
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 1, Z: 1}, "minecraft:tnt", func(compound nbt.Compound) {
		fuse, ok := nbt.Get[nbt.Short](compound, "fuse")
		if !ok {
			t.Fatal("the saved TNT has no fuse")
		}
		if fuse != 75 {
			t.Errorf("fuse = %d, want the seventy-five the step clock left", fuse)
		}
		savedFuse = int16(fuse)
		state, ok := compound["block_state"]
		if !ok {
			t.Fatal("the saved TNT has no block_state")
		}
		if name, _ := registry.BlockStateOf(state); name != "minecraft:tnt" {
			t.Errorf("block_state = %v, want minecraft:tnt", state)
		}
	})

	// The restarted server lights it again mid-burn, at the fuse the file
	// said, and the blast comes at the end of it — on the second step clock
	// too, so the join and the blast land where the test steps them.
	restarted := *cfg
	clock2 := servertest.NewStepClock()
	srv2, again := servertest.Join(t, &restarted, "Keeper", func(s *server.Server) {
		s.SetTickClock(clock2, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	// The saved chunk's entities wake on a tick, not in the join itself.
	servertest.StepTicks(t, srv2, clock2, 2)

	back := again.AwaitSpawn(protocol.EntityTypeTnt)
	servertest.ExpectUUID(t, back.Uuid, burn.Uuid, "primed TNT")
	entry := servertest.MetaOf(again.AwaitMetadataFor(back.EntityID), protocol.MetadataIndexTNTFuse)
	if entry == nil {
		t.Fatal("the restored TNT carries no fuse")
	}
	if entry.VarintVal != int32(savedFuse) {
		t.Errorf("the restored TNT burns for %d, want the %d it was saved with",
			entry.VarintVal, savedFuse)
	}
	// The blast, at the end of the fuse the file said.
	servertest.StepTicks(t, srv2, clock2, int(savedFuse)+2)
	again.AwaitPlay(protocol.IDPlayExplosion)
}

// TestABabyMobIsSavedInTheStateItsKindKeepsAndReturnsOne takes the
// persistence of the baby state into it, each kind's state kept the way
// its vanilla read keeps it: a cow made a baby is written out with the age
// the state is — the minus twenty thousand four hundred the growing counts
// up from, under the Age the animal's compound keeps — and a zombie made a
// baby with the IsBaby boolean the zombie's compound keeps instead. The
// bodies the files give back are those babies still, the metadata their
// returns show the flag on.
func TestABabyMobIsSavedInTheStateItsKindKeepsAndReturnsOne(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()

	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Keeper")

	cowID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if cow.HasBaby {
		t.Error("the grown cow's spawn metadata carried the baby flag")
	}

	// The zombie beside it, its baby-ness the boolean the vanilla zombie
	// keeps instead of the age the animals count up by. Both stand up
	// before either is made a baby: the metadata the change broadcasts
	// would otherwise land among the zombie's spawn packets.
	zombieID := srv.SpawnMob("minecraft:zombie", 7.5, 64, 7.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	srv.SetMobBaby(cowID, true)
	srv.SetMobBaby(zombieID, true)

	// Leaving takes the chunks away and the tick writes the mob standing
	// in the out-of-world chunk into its entity file.
	c.Disconnect()
	servertest.WaitFor(t, srv, 100, func() bool { return srv.MobCount() == 0 })
	srv.FlushMobs()

	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:cow", func(compound nbt.Compound) {
		age, ok := nbt.Get[nbt.Int](compound, "Age")
		if !ok {
			t.Error("the saved cow has no Age")
		} else if age < -24000 || age > -23000 {
			// The baby grows a tick at a time from the -24000 it was made
			// at until its chunk goes, so a few ticks' growth is saved.
			t.Errorf("saved Age = %d, want the -24000 the baby counts up from, grown by the few ticks it lived", age)
		}
	})
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:zombie", func(compound nbt.Compound) {
		if age, ok := nbt.Get[nbt.Int](compound, "Age"); ok {
			t.Errorf("the saved zombie carries an Age of %d, want the IsBaby the vanilla zombie keeps instead", age)
		}
		baby, ok := nbt.Get[nbt.Byte](compound, "IsBaby")
		if !ok {
			t.Error("the saved zombie has no IsBaby")
		} else if baby == 0 {
			t.Error("the saved zombie's IsBaby reads grown, want the baby's one")
		}
	})

	// Back in, the bodies the files kept are the babies they were: the
	// metadata their returns show the flag on, and the herd the server
	// holds babies in.
	second := connect()
	second.Login("Keeper")
	second.Configure()
	returned, back := second.EnterWorldSeeingMob("Keeper", protocol.EntityTypeCow)
	if !back {
		t.Fatal("the cow was not restored with its chunk")
	}
	if !returned.HasBaby || !returned.Baby {
		t.Errorf("the returned cow's metadata carried the baby flag present=%v on=%v, want it present and on", returned.HasBaby, returned.Baby)
	}
	babies := 0
	for _, m := range srv.Mobs() {
		switch m.Kind {
		case "minecraft:cow", "minecraft:zombie":
			if m.Baby {
				babies++
			}
		default:
			t.Errorf("the reloaded world holds a %s, want the two babies it saved", m.Kind)
		}
	}
	if babies != 2 {
		t.Errorf("%d of the reloaded mobs are babies, want the cow and the zombie both", babies)
	}
}

// TestALitCreeperIsSavedLitAndComesBackLit lights a creeper with a flint
// and steel, saves the world under it and restarts: the compound carries
// vanilla's ignited boolean, and the creeper the restart stands up comes
// back lit — the flag in the metadata it is shown with, the fuse it brought
// home running to the blast on its own.
func TestALitCreeperIsSavedLitAndComesBackLit(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Keeper")

	flintID, ok := registry.ItemID("minecraft:flint_and_steel")
	if !ok {
		t.Fatal("the item registry has no flint and steel")
	}
	srv.GiveSlot("Keeper", player.HotbarSlot, protocol.ItemStack{ItemID: flintID, Count: 1})

	// The creeper stands an arm past the swell's own three blocks, so the
	// lighting is the only fuse this click can begin.
	creeperID := srv.SpawnMob("minecraft:creeper", 4.0, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCreeper)

	// The loop is stopped before the click and the ticks are stepped by hand,
	// so a stalled loop cannot catch up through the fuse's thirty ticks
	// ahead of the save.
	srv.StopTick()
	c.UseOn(creeperID)
	c.Settle()

	lit := false
	srv.TickMobs(2)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != creeperID {
			return
		}
		m := servertest.ReadMonsterMetadata(c, data)
		lit = lit || m.HasSwell && m.Swell == 1 && m.HasIgnite && m.Ignited
	})
	if !lit {
		t.Fatal("the lit fuse never showed in the metadata")
	}

	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:creeper", func(compound nbt.Compound) {
		if got := compound["ignited"]; got != nbt.Byte(1) {
			t.Errorf("the saved creeper's ignited = %v, want the lit fuse's 1b", got)
		}
	})
	srv.Shutdown()

	// The restarted world stands the creeper back up lit. Its loop stays
	// stopped until the join is read through: the chunk's wake runs on the
	// tick, so nothing stands up into the join and no fuse carries a blast
	// into the join's own reads. The first tick after stands the creeper
	// up, its spawn and metadata ahead of the blast the fuse ends in.
	restarted := *cfg
	restartedSrv, reconnect := servertest.Start(t, &restarted)
	restartedSrv.StopTick()
	again := reconnect().Join("Keeper")
	restartedSrv.StartTick()

	back := again.AwaitSpawn(protocol.EntityTypeCreeper)
	back.Meta = again.AwaitMetadataFor(back.EntityID)
	if entry := servertest.MetaOf(back.Meta, protocol.MetadataIndexCreeperIgnited); entry == nil || !entry.FlagVal {
		t.Error("the returned creeper's metadata carries no ignited flag")
	}
	if entry := servertest.MetaOf(back.Meta, protocol.MetadataIndexCreeperSwell); entry == nil || entry.VarintVal != 1 {
		t.Error("the returned creeper's metadata carries no running swell")
	}
	again.AwaitPlay(protocol.IDPlayExplosion)
}

// positionOfTag reads the Pos list of a saved entity's compound as the three
// doubles a file keeps a position in.
func positionOfTag(t *testing.T, compound nbt.Compound) (x, y, z float64) {
	t.Helper()
	pos, ok := nbt.Get[nbt.List](compound, "Pos")
	if !ok || len(pos.Items) != 3 {
		t.Fatal("the saved compound has no Pos")
	}
	xyz := [3]float64{}
	for i, item := range pos.Items {
		value, isDouble := item.(nbt.Double)
		if !isDouble {
			t.Fatalf("the saved Pos carries a %s, want doubles", item.Type())
		}
		xyz[i] = float64(value)
	}
	return xyz[0], xyz[1], xyz[2]
}
