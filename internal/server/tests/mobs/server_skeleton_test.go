package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/google/uuid"

	"bytes"
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// The skeleton and the bow, driven the way the hostile tests drive
// everything else — through a real connection, over the wire — with the
// world the flat one the harness grows. The skeleton stands off and
// shoots, the bow puts an arrow into the world and the arrow flies, hits,
// sticks and is picked up by the rules of its own pickup.

// skeletonSkip is the background a client hears while an arrow or a
// skeleton is being watched: the world's own traffic, the movement packets
// everything streams, and the motion packets an arrow in flight sends a
// tick at a time.
func skeletonSkip(id int32) bool {
	switch id {
	case protocol.IDPlayUpdateTime,
		protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
		protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
		protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk,
		protocol.IDPlayUnloadChunk, protocol.IDPlayWorldEvent,
		protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
		protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
		protocol.IDPlayTeleportEntity, protocol.IDPlaySetEntityMotion,
		protocol.IDPlayEntityAnimation, protocol.IDPlayEntityEquipment,
		protocol.IDPlayContainerSlot, protocol.IDPlayPingResponse,
		protocol.IDPlayPlayerInfoUpdate, protocol.IDPlaySpawnEntity,
		protocol.IDPlayEntityMetadata, protocol.IDPlayRemoveEntities,
		protocol.IDPlaySoundEffect:
		return true
	}
	return false
}

// expectArrowMetadata waits for the arrow's own Set Entity Metadata packet
// — the one a critical shot brings — and reports what it carried.
func expectArrowMetadata(c *servertest.Client, srv *server.Server, a servertest.SpawnedArrow) servertest.ArrowMetadata {
	c.T.Helper()
	m := waitForArrowMetadata(c, srv, a, 500)
	if m == nil {
		c.T.Fatal("the arrow's metadata never arrived")
	}
	return *m
}

// waitForArrowMetadata listens for the arrow's own Set Entity Metadata
// packet for the ticks given (none: the packets already sent) and reports it,
// or nil where none came — which is what a plain arrow in flight is supposed
// to bring.
func waitForArrowMetadata(c *servertest.Client, srv *server.Server, a servertest.SpawnedArrow, ticks int) *servertest.ArrowMetadata {
	c.T.Helper()
	var found *servertest.ArrowMetadata
	c.ReadTicks(srv, ticks, func(gotID int32, data []byte) bool {
		if gotID == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == a.EntityID {
			m := servertest.ReadArrowMetadata(c, data)
			found = &m
			return true
		}
		return false
	})
	return found
}

// TestADrawnBowFiresAnArrow draws a bow for the better part of a second and
// lets it go: the arrow of the arrow type comes into the world owned by the
// shooter — the spawn's data field naming them — with an arrow gone from
// the quiver to pay for it.
func TestADrawnBowFiresAnArrow(t *testing.T) {
	t.Parallel()
	srv, c, playerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Archer")
	servertest.GiveBowAndArrows(srv, "Archer")

	// A level shot east, held for the better part of the second a full
	// draw takes — not the whole of it, so the shot is not the critical
	// one. The draw is counted out in hand-driven ticks: a running loop
	// could pass the full draw's twenty between a poll and the release.
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	srv.StopTick()
	servertest.DrawBow(c, -90, 0)
	c.Settle()
	for i := 0; srv.BowTicks("Archer") < 15; i++ {
		if i == 15 {
			t.Fatalf("fifteen ticks drew the bow %d ticks, want 15", srv.BowTicks("Archer"))
		}
		srv.TickSurvival()
	}
	servertest.ReleaseBow(c)
	// The loop resumes only once the release has spent the draw.
	c.SettleKeeping()
	if left := srv.BowTicks("Archer"); left != 0 {
		t.Fatalf("the release left the bow drawn %d ticks, want it spent", left)
	}
	srv.StartTick()

	arrow := servertest.ExpectArrowSpawn(srv, c)
	if arrow.OwnerID != playerID {
		t.Errorf("the arrow's data field names entity %d, want the shooter's %d", arrow.OwnerID, playerID)
	}
	// The velocity: a draw's power carries its shot at three times itself,
	// and a level look's shot flies flat — the walking convention's east
	// being the yaw the arrow carries mirrored.
	speed := arrow.Vx*arrow.Vx + arrow.Vy*arrow.Vy + arrow.Vz*arrow.Vz
	if speed < 1 || speed > 16 {
		t.Errorf("the arrow flies at %v blocks a tick squared, want the one to sixteen of a draw's power", speed)
	}
	if arrow.Vy > 0.5 || arrow.Vy < -0.5 {
		t.Errorf("a level shot's rise = %v, want the flat of a level look", arrow.Vy)
	}
	// The birth: at the shooter's eye less its tenth, over their feet.
	if d := arrow.Y - (64 + 1.62 - 0.1); d < -0.2 || d > 0.2 {
		t.Errorf("the arrow was born at y=%v, want the shooter's eye less its tenth", arrow.Y)
	}
	// A plain arrow in flight carries no metadata of its own — the fields
	// it would carry are the defaults the client assumes, and a vanilla
	// spawn sends no packet for one. What metadata the arrow brings at all
	// is its landing's in-ground turn, which never carries the critical
	// bit a short draw did not earn.
	if metadata := waitForArrowMetadata(c, srv, arrow, 0); metadata != nil && metadata.Crit {
		t.Errorf("a shot short of the full draw came out critical: %#v", *metadata)
	}
	// The quiver paid for it.
	if left := srv.InventorySlot("Archer", 37); left.Count != 4 {
		t.Errorf("the quiver holds %d arrows after the shot, want the four one shot left", left.Count)
	}
}

// TestASneakingArcherFiresUnderTheCrouchingEyes checks an arrow's spawn
// height against the pose: a crouching archer's shot leaves from the pose's
// own eye height, a block and a quarter up, not from the standing eyes.
// vanilla: Projectile.getEyeY
func TestASneakingArcherFiresUnderTheCrouchingEyes(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Archer")
	servertest.GiveBowAndArrows(srv, "Archer")

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	c.SendSneak(true)
	if got := c.ExpectMetadata(); got.Pose != protocol.PoseSneaking {
		t.Fatalf("pose = %d, want %d (sneaking)", got.Pose, protocol.PoseSneaking)
	}

	// The draw counted out in hand-driven ticks, as the standing shot's.
	srv.StopTick()
	servertest.DrawBow(c, -90, 0)
	c.Settle()
	for i := 0; srv.BowTicks("Archer") < 15; i++ {
		if i == 15 {
			t.Fatalf("fifteen ticks drew the bow %d ticks, want 15", srv.BowTicks("Archer"))
		}
		srv.TickSurvival()
	}
	servertest.ReleaseBow(c)
	c.SettleKeeping()
	if left := srv.BowTicks("Archer"); left != 0 {
		t.Fatalf("the release left the bow drawn %d ticks, want it spent", left)
	}
	srv.StartTick()

	arrow := servertest.ExpectArrowSpawn(srv, c)
	// The crouching body's eyes sit 1.27 up, the vanilla POSES entry.
	if want := 64 + 1.27 - 0.1; math.Abs(arrow.Y-want) > 0.05 {
		t.Errorf("the arrow was born at y=%v, want %v under the crouching eyes", arrow.Y, want)
	}
}

// TestAFullyDrawnArrowHitsAMobInFront draws the bow the whole way, aims at
// a cow standing four blocks off, and lets go: the arrow reaches the cow
// and the hurt lands — the health the metadata carries dropping, or the cow
// dying outright where the roll came out the bigger — the damage of a full
// draw being the six to ten of the vanilla arithmetic either way.
func TestAFullyDrawnArrowHitsAMobInFront(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Archer")
	servertest.GiveBowAndArrows(srv, "Archer")

	cowID := srv.SpawnMob("minecraft:cow", 10.5, 64, 0.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if cow.EntityID != cowID {
		t.Fatalf("spawned cow's entity ID = %d, want the one SpawnMob reported (%d)", cow.EntityID, cowID)
	}

	// The draw the whole way, held through the tick loop's real pace. The
	// look dips into the cow: the shot leaves a block and a half over the
	// ground and a pitch's positive half looks down, the twelfth of a
	// circle carrying the arrow onto the body four blocks off.
	c.MoveTo(0.5, 64, 0.5, -90, 12)
	servertest.DrawBow(c, -90, 12)
	servertest.HoldDraw(t, srv, "Archer", 24)
	// The cow is stood at the aim point the moment before the shot, so
	// the stroll its own mind wanders on cannot walk it out of the way of
	// an arrow that takes a tick and a half to cross the distance.
	srv.MoveMob(cowID, 4.5, 0.5)
	servertest.ReleaseBow(c)

	hurt := false
	for gotID, data := range c.Stream() {
		switch gotID {
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if id := r.ReadVarInt(); id != cowID {
				continue
			}
			for r.Err() == nil {
				index := r.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				if fieldType := r.ReadVarInt(); index == protocol.MetadataIndexLivingHealth && fieldType == protocol.MetadataFloat {
					if health := r.ReadF32(); health < 10 {
						hurt = true
					}
				}
			}
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() == cowID && r.ReadU8() == protocol.EntityStatusDeath {
				hurt = true
			}
		}
		if hurt {
			break
		}
	}
	if !hurt {
		t.Fatal("a full draw at a cow four blocks off never hurt or killed it")
	}
}

// TestAnArrowThatHurtsAMobIsGone draws the bow the whole way at a cow on
// the line and lets go: the hurt lands, and the arrow of the blow is taken
// out of the world — vanilla onHitEntity's discard of a shaft that dealt
// its damage — while the flight's motion packets never carry it back the
// way it came, the reflect of a tenth's return being the answer only to a
// hurt that dealt nothing.
func TestAnArrowThatHurtsAMobIsGone(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Archer")
	servertest.GiveBowAndArrows(srv, "Archer")

	cowID := srv.SpawnMob("minecraft:cow", 10.5, 64, 0.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if cow.EntityID != cowID {
		t.Fatalf("spawned cow's entity ID = %d, want the one SpawnMob reported (%d)", cow.EntityID, cowID)
	}

	// The draw the whole way, held through the tick loop's real pace, the
	// look dipped into the body the shot is to meet.
	c.MoveTo(0.5, 64, 0.5, -90, 12)
	servertest.DrawBow(c, -90, 12)
	servertest.HoldDraw(t, srv, "Archer", 24)
	srv.MoveMob(cowID, 4.5, 0.5)
	servertest.ReleaseBow(c)

	arrow := servertest.ExpectArrowSpawn(srv, c)

	var (
		hurt     bool
		removed  bool
		flewBack bool
	)
	for gotID, data := range c.Stream() {
		switch gotID {
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if id := r.ReadVarInt(); id != cowID {
				continue
			}
			for r.Err() == nil {
				index := r.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				if fieldType := r.ReadVarInt(); index == protocol.MetadataIndexLivingHealth && fieldType == protocol.MetadataFloat {
					if health := r.ReadF32(); health < 10 {
						hurt = true
					}
				}
			}
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() == cowID && r.ReadU8() == protocol.EntityStatusDeath {
				hurt = true
			}
		case protocol.IDPlaySetEntityMotion:
			// The flight runs east and nothing of it slows: a motion
			// packet whose easting has turned about is the arrow flying
			// back.
			r := protocol.NewReader(data)
			if r.ReadVarInt() == arrow.EntityID {
				if vx, _, _ := r.ReadLpVec3(); vx < 0 {
					flewBack = true
				}
			}
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				if r.ReadVarInt() == arrow.EntityID {
					removed = true
				}
			}
		}
		if hurt && removed {
			break
		}
	}
	if !hurt {
		t.Fatal("a full draw at a cow four blocks off never hurt or killed it")
	}
	if !removed {
		t.Fatal("the arrow that hurt the cow was never taken out of the world")
	}
	if flewBack {
		t.Error("the arrow's motion carried it back the way it came")
	}
}

// TestAShotRidesItsShooter fires five full draws east from the same spot:
// one standing, one released right after a reported step east, one after a
// reported rise with the feet on the ground, one after the same rise off
// it, and one after a reported step across the aim. The shot rides the
// shooter's own movement, vanilla Projectile.shootFromRotation adding
// getKnownMovement to the arrow's velocity — so the running shot leaves by
// the step's three the faster, the grounded rise rides nothing, and the
// airborne one rides the rise. The heading the spawn tells stays the bow
// velocity's own: vanilla Projectile.shoot sets yRot and xRot from the
// movement it was given, before the ride joins, so the strafing shot still
// spawns facing east and the airborne one still spawns level.
func TestAShotRidesItsShooter(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Archer")
	servertest.GiveBowAndArrows(srv, "Archer")

	// Every shot is a full draw held through the tick loop's real pace,
	// released with the move report named below the last one the client
	// made. A shot's aim carries noise of a few hundredths either way, so
	// each comparison leaves that much room.
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	c.MoveTo(0.5, 64, 0.5, -90, 0) // the same spot again, the movement the standing shot rides nought
	servertest.DrawBow(c, -90, 0)
	servertest.HoldDraw(t, srv, "Archer", 24)
	servertest.ReleaseBow(c)
	standing := servertest.ExpectArrowSpawn(srv, c)

	servertest.DrawBow(c, -90, 0)
	servertest.HoldDraw(t, srv, "Archer", 24)
	c.MoveToGrounded(3.5, 64, 0.5, -90, 0, true)
	servertest.ReleaseBow(c)
	running := servertest.ExpectArrowSpawn(srv, c)

	servertest.DrawBow(c, -90, 0)
	servertest.HoldDraw(t, srv, "Archer", 24)
	c.MoveToGrounded(3.5, 64.42, 0.5, -90, 0, true)
	servertest.ReleaseBow(c)
	grounded := servertest.ExpectArrowSpawn(srv, c)

	servertest.DrawBow(c, -90, 0)
	servertest.HoldDraw(t, srv, "Archer", 24)
	c.MoveToGrounded(3.5, 64.84, 0.5, -90, 0, false)
	servertest.ReleaseBow(c)
	airborne := servertest.ExpectArrowSpawn(srv, c)

	servertest.DrawBow(c, -90, 0)
	servertest.HoldDraw(t, srv, "Archer", 24)
	c.MoveToGrounded(3.5, 64.84, 3.5, -90, 0, true)
	servertest.ReleaseBow(c)
	strafing := servertest.ExpectArrowSpawn(srv, c)

	if got := running.Vx - standing.Vx; math.Abs(got-3) > 0.12 {
		t.Errorf("the running shot left %v faster than the standing one, want the reported step's three", got)
	}
	if got := grounded.Vy - standing.Vy; math.Abs(got) > 0.12 {
		t.Errorf("a rise reported with the feet on the ground rode the shot %v, want the vertical part left out", got)
	}
	if got := airborne.Vy - standing.Vy; math.Abs(got-0.42) > 0.12 {
		t.Errorf("a rise reported off the ground rode the shot %v, want the reported rise's 0.42", got)
	}
	// The heading is the bow velocity's own, whatever the ride was: a
	// strafing step's three south of the aim would turn the spawn's yaw
	// the forty-five of the combined flight if the ride came first. The
	// angles ride the wire as bytes of the circle, a degree and a bit to
	// the step, so the comparisons leave that much room.
	if got := strafing.Yaw; math.Abs(float64(got)-90) > 2 {
		t.Errorf("a shot while strafing spawned at yaw %v, want the bow velocity's own ninety", got)
	}
	if got := airborne.Pitch; got > 2 && got < 358 {
		t.Errorf("a shot released off the ground spawned at pitch %v, want the bow velocity's own level", got)
	}
}

// TestAnArrowSticksInAWallAndIsPickedUp shoots at a wall of stone, waits
// for the shaft to be in the ground, and walks the shooter over to it: the
// Take Item Entity packet that plays the arrow flying to its taker, the
// arrow of the quiver gained back, and the body gone from the world.
func TestAnArrowSticksInAWallAndIsPickedUp(t *testing.T) {
	t.Parallel()
	srv, c, playerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Archer")
	servertest.GiveBowAndArrows(srv, "Archer")

	// The wall: three cells of stone three blocks east of the shooter,
	// high enough that a level shot meets its face.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for y := int32(64); y <= 66; y++ {
		if !srv.SetBlock(4, y, 0, stone) {
			t.Fatalf("could not place the wall at (4, %d, 0)", y)
		}
	}

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	servertest.HoldDraw(t, srv, "Archer", 24)
	servertest.ReleaseBow(c)

	arrow := servertest.ExpectArrowSpawn(srv, c)
	// The full draw's shot is the critical one, and the crit is the one
	// field a flying arrow's metadata carries: the packet a vanilla
	// pairing sends for it and no other.
	if meta := expectArrowMetadata(c, srv, arrow); !meta.Crit {
		t.Error("the full draw's shot came out without its critical bit")
	}
	stuck := pumpTicks(c, srv, 500, nil, func(id int32, data []byte) bool {
		return id == protocol.IDPlayEntityMetadata &&
			protocol.NewReader(data).ReadVarInt() == arrow.EntityID &&
			servertest.ReadArrowMetadata(c, data).InGround
	})
	if !stuck {
		t.Fatal("the arrow never came to rest in the wall")
	}

	// The shooter walks over to where the shaft stands — the west face of
	// the wall, less the offset the shaft stands out by — and the pickup
	// is theirs once the shake of the arrival has settled.
	c.MoveTo(3.4, 64, 0.5, -90, 0)
	picked := false
	removed := false
	for gotID, data := range c.Stream() {
		switch gotID {
		case protocol.IDPlayPickupItem:
			collected, collector, count := servertest.ReadPickupItem(c, data)
			if collected == arrow.EntityID && collector == playerID && count == 1 {
				picked = true
			}
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				if r.ReadVarInt() == arrow.EntityID {
					removed = true
				}
			}
		}
		if picked && removed {
			break
		}
	}
	if !picked {
		t.Fatal("walking over the stuck arrow never picked it up")
	}
	if !removed {
		t.Fatal("the picked-up arrow was never taken out of the world")
	}
	// The quiver gained its arrow back.
	if got := srv.InventorySlot("Archer", 37); got.Count != 5 {
		t.Errorf("the quiver holds %d arrows after the pickup, want the five one arrow came back to", got.Count)
	}
}

// TestASkeletonShootsAPlayerAtNight stands a skeleton eight blocks off a
// survival player at midnight and waits for the shot: the health dropping,
// the damage event naming the arrow and its shooter, and — the fall that
// brought the player within one blow of the end having come first — the
// death message of the arrow's own, naming the skeleton. The world plays
// hard: the skeleton walks to its target at the pace vanilla's travel gives
// a monster before it stands to draw, and the shot's own spread of a hard
// world's two is small enough that the arrow's flight finds the body it
// was aimed at rather than the dice deciding the matter.
func TestASkeletonShootsAPlayerAtNight(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	srv.SetDifficulty(rules.DifficultyHard)
	srv.SetDayTime(18000) // midnight, whose dark is no daylight burn
	c := connect()
	playerID := c.JoinAs("Prey")

	skeletonID := srv.SpawnMob("minecraft:skeleton", 8.5, 64, 0.5)
	skeleton := c.ExpectSkeletonSpawn(skeletonID)
	identity, ok := srv.MobUUID(skeletonID)
	if !ok {
		t.Fatal("skeleton has no UUID")
	}
	if skeleton.Health != 20 {
		t.Errorf("spawned skeleton's health = %v, want the vanilla twenty", skeleton.Health)
	}
	if skeleton.SpeedAttribute != 0.25 {
		t.Errorf("spawned skeleton's speed = %v, want the vanilla 0.25", skeleton.SpeedAttribute)
	}
	if !skeleton.BowInHand {
		t.Error("spawned skeleton holds nothing in its main hand, want the bow")
	}

	// The fall that brings the prey within one blow of the end: a drop of
	// the twenty-two blocks whose hurt is all but the last point of a
	// body's health. The skeleton's arrow, whose easy-difficulty blow is
	// two and a half points and more, ends it.
	c.MoveTo(0.5, 86, 0.5, 0, 0)
	c.MoveToGround(0.5, 64, 0.5, true)

	var (
		healthDropped bool
		damageEvent   bool
		deathData     []byte
	)
	pumpTicks(c, srv, 500, skeletonSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySetHealth:
			h := servertest.ExpectHealthReport(c, data)
			if h.Health < 20 {
				healthDropped = true
			}
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			if victim := r.ReadVarInt(); victim != playerID {
				return false
			}
			// The fall that brought the prey low carries its own damage
			// event, whose fields are nobody's; the arrow's is the one
			// this reads.
			if got := r.ReadVarInt(); got != registry.MustID("minecraft:damage_type", rules.DamageTypeArrow.Name()) {
				return false
			}
			if got := r.ReadVarInt(); got != skeletonID+1 {
				c.T.Errorf("damage cause ID = %d, want the skeleton's %d", got, skeletonID+1)
			}
			if got := r.ReadVarInt(); got == 0 || got == skeletonID+1 {
				c.T.Errorf("damage direct ID = %d, want the arrow's own, above the skeleton's", got)
			}
			damageEvent = true
		case protocol.IDPlayCombatDeath:
			deathData = data
			return true
		}
		return false
	})
	if !healthDropped {
		t.Error("the skeleton's shots never dropped the player's health")
	}
	if !damageEvent {
		t.Error("the arrow's blow never came with its damage event")
	}
	if deathData == nil {
		t.Fatal("the skeleton's shot never landed fatally")
	}
	// The Combat Death the respawn screen is shown with: the arrow's own
	// message, the prey's name in the falling and the skeleton's in the
	// shooting.
	want := protocol.Buffer{}
	want.PutVarInt(playerID)
	want.PutText(rules.DeathMessage(rules.DamageTypeArrow, servertest.PlayerDisplayComponent("Prey"),
		&rules.Killer{Name: servertest.EntityDisplayComponent("minecraft:skeleton", "entity.minecraft.skeleton", uuid.MustParse(identity))}))
	if !bytes.Equal(deathData, want.Bytes()) {
		t.Errorf("combat death = % X, want the arrow's own message % X", deathData, want.Bytes())
	}
}

// TestASkeletonsArrowCannotBePickedUp stands an arrow of the skeleton's own
// pickup rule in a survival player's path — the DISALLOWED the non-player
// shooter's arrows are born to — and walks the player over it: nothing is
// collected and nothing is taken out of the world, while the allowed arrow
// standing beside it is taken the moment the shake settles.
func TestASkeletonsArrowCannotBePickedUp(t *testing.T) {
	t.Parallel()
	srv, c, playerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Walker")

	mine := srv.SpawnStuckArrow("allowed", 1.5, 64.05, 0.5)
	theirs := srv.SpawnStuckArrow("disallowed", 2.5, 64.05, 0.5)

	// Both arrows are in the ground when they arrive and their shake
	// settles within the first ticks of their lives; the walk over them
	// begins now.
	c.MoveTo(2.0, 64, 0.5, -90, 0)

	picked := int32(0)
	c.ReadTicks(srv, 100, func(gotID int32, data []byte) bool {
		if gotID == protocol.IDPlayPickupItem {
			collected, collector, _ := servertest.ReadPickupItem(c, data)
			if collector == playerID {
				picked = collected
			}
		}
		return false
	})
	if picked != mine {
		t.Errorf("the pickup that came was of arrow %d, want the allowed one's %d alone", picked, mine)
	}
	// The skeleton's own arrow is still in the world for whoever watched
	// it: the client was never told it went away.
	if !srv.ArrowAlive(theirs) {
		t.Error("the skeleton's arrow was taken out of the world, want it lying where it stuck")
	}
}

// arrowsHeld counts the arrows a named player carries across the 46 slots
// of their own window, the gain a pickup is judged by.
func arrowsHeld(srv *server.Server, name string) int32 {
	arrow := servertest.MustItem("minecraft:arrow")
	var held int32
	for slot := range 46 {
		if stack := srv.InventorySlot(name, slot); stack.ItemID == arrow {
			held += stack.Count
		}
	}
	return held
}

// awaitArrowTaken reads until the arrow with the given entity ID has been
// taken out of the world, gathering the collectors every Take Item Entity
// for it named on the way, and reads a quiet grace after the removal for
// any second take one shaft must not have had.
func awaitArrowTaken(t *testing.T, srv *server.Server, c *servertest.Client, arrowID int32) []int32 {
	t.Helper()
	var collectors []int32
	removed := false
	for id, data := range c.Stream() {
		switch id {
		case protocol.IDPlayPickupItem:
			if collected, collector, _ := servertest.ReadPickupItem(c, data); collected == arrowID {
				collectors = append(collectors, collector)
			}
		case protocol.IDPlayRemoveEntities:
			if slices.Contains(servertest.RemovedIDs(t, data), arrowID) {
				removed = true
			}
		}
		if removed {
			// The take goes out ahead of the removal it belongs to, and
			// a second one would follow it; the quiet after is read out
			// before the count is judged.
			c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
				if id == protocol.IDPlayPickupItem {
					if collected, collector, _ := servertest.ReadPickupItem(c, data); collected == arrowID {
						collectors = append(collectors, collector)
					}
				}
				return false
			})
			return collectors
		}
	}
	t.Fatalf("the arrow %d was never taken out of the world", arrowID)
	return nil
}

// TestTwoTouchersOfOneStuckArrowTakeOneArrow stands two survival players on
// one allowed stuck arrow and runs the tick that touches it by hand: one
// Take Item Entity apiece names the same single taker, and one arrow in all
// is gained — the second toucher of the pass finds the shaft taken, as
// vanilla's discard leaves them.
func TestTwoTouchersOfOneStuckArrowTakeOneArrow(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	first := connect()
	firstID := first.JoinAs("First")
	second := connect()
	secondID := second.JoinAs("Second")

	// The main loop stopped, so the tick that touches the arrow is the
	// test's to run: both bodies stand on the shaft in the one tick.
	srv.StopTick()
	arrowID := srv.SpawnStuckArrow("allowed", 1.5, 64.05, 0.5)
	first.MoveTo(1.5, 64, 0.5, -90, 0)
	second.MoveTo(1.9, 64, 0.5, -90, 0)
	first.Settle()
	second.Settle()
	if held := arrowsHeld(srv, "First") + arrowsHeld(srv, "Second"); held != 0 {
		t.Fatalf("the two players hold %d arrows before the touch, want none", held)
	}

	srv.TickEntities(1)

	firstTakers := awaitArrowTaken(t, srv, first, arrowID)
	secondTakers := awaitArrowTaken(t, srv, second, arrowID)
	for _, takers := range [][]int32{firstTakers, secondTakers} {
		if len(takers) != 1 {
			t.Fatalf("the stuck arrow was taken %d times for one client, want once: %v", len(takers), takers)
		}
		if takers[0] != firstID && takers[0] != secondID {
			t.Fatalf("the take names entity %d, want one of the two touchers (%d, %d)", takers[0], firstID, secondID)
		}
	}
	if firstTakers[0] != secondTakers[0] {
		t.Errorf("the two clients were told of different takers, %d and %d", firstTakers[0], secondTakers[0])
	}
	if held := arrowsHeld(srv, "First") + arrowsHeld(srv, "Second"); held != 1 {
		t.Errorf("the two players hold %d arrows after the take, want the one the shaft was", held)
	}
	if srv.ArrowAlive(arrowID) {
		t.Error("the taken arrow is still in the world")
	}
}

// TestAStuckArrowIsTouchedWithinABlockOfThePlayersBox lays two allowed
// stuck arrows beside a survival player on X, one whose box starts 1.25
// blocks from the player's centre and one at 1.45: the pickup area is the
// 0.3 half width grown by 1.0, so only the nearer one is taken.
// vanilla: Player.aiStep
func TestAStuckArrowIsTouchedWithinABlockOfThePlayersBox(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Taker")

	srv.StopTick()
	const centre = 1.5
	nearID := srv.SpawnStuckArrow("allowed", centre-1.25-0.25, 64.05, 0.5)
	farID := srv.SpawnStuckArrow("allowed", centre+1.45+0.25, 64.05, 0.5)
	c.MoveTo(centre, 64, 0.5, -90, 0)
	c.Settle()

	srv.TickEntities(4)

	var takes []int32
	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if id == protocol.IDPlayPickupItem {
			collected, _, _ := servertest.ReadPickupItem(c, data)
			takes = append(takes, collected)
		}
		return false
	})
	if slices.Contains(takes, farID) {
		t.Errorf("the arrow 1.45 blocks beside the player was taken, want it out of reach")
	}
	if !slices.Contains(takes, nearID) {
		t.Errorf("no Take Item Entity for the arrow 1.25 blocks beside the player (takes %v)", takes)
	}
	if !srv.ArrowAlive(farID) {
		t.Error("the far arrow left the world")
	}
	if srv.ArrowAlive(nearID) {
		t.Error("the near arrow is still in the world")
	}
	if held := arrowsHeld(srv, "Taker"); held != 1 {
		t.Errorf("the player holds %d arrows, want the near one", held)
	}
}

// TestAnArrowExpiringAsItIsTouchedIsNotPickedUp stands a survival player on
// an allowed stuck arrow and runs by hand the tick its 1200 ticks of life
// run out in: the despawn wins, nothing is taken, and the body going is
// all the client hears of the shaft.
func TestAnArrowExpiringAsItIsTouchedIsNotPickedUp(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Taker")

	srv.StopTick()
	arrowID := srv.SpawnStuckArrow("allowed", 1.5, 64.05, 0.5)
	c.MoveTo(1.5, 64, 0.5, -90, 0)
	c.Settle()
	// 1199 lain: the tick's own count is the one that ends the life.
	srv.AgeArrow(arrowID, 1199)
	srv.TickEntities(1)

	if srv.ArrowAlive(arrowID) {
		t.Fatal("the arrow outlived its life")
	}
	if held := arrowsHeld(srv, "Taker"); held != 0 {
		t.Errorf("the expiring arrow left %d arrows behind, want none", held)
	}
	if takers := awaitArrowTaken(t, srv, c, arrowID); len(takers) != 0 {
		t.Errorf("the despawning arrow was taken by %v, want no take at all", takers)
	}
}

// TestAnArrowAFullInventoryCannotHoldStaysStuck stands a survival player
// whose every carrying slot is full of something no arrow merges with on an
// allowed stuck arrow and runs the touching tick by hand: nothing is gained,
// no take is played, and the shaft stays where it stuck — vanilla's
// tryPickup fails an add the inventory cannot take — and the touch after a
// slot is freed takes it.
func TestAnArrowAFullInventoryCannotHoldStaysStuck(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Full")

	srv.StopTick()
	arrowID := srv.SpawnStuckArrow("allowed", 1.5, 64.05, 0.5)
	c.MoveTo(1.5, 64, 0.5, -90, 0)
	c.Settle()

	// Every slot a pickup lands in — the main inventory, the hotbar and
	// the off hand — full: cobblestone, which no arrow merges with.
	cobble := servertest.MustItem("minecraft:cobblestone")
	for slot := player.MainSlot; slot <= player.OffHandSlot; slot++ {
		srv.GiveSlot("Full", slot, protocol.ItemStack{ItemID: cobble, Count: 64})
	}
	if held := arrowsHeld(srv, "Full"); held != 0 {
		t.Fatalf("the full player holds %d arrows before the touch, want none", held)
	}

	srv.TickEntities(1)

	if held := arrowsHeld(srv, "Full"); held != 0 {
		t.Errorf("the full-inventory touch gained %d arrows, want none", held)
	}
	if !srv.ArrowAlive(arrowID) {
		t.Error("the arrow a full inventory cannot hold is gone from the world")
	}
	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayPickupItem:
			if collected, _, _ := servertest.ReadPickupItem(c, data); collected == arrowID {
				t.Errorf("the arrow the inventory cannot hold was taken anyway")
			}
		case protocol.IDPlayRemoveEntities:
			if slices.Contains(servertest.RemovedIDs(t, data), arrowID) {
				t.Errorf("the arrow the inventory cannot hold was removed anyway")
			}
		}
		return false
	})

	// A slot freed, the next touch takes the shaft it could not before.
	srv.GiveSlot("Full", player.MainSlot, protocol.ItemStack{})
	srv.TickEntities(1)
	if held := arrowsHeld(srv, "Full"); held != 1 {
		t.Errorf("the freed slot left %d arrows held after the touch, want the one the shaft was", held)
	}
	if srv.ArrowAlive(arrowID) {
		t.Error("the arrow is still in the world after the slot was freed")
	}
	awaitArrowTaken(t, srv, c, arrowID)
}

// TestACreativeFullInventoryStillTakesAnArrow is the full inventory in
// creative: the add counts as taken with no room, so the arrow goes and the
// take is played.
func TestACreativeFullInventoryStillTakesAnArrow(t *testing.T) {
	t.Parallel()
	cfg := servertest.BowTestConfig()
	cfg.GameMode = "creative"
	srv, c, _ := servertest.JoinAs(t, cfg, "Full")

	srv.StopTick()
	arrowID := srv.SpawnStuckArrow("allowed", 1.5, 64.05, 0.5)
	c.MoveTo(1.5, 64, 0.5, -90, 0)
	c.Settle()
	cobble := servertest.MustItem("minecraft:cobblestone")
	for slot := player.MainSlot; slot <= player.OffHandSlot; slot++ {
		srv.GiveSlot("Full", slot, protocol.ItemStack{ItemID: cobble, Count: 64})
	}

	srv.TickEntities(1)

	if srv.ArrowAlive(arrowID) {
		t.Error("a creative player's full inventory left the arrow stuck")
	}
	awaitArrowTaken(t, srv, c, arrowID)
}

// skeletonMetadata is one Set Entity Metadata packet's reading of a
// skeleton's state: the shared flags' on-fire bit, the health of the body,
// the Mob byte, and the hand state a skeleton drawing its bow holds up.
type skeletonMetadata struct {
	entityID  int32
	health    float32
	hasHealth bool
	flags     uint8
	hasFlags  bool
	onFire    bool
	hasFire   bool
	handState uint8
	hasHand   bool
}

// readSkeletonMetadata parses a Set Entity Metadata packet, taking the
// fields a skeleton carries.
func readSkeletonMetadata(c *servertest.Client, data []byte) skeletonMetadata {
	c.T.Helper()
	r := protocol.NewReader(data)
	m := skeletonMetadata{entityID: r.ReadVarInt()}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		switch index {
		case protocol.MetadataIndexLivingHealth:
			if fieldType != protocol.MetadataFloat {
				c.T.Fatalf("health field type = %d, want %d (a float)", fieldType, protocol.MetadataFloat)
			}
			m.health = r.ReadF32()
			m.hasHealth = true
		case protocol.MetadataIndexMobFlags:
			if fieldType != protocol.MetadataByte {
				c.T.Fatalf("mob flags field type = %d, want %d (a byte)", fieldType, protocol.MetadataByte)
			}
			m.flags = r.ReadU8()
			m.hasFlags = true
		case protocol.MetadataIndexEntityFlags:
			if fieldType != protocol.MetadataByte {
				c.T.Fatalf("entity flags field type = %d, want %d (a byte)", fieldType, protocol.MetadataByte)
			}
			m.onFire = r.ReadU8()&protocol.EntityFlagOnFire != 0
			m.hasFire = true
		case protocol.MetadataIndexHandState:
			if fieldType != protocol.MetadataByte {
				c.T.Fatalf("hand state field type = %d, want %d (a byte)", fieldType, protocol.MetadataByte)
			}
			m.handState = r.ReadU8()
			m.hasHand = true
		default:
			c.T.Fatalf("unexpected metadata field %d of type %d", index, fieldType)
		}
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("skeleton metadata: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("skeleton metadata has %d bytes left over", r.Remaining())
	}
	return m
}

// TestABurningSkeletonFleesToShade checks the sun goals' work over the
// wire: a skeleton stood in the open sky of a noon beside a roofed shelter
// catches fire the way the burn lights it, and — its hunt idle, the player
// watching being a creative one no monster may hunt — walks the flight the
// sun-fleeing goal asks for, into the shade of the roof, to stand there.
func TestABurningSkeletonFleesToShade(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg, func(s *server.Server) { s.SetTimeSync(0) })
	srv.SetPaused(true)
	srv.SetDayTime(6000) // noon, whose daylight burns and whose sky is bright
	c := connect().Join("ShadeSeeker")

	// The shelter: a stone roof three by three, two blocks over the flat
	// ground, nothing walling it — the shade of it is the light the open
	// sky spreads in under the edge, bright enough at the rim to be a hide
	// spot the flight accepts and dark enough overhead to be no sky.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for dx := int32(4); dx <= 6; dx++ {
		for dz := int32(2); dz <= 4; dz++ {
			if !srv.SetBlock(dx, 66, dz, stone) {
				t.Fatalf("could not place the shelter roof at (%d, 66, %d)", dx, dz)
			}
		}
	}
	srv.RelightAt(5, 3)

	// The skeleton, eight blocks south of the shelter's shade, in the
	// open sky of the noon the clock is paused at.
	skeletonID := srv.SpawnMob("minecraft:skeleton", 0.5, 64, 8.5)
	c.ExpectSkeletonSpawn(skeletonID)

	// The burn first: the shared flags' on-fire bit rises in the noon.
	// The metadata the bit rides in is the watch's own packet here, read
	// past the rest of the background the way skeletonSkip steps over it.
	onFire := false
	for gotID, data := range c.Stream() {
		switch gotID {
		case protocol.IDPlayEntityMetadata:
			m := readSkeletonMetadata(c, data)
			if m.entityID == skeletonID {
				onFire = m.hasFire && m.onFire
			}
		}
		if onFire {
			break
		}
	}
	if !onFire {
		t.Fatal("a skeleton in the noonday sun never caught fire")
	}

	// ...and the flight: the shade lies two to four blocks north of where
	// it stood. The wire's own packets would take a reader of their deltas
	// to follow; the server's own list of the living is plainer, and the
	// wait is for the skeleton's feet to stand under the roof and stay
	// there, which is what the flight's walk ends in.
	servertest.UntilTicks(t, srv, 500, "condition", func() bool {
		for _, m := range srv.Mobs() {
			if m.Kind != "minecraft:skeleton" {
				continue
			}
			if m.X >= 3.5 && m.X <= 7 && m.Z >= 1.5 && m.Z <= 5 {
				return true
			}
		}
		return false
	})
}

// TestAKilledSkeletonDropsItsLoot kills a skeleton with a sword and expects
// the loot of the vanilla table to come out of the death: the body's own
// death told the way every mob's is, and — where the dice rolled anything
// at all — nothing but arrows and bones lying where it fell.
func TestAKilledSkeletonDropsItsLoot(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Hunter")

	skeletonID := srv.SpawnMob("minecraft:skeleton", 2.5, 64, 0.5)
	c.ExpectSkeletonSpawn(skeletonID)

	// The table the death rolls: the generated one, whose two pools are
	// the skeleton's own — arrows and bones, each nought to two — which is
	// what the loot of this death can be and nothing else.
	table, ok := registry.FindLootTable("minecraft:entities/skeleton")
	if !ok || len(table.Pools) != 2 {
		t.Fatalf("the skeleton's loot table has %d pools, want the two of arrows and bones", len(table.Pools))
	}
	for i, want := range []string{"minecraft:arrow", "minecraft:bone"} {
		pool := table.Pools[i]
		if len(pool.Entries) != 1 {
			t.Fatalf("the skeleton's pool %d carries %d entries, want the one of %s", i, len(pool.Entries), want)
		}
		item, ok := pool.Entries[0].(registry.LootItemEntry)
		if !ok || item.Item.Name() != want {
			t.Errorf("the skeleton's pool %d is not the %s entry the vanilla table rolls", i, want)
		}
		count, ok := item.Functions[0].(registry.LootSetCount)
		if !ok {
			t.Fatalf("the skeleton's %s carries no set_count, want the vanilla nought-to-two", want)
		}
		uniform, ok := count.Count.(registry.LootUniform)
		if !ok {
			t.Fatalf("the skeleton's %s count is not a uniform range", want)
		}
		low, high := uniform.Min.(registry.LootConstant), uniform.Max.(registry.LootConstant)
		if low.Value != 0 || high.Value != 2 {
			t.Errorf("the skeleton's %s rolls %.0f to %.0f, want nought to two", want, low.Value, high.Value)
		}
	}

	// The kill: three swings of the diamond sword's seven, spaced past the
	// half-second window a hit opens.
	sword := protocol.ItemStack{ItemID: servertest.MustItem("minecraft:diamond_sword"), Count: 1}
	srv.GiveSlot("Hunter", 36, sword)
	// The sword's arrival in the hand spends the swing's recovery back to
	// nought; the kill waits the cooldown out before the first blow, so
	// every swing lands whole.
	servertest.AwaitFullSwing(t, srv, "Hunter")
	for range 3 {
		c.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(skeletonID) })
		servertest.AwaitTicks(t, srv, 12)
	}

	dead := false
	dropsClean := true
	for gotID, data := range c.Stream() {
		switch gotID {
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() == skeletonID && r.ReadU8() == protocol.EntityStatusDeath {
				dead = true
			}
		case protocol.IDPlayEntityMetadata:
			// An item entity's metadata names the stack it lies as; a
			// skeleton's death may lay several.
			r := protocol.NewReader(data)
			r.ReadVarInt()
			for r.Err() == nil {
				index := r.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				if fieldType := r.ReadVarInt(); index == protocol.MetadataIndexItemSlot && fieldType == protocol.MetadataSlot {
					stack, ok := r.ReadSlot()
					if !ok {
						continue
					}
					if name, known := registry.ItemName(stack.ItemID); known && name != "minecraft:arrow" && name != "minecraft:bone" {
						dropsClean = false
					}
				}
			}
		}
		if dead {
			break
		}
	}
	if !dead {
		t.Fatal("three swings of a diamond sword never killed the skeleton")
	}
	if !dropsClean {
		t.Error("the skeleton's death dropped something that is neither arrow nor bone")
	}
}

// TestASkeletonDrawingItsBowTurnsToFaceItsTarget stands a skeleton off from a
// survival player and lets it close and draw. The prey then steps out to the
// skeleton's side, off the line the walking body kept: while the skeleton
// strafes, RangedBowAttackGoal.lookAt turns the body itself onto the target at
// thirty degrees a tick, so after a few ticks of drawing the body and head
// both face the prey — the turn told to a watcher as the body and head
// rotation packets.
func TestASkeletonDrawingItsBowTurnsToFaceItsTarget(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Prey")
	c.Settle()

	// The loop stopped, so the ticks of the draw are the test's to run.
	srv.StopTick()
	skeletonID := srv.SpawnMob("minecraft:skeleton", 8.5, 64, 0.5)
	c.ExpectSkeletonSpawn(skeletonID)

	// The hunt: the skeleton finds the prey, walks into range and starts
	// drawing, its body still facing the walk it came by.
	for range 80 {
		srv.TickMobs(1)
	}

	// The prey steps to the skeleton's side. The look must swing the whole
	// body round, not just the head.
	c.MoveTo(8.5, 64, 8.5, 0, 0)
	c.Settle()

	var bodyYaw, headYaw float32
	var bodySeen, headSeen bool
	for range 40 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			r := protocol.NewReader(data)
			switch id {
			case protocol.IDPlayEntityRotation:
				if r.ReadVarInt() == skeletonID {
					bodyYaw, bodySeen = r.ReadAngle(), true
				}
			case protocol.IDPlayEntityPositionRot:
				// The rotation rides a move packet while the legs strafe:
				// the entity, the properties, the three deltas, then the angles.
				if r.ReadVarInt() == skeletonID {
					r.ReadVarInt()
					r.ReadI16()
					r.ReadI16()
					r.ReadI16()
					bodyYaw, bodySeen = r.ReadAngle(), true
				}
			case protocol.IDPlayHeadRotation:
				if r.ReadVarInt() == skeletonID {
					headYaw, headSeen = r.ReadAngle(), true
				}
			}
		})
	}

	var mobYaw, mobHead, mx, mz float64
	for _, m := range srv.Mobs() {
		if m.ID == skeletonID {
			mobYaw, mobHead, mx, mz = float64(m.Yaw), float64(m.HeadYaw), m.X, m.Z
		}
	}
	prey := srv.PlayerPosition("Prey")
	want := math.Atan2(-(prey.X-mx), prey.Z-mz) * 180 / math.Pi

	if d := angleApart(mobYaw, want); d > 2.5 {
		t.Errorf("the skeleton's body yaw = %.1f, %.1f degrees from the prey at %.1f", mobYaw, d, want)
	}
	if d := angleApart(mobHead, want); d > 2.5 {
		t.Errorf("the skeleton's head yaw = %.1f, %.1f degrees from the prey at %.1f", mobHead, d, want)
	}
	if !bodySeen {
		t.Error("the skeleton turned no body on the wire")
	} else if d := angleApart(float64(bodyYaw), want); d > 5 {
		t.Errorf("the body rotation packet carried yaw %.1f, %.1f degrees from the prey at %.1f", bodyYaw, d, want)
	}
	if !headSeen {
		t.Error("the skeleton turned no head on the wire")
	} else if d := angleApart(float64(headYaw), want); d > 5 {
		t.Errorf("the head rotation packet carried yaw %.1f, %.1f degrees from the prey at %.1f", headYaw, d, want)
	}
}

// TestASkeletonKeepsItsBowDrawnWhenSightIsLost draws a skeleton's bow and
// steps a wall between it and its prey. RangedBowAttackGoal.tick stops using
// the item only when the prey is unseen and seeTime has run below minus
// sixty, so the draw survives the first unseen ticks — and, once the wall
// comes down, a pull already past twenty ticks looses the tick sight
// returns.
func TestASkeletonKeepsItsBowDrawnWhenSightIsLost(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Prey")
	c.Settle()

	// The loop stopped, so the hunt's ticks are the test's to run.
	srv.StopTick()
	skeletonID := srv.SpawnMob("minecraft:skeleton", 8.5, 64, 0.5)
	c.ExpectSkeletonSpawn(skeletonID)

	// drawing is the bow raised, relaxed the hand state put down; both are
	// the hand-state turns the skeleton's metadata carries.
	var (
		drawing bool
		relaxed bool
	)
	collectHand := func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		m := readSkeletonMetadata(c, data)
		if m.entityID != skeletonID || !m.hasHand {
			return
		}
		if m.handState == protocol.EntityFlagHandActive {
			drawing = true
		} else {
			relaxed = true
		}
	}

	// The skeleton finds the prey and raises its bow. The draw begins on
	// the hunt's first tick, so the wait ends at the pull's start.
	for range 200 {
		srv.TickMobs(1)
		c.SettleAndCollect(collectHand)
		if drawing {
			break
		}
	}
	if !drawing {
		t.Fatal("the skeleton never raised its bow")
	}

	// The wall one block east of the prey: the sight line to the skeleton
	// crosses it wherever the skeleton stands on the far side.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	wall := func(state uint16) {
		for y := int32(64); y <= 67; y++ {
			for z := int32(-8); z <= 8; z++ {
				if !srv.SetBlock(1, y, z, state) {
					t.Fatalf("could not shape the wall at (1, %d, %d)", y, z)
				}
			}
		}
	}
	wall(stone)

	// The first tick without sight: the bow stays drawn rather than falling
	// and spending a fresh twenty ticks after the prey is seen again.
	srv.TickMobs(1)
	c.SettleAndCollect(collectHand)
	if relaxed {
		t.Fatal("the bow relaxed on the first tick the prey was hidden")
	}
	// The draw runs on through the hidden stretch, and no arrow leaves at a
	// prey that cannot be seen.
	for range 24 {
		srv.TickMobs(1)
		c.SettleAndCollect(collectHand)
	}
	if relaxed {
		t.Fatal("the bow relaxed while the prey was hidden inside the sixty-tick memory")
	}
	if got := srv.ArrowCount(); got != 0 {
		t.Fatalf("the skeleton loosed %d arrows at a prey it could not see, want none", got)
	}

	// The wall comes down: the pull is already past its twenty ticks, so
	// the arrow goes the tick sight returns.
	wall(0)
	srv.TickMobs(1)
	if got := srv.ArrowCount(); got != 1 {
		t.Fatalf("the skeleton loosed %d arrows the tick sight returned, want the one", got)
	}
}

// angleApart is the smaller angle between two headings, in degrees.
func angleApart(a, b float64) float64 {
	d := math.Mod(a-b+540, 360) - 180
	if d < 0 {
		return -d
	}
	return d
}

func TestASkeletonRidingASpiderTurnsToFaceItsTargetOnTheWire(t *testing.T) {
	t.Parallel()
	const name = "Prey"
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), name, servertest.OpsListing(t, name))
	c.Settle()
	srv.StopTick()
	c.ChatCommand("summon minecraft:spider 8.5 64 0.5")
	c.ChatCommand("summon minecraft:skeleton 8.5 64 0.5")
	c.ChatCommand("ride @e[type=minecraft:skeleton,limit=1] mount @e[type=minecraft:spider,limit=1]")
	c.Settle()
	var spiderID int32
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:spider" {
			spiderID = m.ID
		}
	}
	ids := srv.MobPassengerIDs(spiderID)
	if len(ids) != 1 {
		t.Fatalf("spider passengers %v, want the skeleton alone", ids)
	}
	skeletonID := ids[0]

	c.MoveTo(8.5, 64, 8.5, 0, 0)
	c.Settle()

	var bodySeen, headSeen bool
	var bodyYaw, headYaw float32
	for range 40 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			r := protocol.NewReader(data)
			switch id {
			case protocol.IDPlayEntityRotation:
				if r.ReadVarInt() == skeletonID {
					r.ReadBool() // onGround
					bodyYaw, bodySeen = r.ReadAngle(), true
				}
			case protocol.IDPlayHeadRotation:
				if r.ReadVarInt() == skeletonID {
					headYaw, headSeen = r.ReadAngle(), true
				}
			}
		})
	}

	var mobYaw, mobHead float64
	for _, m := range srv.Mobs() {
		if m.ID == skeletonID {
			mobYaw, mobHead = float64(m.Yaw), float64(m.HeadYaw)
		}
	}
	if !bodySeen {
		t.Error("the seated skeleton's body rotation never reached the watcher")
	} else if d := angleApart(float64(bodyYaw), mobYaw); d > 5 {
		t.Errorf("body rotation packet yaw %.1f, %.1f degrees from the skeleton's %.1f", bodyYaw, d, mobYaw)
	}
	if !headSeen {
		t.Error("the seated skeleton's head rotation never reached the watcher")
	} else if d := angleApart(float64(headYaw), mobHead); d > 5 {
		t.Errorf("head rotation packet yaw %.1f, %.1f degrees from the skeleton's %.1f", headYaw, d, mobHead)
	}
}
