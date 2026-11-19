package combat

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestArrowImpactPitch checks the float rounding of both impact paths on the wire.
// vanilla: AbstractArrow.onHitEntity, onHitBlock
func TestArrowImpactPitch(t *testing.T) {
	t.Parallel()
	for _, target := range []string{"block", "mob", "player"} {
		for _, sample := range []struct {
			seed       int64
			sampleBits uint32
			pitchBits  uint32
		}{
			// AbstractArrow's float expression evaluated by Java for these samples.
			{1, 0x3f1acb04, 0x3f967365},
			{2, 0x3e2b4fcf, 0x3fa48c9a},
			{4, 0x3e793671, 0x3fa1e900},
		} {
			t.Run(fmt.Sprintf("%s/seed%d", target, sample.seed), func(t *testing.T) {
				t.Parallel()
				f := rand.New(rand.NewSource(sample.seed)).Float32()
				if math.Float32bits(f) != sample.sampleBits {
					t.Fatalf("RNG sample = %08x, want %08x", math.Float32bits(f), sample.sampleBits)
				}
				srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Watcher")
				servertest.HoldTicks(t, srv)
				c.MoveToGround(0.5, 64, 6.5, true)
				switch target {
				case "block":
					srv.SetBlock(4, 65, 0, registry.BlockStone.DefaultState())
				case "mob":
					cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
					srv.SetMobNoAI(cow)
					c.ExpectMobSpawn(protocol.EntityTypeCow)
				case "player":
					c.MoveToGround(4.5, 64, 0.5, true)
				}
				arrow := srv.ShootArrow(1.5, 65, 0.5, 4, 0, 0)
				srv.SeedArrow(arrow, sample.seed)
				if !c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
					if id != protocol.IDPlaySoundEffect {
						return false
					}
					sound := servertest.DecodePlaySoundEffect(t, data)
					if sound.RegistryID != int32(registry.SoundEntityArrowHit)-1 {
						return false
					}
					if bits := math.Float32bits(sound.Pitch); bits != sample.pitchBits {
						t.Errorf("impact pitch bits = %08x, want %08x (sample %08x)", bits, sample.pitchBits, sample.sampleBits)
					}
					return true
				}) {
					t.Fatal("no arrow impact sound within one tick")
				}
			})
		}
	}
}
