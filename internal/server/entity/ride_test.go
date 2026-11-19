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

// TestAChickenJockeyHenAnswersTheDespawnRule is Chicken.removeWhenFarAway: a
// plain hen is an animal the creature category never despawns, while a hen a
// baby zombie took as a jockey leaves like a monster.
func TestAChickenJockeyHenAnswersTheDespawnRule(t *testing.T) {
	plain := NewMob(TypeChicken, 1, 0, 64, 0, 1)
	if plain.removeWhenFarAway() {
		t.Error("a plain hen answers removeWhenFarAway, want the animal's no")
	}
	jockey := NewMob(TypeChicken, 2, 0, 64, 0, 2)
	jockey.ChickenJockey = true
	if !jockey.removeWhenFarAway() {
		t.Error("a jockey hen does not answer removeWhenFarAway, want the monster's yes")
	}
}

// TestAJockeyRiderTicksInItsSeat runs one collection tick over a spider and
// its skeleton: the rider's own tick runs — its age advances — while the seat
// the mount placed it in holds.
func TestAJockeyRiderTicksInItsSeat(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	spider := NewMob(TypeSpider, 1, 0.5, 64, 0.5, 3)
	skeleton := NewMob(TypeSkeleton, 2, 0.5, 64, 0.5, 4)
	Ride(spider, skeleton)

	age := skeleton.Age
	mobs := Mobs{spider, skeleton}
	mobs.Tick(w, nil, nil)

	if skeleton.Age != age+1 {
		t.Errorf("the rider's age went %d to %d over one tick, want the tick a seated body still runs", age, skeleton.Age)
	}
	if skeleton.Vehicle != spider || len(spider.Passengers) != 1 || spider.Passengers[0] != skeleton {
		t.Error("the tick dropped the seat, want the pair still linked")
	}
	if skeleton.Y <= spider.Y {
		t.Errorf("the rider rides at y=%g, want a seat above the mount's y=%g", skeleton.Y, spider.Y)
	}
}

// vanilla: Entity.rideTick, LivingEntity.pushEntities
func TestABoatRiderTicksAndPushesWithoutLeavingItsSeat(t *testing.T) {
	for _, noAI := range []bool{false, true} {
		w := newTestWorld()
		floor64(w, t)
		pig := NewMob(TypePig, 1, 0.5, 64, 0.5, 3)
		pig.RidesVehicle, pig.NoAI = true, noAI
		pig.VX, pig.VY, pig.VZ = 4, 4, 4
		cow := NewMob(TypeCow, 2, 0.9, 64, 0.5, 4)
		cow.NoAI = true
		mobs := Mobs{cow, pig}
		mobs.Tick(w, nil, nil)
		if pig.Age != 1 || pig.X != 0.5 || pig.Y != 64 || pig.Z != 0.5 {
			t.Errorf("NoAI=%v: rider age=%d at (%g, %g, %g), want age 1 in its seat", noAI, pig.Age, pig.X, pig.Y, pig.Z)
		}
		px, _ := PushApart(pig.X, pig.Z, cow.X, cow.Z)
		if math.Abs(cow.VX-2*px) > 1e-9 {
			t.Errorf("NoAI=%v: cow velocity=%g, want both push passes' %g", noAI, cow.VX, 2*px)
		}
		if pig.VX >= 0 || pig.VX < -1 || pig.VY > 0 || pig.VZ != 0 {
			t.Errorf("NoAI=%v: rider velocity=(%g, %g, %g), want old motion cleared before travel and pushing", noAI, pig.VX, pig.VY, pig.VZ)
		}
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

func TestARiderTicksWhenItsVehicleIsOutsideTheHerd(t *testing.T) {
	for _, order := range []string{"rider", "rider-first", "passenger-first"} {
		t.Run(order, func(t *testing.T) {
			w := newTestWorld()
			pig := NewMob(TypePig, 1, 0.5, 64, 0.5, 3)
			skeleton := NewMob(TypeSkeleton, 2, 0.5, 64, 0.5, 4)
			pig.NoAI, skeleton.NoAI = true, true
			Ride(pig, skeleton)
			mobs := Mobs{skeleton}
			if order != "rider" {
				zombie := NewMob(TypeZombie, 3, 0.5, 64, 0.5, 5)
				zombie.NoAI = true
				Ride(skeleton, zombie)
				if order == "rider-first" {
					mobs = Mobs{skeleton, zombie}
				} else {
					mobs = Mobs{zombie, skeleton}
				}
			}
			want := append(Mobs(nil), mobs...)
			for tick := 1; tick <= 2; tick++ {
				mobs.Tick(w, nil, nil)
				if len(mobs) != len(want) {
					t.Fatalf("tick %d: herd has %d bodies, want %d", tick, len(mobs), len(want))
				}
				for i, m := range want {
					if mobs[i] != m || m.Age != tick {
						t.Errorf("tick %d: body %d is %p at age %d, want %p at age %d", tick, i, mobs[i], m.Age, m, tick)
					}
				}
			}
			if pig.Age != 0 {
				t.Errorf("vehicle outside the herd ticked to age %d", pig.Age)
			}
		})
	}
}

// TestAJockeyRiderStillBurns runs a tick over a burning baby zombie on a hen:
// the baseTick's fire bite lands on the rider, the tick its seat used to skip.
func TestAJockeyRiderStillBurns(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	chicken := NewMob(TypeChicken, 1, 0.5, 64, 0.5, 3)
	zombie := NewMob(TypeZombie, 2, 0.5, 64, 0.5, 4)
	zombie.SetBaby(true)
	zombie.FireTicks = 20
	Ride(chicken, zombie)

	mobs := Mobs{chicken, zombie}
	burned := false
	for _, ev := range mobs.Tick(w, nil, nil) {
		if ev.Kind == MobBurned && ev.Mob == zombie {
			burned = true
		}
	}
	if !burned {
		t.Error("a burning rider's fire never bit, want the tick a seated body still runs")
	}
}

// TestASpiderCarryingARiderDoesNotMelee holds the pair's melee to the rider:
// a lone spider swings at the player in its reach, while the same spider with
// a skeleton aboard leaves the attacking to it.
func TestASpiderCarryingARiderDoesNotMelee(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	players := []Player{survivor(7, 1.1)}

	// swingsOf counts the spider's own blows: a bare skeleton rider melees.
	swingsOf := func(spider *Mob, mobs Mobs) int {
		n := 0
		for range 100 {
			for _, ev := range mobs.Tick(w, players, nil) {
				if ev.Kind == MobAttackedPlayer && ev.Mob == spider {
					n++
				}
			}
		}
		return n
	}

	lone := NewMob(TypeSpider, 1, 0.5, 64, 0.5, 3)
	if swingsOf(lone, Mobs{lone}) == 0 {
		t.Fatal("a lone spider never swung at the player in its reach, want the melee goal's attack")
	}

	spider := NewMob(TypeSpider, 2, 0.5, 64, 0.5, 3)
	skeleton := NewMob(TypeSkeleton, 3, 0.5, 64, 0.5, 4)
	Ride(spider, skeleton)
	if n := swingsOf(spider, Mobs{spider, skeleton}); n != 0 {
		t.Errorf("a ridden spider swung %d times in a hundred ticks, want the pair to leave the melee to the rider", n)
	}
}

// TestAJockeyPairWalksToItsTarget is the rider steering the mount: a baby
// zombie on a hen, and the same zombie alone, walk at a player twelve blocks
// off, and the pair closes the distance on the hen's legs the way the lone
// zombie does on its own.
func TestAJockeyPairWalksToItsTarget(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-4); x <= 24; x++ {
		for z := int32(-4); z <= 4; z++ {
			w.set(x, 63, z, stone)
		}
	}
	players := []Player{survivor(7, 12.5)}

	chicken := NewMob(TypeChicken, 1, 0.5, 64, 0.5, 3)
	zombie := NewMob(TypeZombie, 2, 0.5, 64, 0.5, 4)
	zombie.SetBaby(true)
	Ride(chicken, zombie)

	pair := Mobs{chicken, zombie}
	for range 200 {
		pair.Tick(w, players, nil)
	}
	if moved := chicken.X - 0.5; moved < 6 {
		t.Errorf("the hen carried its rider %g east in 200 ticks, want it walking to the player at x=12.5", moved)
	}
	if zombie.Vehicle != chicken || len(chicken.Passengers) != 1 {
		t.Error("the pair came apart while walking to the player")
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

// TestASpiderJockeyTurnsItsMountWhileStrafing checks the strafe's two lookAt
// calls: vanilla turns the controlled vehicle before the rider, and the
// strafe is rotated by the mount's yaw, so the mount must come round too or
// the circle's heading never changes.
// vanilla: RangedBowAttackGoal.tick
func TestASpiderJockeyTurnsItsMountWhileStrafing(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	spider := NewMob(TypeSpider, 1, 0.5, 64, 0.5, 3)
	skeleton := NewMob(TypeSkeleton, 2, 0.5, 64, 0.5, 4)
	skeleton.FinalizeSpawn(GoDice{skeleton.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
	Ride(spider, skeleton)
	// The mount facing north, the target due east: the strafe turns the mount
	// first, then the rider, a step each toward the target.
	spider.Yaw, skeleton.Yaw = 0, 0
	skeleton.TargetID = 7
	players := []Player{survivor(7, 8.5)}
	g := &rangedAttackGoal{m: skeleton, priority: priorityRanged, seeTime: rangedSeeTicks, strafingTime: 0}

	g.Tick(&tickView{w: w, players: players})

	if want := float32(-headTurnSpeed); spider.Yaw != want {
		t.Errorf("the strafing jockey turned its mount to %v, want the target's way by %v to %v", spider.Yaw, headTurnSpeed, want)
	}
	if want := float32(-headTurnSpeed); skeleton.Yaw != want {
		t.Errorf("the strafing jockey turned its own body to %v, want the target's way by %v to %v", skeleton.Yaw, headTurnSpeed, want)
	}
}

// vanilla: RangedBowAttackGoal.tick, Entity.getControlledVehicle
func TestOnlyTheControllingBowRiderTurnsItsMount(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rear    bool
		noAI    bool
		wantYaw float32
	}{
		{"front archer", false, false, -headTurnSpeed},
		{"rear archer", true, false, 0},
		{"NoAI vehicle", false, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			floor64(w, t)
			camel := NewMob(TypeCamelHusk, 1, 0.5, 64, 0.5, 1)
			camel.NoAI = tc.noAI
			archer := NewMob(TypeParched, 2, 0.5, 64, 0.5, 2)
			archer.FinalizeSpawn(GoDice{archer.Rand}, testBiome("minecraft:desert"), nil, rules.LocalDifficulty{})
			if tc.rear {
				Ride(camel, NewMob(TypeHusk, 3, 0.5, 64, 0.5, 3))
			}
			Ride(camel, archer)
			archer.TargetID = 7
			g := &rangedAttackGoal{m: archer, priority: priorityRanged, seeTime: rangedSeeTicks, strafingTime: 0}

			// Tick only the bow goal to isolate its turn from the front rider's steering.
			g.Tick(&tickView{w: w, players: []Player{survivor(7, 8.5)}})

			if camel.Yaw != tc.wantYaw {
				t.Errorf("mount yaw = %v, want %v", camel.Yaw, tc.wantYaw)
			}
			if archer.Yaw != -headTurnSpeed {
				t.Errorf("archer yaw = %v, want %v", archer.Yaw, -headTurnSpeed)
			}
		})
	}
}

// TestASpiderJockeyStrafesItsMount is the rider's bow strafe reaching the
// mount's legs: inside the bow's range with a held sight for the twenty ticks
// it waits, the skeleton circles the player and the spider carries it off the
// straight line to them, where a strafe written to a walk the mount zeroes
// before it moves would leave the spider standing.
func TestASpiderJockeyStrafesItsMount(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-4); x <= 40; x++ {
		for z := int32(-20); z <= 20; z++ {
			w.set(x, 63, z, stone)
		}
	}
	spider := NewMob(TypeSpider, 1, 0.5, 64, 0.5, 3)
	skeleton := NewMob(TypeSkeleton, 2, 0.5, 64, 0.5, 4)
	skeleton.FinalizeSpawn(GoDice{skeleton.Rand}, testBiome("minecraft:plains"), nil, rules.LocalDifficulty{})
	Ride(spider, skeleton)
	players := []Player{survivor(7, 8.5)}

	startZ := spider.Z
	pair := Mobs{spider, skeleton}
	for range 200 {
		pair.Tick(w, players, nil)
	}
	if moved := math.Abs(spider.Z - startZ); moved < 1.0 {
		t.Errorf("the spider carried its rider %g sideways in 200 ticks, want the skeleton's bow strafe to move it off the line to the player", moved)
	}
	if skeleton.Vehicle != spider || len(spider.Passengers) != 1 {
		t.Error("the pair came apart while strafing the player")
	}
}

// TestASeatedRiderNeverDespawns is Mob.requiresCustomPersistence: a rider on
// a mount's back is spared the despawn even where the player stands past the
// distance that would discard a walking monster at once.
// vanilla: Mob.requiresCustomPersistence
func TestASeatedRiderNeverDespawns(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	far := []Player{survivor(7, 200.5)}

	if !NewMob(TypeSkeleton, 1, 0.5, 64, 0.5, 3).despawnStep(w, far) {
		t.Error("a walking skeleton beyond the despawn distance stands, want it discarded")
	}

	spider := NewMob(TypeSpider, 2, 0.5, 64, 0.5, 3)
	rider := NewMob(TypeSkeleton, 3, 0.5, 64, 0.5, 4)
	Ride(spider, rider)
	if rider.despawnStep(w, far) {
		t.Error("a skeleton seated on its mount was despawned, want the passenger spared")
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

// TestTheJockeySeatIsTheAttachmentDifference holds the seat to vanilla's
// numbers: the mount's passenger point less the rider's own attachment. A
// skeleton rides a spider 0.065 up, a baby zombie rides a hen 0.5125 up and
// a tenth back.
// vanilla: Entity.positionRider, getPassengerAttachmentPoint
func TestTheJockeySeatIsTheAttachmentDifference(t *testing.T) {
	spider := NewMob(TypeSpider, 1, 10, 64, 20, 1)
	skeleton := NewMob(TypeSkeleton, 2, 10, 64, 20, 2)
	Ride(spider, skeleton)
	if !closeEnough(skeleton.Y, spider.Y+0.065) {
		t.Errorf("the skeleton rides a spider %g up, want the 0.765 attachment less its own 0.7", skeleton.Y-spider.Y)
	}
	if !closeEnough(skeleton.X, spider.X) || !closeEnough(skeleton.Z, spider.Z) {
		t.Errorf("the skeleton rides at (%g, %g), want the spider's own (%g, %g)", skeleton.X, skeleton.Z, spider.X, spider.Z)
	}

	chicken := NewMob(TypeChicken, 3, 5, 64, 6, 3)
	zombie := NewMob(TypeZombie, 4, 5, 64, 6, 4)
	zombie.SetBaby(true)
	Ride(chicken, zombie)
	if !closeEnough(zombie.Y, chicken.Y+0.5125) {
		t.Errorf("the baby zombie rides a hen %g up, want the 0.7 attachment less its own 0.1875", zombie.Y-chicken.Y)
	}
	if !closeEnough(zombie.Z, chicken.Z-0.1) {
		t.Errorf("the baby zombie rides at z=%g, want the hen's own z=%g less its tenth", zombie.Z, chicken.Z)
	}
}

// TestTheNearbyHenSearchTakesMarkedHensAndSkipsRidingOnes is
// Zombie.finalizeSpawn's selector: a hen already carrying the jockey mark is
// still a candidate, while one that is itself riding is not.
// vanilla: EntitySelector.ENTITY_NOT_BEING_RIDDEN
func TestTheNearbyHenSearchTakesMarkedHensAndSkipsRidingOnes(t *testing.T) {
	baby := NewMob(TypeZombie, 1, 0, 64, 0, 1)
	baby.SetBaby(true)
	marked := NewMob(TypeChicken, 2, 1, 64, 0, 2)
	marked.ChickenJockey = true
	riding := NewMob(TypeChicken, 3, -1, 64, 0, 3)
	riding.RidesVehicle = true // seated in a boat

	if got := NearbyJockeyChicken(baby, []*Mob{baby, marked, riding}); got != marked {
		t.Errorf("the search took the hen with ID %d, want the marked hen %d the vanilla selector still offers", got.ID, marked.ID)
	}
	if got := NearbyJockeyChicken(baby, []*Mob{baby, riding}); got != nil {
		t.Errorf("the search took the riding hen %d, want none: the only one is riding", got.ID)
	}
}

// TestAJockeyHenRewardsTenExperience is Chicken.getBaseExperienceReward: a hen
// a baby zombie took hands back the jockey's ten, where a plain one rolls the
// animal's one to three.
func TestAJockeyHenRewardsTenExperience(t *testing.T) {
	hen := NewMob(TypeChicken, 1, 0, 64, 0, 7)
	hen.ChickenJockey = true
	if got := hen.RollExperience(rules.LootKiller{}); got != 10 {
		t.Errorf("a jockey hen rewarded %d experience, want the jockey's ten", got)
	}
	plain := NewMob(TypeChicken, 2, 0, 64, 0, 8)
	if got := plain.RollExperience(rules.LootKiller{}); got < 1 || got > 3 {
		t.Errorf("a plain hen rewarded %d experience, want the animal's one to three", got)
	}
}

func TestAPigletsPassengerAttachment(t *testing.T) {
	m := NewMob(TypePig, 1, 0, 64, 0, 1)
	m.SetBaby(true)
	x, y, z := vehiclePassengerPoint(m, 0)
	if x != 0 || y != 0.5 || z != 0 {
		t.Fatalf("piglet seat = (%v, %v, %v), want (0, 0.5, 0)", x, y, z)
	}
}

// vanilla: LivingEntity.dismountVehicle
func TestOffRemovedVehicleUsesBodyDimensionsAndLimits(t *testing.T) {
	w := newTestWorld()
	floor(w, -4, -4, 4, 4, 64, defaultState(t, "minecraft:oak_slab"))
	for _, tc := range []struct {
		name                 string
		width, height, wantY float64
	}{
		{"pig", 0.9, 0.9, 64.5},
		{"limit", 4, 4, 64.5},
		{"too-wide", 4.01, 0.9, 64.25},
		{"too-tall", 0.9, 4.01, 64.25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A rider below the hull must start the search at the hull's feet.
			x, y, z := OffRemovedVehicle(w, 0.5, 64, 0.5, 64.25, tc.width, tc.height)
			if x != 0.5 || math.Abs(y-tc.wantY) > 1e-6 || z != 0.5 {
				t.Errorf("dismount = (%g %g %g), want (0.5 %g 0.5)", x, y, z, tc.wantY)
			}
		})
	}
}

// vanilla: Entity.getControlledVehicle, Mob.getNavigation
func TestOnlyTheControllingRiderSharesNavigation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		first    MobType
		noAI     bool
		controls bool
	}{
		{"front husk", TypeHusk, false, true},
		{"non-controlling rider", TypeSlime, false, false},
		{"NoAI vehicle", TypeHusk, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			camel := NewMob(TypeCamelHusk, 1, 0.5, 64, 0.5, 1)
			front := NewMob(tc.first, 2, 0.5, 64, 0.5, 2)
			rear := NewMob(TypeParched, 3, 0.5, 64, 0.5, 3)
			camel.NoAI = tc.noAI
			Ride(camel, front)
			Ride(camel, rear)
			camel.moveTo([]Waypoint{{X: 8, Y: 64}}, Waypoint{X: 8, Y: 64}, 4)
			rear.stopSeeking()
			if camel.navDone() {
				t.Fatal("rear rider stopped the vehicle's charge")
			}
			front.stopSeeking()
			if got := camel.navDone(); got != tc.controls {
				t.Fatalf("front rider stopped vehicle = %v, want %v", got, tc.controls)
			}
		})
	}
}
