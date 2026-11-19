package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The variants of the zombie, the skeleton and the spider, each checked
// against its decompiled class: the type it spawns as, its box and
// attributes as the wire shows them, its equipment and metadata, and the
// one thing it does that its family does not.

// variantHunt stands a survival player in a hard world at noon with a mob
// of the kind at x, z beside them and returns the two, the player's entity
// ID and the mob's pairing.
func variantHunt(t *testing.T, kind string, x, z float64) (*server.Server, *servertest.Client, int32, servertest.VariantSeen) {
	t.Helper()
	cfg := servertest.HostileTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Prey"), func(s *server.Server) { s.SetTimeSync(0) })
	srv.SetPaused(true)
	srv.SetDifficulty(3) // hard
	c := connect()
	playerID := c.JoinAs("Prey")
	c.ChatCommand("time set noon")
	c.Settle()
	srv.SpawnMob(kind, x, 64, z)
	return srv, c, playerID, c.WatchVariant(srv, kind)
}

// TestAHuskHungersItsPreyAndShrugsOffTheSun checks Husk against the
// zombie it extends: the zombie's twenty of health and 0.23 pace, no
// burning in the noonday sun, and the blow's hunger of 140 ticks per whole
// effective difficulty — hard's 2.25 on a fresh world, so 280.
func TestAHuskHungersItsPreyAndShrugsOffTheSun(t *testing.T) {
	t.Parallel()
	srv, c, playerID, husk := variantHunt(t, "minecraft:husk", 4.5, 4.5)
	if husk.Health != 20 || husk.Speed != float64(float32(0.23)) {
		t.Errorf("husk health %v, speed %v; want Zombie.createAttributes' 20 and 0.23", husk.Health, husk.Speed)
	}

	hunger := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayEntityMetadata:
			if r.ReadVarInt() != husk.EntityID {
				return false
			}
			seen := servertest.VariantSeen{Meta: map[uint8][]int32{}}
			c.ReadVariantMetadata(r, &seen)
			if flags, ok := seen.Meta[protocol.MetadataIndexEntityFlags]; ok && flags[0]&protocol.EntityFlagOnFire != 0 {
				t.Error("a husk caught fire in the sun")
				return true
			}
		case protocol.IDPlayUpdateMobEffect:
			got := servertest.MobEffect{Entity: r.ReadVarInt(), Effect: r.ReadVarInt(), Amplifier: r.ReadVarInt(), Duration: r.ReadVarInt(), Flags: r.ReadU8()}
			if got.Entity != playerID {
				return false
			}
			hunger = true
			if got.Effect != registry.MustMobEffect("minecraft:hunger") || got.Amplifier != 0 || got.Duration != 280 {
				t.Errorf("the husk's blow gave %+v, want hunger I for 280 ticks", got)
			}
			return true
		}
		return false
	})
	if !hunger {
		t.Error("the husk's blow never gave its hunger")
	}
	// The noon after the blow: a zombie would have lit within the few
	// seconds, a husk never does.
	c.ReadTicksSkipping(srv, 80, servertest.HostileSkip, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		if id != protocol.IDPlayEntityMetadata || r.ReadVarInt() != husk.EntityID {
			return false
		}
		seen := servertest.VariantSeen{Meta: map[uint8][]int32{}}
		c.ReadVariantMetadata(r, &seen)
		if flags, ok := seen.Meta[protocol.MetadataIndexEntityFlags]; ok && flags[0]&protocol.EntityFlagOnFire != 0 {
			t.Error("a husk caught fire in the sun")
			return true
		}
		return false
	})
}

// variantNight stands a survival player in a hard world at midnight with a
// mob of the kind eight blocks off and returns the two, the player's
// entity ID and the mob's pairing.
func variantNight(t *testing.T, kind string) (*server.Server, *servertest.Client, int32, servertest.VariantSeen) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	srv.SetDifficulty(3) // hard
	srv.SetDayTime(18000)
	c := connect()
	playerID := c.JoinAs("Prey")
	srv.SpawnMob(kind, 8.5, 64, 0.5)
	return srv, c, playerID, c.WatchVariant(srv, kind)
}

// awaitTippedShot waits for the mob's arrow and the effect its hit gives
// the player, and checks both: the arrow's colour metadata the effect's own
// colour, opaque, and the effect at amplifier nought for the ticks given.
func awaitTippedShot(srv *server.Server, t *testing.T, c *servertest.Client, playerID, shooterID int32, effectName string, ticks int32) {
	t.Helper()
	id := registry.MustMobEffect(effectName)
	wantColor := int32(uint32(0xFF000000) | uint32(registry.MobEffectOf(id).Color))
	arrows := map[int32]bool{}
	colored, hit := false, false
	c.ReadTicksSkipping(srv, servertest.SkeletonTicks, servertest.HostileSkip, func(packetID int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch packetID {
		case protocol.IDPlaySpawnEntity:
			arrowID := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeArrow {
				r.ReadF64()
				r.ReadF64()
				r.ReadF64()
				r.ReadLpVec3()
				r.ReadAngle()
				r.ReadAngle()
				r.ReadAngle()
				if r.ReadVarInt() == shooterID {
					arrows[arrowID] = true
				}
			}
		case protocol.IDPlayEntityMetadata:
			if !arrows[r.ReadVarInt()] {
				return false
			}
			for r.Err() == nil {
				index := r.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				switch fieldType := r.ReadVarInt(); fieldType {
				case protocol.MetadataByte, protocol.MetadataBoolean:
					r.ReadU8()
				case protocol.MetadataVarInt, protocol.MetadataHumanoidArm:
					v := r.ReadVarInt()
					if index == protocol.MetadataIndexArrowColor {
						colored = true
						if v != wantColor {
							t.Errorf("arrow colour = %#x, want %s's %#x", uint32(v), effectName, uint32(wantColor))
						}
					}
				default:
					t.Fatalf("arrow metadata field %d of type %d", index, fieldType)
				}
			}
		case protocol.IDPlayUpdateMobEffect:
			got := servertest.MobEffect{Entity: r.ReadVarInt(), Effect: r.ReadVarInt(), Amplifier: r.ReadVarInt(), Duration: r.ReadVarInt(), Flags: r.ReadU8()}
			if got.Entity != playerID {
				return false
			}
			hit = true
			if got.Effect != id || got.Amplifier != 0 || got.Duration != ticks {
				t.Errorf("the arrow gave %+v, want %s I for %d ticks", got, effectName, ticks)
			}
			return true
		}
		return false
	})
	if !colored {
		t.Errorf("the arrow never carried its %s colour", effectName)
	}
	if !hit {
		t.Errorf("the arrow never gave its %s", effectName)
	}
}

// TestAStraysArrowsSlow checks Stray against AbstractSkeleton: the
// skeleton's health, pace and bow, and Stray.getArrow's slowness of 600
// ticks on every arrow.
func TestAStraysArrowsSlow(t *testing.T) {
	t.Parallel()
	srv, c, playerID, stray := variantNight(t, "minecraft:stray")
	if stray.Health != 20 || stray.Speed != 0.25 {
		t.Errorf("stray health %v, speed %v; want 20 and 0.25", stray.Health, stray.Speed)
	}
	if stray.MainHand.ItemID != servertest.MustItem("minecraft:bow") {
		t.Errorf("stray holds %+v, want a bow", stray.MainHand)
	}
	awaitTippedShot(srv, t, c, playerID, stray.EntityID, "minecraft:slowness", 600)
}

// TestABoggedsArrowsPoison checks Bogged against AbstractSkeleton: sixteen
// of health (Bogged.createAttributes), the skeleton's pace and bow, and
// Bogged.getArrow's poison of 100 ticks on every arrow.
func TestABoggedsArrowsPoison(t *testing.T) {
	t.Parallel()
	srv, c, playerID, bogged := variantNight(t, "minecraft:bogged")
	if bogged.Health != 16 || bogged.Speed != 0.25 {
		t.Errorf("bogged health %v, speed %v; want 16 and 0.25", bogged.Health, bogged.Speed)
	}
	if bogged.MainHand.ItemID != servertest.MustItem("minecraft:bow") {
		t.Errorf("bogged holds %+v, want a bow", bogged.MainHand)
	}
	if _, ok := bogged.Meta[protocol.MetadataIndexBoggedSheared]; ok {
		t.Error("a fresh bogged's spawn carries the sheared flag, want its default left out")
	}
	awaitTippedShot(srv, t, c, playerID, bogged.EntityID, "minecraft:poison", 100)
}

// TestShearingABoggedDropsItsMushrooms checks Bogged.shear: shears on a
// bogged drop the two mushrooms of minecraft:shearing/bogged, play
// entity.bogged.shear, and raise the sheared flag; a second shearing gives
// nothing.
func TestShearingABoggedDropsItsMushrooms(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Shearer")
	srv.GiveSlot("Shearer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:shears"), Count: 1})
	c.Settle()
	srv.SpawnMob("minecraft:bogged", 1.5, 64, 0.5)
	bogged := c.WatchVariant(srv, "minecraft:bogged")

	c.UseOn(bogged.EntityID)
	mushrooms, sheared, sound := 0, false, false
	var laid []servertest.ItemDrop
	brown, red := servertest.MustItem("minecraft:brown_mushroom"), servertest.MustItem("minecraft:red_mushroom")
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayEntityMetadata:
			entityID := r.ReadVarInt()
			for r.Err() == nil {
				index := r.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				fieldType := r.ReadVarInt()
				switch {
				case fieldType == protocol.MetadataSlot && index == protocol.MetadataIndexItemSlot:
					if stack, ok := r.ReadSlot(); ok && (stack.ItemID == brown || stack.ItemID == red) {
						mushrooms += int(stack.Count)
					}
				case entityID == bogged.EntityID && index == protocol.MetadataIndexBoggedSheared:
					sheared = r.ReadU8() == 1
				default:
					// One packet holds every entry the same tick turned, the
					// daylight burn's health beside the flag; step over the
					// rest by type so the flag behind them still reads.
					servertest.SkipMetadataField(c, r, fieldType)
				}
			}
		case protocol.IDPlaySpawnEntity:
			if drop := servertest.ParseItemSpawn(data); drop.EntityID != 0 {
				laid = append(laid, drop)
			}
		case protocol.IDPlayEntitySoundEffect:
			sound = sound || servertest.SoundIs(data, "minecraft:entity.bogged.shear")
		}
		return mushrooms == 2 && sheared && sound
	})
	if mushrooms != 2 || !sheared || !sound {
		t.Fatalf("the shearing gave %d mushrooms, sheared flag %v, sound %v; want 2, true, true", mushrooms, sheared, sound)
	}
	// Bogged.spawnShearedMushrooms lays both at the body's own x and z.
	for _, drop := range laid {
		if math.Abs(drop.X-laid[0].X) > 1e-9 || math.Abs(drop.Z-laid[0].Z) > 1e-9 {
			t.Errorf("mushrooms were laid at (%v, %v) and (%v, %v), want one shared point", drop.X, drop.Z, laid[0].X, laid[0].Z)
		}
		if math.Abs(drop.X-1.5) > 0.5 || math.Abs(drop.Z-0.5) > 0.5 {
			t.Errorf("a mushroom was laid at (%v, %v), want the bogged's own x and z near (1.5, 0.5)", drop.X, drop.Z)
		}
	}

	c.UseOn(bogged.EntityID)
	c.ReadTicksSkipping(srv, 40, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id == protocol.IDPlayEntitySoundEffect {
			if servertest.SoundIs(data, "minecraft:entity.bogged.shear") {
				t.Error("a sheared bogged was sheared again")
				return true
			}
		}
		return false
	})
}

// TestACaveSpidersBitePoisons checks CaveSpider against the spider it
// extends: twelve of health (createCaveSpider), the spider's 0.3 pace, and
// the bite's poison of fifteen seconds on hard (doHurtTarget).
func TestACaveSpidersBitePoisons(t *testing.T) {
	t.Parallel()
	srv, c, playerID, spider := variantNight(t, "minecraft:cave_spider")
	if spider.Health != 12 || spider.Speed != float64(float32(0.3)) {
		t.Errorf("cave spider health %v, speed %v; want 12 and 0.3", spider.Health, spider.Speed)
	}
	poisoned := c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayUpdateMobEffect {
			return false
		}
		r := protocol.NewReader(data)
		got := servertest.MobEffect{Entity: r.ReadVarInt(), Effect: r.ReadVarInt(), Amplifier: r.ReadVarInt(), Duration: r.ReadVarInt(), Flags: r.ReadU8()}
		if got.Entity != playerID {
			return false
		}
		if got.Effect != registry.MustMobEffect("minecraft:poison") || got.Amplifier != 0 || got.Duration != 300 {
			t.Errorf("the cave spider's bite gave %+v, want poison I for 300 ticks", got)
		}
		return true
	})
	if !poisoned {
		t.Error("the cave spider's bite never poisoned")
	}
}

// TestADrownedLeavesAPlayerOnLandByDay checks Drowned.okTarget: at noon a
// player out of the water is no target, where a zombie would hunt them.
func TestADrownedLeavesAPlayerOnLandByDay(t *testing.T) {
	t.Parallel()
	srv, c, _, drowned := variantHunt(t, "minecraft:drowned", 4.5, 4.5)
	if drowned.Health != 20 || drowned.Speed != float64(float32(0.23)) {
		t.Errorf("drowned health %v, speed %v; want the zombie's 20 and 0.23", drowned.Health, drowned.Speed)
	}
	if seen := c.WatchDrowned(srv, drowned.EntityID, 80, func(s servertest.DrownedSeen) bool { return s.Aggressive }); seen.Aggressive {
		t.Error("a drowned hunted a player on land at noon")
	}
}

// TestADrownedSwimsAtAPlayerInWater stands a drowned at the bottom of a
// pool at noon with a survival player in the water above it: it hunts
// them, raises the swimming flag (Drowned.updateSwimming) with no pose of
// its own, and neither burns nor drowns while it does.
func TestADrownedSwimsAtAPlayerInWater(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Swimmer"), func(s *server.Server) { s.SetTimeSync(0) })
	srv.SetPaused(true)
	srv.SetDifficulty(3)
	c := connect()
	c.JoinAs("Swimmer")
	c.ChatCommand("time set noon")
	c.Settle()

	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	stone, _ := registry.FindState("minecraft:stone", nil)
	for x := int32(1); x <= 9; x++ {
		for z := int32(-4); z <= 4; z++ {
			for y := int32(64); y <= 69; y++ {
				if x == 1 || x == 9 || z == -4 || z == 4 {
					srv.SetBlock(x, y, z, stone)
				} else {
					srv.SetBlock(x, y, z, water)
				}
			}
		}
	}
	c.MoveTo(5.5, 68, 0.5, 0, 0)
	c.Settle()
	srv.SpawnMob("minecraft:drowned", 5.5, 64, 2.5)
	drowned := c.WatchVariant(srv, "minecraft:drowned")
	seen := c.WatchDrowned(srv, drowned.EntityID, servertest.HostileTicks, func(s servertest.DrownedSeen) bool {
		return s.Aggressive && s.Swimming || s.OnFire || s.Hurt
	})
	if !seen.Aggressive || !seen.Swimming || seen.Pose {
		t.Errorf("the drowned hunted %v, swam %v and sent a pose %v; want a hunt and a swim with no pose", seen.Aggressive, seen.Swimming, seen.Pose)
	}
	if seen.OnFire || seen.Hurt {
		t.Errorf("the drowned in the water burned %v or was hurt %v", seen.OnFire, seen.Hurt)
	}
	// A few seconds more under the noonday water: no fire, no air lost.
	seen = c.WatchDrowned(srv, drowned.EntityID, 60, func(s servertest.DrownedSeen) bool { return s.OnFire || s.Hurt })
	if seen.OnFire || seen.Hurt {
		t.Errorf("the drowned in the water burned %v or was hurt %v", seen.OnFire, seen.Hurt)
	}
}

// TestAZombieVillagerCarriesItsVillagerData checks ZombieVillager against
// the zombie it extends: the zombie's health and pace, and the Villager
// Data its finalizeSpawn gives — the plains type the flat world's biome
// maps to, a profession out of the whole registry at level one, always
// sent — with the finalized flag raised.
func TestAZombieVillagerCarriesItsVillagerData(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Watcher")
	srv.SpawnMob("minecraft:zombie_villager", 4.5, 64, 4.5)
	zv := c.WatchVariant(srv, "minecraft:zombie_villager")
	if zv.Health != 20 || zv.Speed != float64(float32(0.23)) {
		t.Errorf("zombie villager health %v, speed %v; want the zombie's 20 and 0.23", zv.Health, zv.Speed)
	}
	if got := zv.Meta[protocol.MetadataIndexVillagerDataFinalized]; len(got) != 1 || got[0] != 1 {
		t.Errorf("villager data finalized = %v, want true", got)
	}
	// Sent whatever it is: a client told nothing rolls a profession of
	// its own.
	data := zv.Meta[protocol.MetadataIndexVillagerData]
	if len(data) != 3 || data[0] != 2 || data[1] < 0 || data[1] > 14 || data[2] != 1 {
		t.Errorf("villager data = %v, want the plains type (2), a profession, level 1", data)
	}
}

// TestAZombieVillagerKeepsItsDataThroughARestart checks the save
// ZombieVillager.addAdditionalSaveData writes — VillagerData as type,
// profession and level, VillagerDataFinalized — and that a restarted
// server reads it back and writes it again unchanged.
func TestAZombieVillagerKeepsItsDataThroughARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()
	saved := func() nbt.Compound {
		var data nbt.Compound
		servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:zombie_villager", func(compound nbt.Compound) {
			data, _ = nbt.Get[nbt.Compound](compound, "VillagerData")
			if finalized, _ := nbt.Get[nbt.Byte](compound, "VillagerDataFinalized"); finalized != 1 {
				t.Error("the zombie villager was saved unfinalized")
			}
		})
		return data
	}

	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Keeper")
	srv.SpawnMob("minecraft:zombie_villager", 4.5, 64, 4.5)
	c.WatchVariant(srv, "minecraft:zombie_villager")
	srv.FlushMobs()
	srv.Shutdown()
	before := saved()
	typ, _ := nbt.Get[nbt.String](before, "type")
	level, _ := nbt.Get[nbt.Int](before, "level")
	if _, ok := nbt.Get[nbt.String](before, "profession"); !ok || typ != "minecraft:plains" || level != 1 {
		t.Fatalf("saved VillagerData = %v, want the plains type, a profession and level 1", before)
	}

	srv, connect = servertest.Start(t, cfg)
	c = connect()
	c.Join("Keeper")
	c.Settle()
	srv.FlushMobs()
	srv.Shutdown()
	if after := saved(); !reflect.DeepEqual(after, before) {
		t.Errorf("VillagerData after a restart = %v, want %v", after, before)
	}
}

// TestASlimeTouchesItsPreyAndSplits checks the slime against
// AbstractCubeMob and Slime: a size of one, two or four with the size's
// square of health and 0.2 + 0.1 a size of pace, its touch hurting a
// player (dealDamage, with entity.slime.attack), and its death leaving two
// to four slimes of half the size.
func TestASlimeTouchesItsPreyAndSplits(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	srv, c, _ := servertest.JoinAs(t, cfg, "Prey", servertest.OpsListing(t, "Prey"))

	// A slime of size two or four: the finalize's dice, seeded by the
	// spawn's position, give one a third of the time, and the next
	// position is tried.
	var slime servertest.VariantSeen
	size := int32(1)
	for try := 0; size == 1; try++ {
		if try == 10 {
			t.Fatal("ten slimes in a row came out of the smallest size")
		}
		srv.SpawnMob("minecraft:slime", 2.5, 64, 0.5+float64(try))
		slime = c.WatchVariant(srv, "minecraft:slime")
		size = 1
		if got, ok := slime.Meta[protocol.MetadataIndexCubeSize]; ok {
			size = got[0]
		}
		if size == 1 {
			c.ChatCommand("kill @e[type=minecraft:slime]")
			c.Settle()
		}
	}
	if size != 2 && size != 4 {
		t.Fatalf("slime size %d, want 1, 2 or 4", size)
	}
	if slime.Health != float32(size*size) || slime.Speed != float64(float32(0.2)+float32(0.1)*float32(size)) {
		t.Errorf("size %d slime health %v, speed %v; want %d and 0.2 + 0.1 a size", size, slime.Health, slime.Speed, size*size)
	}
	if len(slime.Attrs) != 2 || slime.Attrs[protocol.AttributeMaxHealth] != float64(size*size) {
		t.Errorf("size %d slime attributes %v, want the speed and a max health of %d", size, slime.Attrs, size*size)
	}

	hurt, sound := false, false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySetHealth:
			if servertest.ExpectHealthReport(c, data).Health < 20 {
				hurt = true
			}
		case protocol.IDPlaySoundEffect:
			sound = sound || servertest.SoundIs(data, "minecraft:entity.slime.attack")
		}
		return hurt && sound
	})
	if !hurt || !sound {
		t.Fatalf("the slime's touch hurt %v, sounded %v; want both", hurt, sound)
	}

	c.ChatCommand("kill @e[type=minecraft:slime]")
	children := 0
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		if want, _ := registry.EntityTypeID("minecraft:slime"); r.ReadVarInt() == want {
			children++
		}
		return children == 2
	})
	if children < 2 {
		t.Errorf("the dead slime left %d slimes, want two to four", children)
	}
}

// TestAMagmaCubeTouchesItsPrey checks MagmaCube against the slime: the
// same health and pace by size, and a touch that hurts whatever its size
// (isDealsDamage), its blow two more than the size.
func TestAMagmaCubeTouchesItsPrey(t *testing.T) {
	t.Parallel()
	// Close by: the magma cube's hops wait four times the slime's.
	srv, c, _, magma := variantHunt(t, "minecraft:magma_cube", 1.5, 0.5)
	size := int32(1)
	if got, ok := magma.Meta[protocol.MetadataIndexCubeSize]; ok {
		size = got[0]
	}
	if magma.Health != float32(size*size) || magma.Speed != float64(float32(0.2)+float32(0.1)*float32(size)) {
		t.Errorf("size %d magma cube health %v, speed %v; want %d and 0.2 + 0.1 a size", size, magma.Health, magma.Speed, size*size)
	}
	if len(magma.Attrs) != 3 || magma.Attrs[protocol.AttributeArmor] != float64(3*size) || magma.Attrs[protocol.AttributeMaxHealth] != float64(size*size) {
		t.Errorf("size %d magma cube attributes %v, want the speed, %d armour and a max health of %d", size, magma.Attrs, 3*size, size*size)
	}
	var health float32 = 20
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySetHealth {
			health = servertest.ExpectHealthReport(c, data).Health
		}
		return health < 20
	})
	// Hard scales a mob's blow by half again.
	if want := 20 - float32(size+2)*1.5; health != want {
		t.Errorf("the magma cube's touch left %v, want %v", health, want)
	}
}

// TestZombiesDrownIntoDrownedAndHusksIntoZombies checks Zombie.tick's
// conversion: 600 ticks with the eyes in water start it, with the
// becoming-a-drowned flag raised, and 300 more turn a zombie into a drowned
// (world event 1040) and a husk into a zombie (1041). The name a name tag
// gave the zombie goes to its drowned (ConversionType.convertCommon).
func TestZombiesDrownIntoDrownedAndHusksIntoZombies(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Diver")
	srv.StopTick()

	water, _ := registry.FindState("minecraft:water", nil)
	stone, _ := registry.FindState("minecraft:stone", nil)
	for x := int32(2); x <= 6; x++ {
		for z := int32(-2); z <= 2; z++ {
			for y := int32(64); y <= 68; y++ {
				if x == 2 || x == 6 || z == -2 || z == 2 {
					srv.SetBlock(x, y, z, stone)
				} else {
					srv.SetBlock(x, y, z, water)
				}
			}
		}
	}
	zombieID := srv.SpawnMob("minecraft:zombie", 3.5, 64, 0.5)
	srv.SpawnMob("minecraft:husk", 5.5, 64, 0.5)
	srv.GiveSlot("Diver", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItem("minecraft:name_tag"), Count: 1, CustomName: &protocol.Text{Text: "Bob"},
	})
	c.Settle()
	c.UseOn(zombieID)
	c.Settle()
	srv.TickMobs(drownTicks)

	kinds := map[string]int{}
	for _, m := range srv.Mobs() {
		kinds[m.Kind]++
	}
	if kinds["minecraft:drowned"] != 1 || kinds["minecraft:zombie"] != 1 || kinds["minecraft:husk"] != 0 {
		t.Errorf("after %d ticks under water the mobs are %v, want one drowned and one zombie", drownTicks, kinds)
	}

	flagged := false
	events := map[int32]bool{}
	skip := func(id int32) bool { return id != protocol.IDPlayWorldEvent && servertest.HostileSkip(id) }
	c.ReadTicksSkipping(srv, 100, skip, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayEntityMetadata:
			if r.ReadVarInt() == zombieID {
				v := servertest.VariantSeen{Meta: map[uint8][]int32{}}
				c.ReadVariantMetadata(r, &v)
				if got := v.Meta[protocol.MetadataIndexZombieDrowning]; len(got) == 1 && got[0] == 1 {
					flagged = true
				}
			}
		case protocol.IDPlayWorldEvent:
			events[r.ReadI32()] = true
		}
		return flagged && events[protocol.WorldEventZombieDrowned] && events[protocol.WorldEventHuskDrowned]
	})
	if !flagged || !events[protocol.WorldEventZombieDrowned] || !events[protocol.WorldEventHuskDrowned] {
		t.Errorf("drowning flag %v, world events %v; want the flag and both 1040 and 1041", flagged, events)
	}

	srv.FlushMobs()
	srv.Shutdown()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:drowned", func(compound nbt.Compound) {
		if got := compound["CustomName"]; !reflect.DeepEqual(got, nbt.String("Bob")) {
			t.Errorf("the drowned's CustomName = %v, want the zombie's Bob", got)
		}
		if persistent, _ := nbt.Get[nbt.Byte](compound, "PersistenceRequired"); persistent != 1 {
			t.Error("the drowned lost the persistence the name tag gave the zombie")
		}
	})
}

// TestAZombieThatLeavesTheWaterStopsDrowning drains the pool once a zombie
// has started shaking: the conversion is cancelled and the flag cleared.
func TestAZombieThatLeavesTheWaterStopsDrowning(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Diver")
	srv.StopTick()

	water, _ := registry.FindState("minecraft:water", nil)
	stone, _ := registry.FindState("minecraft:stone", nil)
	for x := int32(2); x <= 6; x++ {
		for z := int32(-2); z <= 2; z++ {
			for y := int32(64); y <= 68; y++ {
				if x == 2 || x == 6 || z == -2 || z == 2 {
					srv.SetBlock(x, y, z, stone)
				} else {
					srv.SetBlock(x, y, z, water)
				}
			}
		}
	}
	zombieID := srv.SpawnMob("minecraft:zombie", 3.5, 64, 0.5)
	c.Settle()
	srv.TickMobs(650)
	for x := int32(3); x <= 5; x++ {
		for z := int32(-1); z <= 1; z++ {
			for y := int32(64); y <= 68; y++ {
				srv.SetBlock(x, y, z, 0)
			}
		}
	}
	// The old conversion would finish 250 ticks after the drain; the sun
	// kills the zombie a little after 300, so stop there.
	srv.TickMobs(300)

	kinds := map[string]int{}
	for _, m := range srv.Mobs() {
		kinds[m.Kind]++
	}
	if kinds["minecraft:zombie"] != 1 || kinds["minecraft:drowned"] != 0 {
		t.Errorf("after leaving the water the mobs are %v, want one zombie and no drowned", kinds)
	}

	// The flag goes up when the conversion starts and down when the eyes
	// leave the water.
	raised, cleared := false, false
	c.ReadTicksSkipping(srv, 100, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() == zombieID {
			v := servertest.VariantSeen{Meta: map[uint8][]int32{}}
			c.ReadVariantMetadata(r, &v)
			if got := v.Meta[protocol.MetadataIndexZombieDrowning]; len(got) == 1 {
				raised = raised || got[0] == 1
				cleared = raised && got[0] == 0
			}
		}
		return cleared
	})
	if !raised || !cleared {
		t.Errorf("drowning flag raised %v, cleared %v; want both after the drain", raised, cleared)
	}
}

// drownTicks is the underwater conversion's 600 and 300 ticks, and a few
// for the eyes to go under.
const drownTicks = 600 + 300 + 5

// TestATaggedSlimeSummonsAtItsSizeAndSplitsAlike checks a tagged summon's
// Size (AbstractCubeMob.readAdditionalSaveData, the size one more) and the
// split's convertCommon: the children of a NoAI slime are NoAI too.
func TestATaggedSlimeSummonsAtItsSizeAndSplitsAlike(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Watcher", servertest.OpsListing(t, "Watcher"))
	c.ChatCommand("summon minecraft:slime 3.5 64 0.5 {Size:3,NoAI:1b}")
	slime := c.WatchVariant(srv, "minecraft:slime")
	if got := slime.Meta[protocol.MetadataIndexCubeSize]; len(got) != 1 || got[0] != 4 || slime.Health != 16 {
		t.Fatalf("a slime summoned with Size:3 has size %v and health %v, want 4 and 16", got, slime.Health)
	}

	c.ChatCommand("kill @e[type=minecraft:slime]")
	want, _ := registry.EntityTypeID("minecraft:slime")
	children := map[int32]bool{}
	checked := 0
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySpawnEntity:
			entityID := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == want {
				children[entityID] = true
			}
		case protocol.IDPlayEntityMetadata:
			entityID := r.ReadVarInt()
			if !children[entityID] {
				return false
			}
			v := servertest.VariantSeen{Meta: map[uint8][]int32{}}
			c.ReadVariantMetadata(r, &v)
			if got := v.Meta[protocol.MetadataIndexCubeSize]; len(got) != 1 || got[0] != 2 {
				t.Errorf("a split slime's size is %v, want 2", got)
			}
			if flags := v.Meta[protocol.MetadataIndexMobFlags]; len(flags) != 1 || flags[0]&protocol.EntityFlagNoAI == 0 {
				t.Errorf("a NoAI slime's split has mob flags %v, want NoAI", flags)
			}
			checked++
		}
		return checked == 2
	})
	if checked < 2 {
		t.Errorf("%d split slimes were shown, want two to four", checked)
	}
}

// TestASwimmingDrownedClimbsTheWaterTowardItsPrey drives the swim's own
// travel, the branch travelInFluid takes while the trackers hold the body
// in water and the swimming flag stands: a drowned at the bottom of a pool
// with its prey in the water above climbs the column toward them — near
// four and a half blocks in the forty ticks the test drives, where the
// plain water travel it would fall back to covers under three.
func TestASwimmingDrownedClimbsTheWaterTowardItsPrey(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Swimmer"))
	srv.SetDifficulty(3)
	c := connect()
	c.JoinAs("Swimmer")

	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	stone, _ := registry.FindState("minecraft:stone", nil)
	for x := int32(1); x <= 9; x++ {
		for z := int32(-4); z <= 4; z++ {
			for y := int32(64); y <= 69; y++ {
				if x == 1 || x == 9 || z == -4 || z == 4 {
					srv.SetBlock(x, y, z, stone)
				} else {
					srv.SetBlock(x, y, z, water)
				}
			}
		}
	}
	c.MoveTo(5.5, 68, 0.5, 0, 0)
	c.Settle()
	srv.StopTick()

	drownedID := srv.SpawnMob("minecraft:drowned", 5.5, 64, 2.5)
	drowned, _ := registry.EntityTypeID("minecraft:drowned")
	c.ExpectMobSpawn(drowned)
	_, startY, _, ok := srv.MobPos(drownedID)
	if !ok {
		t.Fatal("the spawned drowned is nowhere to be found")
	}

	highest := startY
	for range 4 {
		srv.TickMobs(10)
		if _, y, _, ok := srv.MobPos(drownedID); ok && y > highest {
			highest = y
		}
	}
	if highest < startY+3 {
		t.Errorf("the drowned climbed from %v to %v in forty ticks, want the swim's own three or more", startY, highest)
	}
}

// TestADrownedInWaterMoansItsWaterSound waits out a drowned's moan in a
// pool: the water ambient its Sounds() carries — entity.drowned.ambient_water,
// not the dry one the kind's table holds — the moan path's own read, the
// trap an AmbientVoice that read the table would fall into.
func TestADrownedInWaterMoansItsWaterSound(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.StopTick()
	c := connect().Join("Swimmer")

	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	stone, _ := registry.FindState("minecraft:stone", nil)
	for x := int32(2); x <= 6; x++ {
		for z := int32(-2); z <= 2; z++ {
			for y := int32(64); y <= 68; y++ {
				if x == 2 || x == 6 || z == -2 || z == 2 {
					srv.SetBlock(x, y, z, stone)
				} else {
					srv.SetBlock(x, y, z, water)
				}
			}
		}
	}
	srv.SpawnMob("minecraft:drowned", 4.5, 64, 0.5)
	drowned, _ := registry.EntityTypeID("minecraft:drowned")
	c.ExpectMobSpawn(drowned)

	moaned := false
	for tick := 0; tick < servertest.AmbientTicks && !moaned; tick += 10 {
		srv.TickMobs(10)
		c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
			if id != protocol.IDPlaySoundEffect {
				return false
			}
			name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
			switch name {
			case "minecraft:entity.drowned.ambient_water":
				moaned = true
				if category != protocol.SoundCategoryHostile {
					t.Errorf("the drowned's water moan played in category %d, want %d", category, protocol.SoundCategoryHostile)
				}
				if volume != 1.0 {
					t.Errorf("the drowned's water moan played at volume %v, want the kind's 1.0", volume)
				}
				if pitch < 0.79 || pitch > 1.21 {
					t.Errorf("the drowned's water moan played at pitch %v, want the voice's own spread around one", pitch)
				}
			case "minecraft:entity.drowned.ambient":
				t.Error("the drowned in the water moaned the dry sound")
			}
			return false
		})
	}
	if !moaned {
		t.Errorf("the drowned never moaned within %d ticks, want entity.drowned.ambient_water", servertest.AmbientTicks)
	}
}

// TestAZombiesDrownedKeepsTheZombiesTags plants a zombie carrying the
// unmodelled tags a vanilla file gives one — the persistence and the mob
// flags, a tag of its own, the loot pickup, the door breaking — and an
// effect that will outlast the drowning, drowns it, and reads the drowned
// it becomes back from the save: every one of them there, the effect live
// on the drowned the way convertCommon carries it.
func TestAZombiesDrownedKeepsTheZombiesTags(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()

	// The effect the convertCommon copy is tested with is an infinite
	// one: a numbered one counts down in ticks on the live body, and the
	// drowning spends nine hundred of them before the drowned stands up.
	effects := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{
			"id":       nbt.String("minecraft:speed"),
			"duration": nbt.Int(-1),
		},
	}}
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id":                  nbt.String("minecraft:zombie"),
		"UUID":                servertest.UuidWords(servertest.FixtureUUID(7, 7, 7, 7)),
		"Pos":                 nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(4.5), nbt.Double(64), nbt.Double(0.5)}},
		"Health":              nbt.Float(20),
		"PersistenceRequired": nbt.Byte(1),
		"LeftHanded":          nbt.Byte(1),
		"Silent":              nbt.Byte(1),
		"Invulnerable":        nbt.Byte(1),
		"NoGravity":           nbt.Byte(1),
		"Tags":                nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("hunter")}},
		"CanPickUpLoot":       nbt.Byte(1),
		"CanBreakDoors":       nbt.Byte(1),
		"active_effects":      effects,
	})

	// The world stays frozen through the join, so the planted zombie wakes
	// on the first tick driven below, already in the pool: woken on the
	// free loop, it can stroll out of the pool's cells before the loop
	// stops.
	srv, connect := servertest.Start(t, cfg, func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	connect().Join("Diver")
	srv.StopTick()
	srv.SetTickFrozen(false)

	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	stone, _ := registry.FindState("minecraft:stone", nil)
	for x := int32(2); x <= 6; x++ {
		for z := int32(-2); z <= 2; z++ {
			for y := int32(64); y <= 68; y++ {
				if x == 2 || x == 6 || z == -2 || z == 2 {
					srv.SetBlock(x, y, z, stone)
				} else {
					srv.SetBlock(x, y, z, water)
				}
			}
		}
	}
	srv.TickMobs(drownTicks)
	kinds := map[string]int{}
	for _, m := range srv.Mobs() {
		kinds[m.Kind]++
	}
	if kinds["minecraft:drowned"] != 1 {
		t.Fatalf("after %d ticks under water the mobs are %v, want one drowned", drownTicks, kinds)
	}

	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:drowned", func(compound nbt.Compound) {
		want := map[string]nbt.Tag{
			"PersistenceRequired": nbt.Byte(1),
			"LeftHanded":          nbt.Byte(1),
			"Silent":              nbt.Byte(1),
			"Invulnerable":        nbt.Byte(1),
			"NoGravity":           nbt.Byte(1),
			"CanPickUpLoot":       nbt.Byte(1),
			"CanBreakDoors":       nbt.Byte(1),
			// vanilla saves the fresh body's counter: in water, nothing
			// counted yet
			"InWaterTime": nbt.Int(0),
		}
		for key, value := range want {
			if !reflect.DeepEqual(compound[key], value) {
				t.Errorf("the drowned saved %s = %v, want %v", key, compound[key], value)
			}
		}
		if tags, ok := nbt.Get[nbt.List](compound, "Tags"); !ok || !reflect.DeepEqual(tags, nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("hunter")}}) {
			t.Errorf("the drowned saved Tags = %v, want the zombie's hunter", tags)
		}
		active, ok := nbt.Get[nbt.List](compound, "active_effects")
		if !ok || len(active.Items) != 1 {
			t.Fatalf("the drowned saved active_effects = %v, %v; want the zombie's one speed", compound["active_effects"], ok)
		}
		saved := active.Items[0].(nbt.Compound)
		if saved["id"] != nbt.String("minecraft:speed") || saved["duration"] != nbt.Int(-1) ||
			saved["show_icon"] != nbt.Byte(1) || len(saved) != 3 {
			t.Errorf("the drowned saved active_effects = %v, want the zombie's infinite speed with its icon", saved)
		}
	})
}

// TestADrownedInShallowWaterSavesItsWaterCount plants a NoAI drowned
// wading in water one block deep, below its eyes, and reads the save back:
// the counter is saved only while the eyes are in water
// (ConversionTracker.addAdditionalSaveData), so the shallow wade saves -1.
func TestADrownedInShallowWaterSavesItsWaterCount(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.LevelName = t.TempDir()

	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id":     nbt.String("minecraft:drowned"),
		"UUID":   servertest.UuidWords(servertest.FixtureUUID(9, 9, 9, 9)),
		"Pos":    nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(4.5), nbt.Double(64), nbt.Double(0.5)}},
		"Health": nbt.Float(20),
		"NoAI":   nbt.Byte(1),
		"Silent": nbt.Byte(1),
	})

	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Wader")
	c.Configure()
	// The join is what wakes the spawn chunks and loads the planted
	// drowned; the tick loop stops once it is in the world, and the
	// wading pool goes in around it.
	c.JoinSeeingMobs("Wader", servertest.FixtureUUID(9, 9, 9, 9))
	srv.StopTick()

	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the block registry has no water")
	}
	stone, _ := registry.FindState("minecraft:stone", nil)
	for x := int32(2); x <= 6; x++ {
		for z := int32(-2); z <= 2; z++ {
			srv.SetBlock(x, 63, z, stone)
			if x == 2 || x == 6 || z == -2 || z == 2 {
				srv.SetBlock(x, 64, z, stone)
			} else {
				srv.SetBlock(x, 64, z, water)
			}
		}
	}
	// One tick of standing in the water is what fills the body's fluid
	// read the save keeps the counter by.
	srv.TickMobs(1)

	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:drowned", func(compound nbt.Compound) {
		if compound["InWaterTime"] != nbt.Int(-1) {
			t.Errorf("the drowned wading saved InWaterTime = %v, want -1", compound["InWaterTime"])
		}
	})
}

// TestASplitSlimeKeepsItsParentsPersistenceAlone kills a planted NoAI,
// persistent slime that could pick up loot and carries an infinite effect:
// the children in the save keep the persistence, the NoAI, the portal
// cooldown and the effect — the split's convertCommon — and not the loot
// pickup, which only a full conversion carries.
func TestASplitSlimeKeepsItsParentsPersistenceAlone(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()

	// The effect the convertCommon copy is tested with is an infinite one:
	// a numbered one counts down in ticks with the live body, and the join
	// spends however many ticks of its own before the kill.
	effects := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{
			"id":       nbt.String("minecraft:speed"),
			"duration": nbt.Int(-1),
		},
	}}
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id":                  nbt.String("minecraft:slime"),
		"UUID":                servertest.UuidWords(servertest.FixtureUUID(9, 9, 9, 9)),
		"Pos":                 nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(8.5), nbt.Double(64), nbt.Double(8.5)}},
		"Health":              nbt.Float(16),
		"Size":                nbt.Int(3),
		"NoAI":                nbt.Byte(1),
		"PersistenceRequired": nbt.Byte(1),
		"PortalCooldown":      nbt.Int(120),
		"CanPickUpLoot":       nbt.Byte(1),
		"active_effects":      effects,
	})

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Splitter"))
	c := connect()
	c.Login("Splitter")
	c.Configure()
	// The join is what wakes the spawn chunk and loads the planted
	// slime; the wait for its spawn is what makes sure it did.
	c.JoinSeeingMobs("Splitter", servertest.FixtureUUID(9, 9, 9, 9))

	c.ChatCommand("kill @e[type=minecraft:slime]")
	// The parent's death leaves its split behind: two to four children,
	// whose metadata says their size, whose save the flush that follows
	// the last one's arrival takes down.
	want, _ := registry.EntityTypeID("minecraft:slime")
	children := map[int32]bool{}
	checked := 0
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySpawnEntity:
			entityID := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == want {
				children[entityID] = true
			}
		case protocol.IDPlayEntityMetadata:
			entityID := r.ReadVarInt()
			if !children[entityID] {
				return false
			}
			checked++
		}
		return checked >= 2
	})
	srv.FlushMobs()

	saved := 0
	for _, compound := range servertest.ReadChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}) {
		if kind, _ := nbt.Get[nbt.String](compound, "id"); string(kind) != "minecraft:slime" {
			continue
		}
		size, _ := nbt.Get[nbt.Int](compound, "Size")
		if size != 1 { // the parent's four split to twos, saved one less
			t.Errorf("a saved slime of size %d stands in the split's place, want the children's 1", size)
			continue
		}
		saved++
		if persistent, _ := nbt.Get[nbt.Byte](compound, "PersistenceRequired"); persistent != 1 {
			t.Error("a split slime saved unpersistent, want the parent's persistence")
		}
		if noAI, _ := nbt.Get[nbt.Byte](compound, "NoAI"); noAI != 1 {
			t.Error("a split slime saved with its AI, want the parent's NoAI")
		}
		if cooldown, _ := nbt.Get[nbt.Int](compound, "PortalCooldown"); cooldown <= 0 {
			t.Errorf("a split slime saved portal cooldown %d, want the parent's copy", cooldown)
		}
		active, ok := nbt.Get[nbt.List](compound, "active_effects")
		if !ok || len(active.Items) != 1 {
			t.Fatalf("a split slime saved active_effects = %v, %v; want the parent's speed", compound["active_effects"], ok)
		}
		saved := active.Items[0].(nbt.Compound)
		if saved["id"] != nbt.String("minecraft:speed") || saved["duration"] != nbt.Int(-1) ||
			saved["show_icon"] != nbt.Byte(1) || len(saved) != 3 {
			t.Errorf("a split slime saved active_effects = %v, want the parent's infinite speed with its icon", saved)
		}
		// CanPickUpLoot every mob's save carries, the split child's own
		// false standing where the parent's one would have been; the door
		// breaking key only a full conversion carries.
		if got, ok := nbt.Get[nbt.Byte](compound, "CanPickUpLoot"); !ok || got != 0 {
			t.Errorf("a split slime saved CanPickUpLoot = %v (present %v), want the fresh body's false", got, ok)
		}
		if _, ok := compound["CanBreakDoors"]; ok {
			t.Error("a split slime saved CanBreakDoors, which only a full conversion carries")
		}
	}
	if saved < 2 {
		t.Errorf("%d split slimes were saved, want two to four", saved)
	}
}
