package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The riptide trident over the wire: the release in water or rain that
// pushes the player along the look vector instead of throwing, the spin
// flag and pose the other clients draw it from, and the hit that ends it.
// Every test holds the tick loop so the ticks counted are exact.

// riptideFixture is a player facing south (+Z) holding a riptide trident,
// the loop held, with water standing at their feet when wet is set.
type riptideFixture struct {
	srv  *server.Server
	c    *servertest.Client
	id   int32
	wet  bool
	name string
}

func newRiptideFixture(t *testing.T, level int32, wet bool) *riptideFixture {
	t.Helper()
	srv, c, id := servertest.JoinAs(t, servertest.BowTestConfig(), "Diver")
	giveRiptideTrident(srv, "Diver", level)
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	f := &riptideFixture{srv: srv, c: c, id: id, wet: wet, name: "Diver"}
	if wet {
		f.setWater(true)
	}
	c.Settle()
	servertest.HoldTicks(t, srv)
	return f
}

// setWater fills or empties the two cells of the player's body.
func (f *riptideFixture) setWater(on bool) {
	state := uint16(0)
	if on {
		var ok bool
		state, ok = registry.FindState("minecraft:water", map[string]string{"level": "0"})
		if !ok {
			panic("no water state")
		}
	}
	f.srv.SetBlock(0, 64, 0, state)
	f.srv.SetBlock(0, 65, 0, state)
}

// charge draws the trident for the ticks given, past the attack cooldown
// when asked for 25.
func (f *riptideFixture) charge(t *testing.T, ticks int) {
	t.Helper()
	servertest.DrawBow(f.c, 0, 0)
	f.c.Settle()
	servertest.RunTicks(t, f.srv, ticks)
}

// riptideSeen is what a release put on the wire for the diver.
type riptideSeen struct {
	motion    [3]float64
	moved     bool
	sound     int32
	sounded   bool
	handState uint8
	handSeen  bool
	pose      int32
	thrown    bool
}

// release lets the draw go and reads ticks of traffic.
func (f *riptideFixture) release(t *testing.T, ticks int) riptideSeen {
	t.Helper()
	servertest.ReleaseBow(f.c)
	f.c.SettleKeeping()
	return f.read(t, ticks)
}

func (f *riptideFixture) read(t *testing.T, ticks int) riptideSeen {
	t.Helper()
	var seen riptideSeen
	f.c.ReadTicks(f.srv, ticks, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySetEntityMotion:
			if r.ReadVarInt() == f.id {
				x, y, z := r.ReadLpVec3()
				seen.motion, seen.moved = [3]float64{x, y, z}, true
			}
		case protocol.IDPlaySoundEffect:
			seen.sound, seen.sounded = readTridentSound(f.c, data).id, true
		case protocol.IDPlayEntityMetadata:
			if r.ReadVarInt() != f.id {
				return false
			}
			riptideMetadata(r, &seen)
		case protocol.IDPlaySpawnEntity:
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeTrident {
				seen.thrown = true
			}
		}
		return false
	})
	return seen
}

func (f *riptideFixture) wear() int32 { return f.srv.InventorySlot(f.name, 36).Damage }

// TestARiptideReleaseInWaterSpinsThePlayerInsteadOfThrowing releases a
// riptide trident of each level in water: the player is pushed along the look
// vector by 1.5 + 0.75 per level above the first, hears that level's sound,
// is drawn in the spin pose with the spin bit set, loses one point of wear
// and keeps the trident.
// vanilla: TridentItem.releaseUsing, Enchantments.RIPTIDE
func TestARiptideReleaseInWaterSpinsThePlayerInsteadOfThrowing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		level    int32
		strength float64
		sound    string
	}{
		{1, 1.5, "minecraft:item.trident.riptide_1"},
		{2, 2.25, "minecraft:item.trident.riptide_2"},
		{3, 3.0, "minecraft:item.trident.riptide_3"},
	} {
		t.Run(tc.sound, func(t *testing.T) {
			t.Parallel()
			f := newRiptideFixture(t, tc.level, true)
			f.charge(t, 12)
			_, beforeY, _ := f.srv.PlayerVelocity(f.name)
			seen := f.release(t, 1)

			if !seen.moved {
				t.Fatal("the release pushed the diver with no Set Entity Motion")
			}
			if math.Abs(seen.motion[0]) > 0.02 || math.Abs(seen.motion[1]-beforeY) > 0.02 ||
				math.Abs(seen.motion[2]-tc.strength) > 0.02 {
				t.Errorf("push = %v, want (0, %v, %v) retaining vertical motion", seen.motion, beforeY, tc.strength)
			}
			if !seen.sounded || seen.sound != tridentSoundOf(t, tc.sound) {
				t.Errorf("sound = %d (heard %v), want %s", seen.sound, seen.sounded, tc.sound)
			}
			if seen.handState&protocol.EntityFlagSpinAttack == 0 {
				t.Errorf("hand state = %#x (sent %v), want the spin bit", seen.handState, seen.handSeen)
			}
			if seen.pose != protocol.PoseSpinAttack {
				t.Errorf("pose = %d, want the spin attack's %d", seen.pose, protocol.PoseSpinAttack)
			}
			if seen.thrown {
				t.Error("a riptide release threw the trident")
			}
			if count, _ := tridentsHeld(f.srv, f.name); count != 1 {
				t.Errorf("the diver holds %d tridents, want the one kept", count)
			}
			if got := f.wear(); got != 1 {
				t.Errorf("wear = %d, want 1", got)
			}
		})
	}
}

// TestARiptideSpinLastsTwentyTicksThenEnds counts the spin out: still on
// after nineteen ticks, off at the twentieth with the hand state cleared.
// vanilla: LivingEntity.aiStep, Player.startAutoSpinAttack(20, ...)
func TestARiptideSpinLastsTwentyTicksThenEnds(t *testing.T) {
	t.Parallel()
	f := newRiptideFixture(t, 1, true)
	f.charge(t, 12)
	f.release(t, 0)
	servertest.RunTicks(t, f.srv, 18)
	if !f.srv.PlayerSpinAttack(f.name) {
		t.Fatal("the spin ended before its twentieth tick")
	}
	seen := f.read(t, 3)
	if f.srv.PlayerSpinAttack(f.name) {
		t.Error("the spin outlasted its twenty ticks")
	}
	if !seen.handSeen || seen.handState&protocol.EntityFlagSpinAttack != 0 {
		t.Errorf("hand state = %#x (sent %v), want it cleared on the wire", seen.handState, seen.handSeen)
	}
	if seen.pose == protocol.PoseSpinAttack {
		t.Error("the pose stayed the spin attack's")
	}
}

// TestARiptideReleaseOutOfWaterDoesNothing fills the water for the charge and
// empties it before the release: no push, no spin, no wear.
// vanilla: TridentItem.releaseUsing (isInWaterOrRain)
func TestARiptideReleaseOutOfWaterDoesNothing(t *testing.T) {
	t.Parallel()
	f := newRiptideFixture(t, 1, true)
	f.charge(t, 12)
	f.setWater(false)
	seen := f.release(t, 2)
	if seen.moved || seen.thrown || f.srv.PlayerSpinAttack(f.name) || f.wear() != 0 {
		t.Errorf("a dry release moved=%v thrown=%v spinning=%v wear=%d, want nothing",
			seen.moved, seen.thrown, f.srv.PlayerSpinAttack(f.name), f.wear())
	}
}

// TestARiptideSpinHitsAMobOnceAndEnds spins into a cow after a full-cooldown
// charge: the cow takes the spin's 8 once, the spin ends on the hit and the
// diver's velocity is bounced back.
// vanilla: LivingEntity.checkAutoSpinAttack, Player.attack (autoSpinAttackDmg)
func TestARiptideSpinHitsAMobOnceAndEnds(t *testing.T) {
	t.Parallel()
	f := newRiptideFixture(t, 1, true)
	f.charge(t, 25)
	// Planted after the charge so the cow has not wandered off the body.
	cow := f.srv.SpawnMob("minecraft:cow", 0.5, 64, 1.0)
	before, _ := f.srv.MobHealth(cow)
	f.release(t, 1)
	servertest.RunTicks(t, f.srv, 1)

	after, _ := f.srv.MobHealth(cow)
	if got := before - after; math.Abs(got-8) > 0.01 {
		t.Errorf("the cow lost %v, want the spin's 8", got)
	}
	if f.srv.PlayerSpinAttack(f.name) {
		t.Error("the spin went on after its hit")
	}
	if _, _, z := f.srv.PlayerVelocity(f.name); z >= 0 {
		t.Errorf("velocity z = %v after the hit, want it bounced back", z)
	}
	servertest.RunTicks(t, f.srv, 15)
	if again, _ := f.srv.MobHealth(cow); again != after {
		t.Errorf("the cow is at %v after the spin ended, want %v: one hit", again, after)
	}
}

// riptideDuel is a diver in water beside a victim in the given game mode.
type riptideDuel struct {
	*riptideFixture
	victim *servertest.Client
}

func newRiptideDuel(t *testing.T, diverMode, victimMode string) *riptideDuel {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.BowTestConfig(), servertest.OpsListing(t, "Diver"))
	c := connect()
	id := c.JoinAs("Diver")
	victim := connect()
	victim.JoinAs("Victim")
	giveRiptideTrident(srv, "Diver", 1)
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	victim.MoveToGround(0.5, 64, 1.0, true)
	f := &riptideFixture{srv: srv, c: c, id: id, wet: true, name: "Diver"}
	f.setWater(true)
	for name, mode := range map[string]string{"Diver": diverMode, "Victim": victimMode} {
		if mode == "survival" {
			continue
		}
		c.ChatCommand("gamemode " + mode + " " + name)
		want := map[string]int32{"creative": protocol.GameModeCreative, "spectator": protocol.GameModeSpectator}[mode]
		servertest.UntilTicks(t, srv, 1200, "game mode", func() bool {
			return srv.PlayerGameMode(name) == want
		})
	}
	c.Settle()
	victim.Settle()
	servertest.HoldTicks(t, srv)
	return &riptideDuel{riptideFixture: f, victim: victim}
}

// TestARiptideSpinHitsAPlayer spins into a survival player: they take the
// spin's 8 once and the spin ends.
// vanilla: LivingEntity.checkAutoSpinAttack, Player.attack
func TestARiptideSpinHitsAPlayer(t *testing.T) {
	t.Parallel()
	d := newRiptideDuel(t, "survival", "survival")
	d.charge(t, 25)
	before := d.srv.PlayerHealth("Victim")
	d.release(t, 1)
	servertest.RunTicks(t, d.srv, 1)
	if got := before - d.srv.PlayerHealth("Victim"); math.Abs(got-8) > 0.01 {
		t.Errorf("the victim lost %v, want the spin's 8", got)
	}
	if d.srv.PlayerSpinAttack(d.name) {
		t.Error("the spin went on after its hit")
	}
}

// TestARiptideSpinIntoAWallEnds sends a move whose horizontal-collision
// flag is set mid-spin: the spin ends within the tick, for the server and
// for a watcher.
// vanilla: LivingEntity.checkAutoSpinAttack (horizontalCollision)
func TestARiptideSpinIntoAWallEnds(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig())
	c := connect()
	id := c.JoinAs("Diver")
	watcher := connect().Join("Watcher")
	giveRiptideTrident(srv, "Diver", 1)
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	watcher.MoveToGround(6.5, 64, 0.5, true)
	f := &riptideFixture{srv: srv, c: c, id: id, wet: true, name: "Diver"}
	f.setWater(true)
	c.Settle()
	watcher.Settle()
	servertest.HoldTicks(t, srv)
	f.charge(t, 25)
	f.release(t, 1)
	if !srv.PlayerSpinAttack(f.name) {
		t.Fatal("the release did not start a spin")
	}
	watcher.Settle()

	c.Send(protocol.IDPlayMovementFlags, func(b *protocol.Buffer) { b.PutU8(0x02) })
	c.Settle()
	var seen riptideSeen
	watcher.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
		r := protocol.NewReader(data)
		if pid == protocol.IDPlayEntityMetadata && r.ReadVarInt() == id {
			riptideMetadata(r, &seen)
		}
		return false
	})
	if srv.PlayerSpinAttack(f.name) {
		t.Error("the spin went on after running into a wall")
	}
	if !seen.handSeen || seen.handState&protocol.EntityFlagSpinAttack != 0 {
		t.Errorf("watcher saw hand state %#x (sent %v), want the spin bit cleared", seen.handState, seen.handSeen)
	}
}

// riptideMetadata reads the fields of a player's Set Entity Metadata that the
// spin changes, past the entity ID, stopping at a field type it cannot step
// over.
func riptideMetadata(r *protocol.Reader, seen *riptideSeen) {
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			return
		}
		switch typ := r.ReadVarInt(); typ {
		case protocol.MetadataByte:
			v := r.ReadU8()
			if index == protocol.MetadataIndexHandState {
				seen.handState, seen.handSeen = v, true
			}
		case protocol.MetadataPose:
			seen.pose = r.ReadVarInt()
		case protocol.MetadataVarInt, protocol.MetadataHumanoidArm:
			r.ReadVarInt()
		case protocol.MetadataFloat:
			r.ReadF32()
		case protocol.MetadataBoolean:
			r.ReadBool()
		default:
			return
		}
	}
}
