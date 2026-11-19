package mobs

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ConversionType.SINGLE.convert, ZombieVillager.finishConversion
func TestCuredZombieVillagerKeepsVehicle(t *testing.T) {
	t.Parallel()
	for _, vehicle := range []string{"oak_boat", "minecart"} {
		t.Run(vehicle, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
			servertest.HoldTicks(t, srv)
			c.MoveToGround(0.5, 64, 0.5, true)
			srv.RunLine(fmt.Sprintf(`summon %s 2.5 64 0.5 {Tags:[vehicle]}`, vehicle))
			srv.RunLine(`summon zombie_villager 2.5 64 0.5 {NoAI:1b,ConversionTime:1}`)
			srv.RunLine(`ride @e[type=zombie_villager,limit=1] mount @e[tag=vehicle,limit=1]`)
			passengers := srv.BoatPassengerIDs
			if vehicle == "minecart" {
				passengers = srv.MinecartPassengerIDs
			}
			c.SettleKeeping()
			typ, _ := registry.EntityTypeID("minecraft:" + vehicle)
			veh := c.AwaitSpawn(typ).EntityID
			old := srv.MobIDsOfKind("minecraft:zombie_villager")
			if len(old) != 1 || !slices.Equal(passengers(veh), old) {
				t.Fatalf("summoned passengers = %v, zombies = %v", passengers(veh), old)
			}
			c.Settle()
			servertest.UntilTicks(t, srv, 5, "the zombie villager cured", func() bool {
				return len(srv.MobIDsOfKind("minecraft:villager")) == 1
			})
			next := srv.MobIDsOfKind("minecraft:villager")[0]
			assertConvertedPassengers(t, c, veh, next, passengers)
			// Exercise the replacement's reverse link: dismount must remove its seat.
			srv.RunLine(`ride @e[type=villager,limit=1] dismount`)
			if got := passengers(veh); len(got) != 0 {
				t.Errorf("passengers after dismount = %v", got)
			}
		})
	}
}

func assertConvertedPassengers(t *testing.T, c *servertest.Client, vehicle, next int32, passengers func(int32) []int32) map[int32][]int32 {
	t.Helper()
	want := []int32{next}
	if got := passengers(vehicle); !slices.Equal(got, want) {
		t.Errorf("server passengers = %v, want %v", got, want)
	}
	seats := make(map[int32][]int32)
	spawned := false
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySpawnEntity:
			spawned = spawned || protocol.NewReader(data).ReadVarInt() == next
		case protocol.IDPlaySetPassengers:
			veh, ids := servertest.ReadPassengers(c, data)
			seats[veh] = ids
			if slices.Contains(ids, next) && !spawned {
				t.Error("Set Passengers named the replacement before its spawn")
			}
		}
	})
	if !slices.Equal(seats[vehicle], want) {
		t.Errorf("Set Passengers = %v, want %v", seats[vehicle], want)
	}
	return seats
}

// vanilla: ConversionTracker.tick, ConversionType.SINGLE.convert
func TestDrownedConversionKeepsChicken(t *testing.T) {
	t.Parallel()
	for _, bubble := range []bool{false, true} {
		t.Run(fmt.Sprintf("bubble=%t", bubble), func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
			servertest.HoldTicks(t, srv)
			c.MoveToGround(0.5, 64, 0.5, true)
			fluid := "water"
			if bubble {
				fluid = "bubble_column"
				srv.RunLine(`fill 2 63 0 4 63 2 soul_sand`)
			}
			srv.RunLine(`fill 2 64 0 4 67 2 ` + fluid)
			srv.RunLine(`summon chicken 3.5 64 1.5 {NoAI:1b,Invulnerable:1b,Passengers:[{id:"minecraft:zombie",IsBaby:1b,PersistenceRequired:1b,DrownedConversionTime:1}]}`)
			chicken := srv.MobIDsOfKind("minecraft:chicken")[0]
			old := srv.MobIDsOfKind("minecraft:zombie")[0]
			if got := srv.MobVehicleID(old); got != chicken {
				t.Fatalf("zombie's vehicle = %d, want %d", got, chicken)
			}
			c.Settle()
			servertest.UntilTicks(t, srv, 5, "the zombie became a drowned", func() bool {
				return len(srv.MobIDsOfKind("minecraft:drowned")) == 1
			})
			next := srv.MobIDsOfKind("minecraft:drowned")[0]
			if bubble {
				assertConvertedPassengers(t, c, chicken, next, srv.MobPassengerIDs)
				if got := srv.MobVehicleID(next); got != chicken {
					t.Errorf("drowned's vehicle = %d, want %d", got, chicken)
				}
			} else {
				// LivingEntity.baseTick dismounts from #dismounts_underwater before
				// conversion, except at bubble columns.
				if got := srv.MobVehicleID(next); got != 0 {
					t.Errorf("drowned in ordinary water still rides %d", got)
				}
				if got := srv.MobPassengerIDs(chicken); len(got) != 0 {
					t.Errorf("chicken kept passengers in ordinary water: %v", got)
				}
			}
		})
	}
}

// vanilla: ConversionType.SINGLE.convert
func TestOutOfTickConversionKeepsMobRides(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		from, into string
		shear      bool
	}{
		{"pig", "zombified_piglin", false},
		{"villager", "witch", false},
		{"mooshroom", "cow", true},
	} {
		t.Run(tc.from, func(t *testing.T) {
			t.Parallel()
			srv, c := wolfScene(t, "Watcher")
			srv.SetDifficulty(2)
			// A three-body stack exercises both the old body's rider and vehicle.
			srv.RunLine(`summon minecraft:spider 2.5 64 0.5 {NoAI:1b,Invulnerable:1b,Tags:[base]}`)
			srv.RunLine(fmt.Sprintf(`summon minecraft:%s 2.5 64 0.5 {NoAI:1b,Tags:[converting]}`, tc.from))
			srv.RunLine(`summon minecraft:skeleton 2.5 64 0.5 {NoAI:1b,Invulnerable:1b,Tags:[rider]}`)
			srv.RunLine(`ride @e[tag=converting,limit=1] mount @e[tag=base,limit=1]`)
			srv.RunLine(`ride @e[tag=rider,limit=1] mount @e[tag=converting,limit=1]`)
			old := srv.MobIDsOfKind("minecraft:" + tc.from)[0]
			base := srv.MobVehicleID(old)
			riders := srv.MobPassengerIDs(old)
			if base == 0 || len(riders) != 1 {
				t.Fatalf("initial stack: vehicle %d, riders %v", base, riders)
			}
			c.Settle()
			if tc.shear {
				giveHand(t, srv, "Watcher", "minecraft:shears", 1)
				c.UseOn(old)
			} else {
				srv.RunLine(`execute at @e[tag=converting,limit=1] run summon minecraft:lightning_bolt ~ ~ ~`)
				servertest.RunTicks(t, srv, 1)
			}
			seats := make(map[int32][]int32)
			c.SettleAndCollect(func(id int32, data []byte) {
				if id != protocol.IDPlaySetPassengers {
					return
				}
				vehicle, ids := servertest.ReadPassengers(c, data)
				seats[vehicle] = ids
			})
			converted := srv.MobIDsOfKind("minecraft:" + tc.into)
			if len(converted) != 1 {
				t.Fatalf("converted mobs = %v, want one %s", converted, tc.into)
			}
			next := converted[0]
			if got := srv.MobVehicleID(riders[0]); got != next {
				t.Errorf("skeleton's vehicle = %d, want converted mob %d (old %d)", got, next, old)
			}
			if got := srv.MobVehicleID(next); got != base {
				t.Errorf("converted mob's vehicle = %d, want %d", got, base)
			}
			if !slices.Equal(seats[next], riders) {
				t.Errorf("Set Passengers for converted mob = %v, want %v", seats[next], riders)
			}
			if !slices.Equal(seats[base], []int32{next}) {
				t.Errorf("Set Passengers for base = %v, want [%d]", seats[base], next)
			}
		})
	}
}
