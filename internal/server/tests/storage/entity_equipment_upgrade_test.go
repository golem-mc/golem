package storage

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: SaddleEquipmentSlotFix, DropChancesFormatFix, EquipmentFormatFix
func TestLegacyPassengerEquipmentResavesAfterRegionReload(t *testing.T) {
	for _, version := range []int32{4189, 4299} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			t.Parallel()
			cfg := servertest.MobTestConfig()
			cfg.LevelName = t.TempDir()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			pos := world.ChunkPos{}
			pig, arrow, strider := servertest.FixtureUUID(1, 2, 3, 4), servertest.FixtureUUID(5, 6, 7, 8), servertest.FixtureUUID(9, 10, 11, 12)
			passenger := nbt.Compound{
				"id": nbt.String("minecraft:strider"), "UUID": nbt.UUIDToIntArray(strider), "Saddle": nbt.Byte(1),
			}
			if version < 4293 {
				passenger["HandDropChances"] = nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(0.5), nbt.Float(0.085)}}
			} else {
				passenger["drop_chances"] = nbt.Compound{"mainhand": nbt.Float(0.5)}
			}
			servertest.PlantChunkEntitiesAt(t, cfg.LevelName, pos, version, nbt.Compound{
				"id": nbt.String("minecraft:pig"), "UUID": nbt.UUIDToIntArray(pig),
				"Pos":  nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(3.5), nbt.Double(65), nbt.Double(3.5)}},
				"NoAI": nbt.Byte(1), "PersistenceRequired": nbt.Byte(1),
				"Passengers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
					"id": nbt.String("minecraft:arrow"), "UUID": nbt.UUIDToIntArray(arrow),
					"Passengers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{passenger}},
				}}},
			})
			srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"), func(s *server.Server) {
				servertest.FreezeUntilHeld(t, s)
			})
			servertest.HoldTicks(t, srv)
			servertest.RunTicks(t, srv, 1)
			c.Settle()
			if pigs := srv.MobIDsOfKind("minecraft:pig"); len(pigs) != 1 {
				t.Fatalf("loaded pigs = %v, want %v", pigs, pig)
			}
			srv.FlushMobs()
			servertest.EntityFileIs(t, cfg.LevelName, pos, "minecraft:pig", func(saved nbt.Compound) {
				if !nbt.Equal(saved["UUID"], nbt.UUIDToIntArray(pig)) {
					t.Fatalf("saved pig UUID = %v, want %v", saved["UUID"], pig)
				}
				for _, id := range []nbt.IntArray{nbt.UUIDToIntArray(arrow), nbt.UUIDToIntArray(strider)} {
					passengers, _ := nbt.Get[nbt.List](saved, "Passengers")
					if len(passengers.Items) != 1 {
						t.Fatalf("saved passengers = %v, want one", passengers)
					}
					var ok bool
					saved, ok = nbt.CompoundView(passengers.Items[0])
					if !ok || !nbt.Equal(saved["UUID"], id) {
						t.Fatalf("saved passenger = %v, want UUID %v", saved, id)
					}
				}
				for _, key := range []string{"Saddle", "HandDropChances"} {
					if _, ok := saved[key]; ok {
						t.Errorf("saved passenger retains %s", key)
					}
				}
				equipment, _ := nbt.Get[nbt.Compound](saved, "equipment")
				want := nbt.Compound{"id": nbt.String("minecraft:saddle"), "count": nbt.Int(1)}
				if !nbt.Equal(equipment["saddle"], want) {
					t.Errorf("saved saddle = %v, want %v", equipment["saddle"], want)
				}
				chances, _ := nbt.Get[nbt.Compound](saved, "drop_chances")
				if !nbt.Equal(chances, nbt.Compound{"saddle": nbt.Float(2), "mainhand": nbt.Float(0.5)}) {
					t.Errorf("saved drop chances = %v", chances)
				}
			})
		})
	}
}

// TestAnOlderEntityFileKeepsZombieArmour plants a 1.21.4 entity file
// (DataVersion 4189) whose zombie wears its gear in the old ArmorItems and
// HandItems lists with ArmorDropChances beside them, runs one server over it,
// and finds the helmet, sword and helmet odds in the equipment and
// drop_chances compounds the server writes back.
// vanilla: EquipmentFormatFix, DropChancesFormatFix
func TestAnOlderEntityFileKeepsZombieArmour(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	cfg.LevelName = t.TempDir()
	zombie := servertest.FixtureUUID(41, 88, 41, 89)
	stack := func(id string) nbt.Compound {
		return nbt.Compound{"id": nbt.String(id), "count": nbt.Int(1)}
	}
	compounds := func(items ...nbt.Tag) nbt.List { return nbt.List{Elem: nbt.TypeCompound, Items: items} }
	floats := func(v ...float32) nbt.List {
		l := nbt.List{Elem: nbt.TypeFloat}
		for _, f := range v {
			l.Items = append(l.Items, nbt.Float(f))
		}
		return l
	}
	servertest.PlantChunkEntitiesAt(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, 4189, nbt.Compound{
		"id":   nbt.String("minecraft:zombie"),
		"UUID": servertest.UuidWords(zombie),
		"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(8.5), nbt.Double(64), nbt.Double(8.5)}},
		"NoAI":                nbt.Byte(1),
		"NoGravity":           nbt.Byte(1),
		"Invulnerable":        nbt.Byte(1),
		"PersistenceRequired": nbt.Byte(1),
		"ArmorItems":          compounds(nbt.Compound{}, nbt.Compound{}, nbt.Compound{}, stack("minecraft:iron_helmet")),
		"HandItems":           compounds(stack("minecraft:iron_sword"), nbt.Compound{}),
		"ArmorDropChances":    floats(0.085, 0.085, 0.085, 1),
		"HandDropChances":     floats(0.085, 0.085),
	})

	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Keeper")
	c.Configure()
	c.JoinSeeingMobs("Keeper", zombie)
	servertest.HoldTicks(t, srv)
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()

	got, ok := servertest.SavedMobsByUUID(t, cfg.LevelName)[zombie]
	if !ok {
		t.Fatal("the zombie was not saved back under its UUID")
	}
	equipment, _ := nbt.Get[nbt.Compound](got, "equipment")
	for slot, want := range map[string]string{"head": "minecraft:iron_helmet", "mainhand": "minecraft:iron_sword"} {
		item, _ := nbt.Get[nbt.Compound](equipment, slot)
		if id, _ := nbt.Get[nbt.String](item, "id"); string(id) != want {
			t.Errorf("the zombie saved equipment.%s = %v, want %s", slot, equipment[slot], want)
		}
	}
	for _, slot := range []string{"feet", "legs", "chest", "offhand"} {
		if v, ok := equipment[slot]; ok {
			t.Errorf("the zombie saved equipment.%s = %v, an empty slot", slot, v)
		}
	}
	chances, _ := nbt.Get[nbt.Compound](got, "drop_chances")
	if head, _ := nbt.Get[nbt.Float](chances, "head"); head != 1 {
		t.Errorf("the zombie saved drop_chances = %v, want head 1", got["drop_chances"])
	}
	for _, key := range []string{"ArmorItems", "HandItems", "ArmorDropChances", "HandDropChances"} {
		if v, ok := got[key]; ok {
			t.Errorf("the zombie saved the old %s = %v", key, v)
		}
	}
}
