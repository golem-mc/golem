package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The black-box side of the /tick command's rate, freeze, step and sprint
// subcommands: the translation keys a change answers with, the Set Ticking
// State and Step Tick packets every change and every join carry, and the
// pacing the rate and a sprint run the loop itself by.

// gateClock is a clock the test steps one wait at a time: each release lets
// the loop finish its current wait, and the clock jumps to whatever the
// loop asked for. The duration each released wait asked for is recorded,
// so the pacing the loop runs by is what the test reads.
type gateClock struct {
	mu      sync.Mutex
	now     time.Time
	release chan struct{}
	waits   []time.Duration
}

func newGateClock() *gateClock {
	return &gateClock{now: time.Unix(2_000_000, 0), release: make(chan struct{})}
}

func (c *gateClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Wait jumps the clock forward to t when t lies ahead, then blocks until
// the test releases the wait; the release is what makes a wait one of the
// recorded ones. It returns false once the loop is stopped.
func (c *gateClock) Wait(t time.Time, done <-chan struct{}) bool {
	c.mu.Lock()
	d := t.Sub(c.now)
	if d > 0 {
		c.now = c.now.Add(d)
	}
	c.mu.Unlock()
	select {
	case <-c.release:
		c.mu.Lock()
		c.waits = append(c.waits, d)
		c.mu.Unlock()
		return true
	case <-done:
		return false
	}
}

// step releases one wait.
func (c *gateClock) step() { c.release <- struct{}{} }

// paced reports the per-wait durations the loop asked for, in order.
func (c *gateClock) paced() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

// assertPacing checks the per-wait pacing the loop asked for, waiting for
// the released waits to have recorded themselves first: a release returns
// before the loop has written its wait down.
func assertPacing(t *testing.T, clock *gateClock, want ...time.Duration) {
	t.Helper()
	servertest.PollUntil(t, 5*time.Second, func() bool { return len(clock.paced()) == len(want) })
	assertWaits(t, clock.paced(), want...)
}

// assertWaits checks the pacing the loop asked for wait by wait.
func assertWaits(t *testing.T, got []time.Duration, want ...time.Duration) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("the loop paced %d waits, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("wait %d paced %v, want %v", i, got[i], want[i])
		}
	}
}

// TestTickFreezeHoldsTheWorldAndTellsEveryone: freeze answers with the
// frozen status and tells every client the world is frozen; the world's
// age, its clocks and a hanging block the world would otherwise take out
// stand still while it is; and unfreeze lets them move again.
func TestTickFreezeHoldsTheWorldAndTellsEveryone(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("tick freeze")
	c.ExpectTickingState(20, true)
	c.ExpectCommandFeedback("commands.tick.status.frozen")

	// A tick in flight when the freeze landed may still advance the clock;
	// from one whole frozen tick to the next, nothing moves.
	servertest.AwaitTicks(t, srv, 5)
	age, day := srv.Time()
	end := srv.EndClockTime()

	// A hanging sand block holds where it stands: the block ticks and the
	// falling entity both are of the world's passes a freeze gates.
	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}
	c.ChatCommand("setblock 3 70 3 sand")
	servertest.AwaitCommandFeedback(t, c, "commands.setblock.success", "3", "70", "3")
	c.AwaitBlockState(3, 70, 3, sand)

	servertest.AwaitTicks(t, srv, 20)
	frozen, day2 := srv.Time()
	if age != frozen || day != day2 {
		t.Errorf("the frozen world's clocks moved: age %d -> %d, clock %d -> %d", age, frozen, day, day2)
	}
	if got := srv.EndClockTime(); got != end {
		t.Errorf("the frozen end clock moved: %d -> %d", end, got)
	}
	if got := srv.BlockAt(3, 70, 3); got != sand {
		t.Errorf("the frozen world let a hanging block go: state %d, want %d", got, sand)
	}

	c.ChatCommand("tick unfreeze")
	c.ExpectTickingState(20, false)
	c.ExpectCommandFeedback("commands.tick.status.running")
	servertest.AwaitTicks(t, srv, 3)

	moving, _ := srv.Time()
	if moving <= age {
		t.Error("the unfrozen world's age did not move again")
	}
	// The released sand goes out of the world as a falling body.
	servertest.UntilTicks(t, srv, 100, "block at (3, 70, 3)", func() bool { return srv.BlockAt(3, 70, 3) == 0 })
	c.Disconnect()
}

// TestTickStepRunsAFrozenGameForward: the steps a frozen game is asked to
// run are broadcast in the Step Tick packet and run exactly that many game
// ticks; a time argument's count is what the packet carries, a stop cancels
// what is left, and an unfrozen game refuses a step.
func TestTickStepRunsAFrozenGameForward(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("tick freeze")
	c.ExpectTickingState(20, true)
	c.ExpectCommandFeedback("commands.tick.status.frozen")
	servertest.AwaitTicks(t, srv, 5)
	age, _ := srv.Time()

	// A bare step runs one tick.
	c.ChatCommand("tick step")
	c.ExpectTickingStep(1)
	one := int64(1)
	c.ExpectCommandFeedbackText("commands.tick.step.success", protocol.Text{Number: &one})
	servertest.UntilTicks(t, srv, 600, "condition", func() bool {
		at, _ := srv.Time()
		return at > age
	})
	servertest.AwaitTicks(t, srv, 5)
	age, _ = srv.Time()

	c.ChatCommand("tick step 5")
	c.ExpectTickingStep(5)
	five := int64(5)
	c.ExpectCommandFeedbackText("commands.tick.step.success", protocol.Text{Number: &five})
	servertest.UntilTicks(t, srv, 600, "condition", func() bool {
		at, _ := srv.Time()
		return at >= age+5
	})
	servertest.AwaitTicks(t, srv, 20)
	if at, _ := srv.Time(); at != age+5 {
		t.Errorf("the stepped game ran %d ticks, want 5", at-age)
	}

	// The steps ran out, so there is no step in progress to stop.
	c.ChatCommand("tick step stop")
	c.ExpectFailure("commands.tick.step.stop.fail")

	// A stopped step cancels the ticks left; some of the twenty may already
	// be in flight before the stop lands.
	c.ChatCommand("tick step 1s")
	c.ExpectTickingStep(20)
	twenty := int64(20)
	c.ExpectCommandFeedbackText("commands.tick.step.success", protocol.Text{Number: &twenty})
	c.ChatCommand("tick step stop")
	c.ExpectTickingStep(0)
	c.ExpectCommandFeedback("commands.tick.step.stop.success")
	servertest.AwaitTicks(t, srv, 30)
	if at, _ := srv.Time(); at > age+5+21 {
		t.Errorf("the stopped step still ran %d ticks of its twenty", at-(age+5))
	}

	// An unfrozen game refuses a step.
	c.ChatCommand("tick unfreeze")
	c.ExpectTickingState(20, false)
	c.ExpectCommandFeedback("commands.tick.status.running")
	c.ChatCommand("tick step 3")
	c.ExpectFailure("commands.tick.step.fail")

	c.ChatCommand("tick sprint stop")
	c.ExpectFailure("commands.tick.sprint.stop.fail")
	c.Disconnect()
}

// TestAFrozenWorldHoldsTheFurnaces: a freeze holds a loaded furnace's
// cooking, its fuel and its progress standing still, and unfreezing sets it
// going again.
// vanilla: Level.tickBlockEntities
func TestAFrozenWorldHoldsTheFurnaces(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	servertest.LoadFurnace(t, srv, w, "minecraft:raw_iron", 1, 1)

	srv.RunLine("tick freeze")
	// One whole frozen tick settles the state; the twenty after it are the
	// ones nothing may move on.
	srv.RunTick()
	spent, total, lit, _, ok := srv.FurnaceAt(4, 64, 0)
	if !ok {
		t.Fatal("the furnace's block entity went missing")
	}
	for range 20 {
		srv.RunTick()
	}
	spent2, total2, lit2, _, _ := srv.FurnaceAt(4, 64, 0)
	if spent2 != spent || total2 != total || lit2 != lit {
		t.Errorf("the frozen furnace cooked: (%d,%d,%d) -> (%d,%d,%d)", spent, total, lit, spent2, total2, lit2)
	}
	if spent != 0 || lit != 0 {
		t.Errorf("the furnace had cooked before the freeze lifted: spent %d, lit %d", spent, lit)
	}

	srv.RunLine("tick unfreeze")
	srv.RunTick()
	if _, _, lit3, _, _ := srv.FurnaceAt(4, 64, 0); lit3 == 0 {
		t.Error("the furnace did not light again after the freeze lifted")
	}
}

// TestAPlayerTakesItemsAndOrbsWhileFrozen: a frozen world still lets the
// player take an item and an experience orb at their feet, the touching a
// player keeps doing while the entities' own ticks are held.
// vanilla: Player.tick, ItemEntity.playerTouch, ExperienceOrb.playerTouch
func TestAPlayerTakesItemsAndOrbsWhileFrozen(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	at := srv.PlayerPosition("Testificate")

	srv.RunLine("tick freeze")
	srv.SpawnItemEntityDelay(at.X, at.Y, at.Z, "minecraft:dirt", 1, 0)
	srv.SpawnOrb(at.X, at.Y, at.Z, 1)
	for range 20 {
		srv.RunTick()
	}
	if n := srv.ItemCount(); n != 0 {
		t.Errorf("the frozen world left %d item(s) unpicked", n)
	}
	if n := srv.OrbCount(); n != 0 {
		t.Errorf("the frozen world left %d orb(s) unpicked", n)
	}
}

// TestAPlayerTakesArrowsWhileFrozen: a frozen world still lets the player
// take a stuck arrow at their feet, the touching a player keeps doing while
// the shaft's own tick is held.
// vanilla: Player.aiStep, AbstractArrow.playerTouch
func TestAPlayerTakesArrowsWhileFrozen(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	at := srv.PlayerPosition("Testificate")

	srv.RunLine("tick freeze")
	id := srv.SpawnStuckArrow("allowed", at.X, at.Y, at.Z)
	if id == 0 {
		t.Fatal("SpawnStuckArrow came to nothing")
	}
	for range 20 {
		srv.RunTick()
	}
	if srv.ArrowAlive(id) {
		t.Error("the frozen world left a stuck arrow unpicked")
	}
}

// TestAFrozenWorldStillShowsItemsWalkedIntoRange: the item's own tick is
// held by a freeze, but the tracking is not: a player who walks into an
// item's range is still shown it.
// vanilla: ChunkMap.tick
func TestAFrozenWorldStillShowsItemsWalkedIntoRange(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4 // the sixty-four blocks of sight the range binds to
	srv, c := servertest.Join(t, cfg, "Walker")

	// A hundred blocks east, past the range, and a long pickup delay so the
	// item stays lying through the test.
	id := srv.SpawnItemEntityDelay(100.5, 64, 0.5, "minecraft:dirt", 1, 1000000)
	if id == 0 {
		t.Fatal("SpawnItemEntityDelay came to nothing")
	}

	srv.RunLine("tick freeze")
	servertest.AwaitTicks(t, srv, 2)

	// Fifty blocks short of it: inside the range now, the frozen world owes
	// the player the spawn.
	c.MoveToGround(50.5, 64, 0.5, true)
	got := c.AwaitSpawn(protocol.EntityTypeItem)
	if got.EntityID != id {
		t.Errorf("the frozen world showed item %d, want %d", got.EntityID, id)
	}

	// And back out past the range: the frozen world takes it out again.
	c.MoveToGround(0.5, 64, 0.5, true)
	servertest.AwaitRemovalsOfTicks(t, c, srv, 1200, id)
	c.Disconnect()
}

// TestAFrozenWorldStillShowsOrbsWalkedIntoRange: the orb's own tick is held
// by a freeze, but the tracking is not.
// vanilla: ChunkMap.tick
func TestAFrozenWorldStillShowsOrbsWalkedIntoRange(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4
	srv, c := servertest.Join(t, cfg, "Walker")

	id := srv.SpawnOrb(100.5, 64, 0.5, 1)
	if id == 0 {
		t.Fatal("SpawnOrb came to nothing")
	}

	srv.RunLine("tick freeze")
	servertest.AwaitTicks(t, srv, 2)

	c.MoveToGround(50.5, 64, 0.5, true)
	got := c.AwaitSpawn(protocol.EntityTypeExperienceOrb)
	if got.EntityID != id {
		t.Errorf("the frozen world showed orb %d, want %d", got.EntityID, id)
	}
	c.Disconnect()
}

// TestAFrozenWorldStillShowsMobsWalkedIntoRange: the mobs' own tick is held
// by a freeze, but the tracking is not.
// vanilla: ChunkMap.tick
func TestAFrozenWorldStillShowsMobsWalkedIntoRange(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = 4
	srv, c := servertest.Join(t, cfg, "Walker")

	// Hold the mob's chunk so the herd keeps it while nobody stands there,
	// and plant the cow a hundred blocks east, past the range.
	release := srv.HoldChunk(100, 0)
	defer release()
	id := srv.SpawnMob("minecraft:cow", 100.5, 64, 0.5)
	if id == 0 {
		t.Fatal("SpawnMob came to nothing")
	}

	srv.RunLine("tick freeze")
	servertest.AwaitTicks(t, srv, 2)

	c.MoveToGround(50.5, 64, 0.5, true)
	got := c.AwaitSpawn(protocol.EntityTypeCow)
	if got.EntityID != id {
		t.Errorf("the frozen world showed cow %d, want %d", got.EntityID, id)
	}
	c.Disconnect()
}

// TestAFrozenWorldStillShowsArrowsWalkedIntoRange: the shaft's own tick is
// held by a freeze, but the tracking is not.
// vanilla: ChunkMap.tick
func TestAFrozenWorldStillShowsArrowsWalkedIntoRange(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4
	srv, c := servertest.Join(t, cfg, "Walker")

	release := srv.HoldChunk(100, 0)
	defer release()
	id := srv.SpawnStuckArrow("disallowed", 100.5, 64, 0.5)
	if id == 0 {
		t.Fatal("SpawnStuckArrow came to nothing")
	}

	srv.RunLine("tick freeze")
	servertest.AwaitTicks(t, srv, 2)

	c.MoveToGround(50.5, 64, 0.5, true)
	got := c.AwaitSpawn(protocol.EntityTypeArrow)
	if got.EntityID != id {
		t.Errorf("the frozen world showed arrow %d, want %d", got.EntityID, id)
	}
	c.Disconnect()
}

// TestAFrozenWorldStillShowsThrownWalkedIntoRange: the thrown body's own tick
// is held by a freeze, but the tracking is not.
// vanilla: ChunkMap.tick
func TestAFrozenWorldStillShowsThrownWalkedIntoRange(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4
	srv, c := servertest.Join(t, cfg, "Walker")

	release := srv.HoldChunk(100, 0)
	defer release()
	id := srv.ThrowThrown(entity.ThrownSnowball, 100.5, 100, 0.5, 0, 0, 0, 1)
	if id == 0 {
		t.Fatal("ThrowThrown came to nothing")
	}

	srv.RunLine("tick freeze")
	servertest.AwaitTicks(t, srv, 2)

	c.MoveToGround(50.5, 64, 0.5, true)
	got := c.AwaitSpawn(protocol.EntityTypeSnowball)
	if got.EntityID != id {
		t.Errorf("the frozen world showed thrown body %d, want %d", got.EntityID, id)
	}
	c.Disconnect()
}

// TestAFrozenWorldStillShowsTNTWalkedIntoRange: the fuse's own tick is held
// by a freeze, but the tracking is not.
// vanilla: ChunkMap.tick
func TestAFrozenWorldStillShowsTNTWalkedIntoRange(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4
	srv, c := servertest.Join(t, cfg, "Walker")

	release := srv.HoldChunk(100, 0)
	defer release()
	id := srv.SpawnTNT(100.5, 100, 0.5, 1000000)
	if id == 0 {
		t.Fatal("SpawnTNT came to nothing")
	}

	srv.RunLine("tick freeze")
	servertest.AwaitTicks(t, srv, 2)

	c.MoveToGround(50.5, 64, 0.5, true)
	got := c.AwaitSpawn(protocol.EntityTypeTnt)
	if got.EntityID != id {
		t.Errorf("the frozen world showed TNT %d, want %d", got.EntityID, id)
	}
	c.Disconnect()
}

// TestAFrozenWorldStillShowsFallingBlocksWalkedIntoRange: the fall's own tick
// is held by a freeze, but the tracking is not.
// vanilla: ChunkMap.tick
func TestAFrozenWorldStillShowsFallingBlocksWalkedIntoRange(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4
	srv, c := servertest.Join(t, cfg, "Walker")

	release := srv.HoldChunk(100, 0)
	defer release()
	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}
	id := srv.SpawnFallingBlock(100, 100, 0, sand)
	if id == 0 {
		t.Fatal("SpawnFallingBlock came to nothing")
	}

	srv.RunLine("tick freeze")
	servertest.AwaitTicks(t, srv, 2)

	c.MoveToGround(50.5, 64, 0.5, true)
	got := c.AwaitSpawn(protocol.EntityTypeFallingBlock)
	if got.EntityID != id {
		t.Errorf("the frozen world showed falling block %d, want %d", got.EntityID, id)
	}
	c.Disconnect()
}

// TestAFrozenWorldStillShowsBobbersWalkedIntoRange: the hook's own tick is
// held by a freeze, but the tracking is not.
// vanilla: ChunkMap.tick
func TestAFrozenWorldStillShowsBobbersWalkedIntoRange(t *testing.T) {
	t.Parallel()
	cfg := servertest.BowTestConfig()
	cfg.ViewDistance = 4
	srv, connect := servertest.Start(t, cfg)
	angler := connect().Join("Angler")
	srv.GiveSlot("Angler", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	walker := connect().Join("Walker")
	walker.MoveToGround(50.5, 64, 0.5, true)

	angler.MoveTo(0.5, 64, 0.5, 0, 0)
	servertest.UseHeld(angler, 0, 0)
	_, spawns, _, _, _, _ := servertest.FishingRead(angler, srv, 10)
	cast := servertest.SpawnsOf(spawns, protocol.EntityTypeFishingBobber)
	if len(cast) != 1 {
		t.Fatalf("the cast spawned %d fishing bobbers, want the one rod's", len(cast))
	}
	id := cast[0].EntityID

	srv.RunLine("tick freeze")
	servertest.AwaitTicks(t, srv, 2)

	walker.MoveToGround(0.5, 64, 0.5, true)
	got := walker.AwaitSpawn(protocol.EntityTypeFishingBobber)
	if got.EntityID != id {
		t.Errorf("the frozen world showed bobber %d, want %d", got.EntityID, id)
	}
	walker.Disconnect()
	angler.Disconnect()
}

// TestAFrozenWorldStillShowsPaintingsWalkedIntoRange: the survives poll is
// held by a freeze, but the tracking is not.
// vanilla: ChunkMap.tick
func TestAFrozenWorldStillShowsPaintingsWalkedIntoRange(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4
	srv, connect := servertest.Start(t, cfg)
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.PaintingItem(t), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	id := watcher.AwaitSpawn(protocol.EntityTypePainting).EntityID

	srv.RunLine("tick freeze")
	servertest.AwaitTicks(t, srv, 2)

	// Out past the range: the frozen world takes the painting out of view.
	watcher.MoveToGround(100.5, 64, 0.5, true)
	servertest.AwaitRemovalsOfTicks(t, watcher, srv, 1200, id)

	// And back in: it is shown again, still frozen.
	watcher.MoveToGround(50.5, 64, 0.5, true)
	got := watcher.AwaitSpawn(protocol.EntityTypePainting)
	if got.EntityID != id {
		t.Errorf("the frozen world showed painting %d, want %d", got.EntityID, id)
	}
	actor.Disconnect()
	watcher.Disconnect()
}

// TestAFrozenWorldStillShowsBoatsWalkedIntoRange: the driverless hull's own
// tick is held by a freeze, but the tracking is not: a player who walks into
// its range is still shown it, and one who walks out is still taken out of
// its view.
// vanilla: ChunkMap.tick
func TestAFrozenWorldStillShowsBoatsWalkedIntoRange(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4
	srv, c := servertest.Join(t, cfg, "Walker")

	release := srv.HoldChunk(100, 0)
	defer release()
	id := srv.SpawnBoat(100.5, 64, 0.5, "minecraft:oak_boat")
	if id == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}

	srv.RunLine("tick freeze")
	servertest.AwaitTicks(t, srv, 2)

	// In range: the frozen world shows the hull.
	c.MoveToGround(50.5, 64, 0.5, true)
	got := c.AwaitSpawn(protocol.EntityTypeOakBoat)
	if got.EntityID != id {
		t.Errorf("the frozen world showed boat %d, want %d", got.EntityID, id)
	}

	// Out of range: the frozen world takes it out again.
	c.MoveToGround(0.5, 64, 0.5, true)
	servertest.AwaitRemovalsOfTicks(t, c, srv, 1200, id)
	c.Disconnect()
}

// TestAFrozenWorldStillShowsStandsWalkedIntoRange: the stand's own tick is
// held by a freeze, but the tracking is not: a player who walks into its
// range is still shown it.
// vanilla: ChunkMap.tick
func TestAFrozenWorldStillShowsStandsWalkedIntoRange(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4
	srv, c := servertest.Join(t, cfg, "Walker", servertest.FullSimulationDistance)

	srv.RunLine("summon minecraft:armor_stand 1.5 64 1.5")
	id := c.AwaitSpawn(protocol.EntityTypeArmorStand).EntityID

	// Away past the range: the unfrozen world takes the stand out of view.
	c.MoveToGround(100.5, 64, 0.5, true)
	servertest.AwaitRemovalsOfTicks(t, c, srv, 1200, id)

	srv.RunLine("tick freeze")
	servertest.AwaitTicks(t, srv, 2)

	// Back inside the range, still frozen: the stand is shown again.
	c.MoveToGround(50.5, 64, 0.5, true)
	got := c.AwaitSpawn(protocol.EntityTypeArmorStand)
	if got.EntityID != id {
		t.Errorf("the frozen world showed stand %d, want %d", got.EntityID, id)
	}
	c.Disconnect()
}

// TestAFrozenWorldHoldsTheStandsBurn: a burning stand's own tick is held by
// a freeze, so the fire never bites and a stand one burn would break keeps
// standing.
// vanilla: TickRateManager.isEntityFrozen, ArmorStand.tick
func TestAFrozenWorldHoldsTheStandsBurn(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Walker")

	srv.RunLine("tick freeze")
	servertest.AwaitTicks(t, srv, 2)

	// Four points of health: one of the burn's four-point bites takes the
	// stand down.
	srv.RunLine("summon minecraft:armor_stand 1.5 64 1.5 {Fire:100s,Health:4.0f}")
	id := c.AwaitSpawn(protocol.EntityTypeArmorStand).EntityID

	servertest.AwaitTicks(t, srv, 25)
	if got := servertest.StandsStanding(srv); got != 1 {
		t.Errorf("the frozen world left %d stands, want the burn held and the stand standing", got)
	}
	servertest.ExpectNoRemovalOfTicks(t, c, srv, 4, id)
	c.Disconnect()
}

// TestTickRateSetsTheLoopPace: /tick rate asks the loop to wait out the
// spacing of the new rate between its ticks — two a second here, after the
// twenty a second it starts at — and the console reads the rate back in
// the success line.
func TestTickRateSetsTheLoopPace(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	clock := newGateClock()
	srv, _ := servertest.Start(t, cfg, func(s *server.Server) { s.SetTickClock(clock, nil) })

	out := srv.RunLine("tick rate 2")
	if len(out) != 1 || out[0].Translate != "commands.tick.rate.success" ||
		len(out[0].With) != 1 || out[0].With[0].Text != "2.0" {
		t.Errorf("tick rate answer = %+v, want the one 2.0 rate.success line", out)
	}

	// The loop's first wait is on the spacing it started at; every wait
	// after waits the 500ms the rate of two paces by. One wait more is
	// released than paced, so the reads race no partial wait.
	clock.step()
	for range 6 {
		clock.step()
	}
	assertPacing(t, clock,
		50*time.Millisecond,
		500*time.Millisecond, 500*time.Millisecond, 500*time.Millisecond,
		500*time.Millisecond, 500*time.Millisecond, 500*time.Millisecond)
	if ticks := srv.TicksRun(); ticks < 6 {
		t.Errorf("the loop ran %d ticks, want at least the six released", ticks)
	}
}

// TestTickSprintRunsItsTicksBackToBack: a sprint's claimed ticks pace the
// loop at zero — back to back with no wait between them — and the sprint
// ends on its own with the report line, the pacing back at the rate's
// spacing after it.
func TestTickSprintRunsItsTicksBackToBack(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	clock := newGateClock()
	var logged strings.Builder
	log := slog.New(slog.NewTextHandler(&logged, nil))
	srv, _ := servertest.Start(t, cfg, func(s *server.Server) { s.SetTickClock(clock, log) })

	out := srv.RunLine("tick sprint 20")
	if len(out) != 1 || out[0].Translate != "commands.tick.status.sprinting" {
		t.Errorf("tick sprint answer = %+v, want the sprinting status line", out)
	}

	// The initial wait is the start's; the twenty sprint ticks then run
	// with no wait between them; the iteration that finishes the sprint
	// paces the plain rate spacing again, and so does the one after it.
	clock.step()
	for range 22 {
		clock.step()
	}
	wants := []time.Duration{50 * time.Millisecond}
	for range 20 {
		wants = append(wants, 0)
	}
	wants = append(wants, 50*time.Millisecond, 50*time.Millisecond)
	assertPacing(t, clock, wants...)
	if ticks := srv.TicksRun(); ticks < 21 {
		t.Errorf("the loop ran %d ticks, want at least the twenty-one a finished sprint left", ticks)
	}
	if !strings.Contains(logged.String(), "Sprint completed with") {
		t.Errorf("the finished sprint did not report:\n%s", logged.String())
	}
}

// TestTickSprintStopsAndFreezeInterrupts: /tick sprint stop ends a sprint
// with the report line and its own stopped message, and a freeze takes a
// sprint down the same way before its own status line, restoring the state
// the sprint found before it freezes.
func TestTickSprintStopsAndFreezeInterrupts(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	sprints := []struct {
		name   string
		stop   string
		freeze bool
	}{
		{name: "tick sprint stop", stop: "tick sprint stop"},
		{name: "tick freeze", stop: "tick freeze", freeze: true},
	}
	for _, s := range sprints {
		c.ChatCommand("tick sprint 100000")
		c.ExpectTickingState(20, false)
		c.ExpectCommandFeedback("commands.tick.status.sprinting")
		servertest.UntilTicks(t, srv, 600, "condition", func() bool { return srv.TicksRun() >= 20 })

		c.ChatCommand(s.stop)
		c.AwaitSprintReport()
		// The sprint found the world unfrozen and restores that.
		c.ExpectTickingState(20, false)
		if s.freeze {
			c.ExpectTickingState(20, true)
			c.ExpectCommandFeedback("commands.tick.status.frozen")
		} else {
			c.ExpectCommandFeedback("commands.tick.sprint.stop.success")
			c.ChatCommand("tick sprint stop")
			c.ExpectFailure("commands.tick.sprint.stop.fail")
		}
		c.Settle()
	}
	c.Disconnect()
}

// TestTickQueryNamesTheFrozenAndSprintingState: the query's status line
// answers the state the game is in, the frozen one while it is frozen and
// the sprinting one while a sprint turns.
func TestTickQueryNamesTheFrozenAndSprintingState(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("tick freeze")
	c.ExpectTickingState(20, true)
	c.ExpectCommandFeedback("commands.tick.status.frozen")
	servertest.AwaitTicks(t, srv, 5)

	c.ChatCommand("tick query")
	c.ExpectCommandFeedback("commands.tick.status.frozen")
	c.ExpectFeedbackLine("commands.tick.query.rate.running")
	c.ExpectFeedbackLine("commands.tick.query.percentiles")

	c.ChatCommand("tick sprint 100000")
	c.ExpectTickingState(20, false)
	c.ExpectCommandFeedback("commands.tick.status.sprinting")
	servertest.UntilTicks(t, srv, 600, "condition", func() bool { return srv.TicksRun() >= 20 })

	c.ChatCommand("tick query")
	c.ExpectCommandFeedback("commands.tick.status.sprinting")
	c.ExpectFeedbackLine("commands.tick.query.rate.sprinting")
	c.ExpectFeedbackLine("commands.tick.query.percentiles")

	// The sprint was asked over a frozen world, so its end restores the
	// freeze.
	c.ChatCommand("tick sprint stop")
	c.AwaitSprintReport()
	c.ExpectTickingState(20, true)
	c.ExpectCommandFeedback("commands.tick.sprint.stop.success")
	c.Disconnect()
}

// TestAFrozenWorldHoldsTheHullsNoOneRides: a freeze holds an empty hull
// standing in mid-air, while the hull a player rides keeps serving its
// driver's reports, the rider carried where the seats hold them.
func TestAFrozenWorldHoldsTheHullsNoOneRides(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Frosty", 0)

	srv.RunLine("tick freeze")
	c.ExpectTickingState(20, true)
	// A tick in flight when the freeze landed may still step the world;
	// the boats are stood once one whole frozen tick has run.
	servertest.AwaitTicks(t, srv, 2)

	empty := srv.SpawnBoat(2.5, 70, 0.5, "minecraft:oak_boat")
	ridden := srv.SpawnBoat(6.5, 70, 0.5, "minecraft:oak_boat")
	if empty == 0 || ridden == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.MoveToGrounded(8.3, 70, 0.5, 0, 0, false)
	c.Settle()
	c.UseOn(ridden)
	servertest.AwaitPassengersFor(c, ridden)

	// The frozen world still takes the driver's report of the boat.
	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(5.5)
		b.PutF64(70)
		b.PutF64(0.5)
		b.PutF32(90)
		b.PutF32(0)
		b.PutBool(false)
	})
	servertest.UntilTicks(t, srv, 200, "condition", func() bool {
		x, _, _, _, _, ok := srv.BoatState(ridden)
		return ok && x == 5.5
	})

	servertest.AwaitTicks(t, srv, 12)
	if x := srv.PlayerPosition("Frosty").X; x != 5.5 {
		t.Errorf("the rider sits at x=%v, want the seat of the boat they steered", x)
	}
	if _, emptyY, _, _, _, ok := srv.BoatState(empty); !ok || emptyY != 70 {
		t.Errorf("the frozen empty hull stands at y=%v, want the 70 it was stood at", emptyY)
	}

	srv.RunLine("tick unfreeze")
	c.ExpectTickingState(20, false)
	servertest.AwaitTicks(t, srv, 12)
	if _, emptyY, _, _, _, _ := srv.BoatState(empty); emptyY >= 70 {
		t.Errorf("the released hull stands at y=%v, want its fall running", emptyY)
	}
	c.Disconnect()
}

// TestTickingStateFollowsAJoiningPlayer: the state the game is in reaches
// a player who joins in the Set Ticking State and Step Tick packets the
// join already carries.
func TestTickingStateFollowsAJoiningPlayer(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate"))

	op := connect().Join("Testificate")

	op.ChatCommand("tick freeze")
	op.ExpectTickingState(20, true)
	op.ExpectCommandFeedback("commands.tick.status.frozen")

	c := connect()
	c.Login("Bee")
	c.Configure()
	var state, step []byte
	c.ReadJoin(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayTickingState:
			state = data
		case protocol.IDPlayTickingStep:
			step = data
		}
	})
	if state == nil {
		t.Fatal("the joining player was told no ticking state")
	}
	r := protocol.NewReader(state)
	if got := r.ReadF32(); got != 20 {
		t.Errorf("joined tick rate = %v, want 20", got)
	}
	if got := r.ReadBool(); !got {
		t.Error("the joining player was told the frozen world is running")
	}
	if err := r.Err(); err != nil {
		t.Fatalf("set ticking state: %v", err)
	}
	if step == nil {
		t.Fatal("the joining player was told no tick steps")
	}
	if got := protocol.NewReader(step).ReadVarInt(); got != 0 {
		t.Errorf("joined tick steps = %d, want 0", got)
	}
	c.MarkLoaded()
	c.Disconnect()
	op.Disconnect()
}

// TestTickStepWhenNotFrozenStoresNoSuccess checks that the /tick refusals
// throw, so execute store success reads 0, and that the subcommands answer
// what vanilla's return: the rate as an int, 1 for a freeze, 0 for an
// unfreeze.
// vanilla: TickCommand.step, tickQuery, setTickingRate, setFreeze
func TestTickStepWhenNotFrozenStoresNoSuccess(t *testing.T) {
	t.Parallel()
	srv, c := joinOpForStore(t)
	c.ChatCommand("tick step")
	servertest.AwaitCommandError(t, c, "commands.tick.step.fail")
	for _, cmd := range []string{"tick step", "tick step stop", "tick sprint stop"} {
		if got := storedSuccess(t, srv, c, cmd); got != "0" {
			t.Errorf("%s stored success %s, want 0", cmd, got)
		}
	}
	expectStored(t, srv, c, [][2]string{
		{"tick query", "20"},
		{"tick rate 40.5", "40"},
		{"tick freeze", "1"},
		{"tick unfreeze", "0"},
		{"tick rate 20", "20"},
	})
}
