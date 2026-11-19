package blocks

// The blocks a projectile's landing answers whatever its fire: the bell
// rings, the chorus flower and decorated pot break, the amethyst chimes, the
// big dripleaf tilts down and a fast trident breaks a speleothem.
// vanilla: BellBlock.onProjectileHit, ChorusFlowerBlock.onProjectileHit,
// DecoratedPotBlock.onProjectileHit, AmethystBlock.onProjectileHit,
// BigDripleafBlock.onProjectileHit, SpeleothemBlock.onProjectileHit

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// shotItems counts the dropped item entities of the overworld by name.
// flameScene stops the loop and returns the server a burning arrow is flown
// against, the placement of the block under test left to the caller.
func flameScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()
	return srv, c
}

func shotItems(srv *server.Server, name string) int {
	n := 0
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == name {
			n += int(it.Count)
		}
	}
	return n
}

// TestAnArrowRingsTheBellItHits flies a plain shaft into the side of a floor
// bell: the bell's block action and sound go out, swung from the face hit.
func TestAnArrowRingsTheBellItHits(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)

	if !srv.SetBlock(6, 65, 0, servertest.BellState(t, "floor", "north", false)) {
		t.Fatal("could not stand the bell")
	}
	srv.ShootArrow(6.5, 65.4, 3, 0, 0, -3)
	servertest.DriveTargetTicks(srv, 8)

	actions, sounds := 0, 0
	chime, _ := registry.SoundEventID("minecraft:block.bell.use")
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayBlockAction:
			p := protocol.NewReader(data)
			x, y, z := p.ReadPosition()
			action, dir, block := int32(p.ReadU8()), int32(p.ReadU8()), p.ReadVarInt()
			if x == 6 && y == 65 && z == 0 && action == 1 && dir == int32(protocol.FaceSouth) && block == protocol.BlockBell {
				actions++
			}
		case protocol.IDPlaySoundEffect:
			if protocol.NewReader(data).ReadVarInt()-1 == chime {
				sounds++
			}
		}
	})
	if actions != 1 || sounds != 1 {
		t.Errorf("bell rings = %d actions and %d sounds, want 1 of each", actions, sounds)
	}
}

// TestAnArrowBreaksTheChorusFlowerItHits flies a shaft into a chorus flower:
// it breaks and drops itself.
func TestAnArrowBreaksTheChorusFlowerItHits(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	if !srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:chorus_flower", map[string]string{"age": "0"})) {
		t.Fatal("could not place the flower")
	}
	srv.ShootArrow(8, 65.5, 0.5, -3, 0, 0)
	servertest.DriveTargetTicks(srv, 8)

	if got := srv.BlockAt(6, 65, 0); got != 0 {
		t.Errorf("the chorus flower = %d, want air", got)
	}
	if n := shotItems(srv, "minecraft:chorus_flower"); n != 1 {
		t.Errorf("chorus flowers dropped = %d, want 1", n)
	}
}

// TestAnArrowBreaksTheDecoratedPotItHits flies a shaft into a decorated pot
// set with no decorations: it breaks cracked, and its EMPTY sides drop
// nothing.
// vanilla: DecoratedPotBlock.getDrops, DecoratedPotBlockEntity.loadAdditional
func TestAnArrowBreaksTheDecoratedPotItHits(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	if !srv.SetBlock(6, 65, 0, servertest.BlockState(t, "minecraft:decorated_pot")) {
		t.Fatal("could not place the pot")
	}
	srv.ShootArrow(8, 65.5, 0.5, -3, 0, 0)
	servertest.DriveTargetTicks(srv, 8)

	if got := srv.BlockAt(6, 65, 0); got != 0 {
		t.Errorf("the decorated pot = %d, want air", got)
	}
	if items := srv.ItemsInDimension(world.DimensionName); len(items) != 0 {
		t.Errorf("the bare pot dropped %v, want nothing", items)
	}
}

// TestAnArrowChimesTheAmethystItHits flies a shaft into an amethyst block:
// the chime plays and the block stays.
func TestAnArrowChimesTheAmethystItHits(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)

	amethyst := servertest.BlockState(t, "minecraft:amethyst_block")
	if !srv.SetBlock(6, 65, 0, amethyst) {
		t.Fatal("could not place the amethyst")
	}
	srv.ShootArrow(8, 65.5, 0.5, -3, 0, 0)
	servertest.DriveTargetTicks(srv, 8)

	chime, _ := registry.SoundEventID("minecraft:block.amethyst_block.chime")
	chimes := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect && protocol.NewReader(data).ReadVarInt()-1 == chime {
			chimes++
		}
	})
	if chimes != 1 {
		t.Errorf("amethyst chimes = %d, want 1", chimes)
	}
	if got := srv.BlockAt(6, 65, 0); got != amethyst {
		t.Errorf("the amethyst = %d, want it standing", got)
	}
}

// TestAnArrowTiltsTheBigDripleafItHits flies a shaft into a big dripleaf: it
// tilts fully down, and rights itself a hundred ticks on.
func TestAnArrowTiltsTheBigDripleafItHits(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	if !srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:big_dripleaf", map[string]string{"tilt": "none"})) {
		t.Fatal("could not place the leaf")
	}
	srv.ShootArrow(8, 65.8, 0.5, -3, 0, 0)
	servertest.DriveTargetTicks(srv, 8)

	if _, props := servertest.StateProps(srv.BlockAt(6, 65, 0)); props["tilt"] != "full" {
		t.Fatalf("the leaf's tilt = %q, want full", props["tilt"])
	}
	servertest.DriveTargetTicks(srv, 110)
	if _, props := servertest.StateProps(srv.BlockAt(6, 65, 0)); props["tilt"] != "none" {
		t.Errorf("the leaf's tilt after the hold = %q, want none", props["tilt"])
	}
}

// TestAFastTridentBreaksTheDripstoneItHits flies a trident past 0.6 blocks a
// tick into a pointed dripstone: it breaks; an arrow leaves it be.
func TestAFastTridentBreaksTheDripstoneItHits(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	stone := servertest.BlockState(t, "minecraft:pointed_dripstone")
	if !srv.SetBlock(6, 65, 0, stone) || !srv.SetBlock(6, 65, 4, stone) {
		t.Fatal("could not place the dripstone")
	}
	srv.ShootArrow(8, 65.5, 4.5, -3, 0, 0)
	srv.ShootTrident(8, 65.5, 0.5, -3, 0, 0)
	servertest.DriveTargetTicks(srv, 8)

	if got := srv.BlockAt(6, 65, 0); got != 0 {
		t.Errorf("the dripstone hit by the trident = %d, want air", got)
	}
	if got := srv.BlockAt(6, 65, 4); got != stone {
		t.Errorf("the dripstone hit by the arrow = %d, want the %d it stood as", got, stone)
	}
}
