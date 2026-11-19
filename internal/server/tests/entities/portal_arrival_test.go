package entities

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: NetherPortalBlock.createDimensionTransition
func TestABoatPortalArrivalKeepsTheHullsRelativeOffset(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		ridden       bool
		x, y         float64
		wantX, wantY float64
	}{
		{"empty-edge", false, 4.5, 64, 8.6875, 100},
		{"ridden-edge", true, 4.5, 64, 8.6875, 100},
		{"empty-offset", false, 5.25, 64.5, 9.25, 100.5},
		{"ridden-offset", true, 5.25, 64.5, 9.25, 100.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.CrossingTestConfig(t)
			srv, c := servertest.Join(t, cfg, "Rower", servertest.OpsListing(t, "Rower"))
			servertest.HoldTicks(t, srv)
			servertest.PortalFrame(t, srv)
			t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, 0))
			// The same two-wide opening, translated to (8,100,8). Clear
			// the surrounding terrain so only the frame can move the exit.
			for x := int32(6); x <= 11; x++ {
				for y := int32(99); y <= 104; y++ {
					for z := int32(6); z <= 10; z++ {
						state := uint16(0)
						if z == 8 && x >= 7 && x <= 10 && y <= 103 {
							state = rules.ObsidianState
							if x >= 8 && x <= 9 && y >= 100 && y <= 102 {
								state = rules.NetherPortalX
							}
						}
						srv.SetBlockInDimension(world.NetherName, x, y, z, state)
					}
				}
			}
			// x=4.5 is inside the first portal cell, but the 1.375-wide
			// hull needs x=4.6875 to clear its left edge at the exit.
			id := srv.SpawnBoat(tc.x, tc.y, 4.7, "minecraft:oak_boat")
			if id == 0 {
				t.Fatal("spawning the boat failed")
			}
			if tc.ridden {
				c.ChatCommand("ride @s mount @e[type=oak_boat,limit=1]")
				servertest.AwaitPassengersFor(c, id)
				servertest.RunTicks(t, srv, 1)
				if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayRespawn); !ok {
					t.Fatal("the rider never crossed")
				}
			}
			servertest.UntilTicks(t, srv, 5, "the boat crossing", func() bool {
				return srv.BoatCountInDimension(world.NetherName) == 1
			})
			ids := srv.BoatIDs()
			if len(ids) != 1 || srv.BoatCountInDimension(world.DimensionName) != 0 {
				t.Fatalf("boats after crossing = %v, want only the arrival", ids)
			}
			x, y, z, _, _, ok := srv.BoatState(ids[0])
			if !ok || math.Abs(x-tc.wantX) > 1e-6 || math.Abs(y-tc.wantY) > 1e-6 || math.Abs(z-8.5) > 1e-6 {
				t.Errorf("hull arrival = (%v,%v,%v), present %v; want (%v,%v,8.5)", x, y, z, ok, tc.wantX, tc.wantY)
			}
			if tc.ridden {
				if rider, ok := srv.BoatRider(ids[0]); !ok || rider != "Rower" {
					t.Errorf("rider = %q, present %v; want Rower still aboard", rider, ok)
				}
			}
		})
	}
}

// vanilla: LivingEntity.getRelativePortalPosition, AbstractBoat.getRelativePortalPosition, AbstractMinecart.getRelativePortalPosition
func TestMobsAndEmptyVehiclesArriveOnThePortalPlane(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		entryZ, exitZ bool
		across        float64
	}{
		{"x-to-x", false, false, 4.7},
		{"x-to-z", false, true, 4.3},
		{"z-to-x", true, false, 4.3},
		{"z-to-z", true, true, 4.7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.CrossingTestConfig(t)
			srv, c := servertest.Join(t, cfg, "Observer", servertest.OpsListing(t, "Observer"))
			servertest.HoldTicks(t, srv)
			t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 0, 0))
			for _, frame := range []struct {
				dimension   string
				base, floor int32
				axisZ       bool
			}{
				{world.DimensionName, 4, 64, tc.entryZ},
				{world.NetherName, 8, 100, tc.exitZ},
			} {
				for along := int32(-1); along <= 6; along++ {
					for up := int32(-1); up <= 4; up++ {
						for across := int32(-2); across <= 2; across++ {
							state := uint16(0)
							if across == 0 && up <= 3 {
								state = rules.ObsidianState
								if along >= 0 && along < 6 && up >= 0 && up < 3 {
									state = rules.NetherPortalX
									if frame.axisZ {
										state = rules.NetherPortalZ
									}
								}
							}
							x, z := frame.base+along, frame.base+across
							if frame.axisZ {
								x, z = z, x
							}
							srv.SetBlockInDimension(frame.dimension, x, frame.floor+up, z, state)
						}
					}
				}
			}
			for i, kind := range []string{"oak_boat", "minecart", "pig"} {
				x, z := 5.25+2*float64(i), tc.across
				if tc.entryZ {
					x, z = z, x
				}
				c.ChatCommand(fmt.Sprintf("summon %s %g 64.5 %g {NoAI:1b,NoGravity:1b}", kind, x, z))
				c.Settle()
			}
			servertest.UntilTicks(t, srv, 5, "all three portal crossings", func() bool {
				return srv.BoatCountInDimension(world.NetherName) == 1 &&
					len(srv.MinecartsInDimension(world.NetherName)) == 1 &&
					len(srv.MobsInDimension(world.NetherName)) == 1
			})
			x, y, z, _, _, ok := srv.BoatState(srv.BoatIDs()[0])
			if !ok {
				t.Fatal("arrival boat missing")
			}
			cart := srv.MinecartsInDimension(world.NetherName)[0]
			mob := srv.MobsInDimension(world.NetherName)[0]
			for i, got := range [][3]float64{{x, y, z}, {cart.X, cart.Y, cart.Z}, {mob.X, mob.Y, mob.Z}} {
				want := [3]float64{9.25 + 2*float64(i), 100.5, 8.5}
				if tc.exitZ {
					want[0], want[2] = want[2], want[0]
				}
				for axis := range got {
					if math.Abs(got[axis]-want[axis]) > 1e-6 {
						t.Errorf("%s arrival = %v, want %v", []string{"boat", "minecart", "pig"}[i], got, want)
						break
					}
				}
			}
		})
	}
}

// vanilla: NetherPortalBlock.createDimensionTransition, PortalShape.findCollisionFreePosition
func TestAHorsePortalArrivalClearsTheFrameAndKeepsItsHealth(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	srv, c := servertest.Join(t, cfg, "Crosser", servertest.OpsListing(t, "Crosser"))
	c.MoveTo(160.5, 102, 176.5, 0, 0)
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("source view never arrived")
	}
	servertest.HoldTicks(t, srv)
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 25, 22))
	for _, cmd := range []string{
		"fill 140 100 140 180 100 200 stone",
		"fill 157 101 178 162 105 178 obsidian",
		"fill 158 102 178 161 104 178 nether_portal[axis=x]",
		`summon horse 160.5 102 178.5 {Tame:1b,NoAI:1b,equipment:{saddle:{id:"minecraft:saddle",count:1}}}`,
	} {
		c.ChatCommand(cmd)
		c.Settle()
	}
	for x := int32(23); x <= 29; x++ {
		for y := int32(100); y <= 106; y++ {
			for z := int32(20); z <= 24; z++ {
				state := uint16(0)
				if y == 100 {
					state = rules.ObsidianState
				}
				if z == 22 && x >= 24 && x <= 27 && y <= 104 {
					state = rules.ObsidianState
					if x > 24 && x < 27 && y > 100 && y < 104 {
						state = rules.NetherPortalX
					}
				}
				srv.SetBlockInDimension(world.NetherName, x, y, z, state)
			}
		}
	}
	old := srv.MobsInDimension(world.DimensionName)[0]
	health, _ := srv.MobHealth(old.ID)
	c.ChatCommand("ride @s mount @e[type=horse,limit=1]")
	c.Settle()
	var twin server.TestMob
	servertest.UntilTicks(t, srv, 5, "the horse crossing", func() bool {
		for _, m := range srv.MobsInDimension(world.NetherName) {
			if m.UUID == old.UUID {
				twin = m
				return true
			}
		}
		return false
	})
	width, height := entity.KindDims(entity.TypeHorse)
	box := entity.NewAABB(twin.X-width/2, twin.Y, twin.Z-width/2, twin.X+width/2, twin.Y+height, twin.Z+width/2)
	for x := int32(24); x <= 27; x++ {
		for y := int32(100); y <= 104; y++ {
			if x > 24 && x < 27 && y > 100 && y < 104 {
				continue
			}
			if box.Crosses(entity.NewAABB(float64(x), float64(y), 22, float64(x+1), float64(y+1), 23)) {
				t.Errorf("arrival at (%v,%v,%v) overlaps frame block (%d,%d,22)", twin.X, twin.Y, twin.Z, x, y)
			}
		}
	}
	servertest.RunTicks(t, srv, 100)
	if got, ok := srv.MobHealthInDimension(world.NetherName, twin.ID); !ok || got != health {
		t.Errorf("horse health after 100 ticks = %v (present %v), want %v", got, ok, health)
	}
}
