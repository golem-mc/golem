package entities

// The projectile tests of the paintings: the shots and the thrown bodies a
// hit takes one down at, hung the frames' way — the per-tick sweep meets
// the sliver the painting occupies ahead of the wall behind it, and the
// hurt runs through the same end a blow's and a blast's do. Each kind the
// server throws or shoots has its own test, and the rules the hurt reads
// — mob griefing, the creative hand that drops nothing — theirs.

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The kebab the shot tests aim at: one block square on the north face of
// the standard wall, its box's centre (shotPaintingX, shotPaintingY,
// shotPaintingZ) the place both its break sound plays from and its drop
// falls from.
const (
	shotPaintingX = 4.5
	shotPaintingY = 64.5
	shotPaintingZ = 4.96875
	// shotPaintingZ8 is the box centre's Z in the eighths the sound
	// positions travel in, the wire carrying the 39.75 of it truncated.
	shotPaintingZ8 = 39
)

// hangShotPainting hangs a kebab on the north face of the standard wall
// from a creative hand and returns it as the watcher saw it arrive. The
// one-by-one keeps the box the shots fly at small and known; the stack's
// variant component pins it against create's roll.
func hangShotPainting(t *testing.T, srv *server.Server, actor, watcher *servertest.Client) servertest.PaintingSpawn {
	t.Helper()
	actor.SetCreativeSlot(player.HotbarSlot, servertest.PaintingStack(t, servertest.PaintingVariantID(t, "minecraft:kebab")))
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	got := watcher.ExpectPaintingSpawn(t, protocol.FaceNorth, 4, 64, 4)
	if kebab := servertest.PaintingVariantID(t, "minecraft:kebab"); got.Variant != kebab {
		t.Fatalf("the hung painting's variant = %d, want the stack's own kebab %d", got.Variant, kebab)
	}
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.place", shotPaintingX, shotPaintingY, shotPaintingZ)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)
	if n := srv.PaintingCount(); n != 1 {
		t.Fatalf("the server holds %d paintings, want the hung one", n)
	}
	return got
}

// TestAnArrowBreaksAPainting flies an ownerless shaft through the cell a
// kebab hangs in: the sweep meets the sliver ahead of the wall behind it,
// and the hurt takes the painting down at one blow whatever the damage —
// the removal out of every view, the break sound at the box's centre, and
// the item laid, an ownerless shaft carrying no creative hand to hold the
// drop back. The shaft is spent on the hit.
// vanilla: BlockAttachedEntity.hurtServer, Painting.dropItem
func TestAnArrowBreaksAPainting(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	hung := hangShotPainting(t, srv, actor, watcher)

	// The shaft's line must miss the shooters: the actor steps out of it.
	actor.MoveToGround(2.5, 64, 3.5, true)
	actor.Settle()

	if id := srv.ShootArrow(shotPaintingX, shotPaintingY, 1.5, 0, 0, 10); id == 0 {
		t.Fatal("the arrow never flew")
	}
	watcher.ExpectPaintingRemoval(t, hung.EntityID)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.break", shotPaintingX, shotPaintingY, shotPaintingZ)
	if dropped := servertest.ExpectDroppedStack(t, watcher); dropped.ItemID != servertest.PaintingItem(t) || dropped.Count != 1 {
		t.Fatalf("the shot laid %+v, want the painting item", dropped)
	}
	// The landed hit plays the shaft's own sound, after the break's drop
	// and before the shaft's removal.
	// vanilla: AbstractArrow.onHitEntity
	servertest.ExpectSoundEvent(t, watcher, "minecraft:entity.arrow.hit")
	if n := srv.PaintingCount(); n != 0 {
		t.Errorf("the server holds %d paintings after the shot, want none", n)
	}
	if n := srv.ArrowCount(); n != 0 {
		t.Errorf("the server holds %d arrows after the shot, want the spent shaft gone", n)
	}
}

// TestATridentBreaksAPaintingAndIsNotSpent throws a trident into the cell
// a kebab hangs in: the sweep meets the sliver ahead of the wall behind it
// and the hurt takes the painting down, while the trident itself is never
// spent on the hit — it plays its own item.trident.hit voice, deflects back
// off the reverse, and stays in the world.
// vanilla: BlockAttachedEntity.hurtServer, ThrownTrident.onHitEntity
func TestATridentBreaksAPaintingAndIsNotSpent(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	hung := hangShotPainting(t, srv, actor, watcher)

	// The thrower out of the trident's line.
	actor.MoveToGround(2.5, 64, 3.5, true)
	actor.Settle()

	trident := srv.ShootTrident(shotPaintingX, shotPaintingY, 1.5, 0, 0, 10)
	if trident == 0 {
		t.Fatal("the trident never flew")
	}
	watcher.ExpectPaintingRemoval(t, hung.EntityID)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.break", shotPaintingX, shotPaintingY, shotPaintingZ)
	if dropped := servertest.ExpectDroppedStack(t, watcher); dropped.ItemID != servertest.PaintingItem(t) || dropped.Count != 1 {
		t.Fatalf("the throw laid %+v, want the painting item", dropped)
	}
	// The trident's own voice, not the arrow's, before it flies on.
	servertest.ExpectSoundEvent(t, watcher, "minecraft:item.trident.hit")
	if n := srv.PaintingCount(); n != 0 {
		t.Errorf("the server holds %d paintings after the throw, want none", n)
	}
	if !srv.ArrowAlive(trident) {
		t.Error("the trident was spent on the painting, want it deflected and still in the world")
	}
}

// TestACreativePlayersArrowBreaksAPaintingWithoutDropping shoots the
// actor's own shaft into a kebab: the hurt takes the painting down the
// same, while the dropItem of it skips the item for a shooter whose
// infinite materials leave no item to lay.
// vanilla: BlockAttachedEntity.hurtServer, Painting.dropItem
func TestACreativePlayersArrowBreaksAPaintingWithoutDropping(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	hung := hangShotPainting(t, srv, actor, watcher)

	actor.MoveToGround(2.5, 64, 3.5, true)
	actor.Settle()

	if id := srv.ShootPlayerArrow("Actor", shotPaintingX, shotPaintingY, 1.5, 0, 0, 10); id == 0 {
		t.Fatal("the actor's arrow never flew")
	}
	watcher.ExpectPaintingRemoval(t, hung.EntityID)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.break", shotPaintingX, shotPaintingY, shotPaintingZ)
	servertest.HoldTicks(t, srv)
	if servertest.ASpawnOfWithinTicks(watcher, srv, 60, protocol.EntityTypeItem) {
		t.Error("a creative shooter's shot laid the painting's item")
	}
	if n := srv.PaintingCount(); n != 0 {
		t.Errorf("the server holds %d paintings after the shot, want none", n)
	}
}

// TestASkeletonsArrowCannotBreakAPaintingWithMobGriefingOff flies a mob's
// own shaft into a kebab with mob_griefing off: the hurt is refused the way
// a mob's block griefing is — the painting stands, no sound, nothing laid
// — and the shaft deflects off rather than dying on the hit. The rule back
// on, the same shaft takes the painting down, the mob's hand dropping what
// its earlier refusal held back.
// vanilla: BlockAttachedEntity.hurtServer, Painting.dropItem
func TestASkeletonsArrowCannotBreakAPaintingWithMobGriefingOff(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	hung := hangShotPainting(t, srv, actor, watcher)
	srv.SetGameRule("mob_griefing", false)

	actor.MoveToGround(2.5, 64, 3.5, true)
	actor.Settle()

	skeleton := srv.SpawnMob("minecraft:skeleton", 0.5, 64, 0.5)
	if skeleton == 0 {
		t.Fatal("the skeleton never stood up")
	}

	shaft := srv.ShootMobArrowFrom(skeleton, shotPaintingX, shotPaintingY, 1.5, 0, 0, 10)
	if shaft == 0 {
		t.Fatal("the skeleton's arrow never flew")
	}
	// The refusal's whole telling: no removal, no sound, no drop.
	breaking, hasBreaking := registry.SoundEventID("minecraft:entity.painting.break")
	if !hasBreaking {
		t.Fatal("the sound registry carries no painting break sound")
	}
	removed, sounded, dropped := false, false, false
	watcher.ReadTicks(srv, 40, func(packetID int32, data []byte) bool {
		switch packetID {
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for i, n := int32(0), r.ReadVarInt(); i < n; i++ {
				if r.ReadVarInt() == hung.EntityID {
					removed = true
				}
			}
		case protocol.IDPlaySoundEffect:
			if protocol.NewReader(data).ReadVarInt() == breaking+1 {
				sounded = true
			}
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeItem {
				dropped = true
			}
		}
		return false
	})
	if removed || sounded || dropped {
		t.Errorf("the refused hurt removed = %v sounded = %v dropped = %v, want no telling at all",
			removed, sounded, dropped)
	}
	if n := srv.PaintingCount(); n != 1 {
		t.Fatalf("the skeleton's arrow broke the painting with mob_griefing off: %d left", n)
	}
	if !srv.ArrowAlive(shaft) {
		t.Error("the refused hurt spent the shaft, want it deflected and still in the world")
	}

	// The rule back on: the same shaft's next flight takes it down.
	srv.SetGameRule("mob_griefing", true)
	if id := srv.ShootMobArrowFrom(skeleton, shotPaintingX, shotPaintingY, 1.5, 0, 0, 10); id == 0 {
		t.Fatal("the skeleton's second arrow never flew")
	}
	watcher.ExpectPaintingRemoval(t, hung.EntityID)
	watcher.ExpectPaintingSound(t, "minecraft:entity.painting.break", shotPaintingX, shotPaintingY, shotPaintingZ)
	if dropped := servertest.ExpectDroppedStack(t, watcher); dropped.ItemID != servertest.PaintingItem(t) || dropped.Count != 1 {
		t.Fatalf("the breaking shot laid %+v, want the painting item", dropped)
	}
	if n := srv.PaintingCount(); n != 0 {
		t.Errorf("the skeleton's arrow left %d paintings with mob_griefing on, want none", n)
	}
}

// TestASnowballBreaksAPainting throws a snowball into the cell a kebab
// hangs in: the sweep meets the sliver ahead of the wall behind it, and
// the landing's hurt takes the painting down — the removal, the break
// sound at the box's centre, the item laid, the thrower behind the ball
// no creative hand — before the ball's own break follows: the entity
// event of its end and its body out of every view.
// vanilla: BlockAttachedEntity.hurtServer, Painting.dropItem
func TestASnowballBreaksAPainting(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	hung := hangShotPainting(t, srv, actor, watcher)

	// The thrower out of the line the ball flies.
	actor.MoveToGround(2.5, 64, 3.5, true)
	actor.Settle()

	servertest.HoldTicks(t, srv)
	ball := srv.ThrowThrown(entity.ThrownSnowball, shotPaintingX, shotPaintingY, 1.5, 0, 0, 0, 7)
	if ball == 0 {
		t.Fatal("the snowball never flew")
	}

	traffic := servertest.ReadThrownTraffic(watcher, srv, 80)
	threw := traffic.SpawnOfType(protocol.EntityTypeSnowball)
	if threw == nil || threw.EntityID != ball {
		t.Fatalf("the throw's spawn = %#v, want the snowball %d", threw, ball)
	}
	// The hurt runs ahead of the ball's own break.
	if !traffic.RemovedOnce(hung.EntityID) {
		t.Error("the snowball's hit did not take the painting out of every view exactly once")
	}
	broke := false
	for _, s := range traffic.Sounds {
		if s.Name == "minecraft:entity.painting.break" &&
			s.X == shotPaintingX && s.Y == shotPaintingY && int32(s.Z*8) == shotPaintingZ8 {
			broke = true
		}
	}
	if !broke {
		t.Errorf("no painting break sound played from the box's centre: %+v", traffic.Sounds)
	}
	// The drop: the painting's own item, one to a stack.
	drop := traffic.SpawnOfType(protocol.EntityTypeItem)
	if drop == nil {
		t.Fatal("the break laid no item")
	}
	laid := false
	for _, m := range traffic.MetasOf(drop.EntityID) {
		if m.Item == servertest.PaintingItem(t) && m.Count == 1 {
			laid = true
		}
	}
	if !laid {
		t.Errorf("the drop's metadata = %+v, want the painting item", traffic.MetasOf(drop.EntityID))
	}
	// The ball's own break after the hurt: one entity event, one removal.
	if events := traffic.EventsOf(ball); len(events) != 1 || events[0].Status != protocol.EntityStatusDeath {
		t.Errorf("the snowball's entity events = %#v, want the break's one", events)
	}
	if !traffic.RemovedOnce(ball) {
		t.Error("the snowball was not taken out of the world exactly once")
	}
	if n := srv.PaintingCount(); n != 0 {
		t.Errorf("the server holds %d paintings after the throw, want none", n)
	}
}

// TestAThrownEggBreaksAPainting is TestASnowballBreaksAPainting with the
// thrown body an egg: the same break's telling, no chick hatched out of
// the seed the flight rolls with.
// vanilla: BlockAttachedEntity.hurtServer, Painting.dropItem
func TestAThrownEggBreaksAPainting(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	hung := hangShotPainting(t, srv, actor, watcher)

	actor.MoveToGround(2.5, 64, 3.5, true)
	actor.Settle()

	seed := int64(-1)
	for candidate := int64(1); candidate < 100000; candidate++ {
		if thrownRolls(candidate) == 0 {
			seed = candidate
			break
		}
	}
	if seed < 0 {
		t.Fatal("no seed under a hundred thousand passed the roll of eight")
	}
	servertest.HoldTicks(t, srv)
	egg := srv.ThrowThrown(entity.ThrownEgg, shotPaintingX, shotPaintingY, 1.5, 0, 0, 0, seed)
	if egg == 0 {
		t.Fatal("the egg never flew")
	}

	traffic := servertest.ReadThrownTraffic(watcher, srv, 80)
	threw := traffic.SpawnOfType(protocol.EntityTypeEgg)
	if threw == nil || threw.EntityID != egg {
		t.Fatalf("the throw's spawn = %#v, want the egg %d", threw, egg)
	}
	if !traffic.RemovedOnce(hung.EntityID) {
		t.Error("the egg's hit did not take the painting out of every view exactly once")
	}
	broke := false
	for _, s := range traffic.Sounds {
		if s.Name == "minecraft:entity.painting.break" &&
			s.X == shotPaintingX && s.Y == shotPaintingY && int32(s.Z*8) == shotPaintingZ8 {
			broke = true
		}
	}
	if !broke {
		t.Errorf("no painting break sound played from the box's centre: %+v", traffic.Sounds)
	}
	if chick := traffic.SpawnOfType(protocol.EntityTypeChicken); chick != nil {
		t.Errorf("the seed that passes the chicken roll hatched chicken %d anyway", chick.EntityID)
	}
	drop := traffic.SpawnOfType(protocol.EntityTypeItem)
	if drop == nil {
		t.Fatal("the break laid no item")
	}
	laid := false
	for _, m := range traffic.MetasOf(drop.EntityID) {
		if m.Item == servertest.PaintingItem(t) && m.Count == 1 {
			laid = true
		}
	}
	if !laid {
		t.Errorf("the drop's metadata = %+v, want the painting item", traffic.MetasOf(drop.EntityID))
	}
	if events := traffic.EventsOf(egg); len(events) != 1 || events[0].Status != protocol.EntityStatusDeath {
		t.Errorf("the egg's entity events = %#v, want the break's one", events)
	}
	if !traffic.RemovedOnce(egg) {
		t.Error("the egg was not taken out of the world exactly once")
	}
	if n := srv.PaintingCount(); n != 0 {
		t.Errorf("the server holds %d paintings after the throw, want none", n)
	}
}

// TestAnEnderPearlBreaksAPaintingAndLandsItsThrower throws a pearl from
// the actor's own hand into the cell a kebab hangs in, aimed by the look:
// the hurt takes the painting down — and drops nothing, the thrower's
// creative hand holding the item back — while the landing's own telling
// still runs: the thrower teleported to the place the pearl stood one tick
// short of the painting, the pearl out of every view exactly once, and
// with none of a break's entity events, its own break being the quiet one
// the client draws for vanilla.
// vanilla: BlockAttachedEntity.hurtServer, Painting.dropItem,
// ThrownEnderpearl.onHit
func TestAnEnderPearlBreaksAPaintingAndLandsItsThrower(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	hung := hangShotPainting(t, srv, actor, watcher)

	// The pearl in the actor's hand, the throw aimed south and a little
	// down from in front of the painting. The second move to the same
	// spot zeros the movement the throw rides.
	actor.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:ender_pearl", 1))
	actor.MoveTo(shotPaintingX, 64, 1.5, 0, 10)
	actor.MoveTo(shotPaintingX, 64, 1.5, 0, 10)
	actor.Settle()
	servertest.HoldTicks(t, srv)
	servertest.UseHeld(actor, 0, 10)

	live := servertest.ReadPearlLive(actor, srv, 80)
	pearls := servertest.SpawnsOf(live.Spawns, protocol.EntityTypeEnderPearl)
	if len(pearls) != 1 {
		t.Fatalf("the throw put %d ender pearls into the world, want one", len(pearls))
	}
	pearl := pearls[0]

	if !live.RemovedOnce(hung.EntityID) {
		t.Error("the pearl's hit did not take the painting out of every view exactly once")
	}
	broke := false
	for _, s := range live.Sounds {
		if s.Name == "minecraft:entity.painting.break" &&
			s.X == shotPaintingX && s.Y == shotPaintingY && int32(s.Z*8) == shotPaintingZ8 {
			broke = true
		}
	}
	if !broke {
		t.Errorf("no painting break sound played from the box's centre: %+v", live.Sounds)
	}
	for _, sp := range live.Spawns {
		if sp.EntityType == protocol.EntityTypeItem {
			t.Error("a creative thrower's hit laid an item")
		}
	}

	// The landing still carries the thrower: one teleport to the place the
	// pearl stood one tick short of the painting, not through the wall.
	if len(live.Syncs) != 1 {
		t.Fatalf("the thrower was told %d teleports, want the landing's one", len(live.Syncs))
	}
	sync := live.Syncs[0]
	if sync.X < 4.0 || sync.X > 5.0 {
		t.Errorf("the teleport put the thrower at x=%v, want the painting's own column", sync.X)
	}
	if sync.Z < 2.0 || sync.Z > 4.7 {
		t.Errorf("the teleport put the thrower at z=%v, want short of the painting, not through it", sync.Z)
	}
	for _, e := range live.Events {
		if e.Id == pearl.EntityID {
			t.Errorf("the pearl's told entity event %d, want the quiet break", e.Status)
		}
	}
	if !live.RemovedOnce(pearl.EntityID) {
		t.Error("the pearl was not taken out of the world exactly once")
	}
	if n := srv.PaintingCount(); n != 0 {
		t.Errorf("the server holds %d paintings after the landing, want none", n)
	}
}

// thrownRolls replays the rolls a thrown egg makes from a seed: the shoot's
// six spread draws, then the roll of eight and the roll of thirty-two. What
// comes back is how many chicks the break hatched.
func thrownRolls(seed int64) (chicks int) {
	rng := rand.New(rand.NewSource(seed))
	for range 6 {
		rng.Float64()
	}
	if rng.Intn(8) != 0 {
		return 0
	}
	n := 1
	if rng.Intn(32) == 0 {
		n = 4
	}
	return n
}
