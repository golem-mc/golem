package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// standDustAfterDrop drops an armor stand from height blocks above a stone
// floor and returns the block-particle bursts the watcher is sent.
func standDustAfterDrop(t *testing.T, height float64) (counts []int32, stone uint16) {
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
		r.ReadBool()
		r.ReadBool()
		r.ReadF64()
		r.ReadF64()
		r.ReadF64()
		r.ReadF32()
		r.ReadF32()
		r.ReadF32()
		r.ReadF32()
		counts = append(counts, r.ReadVarInt())
	})
	return counts, stone
}

// An armor stand that falls more than 3 blocks bursts block particles where
// it lands; a shorter fall sends none.
// vanilla: LivingEntity.checkFallDamage
func TestAnArmorStandBurstsBlockParticlesAfterALongFall(t *testing.T) {
	t.Parallel()
	if got, _ := standDustAfterDrop(t, 6); len(got) != 1 || got[0] <= 0 {
		t.Errorf("6-block drop sent block bursts %v, want one", got)
	}
	if got, _ := standDustAfterDrop(t, 3.5); len(got) != 0 {
		t.Errorf("3.5-block drop sent block bursts %v, want none", got)
	}
}
