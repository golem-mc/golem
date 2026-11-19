package mobs

import (
	"fmt"
	"math"
	"slices"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAShulkerBoxGrowsInTheTickAsTheShellOpens: the box a shell's opening
// grows is the one the server ticks and tells arrows and blows about.
// vanilla: Shulker.tick, updatePeekAmount
func TestAShulkerBoxGrowsInTheTickAsTheShellOpens(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	id := srv.SpawnMob("minecraft:shulker", math.Floor(at.X)+40.5, y, math.Floor(at.Z)+0.5)
	if _, _, _, _, top, _, _ := srv.MobBox(id); top != y+1 {
		t.Fatalf("a shut shulker's box tops out at %v, want %v", top, y+1)
	}
	srv.SetShulkerPeek(id, 100)
	srv.TickMobs(5)
	_, _, _, _, top, _, _ := srv.MobBox(id)
	if top <= y+1.1 {
		t.Errorf("five ticks into an opening shell the box tops out at %v, want it grown past %v", top, y+1.1)
	}
}

// TestAShulkerWithoutAIStillGrowsItsBox: Shulker.tick runs the opening
// whether or not the mob thinks.
// vanilla: Shulker.tick, Mob.serverAiStep
func TestAShulkerWithoutAIStillGrowsItsBox(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	id := srv.SpawnMob("minecraft:shulker", math.Floor(at.X)+40.5, y, math.Floor(at.Z)+0.5)
	srv.SetMobNoAI(id)
	srv.SetShulkerPeek(id, 100)
	srv.TickMobs(40)
	_, _, _, _, top, _, _ := srv.MobBox(id)
	if math.Abs(top-(y+2)) > 1e-6 {
		t.Errorf("a shulker without AI opened its box to %v, want %v", top, y+2)
	}
}

// TestAnOpeningShellLiftsAPigButNotAVex: the shell shoves what stands in the
// cell it opens into, except a body with noPhysics.
// vanilla: Shulker.onPeekAmountChange
func TestAnOpeningShellLiftsAPigButNotAVex(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	x, z := math.Floor(at.X)+40.5, math.Floor(at.Z)+0.5
	id := srv.SpawnMob("minecraft:shulker", x, y, z)
	pig := srv.SpawnMob("minecraft:pig", x, y+1, z)
	vex := srv.SpawnMob("minecraft:vex", x, y+1, z)
	srv.SetMobNoAI(id)
	srv.SetMobNoAI(pig)
	srv.SetMobNoAI(vex)
	srv.SetShulkerPeek(id, 100)
	srv.TickMobs(40)
	if _, py, _, ok := srv.MobPos(pig); !ok || py <= y+1.5 {
		t.Errorf("the pig over the shell stands at %v (alive %v), want it lifted toward %v", py, ok, y+2)
	}
	if _, vy, _, ok := srv.MobPos(vex); !ok || vy != y+1 {
		t.Errorf("the vex over the shell stands at %v (alive %v), want it left at %v", vy, ok, y+1)
	}
}

// gameEvents collects the game events posted at a place.
type gameEvents struct {
	mu   sync.Mutex
	seen map[string][][3]float64
}

func (g *gameEvents) add(event string, x, y, z float64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seen[event] = append(g.seen[event], [3]float64{x, y, z})
}

func (g *gameEvents) at(event string, x, y, z float64) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Contains(g.seen[event], [3]float64{x, y, z})
}

// TestASilentShulkerShellPostsItsContainerEvents: a shell that opens and
// shuts posts the container events at the shulker, silent or not.
// vanilla: Shulker.setRawPeekAmount
func TestASilentShulkerShellPostsItsContainerEvents(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	events := &gameEvents{seen: map[string][][3]float64{}}
	srv.ObserveGameEvents(events.add)
	at := srv.PlayerPosition("Watcher")
	x, z := math.Floor(at.X)+40.5, math.Floor(at.Z)+0.5
	id := srv.SpawnMob("minecraft:shulker", x, y, z)
	srv.SetMobSilent(id, true)
	// Out of any player's reach the peek goal opens the shell now and then
	// and shuts it again.
	for range 600 {
		srv.TickMobs(1)
		if events.at("minecraft:container_open", x, y, z) && events.at("minecraft:container_close", x, y, z) {
			return
		}
	}
	t.Errorf("container events at the shulker: %v", events.seen)
}

// TestAShulkerStaysInsideTheWorldBorderWhenItsBlockGoes: with the border
// excluding every cell it could hop to, a shulker whose block is broken
// stays put.
// vanilla: Shulker.teleportSomewhere
func TestAShulkerStaysInsideTheWorldBorderWhenItsBlockGoes(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	x, z := int32(math.Floor(at.X))+6, int32(math.Floor(at.Z))
	stone, _ := registry.FindState("minecraft:stone", nil)
	top := int32(y) + 2
	for yy := int32(y) - 1; yy < top; yy++ {
		srv.SetBlock(x, yy, z, stone)
	}
	id := srv.SpawnMob("minecraft:shulker", float64(x)+0.5, float64(top), float64(z)+0.5)
	srv.TickMobs(5)
	srv.SetOverworldBorder(at.X+200, at.Z+200, 10)
	srv.SetBlock(x, top-1, z, 0)
	srv.TickMobs(120)
	mx, my, mz, ok := srv.MobPos(id)
	if !ok || mx != float64(x)+0.5 || my != float64(top) || mz != float64(z)+0.5 {
		t.Errorf("the shulker stands at %v,%v,%v (alive %v), want it still on its cell", mx, my, mz, ok)
	}
}

// TestAShulkerTellsItsTeleportEventFromTheCellItLeft: the teleport game event
// is posted at the centre of the old block.
// vanilla: Shulker.teleportSomewhere
func TestAShulkerTellsItsTeleportEventFromTheCellItLeft(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	events := &gameEvents{seen: map[string][][3]float64{}}
	srv.ObserveGameEvents(events.add)
	at := srv.PlayerPosition("Watcher")
	x, z := int32(math.Floor(at.X))+6, int32(math.Floor(at.Z))
	stone, _ := registry.FindState("minecraft:stone", nil)
	top := int32(y) + 2
	for yy := int32(y) - 1; yy < top; yy++ {
		srv.SetBlock(x, yy, z, stone)
	}
	id := srv.SpawnMob("minecraft:shulker", float64(x)+0.5, float64(top), float64(z)+0.5)
	srv.TickMobs(5)
	srv.SetBlock(x, top-1, z, 0)
	for range 80 {
		srv.TickMobs(1)
		if mx, my, mz, ok := srv.MobPos(id); ok && (mx != float64(x)+0.5 || my != float64(top) || mz != float64(z)+0.5) {
			break
		}
	}
	if !events.at("minecraft:teleport", float64(x)+0.5, float64(top)+0.5, float64(z)+0.5) {
		t.Errorf("teleport events: %v, want one at the old cell's centre", events.seen["minecraft:teleport"])
	}
}

// TestAnOpeningShellShovesItemsThroughTheTick checks the event and its application.
func TestAnOpeningShellShovesItemsThroughTheTick(t *testing.T) {
	t.Parallel()
	for _, obstructed := range []bool{false, true} {
		t.Run(fmt.Sprint(obstructed), func(t *testing.T) {
			srv, _, y := shulkerTests(t)
			at := srv.PlayerPosition("Watcher")
			x, z := math.Floor(at.X)+40.5, math.Floor(at.Z)+0.5
			id := srv.SpawnMob("minecraft:shulker", x, y, z)
			srv.SetMobNoAI(id)
			start := y + 1.125
			item := srv.SpawnItemEntity(x, start, z, "minecraft:stone", 1)
			outside := srv.SpawnItemEntity(x+3, start, z, "minecraft:stone", 1)
			if obstructed {
				stone, _ := registry.FindState("minecraft:stone", nil)
				srv.SetBlock(int32(math.Floor(x)), int32(y)+2, int32(math.Floor(z)), stone)
			}
			srv.SetShulkerPeek(id, 100)
			srv.TickMobs(20)
			found := false
			for _, it := range srv.ItemsInDimension("minecraft:overworld") {
				if it.ID == outside && it.Y != start {
					t.Errorf("outside item moved to %v", it.Y)
				}
				if it.ID != item {
					continue
				}
				found = true
				if it.Y <= start+0.5 {
					t.Errorf("swept item stayed at %v", it.Y)
				}
				if obstructed && math.Abs(it.Y-(y+2-0.125)) > 1e-6 {
					t.Errorf("item crossed ceiling: y=%v", it.Y)
				}
				if !obstructed && it.Y < y+2 {
					t.Errorf("unobstructed item stopped at %v", it.Y)
				}
			}
			if !found {
				t.Fatal("swept item disappeared")
			}
		})
	}
}

// TestAnOpeningShellSendsNoPlayerPositionSync leaves the player shove to the client.
func TestAnOpeningShellSendsNoPlayerPositionSync(t *testing.T) {
	t.Parallel()
	srv, c, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	x, z := math.Floor(at.X)+3.5, math.Floor(at.Z)+0.5
	id := srv.SpawnMob("minecraft:shulker", x, y, z)
	srv.SetMobNoAI(id)
	c.MoveTo(x, y+1, z, 0, 0)
	c.Settle()
	if got := srv.PlayerPosition("Watcher"); math.Abs(got.Y-(y+1)) > 1e-6 {
		t.Fatalf("player starts at %v, want %v", got.Y, y+1)
	}
	srv.SetShulkerPeek(id, 100)
	srv.TickMobs(20)
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlaySyncPosition {
			t.Error("opening shell sent a player position sync")
		}
	})
	if _, _, _, _, top, _, _ := srv.MobBox(id); math.Abs(top-(y+2)) > 1e-6 {
		t.Fatalf("shell top at %v, want fully open at %v", top, y+2)
	}
}

func TestAnOpeningShellShovesMinecartsAndThrownBodies(t *testing.T) {
	t.Parallel()
	for _, obstructed := range []bool{false, true} {
		t.Run(fmt.Sprint(obstructed), func(t *testing.T) {
			srv, _, y := shulkerTests(t)
			servertest.HoldTicks(t, srv)
			at := srv.PlayerPosition("Watcher")
			x, z := math.Floor(at.X)+40.5, math.Floor(at.Z)+0.5
			shell := srv.SpawnMob("minecraft:shulker", x, y, z)
			srv.SetMobNoAI(shell)
			cart := srv.SpawnMinecart(x, y+1, z)
			outside := srv.SpawnMinecart(x+3, y+1, z)
			ids := make(map[int32]bool)
			for kind := entity.ThrownEgg; kind <= entity.ThrownLlamaSpit; kind++ {
				id := srv.ThrowThrown(kind, x, y+1, z, 0, 0, 0, 1)
				ids[id] = kind != entity.ThrownShulkerBullet
			}
			if obstructed {
				stone, _ := registry.FindState("minecraft:stone", nil)
				srv.SetBlock(int32(math.Floor(x)), int32(y)+2, int32(math.Floor(z)), stone)
			}
			srv.SetShulkerPeek(shell, 100)
			srv.TickMobs(20)
			_, cy, _, _, ok := srv.MinecartState(cart)
			wantCartY := y + 2
			if obstructed {
				wantCartY -= entity.MinecartHeight
			}
			if !ok || math.Abs(cy-wantCartY) > 1e-6 {
				t.Errorf("swept cart y=%v alive=%v, want %v", cy, ok, wantCartY)
			}
			if _, oy, _, _, _ := srv.MinecartState(outside); oy != y+1 {
				t.Errorf("outside cart moved to %v", oy)
			}
			for _, body := range srv.ThrownInDimension("minecraft:overworld") {
				push, found := ids[body.ID]
				if !found {
					continue
				}
				delete(ids, body.ID)
				// The shell lifts the body by its whole rise, or until the
				// kind's own box meets the stone. A wind charge's box sits
				// 0.15 below its position (AbstractWindCharge.makeBoundingBox).
				height, below := entity.ThrownHeight, 0.0
				switch body.Kind {
				case "minecraft:fireball", "minecraft:dragon_fireball":
					height = 1
				case "minecraft:wind_charge", "minecraft:breeze_wind_charge":
					height, below = 0.3125, 0.15
				case "minecraft:small_fireball", "minecraft:wither_skull":
					height = 0.3125
				}
				want := y + 1
				if push {
					want = y + 2
					if obstructed {
						want += below - height
					}
				}
				if math.Abs(body.Y-want) > 1e-6 {
					t.Errorf("%s y=%v, want %v", body.Kind, body.Y, want)
				}
			}
			if len(ids) != 0 {
				t.Errorf("missing thrown bodies: %v", ids)
			}
		})
	}
}
