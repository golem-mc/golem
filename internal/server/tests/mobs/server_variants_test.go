package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

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
