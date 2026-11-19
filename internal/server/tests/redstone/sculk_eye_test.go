package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

type eyeTarget struct{ found bool }

func (e eyeTarget) NearestEyeTarget(_, _, _ int32, _ int) (int32, int32, int32, bool) {
	return 40, 0, 4, e.found
}

// vanilla: EnderEyeItem.use
func TestAnEyeOfEnderLaunchEmitsAVibration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		hand int32
		slot int16
	}{
		{"main hand", protocol.HandMain, 36},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := sensorScene(t)
			srv.SetEyeTargets(eyeTarget{found: true})
			c.SetCreativeSlot(tc.slot, protocol.ItemStack{ItemID: int32(registry.ItemEnderEye), Count: 1})
			c.Settle()
			c.UseItemRightClicks(tc.hand)
			c.Settle()
			servertest.RunTicks(t, srv, 8)

			expectSensor(t, srv, 4, 64, 4, "active", "8")
			tag, _ := srv.BlockEntityTagAt(4, 64, 4)
			if got, _ := nbt.IntField(tag, "last_vibration_frequency"); got != 3 {
				t.Errorf("eye use gave frequency %d, want 3", got)
			}
			if got := srv.EyeCount(); got != 1 {
				t.Errorf("flying eyes = %d, want 1", got)
			}

			vibration, _, _ := registry.ParticleID("minecraft:vibration")
			particles := 0
			c.SettleAndCollect(func(id int32, data []byte) {
				if id != protocol.IDPlayLevelParticles {
					return
				}
				r := protocol.NewReader(data)
				if r.ReadVarInt() != vibration {
					return
				}
				particles++
				if source := r.ReadVarInt(); source != 0 {
					t.Fatalf("vibration source type = %d, want block", source)
				}
				if x, y, z := r.ReadPosition(); x != 4 || y != 64 || z != 4 {
					t.Errorf("vibration target = (%d, %d, %d), want sensor", x, y, z)
				}
				if arrival := r.ReadVarInt(); arrival != 4 {
					t.Errorf("vibration travel = %d ticks, want 4", arrival)
				}
				tail := servertest.ReadParticleTail(t, r)
				if tail.X != 0.5 || tail.Y != 64.9 || tail.Z != 4.5 || tail.Count != 1 {
					t.Errorf("vibration origin = (%g, %g, %g) x%d, want eye spawn (0.5, 64.9, 4.5) x1", tail.X, tail.Y, tail.Z, tail.Count)
				}
			})
			if particles != 1 {
				t.Errorf("vibration particles = %d, want 1", particles)
			}
		})
	}
}
