package commands

import (
	"fmt"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// vanilla: SummonCommand.createEntity, EntityType.loadPassengersRecursive
func TestSummonedVehiclesLoadNestedMobPassengers(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"oak_boat", "minecart"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
			servertest.HoldTicks(t, srv)
			c.Settle()
			c.ChatCommand(fmt.Sprintf(`summon %s 3.5 64 3.5 {NoGravity:1b,Passengers:[{id:"minecraft:pig",NoAI:1b,Pos:[1000d,200d,1000d],Passengers:[{id:"minecraft:chicken",NoAI:1b}]}]}`, kind))
			spawned := make(map[int32]bool)
			byType := make(map[int32]int32)
			seats := make(map[int32][]int32)
			c.SettleAndCollect(func(id int32, data []byte) {
				switch id {
				case protocol.IDPlaySpawnEntity:
					r := protocol.NewReader(data)
					body := r.ReadVarInt()
					r.ReadUUID()
					typ := r.ReadVarInt()
					spawned[body], byType[typ] = true, body
					x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
					if x != 3.5 || y != 64 || z != 3.5 {
						t.Errorf("type %d spawned at (%g, %g, %g), want command position", typ, x, y, z)
					}
				case protocol.IDPlaySetPassengers:
					vehicle, riders := servertest.ReadPassengers(c, data)
					if !spawned[vehicle] {
						t.Errorf("Set Passengers preceded vehicle %d's spawn", vehicle)
					}
					for _, rider := range riders {
						if !spawned[rider] {
							t.Errorf("Set Passengers preceded rider %d's spawn", rider)
						}
					}
					seats[vehicle] = riders
				}
			})
			vehicleType, _ := registry.EntityTypeID("minecraft:" + kind)
			vehicle, pig, chicken := byType[vehicleType], byType[protocol.EntityTypePig], byType[protocol.EntityTypeChicken]
			if len(spawned) != 3 || vehicle == 0 || pig == 0 || chicken == 0 {
				t.Fatalf("spawned types = %v, want vehicle, pig and chicken", byType)
			}
			passengers := srv.BoatPassengerIDs
			if kind == "minecart" {
				passengers = srv.MinecartPassengerIDs
			}
			if got := passengers(vehicle); !slices.Equal(got, []int32{pig}) {
				t.Errorf("server vehicle passengers = %v, want [%d]", got, pig)
			}
			if got := srv.MobPassengerIDs(pig); !slices.Equal(got, []int32{chicken}) {
				t.Errorf("server pig passengers = %v, want [%d]", got, chicken)
			}
			if got := srv.MobVehicleID(chicken); got != pig {
				t.Errorf("chicken's vehicle = %d, want %d", got, pig)
			}
			for mount, want := range map[int32]int32{vehicle: pig, pig: chicken} {
				if !slices.Equal(seats[mount], []int32{want}) {
					t.Errorf("wire passengers of %d = %v, want [%d]", mount, seats[mount], want)
				}
			}
			// Dismount reads the pig's reverse link to the boat or cart.
			c.ChatCommand(`ride @e[type=pig,limit=1] dismount`)
			c.Settle()
			if got := passengers(vehicle); len(got) != 0 {
				t.Errorf("vehicle passengers after dismount = %v", got)
			}
		})
	}
}

// vanilla: SummonCommand.createEntity, ServerLevel.tryAddFreshEntityWithPassengers
func TestSummonedVehiclesRefuseALivePassengerUUID(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"oak_boat", "minecart"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand(`summon pig 3.5 64 3.5 {UUID:[I;1,2,3,4],NoAI:1b}`)
			c.Settle()
			c.ChatCommand(fmt.Sprintf(`summon %s 3.5 64 3.5 {Passengers:[{id:"minecraft:pig",UUID:[I;1,2,3,4]}]}`, kind))
			servertest.AwaitCommandError(t, c, "commands.summon.failed.uuid")
			if mobs, boats, carts := srv.MobCount(), srv.BoatCount(), len(srv.MinecartsInDimension(world.DimensionName)); mobs != 1 || boats != 0 || carts != 0 {
				t.Errorf("after refused summon: %d mobs, %d boats, %d carts; want only the original pig", mobs, boats, carts)
			}
		})
	}
}
