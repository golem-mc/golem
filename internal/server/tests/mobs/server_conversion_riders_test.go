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

func TestInfectedVillagerKeepsMinecartAndRider(t *testing.T) {
	t.Parallel()
	srv, c, old, zombie := infectionYard(t, 3)
	srv.RunLine(`summon minecart 0.5 64 0.5 {Tags:[cart]}`)
	c.SettleKeeping()
	cart := c.AwaitSpawn(int32(registry.EntityTypeMinecart)).EntityID
	srv.RunLine(`ride @e[type=villager,limit=1] mount @e[tag=cart,limit=1]`)
	srv.RunLine(`summon skeleton 0.5 64 0.5 {NoAI:1b}`)
	srv.RunLine(`ride @e[type=skeleton,limit=1] mount @e[type=villager,limit=1]`)
	rider := srv.MobIDsOfKind("minecraft:skeleton")[0]
	c.Settle()
	// Credit the killing blow to a zombie without waiting for it to path to the cart.
	u, _ := srv.MobUUID(zombie)
	srv.RunLine(`damage @e[type=villager,limit=1] 100 minecraft:mob_attack by ` + u)
	servertest.UntilTicks(t, srv, 5, "the villager was infected", func() bool {
		return len(srv.MobIDsOfKind("minecraft:zombie_villager")) == 1
	})
	next := srv.MobIDsOfKind("minecraft:zombie_villager")[0]
	seats := assertConvertedPassengers(t, c, cart, next, srv.MinecartPassengerIDs)
	if !slices.Equal(seats[next], []int32{rider}) {
		t.Errorf("replacement Set Passengers = %v, want [%d]", seats[next], rider)
	}
	if got := srv.MobVehicleID(rider); got != next {
		t.Errorf("rider's vehicle = %d, want %d (old %d)", got, next, old)
	}
	if got := srv.MobPassengerIDs(next); !slices.Equal(got, []int32{rider}) {
		t.Errorf("replacement's riders = %v, want [%d]", got, rider)
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

// vanilla: ConversionType.SINGLE.convert transfers only getFirstPassenger.
func TestShearedMooshroomKeepsOnlyItsFirstPassenger(t *testing.T) {
	t.Parallel()
	for _, playerFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("playerFirst=%t", playerFirst), func(t *testing.T) {
			t.Parallel()
			srv, c := wolfScene(t, "Watcher")
			srv.RunLine(`summon minecraft:mooshroom 1.5 64 0.5 {NoAI:1b,Tags:[converting]}`)
			old := srv.MobIDsOfKind("minecraft:mooshroom")[0]
			for range 2 {
				srv.RunLine(`summon minecraft:skeleton 1.5 64 0.5 {NoAI:1b,Tags:[rider]}`)
			}
			riders := srv.MobIDsOfKind("minecraft:skeleton")
			for _, rider := range riders {
				u, _ := srv.MobUUID(rider)
				srv.RunLine("ride " + u + " mount @e[tag=converting,limit=1]")
			}
			want := riders[0]
			if playerFirst {
				srv.RunLine(`ride Watcher mount @e[tag=converting,limit=1]`)
				want = c.PlayerEntityID
			}
			c.Settle()
			giveHand(t, srv, "Watcher", "minecraft:shears", 1)
			c.UseOn(old)
			c.SettleKeeping()
			ids := srv.MobIDsOfKind("minecraft:cow")
			if len(ids) != 1 {
				t.Fatalf("cows = %v, want one", ids)
			}
			if got := servertest.AwaitPassengersFor(c, ids[0]); !slices.Equal(got, []int32{want}) {
				t.Errorf("cow's passengers = %v, want [%d]", got, want)
			}
			for _, rider := range riders {
				vehicle := int32(0)
				if rider == want {
					vehicle = ids[0]
				}
				if got := srv.MobVehicleID(rider); got != vehicle {
					t.Errorf("rider %d vehicle = %d, want %d", rider, got, vehicle)
				}
			}
			if playerFirst {
				srv.RunLine("ride Watcher dismount")
				if got := srv.MobPassengerIDs(ids[0]); len(got) != 0 {
					t.Errorf("cow's passengers after player dismount = %v, want none", got)
				}
			}
		})
	}
}

func TestTimedConversionKeepsFirstRiderAndVehicle(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ from, into, tag, block string }{
		{"zombie_villager", "villager", "NoAI:1b,ConversionTime:1", "air"},
		{"skeleton", "stray", "StrayConversionTime:1", "powder_snow"},
		{"piglin", "zombified_piglin", "TimeInOverworld:300", "air"},
		{"hoglin", "zoglin", "TimeInOverworld:300", "air"},
		{"tadpole", "frog", "NoAI:1b,Age:23999", "water"},
	} {
		t.Run(tc.from, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
			servertest.HoldTicks(t, srv)
			srv.SetDayTime(18000)
			c.MoveToGround(0.5, 64, 0.5, true)
			srv.RunLine(`fill 2 64 0 4 67 2 ` + tc.block)
			srv.RunLine(`summon cow 3.5 64 1.5 {NoAI:1b,Invulnerable:1b,Tags:[base]}`)
			srv.RunLine(fmt.Sprintf(`summon %s 3.5 64 1.5 {Tags:[converting],PersistenceRequired:1b,%s}`, tc.from, tc.tag))
			srv.RunLine(`ride @e[tag=converting,limit=1] mount @e[tag=base,limit=1]`)
			for range 2 {
				srv.RunLine(`summon silverfish 3.5 64 1.5 {NoAI:1b}`)
			}
			riders := srv.MobIDsOfKind("minecraft:silverfish")
			for _, id := range riders {
				u, _ := srv.MobUUID(id)
				srv.RunLine(`ride ` + u + ` mount @e[tag=converting,limit=1]`)
			}
			base := srv.MobIDsOfKind("minecraft:cow")[0]
			if tc.from == "skeleton" {
				// Skeleton.tick reads the previous tick's powder-snow contact.
				servertest.RunTicks(t, srv, 1)
				srv.RunLine(`data merge entity @e[tag=converting,limit=1] {StrayConversionTime:1}`)
			}
			c.Settle()
			servertest.UntilTicks(t, srv, 5, "the body converted", func() bool {
				return len(srv.MobIDsOfKind("minecraft:"+tc.into)) == 1
			})
			next := srv.MobIDsOfKind("minecraft:" + tc.into)[0]
			seats := assertConvertedPassengers(t, c, base, next, srv.MobPassengerIDs)
			if !slices.Equal(seats[next], riders[:1]) {
				t.Errorf("replacement Set Passengers = %v, want %v", seats[next], riders[:1])
			}
			if got := srv.MobVehicleID(next); got != base {
				t.Errorf("replacement vehicle = %d, want %d", got, base)
			}
			if got := srv.MobPassengerIDs(next); !slices.Equal(got, riders[:1]) {
				t.Errorf("replacement riders = %v, want %v", got, riders[:1])
			}
			if got := srv.MobVehicleID(riders[1]); got != 0 {
				t.Errorf("second rider still rides %d", got)
			}
		})
	}
}

func TestCuredZombieVillagerKeepsFirstPlayerRider(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.RunLine(`summon zombie_villager 2.5 64 0.5 {NoAI:1b,ConversionTime:1,Tags:[converting]}`)
	srv.RunLine(`ride Watcher mount @e[tag=converting,limit=1]`)
	srv.RunLine(`summon silverfish 2.5 64 0.5 {NoAI:1b}`)
	srv.RunLine(`ride @e[type=silverfish,limit=1] mount @e[tag=converting,limit=1]`)
	c.Settle()
	servertest.UntilTicks(t, srv, 5, "the zombie villager cured", func() bool {
		return len(srv.MobIDsOfKind("minecraft:villager")) == 1
	})
	next := srv.MobIDsOfKind("minecraft:villager")[0]
	c.SettleKeeping()
	if got := servertest.AwaitPassengersFor(c, next); !slices.Equal(got, []int32{c.PlayerEntityID}) {
		t.Errorf("replacement's passengers = %v, want player %d", got, c.PlayerEntityID)
	}
	if got := srv.RiddenMobIDs(); !slices.Equal(got, []int32{next}) {
		t.Errorf("player's mount = %v, want [%d]", got, next)
	}
	rider := srv.MobIDsOfKind("minecraft:silverfish")[0]
	if got := srv.MobVehicleID(rider); got != 0 {
		t.Errorf("second passenger still rides %d", got)
	}
}

func TestOutOfTickConversionKeepsMinecart(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ from, into string }{
		{"pig", "zombified_piglin"},
		{"villager", "witch"},
		{"mooshroom", "cow"},
	} {
		t.Run(tc.from, func(t *testing.T) {
			t.Parallel()
			srv, c := wolfScene(t, "Watcher")
			srv.SetDifficulty(2)
			srv.RunLine(`summon minecart 2.5 64 0.5 {Invulnerable:1b,Tags:[cart]}`)
			c.SettleKeeping()
			cart := c.AwaitSpawn(int32(registry.EntityTypeMinecart)).EntityID
			srv.RunLine(fmt.Sprintf(`summon %s 2.5 64 0.5 {NoAI:1b,Tags:[converting]}`, tc.from))
			srv.RunLine(`ride @e[tag=converting,limit=1] mount @e[tag=cart,limit=1]`)
			c.Settle()
			if tc.from == "mooshroom" {
				giveHand(t, srv, "Watcher", "minecraft:shears", 1)
				c.UseOn(srv.MobIDsOfKind("minecraft:mooshroom")[0])
			} else {
				srv.RunLine(`execute at @e[tag=converting,limit=1] run summon lightning_bolt ~ ~ ~`)
				servertest.RunTicks(t, srv, 1)
			}
			c.SettleKeeping()
			next := srv.MobIDsOfKind("minecraft:" + tc.into)
			if len(next) != 1 {
				t.Fatalf("replacement = %v, want one %s", next, tc.into)
			}
			assertConvertedPassengers(t, c, cart, next[0], srv.MinecartPassengerIDs)
		})
	}
}
