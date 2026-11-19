package blocks

// The big dripleaf's tilt from a body landing on it and its redstone reset.
// vanilla: BigDripleafBlock.entityInside, neighborChanged, tick

import (
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/servertest"
)

func dripleafTilt(t *testing.T, srvBlock uint16) string {
	t.Helper()
	_, props := servertest.StateProps(srvBlock)
	return props["tilt"]
}

// TestAGroundedPlayerTiltsTheBigDripleafStepByStep stands the actor on a
// leaf: it goes unstable, partial and full at 10, 10 and 100 ticks, then
// rights itself.
func TestAGroundedPlayerTiltsTheBigDripleafStepByStep(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)
	if !srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:big_dripleaf", map[string]string{"tilt": "none"})) {
		t.Fatal("could not place the leaf")
	}
	c.MoveToGround(6.5, 65.9375, 0.5, true)
	c.Settle()

	srv.TickRedstoneTouch(1)
	if got := dripleafTilt(t, srv.BlockAt(6, 65, 0)); got != "unstable" {
		t.Fatalf("tilt after landing = %q, want unstable", got)
	}
	for _, step := range []struct {
		ticks int
		want  string
	}{{8, "unstable"}, {3, "partial"}, {8, "partial"}, {3, "full"}, {95, "full"}, {10, "none"}} {
		if step.want == "none" {
			// A body still standing there would tilt it again.
			c.MoveToGround(6.5, 66, 4.5, false)
			c.Settle()
		}
		for range step.ticks {
			servertest.DriveTargetTicks(srv, 1)
			srv.TickRedstoneTouch(1)
		}
		if got := dripleafTilt(t, srv.BlockAt(6, 65, 0)); got != step.want {
			t.Fatalf("after %d more ticks tilt = %q, want %s", step.ticks, got, step.want)
		}
	}
}

// TestAFlyingPlayerDoesNotTiltTheBigDripleaf keeps the actor off the ground.
func TestAFlyingPlayerDoesNotTiltTheBigDripleaf(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)
	srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:big_dripleaf", map[string]string{"tilt": "none"}))
	c.MoveToGround(6.5, 65.9375, 0.5, false)
	c.Settle()
	srv.TickRedstoneTouch(3)
	if got := dripleafTilt(t, srv.BlockAt(6, 65, 0)); got != "none" {
		t.Errorf("tilt = %q, want none", got)
	}
}

// TestAPlayerBelowTheBigDripleafMarkDoesNotTiltIt puts grounded feet under
// the leaf's 11/16 mark.
func TestAPlayerBelowTheBigDripleafMarkDoesNotTiltIt(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)
	srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:big_dripleaf", map[string]string{"tilt": "none"}))
	c.MoveToGround(6.5, 65.5, 0.5, true)
	c.Settle()
	srv.TickRedstoneTouch(3)
	if got := dripleafTilt(t, srv.BlockAt(6, 65, 0)); got != "none" {
		t.Errorf("tilt = %q, want none", got)
	}
}

// TestAPoweredBigDripleafStaysFlatAndAPowerResetsATilt checks both halves of
// the redstone rule.
func TestAPoweredBigDripleafStaysFlatAndAPowerResetsATilt(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)
	srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:big_dripleaf", map[string]string{"tilt": "none"}))
	c.ChatCommand("setblock 7 65 0 redstone_block")
	c.MoveToGround(6.5, 65.9375, 0.5, true)
	c.Settle()
	srv.TickRedstoneTouch(3)
	if got := dripleafTilt(t, srv.BlockAt(6, 65, 0)); got != "none" {
		t.Fatalf("powered leaf tilt = %q, want none", got)
	}

	c.ChatCommand("setblock 7 65 0 air")
	c.MoveToGround(6.5, 66.5, 3.5, false)
	c.Settle()
	srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:big_dripleaf", map[string]string{"tilt": "partial"}))
	c.ChatCommand("setblock 7 65 0 redstone_block")
	c.Settle()
	servertest.DriveTargetTicks(srv, 2)
	if got := dripleafTilt(t, srv.BlockAt(6, 65, 0)); got != "none" {
		t.Errorf("tilt after power = %q, want none", got)
	}
}

// TestAnItemAMobAndAnOrbRestingOnABigDripleafTiltIt lands each body on its
// own leaf and steps the touch pass.
func TestAnItemAMobAndAnOrbRestingOnABigDripleafTiltIt(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)
	for i, kind := range []string{"item", "pig", "orb"} {
		x := int32(6 + 3*i)
		if !srv.SetBlock(x, 65, 0, servertest.MustState(t, "minecraft:big_dripleaf", map[string]string{"tilt": "none"})) {
			t.Fatal("could not place the leaf")
		}
		fx, fy, fz := float64(x)+0.5, 66.5, 0.5
		switch kind {
		case "item":
			srv.SpawnItemEntity(fx, fy, fz, "minecraft:stone", 1)
		case "pig":
			srv.SpawnMob("minecraft:pig", fx, fy, fz)
		default:
			srv.SpawnOrb(fx, fy, fz, 1)
		}
	}
	// The pig lands within a few ticks and then wanders off the leaf, so the
	// touch pass runs between its ticks.
	for range 10 {
		srv.TickEntities(1)
		srv.TickMobs(1)
		srv.TickRedstoneTouch(1)
	}
	for i, kind := range []string{"item", "pig", "orb"} {
		if got := dripleafTilt(t, srv.BlockAt(int32(6+3*i), 65, 0)); got != "unstable" {
			t.Errorf("%s: tilt = %q, want unstable", kind, got)
		}
	}
}

// TestASpectatorOnABigDripleafDoesNotTiltIt stands a grounded spectator on a leaf.
func TestASpectatorOnABigDripleafDoesNotTiltIt(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)
	srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:big_dripleaf", map[string]string{"tilt": "none"}))
	c.ChatCommand("gamemode spectator")
	c.MoveToGround(6.5, 65.9375, 0.5, true)
	c.Settle()
	srv.TickRedstoneTouch(3)
	if got := dripleafTilt(t, srv.BlockAt(6, 65, 0)); got != "none" {
		t.Errorf("tilt = %q, want none", got)
	}
}

// vanilla: BigDripleafBlock.setTilt, Tilt.causesVibration
func TestDripleafTiltVibrations(t *testing.T) {
	for _, projectile := range []bool{false, true} {
		name := "scheduled"
		if projectile {
			name = "projectile"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))
			servertest.HoldTicks(t, srv)
			srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:big_dripleaf", nil))
			var mu sync.Mutex
			var events []rules.GameEventContext
			srv.ObserveGameEventContexts(func(event string, x, y, z float64, ctx rules.GameEventContext) {
				if event == rules.GameEventBlockChange.Name() && x == 6.5 && y == 65.5 && z == 0.5 {
					mu.Lock()
					events = append(events, ctx)
					mu.Unlock()
				}
			})
			expect := func(tilt string, count int) {
				t.Helper()
				if got := dripleafTilt(t, srv.BlockAt(6, 65, 0)); got != tilt {
					t.Fatalf("tilt = %s, want %s", got, tilt)
				}
				mu.Lock()
				defer mu.Unlock()
				if len(events) != count {
					t.Fatalf("tilt %s: %d block_change events, want %d", tilt, len(events), count)
				}
				for _, ctx := range events {
					if ctx.Player != uuid.Nil || ctx.Mob != uuid.Nil || ctx.ProjectileOwner != uuid.Nil {
						t.Errorf("event has source: %+v", ctx)
					}
				}
			}
			if projectile {
				// Five wires reduce the west-side input to frequency 11, filtering out
				// the arrow's shoot and landing vibrations.
				srv.RunLine("setblock 6 65 3 calibrated_sculk_sensor[facing=east]")
				srv.RunLine("setblock 0 65 3 redstone_block")
				srv.RunLine("fill 1 65 3 5 65 3 redstone_wire")
				servertest.RunTicks(t, srv, 50)
				srv.RunLine(`summon arrow 6.5 68 0.5 {Motion:[0d,-1d,0d],NoGravity:1b,HasBeenShot:1b}`)
				servertest.RunTicks(t, srv, 10)
				expect("full", 1)
				tag, ok := srv.BlockEntityTagAt(6, 65, 3)
				frequency, _ := nbt.Get[nbt.Int](tag, "last_vibration_frequency")
				if !ok || frequency != 11 || registry.StateValue(srv.BlockAt(6, 65, 3), registry.PropSculkSensorPhase) != "active" {
					t.Fatalf("calibrated sensor did not activate at frequency 11: %v", tag)
				}
				srv.RunLine(`summon arrow 6.5 68 0.5 {Motion:[0d,-1d,0d],NoGravity:1b,HasBeenShot:1b}`)
				servertest.RunTicks(t, srv, 10)
				expect("full", 1)
			} else {
				c.MoveToGround(6.5, 65.9375, 0.5, true)
				c.Settle()
				servertest.RunTicks(t, srv, 1)
				expect("unstable", 0)
				c.MoveToGround(6.5, 67, 4.5, false)
				c.Settle()
				servertest.RunTicks(t, srv, 10)
				expect("partial", 1)
				servertest.RunTicks(t, srv, 10)
				expect("full", 2)
				servertest.RunTicks(t, srv, 100)
				expect("none", 3)
			}
			// Power resets the leaf immediately, including when already flat.
			count := 3
			if projectile {
				count = 2
			}
			srv.RunLine("setblock 7 65 0 redstone_block")
			expect("none", count)
			srv.RunLine("setblock 7 65 0 air")
			srv.RunLine("setblock 7 65 0 redstone_block")
			expect("none", count)
		})
	}
}
