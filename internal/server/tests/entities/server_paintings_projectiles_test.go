package entities

// The projectile tests of the paintings: the shots and the thrown bodies a
// hit takes one down at, hung the frames' way — the per-tick sweep meets
// the sliver the painting occupies ahead of the wall behind it, and the
// hurt runs through the same end a blow's and a blast's do. Each kind the
// server throws or shoots has its own test, and the rules the hurt reads
// — mob griefing, the creative hand that drops nothing — theirs.

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
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
