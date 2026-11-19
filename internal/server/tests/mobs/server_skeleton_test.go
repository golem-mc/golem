package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/google/uuid"

	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
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
