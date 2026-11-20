package entities

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// landingBurst is one decoded block-particle Level Particles packet.
type landingBurst struct {
	state   int32
	x, y, z float64
	count   int32
}

// standDustAfterDrop drops an armor stand from height blocks above a stone
// floor and returns the block-particle bursts the watcher is sent.
func standDustAfterDrop(t *testing.T, height float64) (bursts []landingBurst, stone uint16) {
	t.Helper()
	srv, c := servertest.BreedingPlayer(t, "creative", "Watcher", "", 0)
	stone = servertest.MustState(t, "minecraft:stone", nil)
	for x := int32(2); x <= 4; x++ {
		for z := int32(0); z <= 2; z++ {
			srv.SetBlock(x, 63, z, stone)
		}
	}
	servertest.HoldTicks(t, srv)
	srv.SpawnArmorStand(3.5, 64+height, 1.5)
	c.Settle()
	servertest.RunTicks(t, srv, 60)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayLevelParticles {
			return
		}
		r := protocol.NewReader(data)
		if particle := r.ReadVarInt(); particle != protocol.ParticleBlock {
			return
		}
		var b landingBurst
		b.state = r.ReadVarInt()
		r.ReadBool()
		r.ReadBool()
		b.x, b.y, b.z = r.ReadF64(), r.ReadF64(), r.ReadF64()
		for range 6 {
			r.ReadF32()
		}
		b.count = r.ReadVarInt()
		r.ReadVarInt()
		if r.Err() != nil || r.Remaining() != 0 {
			t.Errorf("block particle packet misdecoded: err %v, %d bytes left", r.Err(), r.Remaining())
			return
		}
		bursts = append(bursts, b)
	})
	return bursts, stone
}

// An armor stand that falls more than 3 blocks bursts block particles where
// it lands; a shorter fall sends none.
// vanilla: LivingEntity.checkFallDamage
func TestAnArmorStandBurstsBlockParticlesAfterALongFall(t *testing.T) {
	t.Parallel()
	got, stone := standDustAfterDrop(t, 6)
	if len(got) != 1 {
		t.Fatalf("6-block drop sent block bursts %v, want one", got)
	}
	b := got[0]
	if b.state != int32(stone) || b.count <= 0 {
		t.Errorf("burst state %d count %d, want stone %d and count > 0", b.state, b.count, stone)
	}
	if math.Abs(b.x-3.5) > 0.5 || b.y != 64 || math.Abs(b.z-1.5) > 0.5 {
		t.Errorf("burst at (%v, %v, %v), want the landing position (3.5, 64, 1.5)", b.x, b.y, b.z)
	}
	if got, _ := standDustAfterDrop(t, 3.5); len(got) != 0 {
		t.Errorf("3.5-block drop sent block bursts %v, want none", got)
	}
}
