package entity

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/google/uuid"
)

// The jockey pair as ordinary unit tests: the rolls a finalize hands back and
// the seat, the despawn and the tick the collection runs on a body riding
// another. The black-box tests in internal/server/server_jockey_test.go remain the
// proof of the whole pipeline over the wire.

// TestOnlyTheSpiderItselfRollsItsJockey holds the roll to vanilla's own: the
// cave spider's finalize overrides the spider's without calling it, so a cave
// spider never takes a rider, where a spider lands about one in a hundred.
func TestOnlyTheSpiderItselfRollsItsJockey(t *testing.T) {
	cave := 0
	for i := range 2400 {
		m := NewMob(TypeCaveSpider, int32(i+1), 0, 64, 0, int64(i+1))
		m.rollJockey(nil)
		if m.Jockey != JockeyNone {
			cave++
		}
	}
	if cave != 0 {
		t.Errorf("%d of 2400 cave spiders rolled a jockey, want none: the cave spider's finalize never reaches the spider's roll", cave)
	}

	spiders := 0
	for i := range 2400 {
		m := NewMob(TypeSpider, int32(i+1), 0, 64, 0, int64(i+1))
		m.rollJockey(nil)
		if m.Jockey == JockeyRider {
			spiders++
			if m.RiderKind != TypeSkeleton {
				t.Errorf("the spider's rider kind is %s, want a skeleton", m.RiderKind)
			}
		}
	}
	if spiders < 8 || spiders > 42 {
		t.Errorf("%d of 2400 spiders rolled a skeleton jockey, want the one-in-a-hundred's share (8..42)", spiders)
	}
}

// vanilla: Entity.push
func TestOccupiedMobsIgnoreCollisionImpulses(t *testing.T) {
	for _, passenger := range []string{"mob", "player", "extra-player"} {
		for _, mountFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/mount-first=%v", passenger, mountFirst), func(t *testing.T) {
				w := newTestWorld()
				mount := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
				neighbor := NewMob(TypeCow, 2, 0.9, 64, 0.5, 2)
				switch passenger {
				case "mob":
					Ride(mount, NewMob(TypeSkeleton, 3, 0.5, 64, 0.5, 3))
				case "player":
					mount.Horse.Rider, mount.Horse.RiderID = uuid.New(), 3
				case "extra-player":
					mount.Horse.ExtraRiderIDs = []int32{3}
				}
				mount.VX, mount.VZ = 0.2, 0.4
				pair := []*Mob{neighbor, mount}
				if mountFirst {
					pair[0], pair[1] = pair[1], pair[0]
				}
				for _, m := range pair {
					m.pushStep(w, nil, pair, nil)
				}
				if mount.VX != 0.2 || mount.VZ != 0.4 || mount.Impulse {
					t.Errorf("occupied mob motion = (%g, %g), impulse=%v; collision changed its motion", mount.VX, mount.VZ, mount.Impulse)
				}
				px, _ := PushApart(mount.X, mount.Z, neighbor.X, neighbor.Z)
				if neighbor.VX != 2*px || !neighbor.Impulse {
					t.Errorf("neighbor motion = %g, impulse=%v; want both push passes' %g", neighbor.VX, neighbor.Impulse, 2*px)
				}
			})
		}
	}
}

// TestASpiderJockeyWalksToItsTarget is the rider steering a mount whose own
// goals are off: a skeleton on a spider walks the spider at a player inside
// its bow's range, closing the distance before the strafe takes over.
func TestASpiderJockeyWalksToItsTarget(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-4); x <= 24; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 63, z, stone)
		}
	}
	spider := NewMob(TypeSpider, 1, 0.5, 64, 0.5, 3)
	skeleton := NewMob(TypeSkeleton, 2, 0.5, 64, 0.5, 4)
	skeleton.FinalizeSpawn(GoDice{skeleton.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
	Ride(spider, skeleton)
	players := []Player{{X: 12.5, Y: 64, Z: 0.5, EntityID: 7, Huntable: true}}

	start := math.Hypot(players[0].X-spider.X, players[0].Z-spider.Z)
	pair := Mobs{spider, skeleton}
	for range 30 {
		pair.Tick(w, players, nil)
	}
	closed := start - math.Hypot(players[0].X-spider.X, players[0].Z-spider.Z)
	if closed < 1.0 {
		t.Errorf("the spider carried its rider %g nearer the player, want the skeleton's hunt to walk it in", closed)
	}
	if skeleton.Vehicle != spider || len(spider.Passengers) != 1 {
		t.Error("the pair came apart while walking to the player")
	}
}

// TestARideEndsWhenEitherBodyDies runs the tick that reports a death: a dead
// mount puts its passenger down and a dead passenger leaves its mount's back,
// the seat ending with the body.
// vanilla: Entity.remove
func TestARideEndsWhenEitherBodyDies(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)

	spider := NewMob(TypeSpider, 1, 0.5, 64, 0.5, 3)
	skeleton := NewMob(TypeSkeleton, 2, 0.5, 64, 0.5, 4)
	Ride(spider, skeleton)
	spider.Kill()
	pair := Mobs{spider, skeleton}
	pair.Tick(w, nil, nil)
	if skeleton.Vehicle != nil || skeleton.RidesVehicle || len(spider.Passengers) != 0 {
		t.Error("the rider stayed seated on a mount that died")
	}

	chicken := NewMob(TypeChicken, 3, 0.5, 64, 0.5, 5)
	zombie := NewMob(TypeZombie, 4, 0.5, 64, 0.5, 6)
	zombie.SetBaby(true)
	Ride(chicken, zombie)
	zombie.Kill()
	pair = Mobs{chicken, zombie}
	pair.Tick(w, nil, nil)
	if len(chicken.Passengers) != 0 {
		t.Error("a mount kept a rider that died")
	}
}
