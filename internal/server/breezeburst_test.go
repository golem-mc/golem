package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
)

func TestBreezeAndPlayerWindChargesSelectTheirBurstRadiusSoundAndEmitter(t *testing.T) {
	for _, tc := range []struct {
		kind            entity.ThrownKind
		radius          float64
		particle, sound int32
	}{
		{entity.ThrownWindCharge, 1.2, protocol.ParticleGustEmitterSmall, protocol.SoundEventWindChargeBurst},
		{entity.ThrownBreezeWindCharge, 3, protocol.ParticleGustEmitterLarge, protocol.SoundEventBreezeWindBurst},
	} {
		charge := entity.NewThrown(1, tc.kind, 0, 65, 0, 0.7, 0, 0, 2, 1)
		it := &thrownEntity{throwable: charge}
		var fx thrownEffects
		(&Server{}).endThrown(&fx, nil, nil, it, []entity.ThrownEvent{{Kind: entity.ThrownHitTarget, X: 2, Y: 65, Z: 0}}, 0, 65, 0)
		if len(fx.windBursts) != 1 {
			t.Fatal("charge did not collect a wind burst")
		}
		burst := fx.windBursts[0]
		if burst.radius != tc.radius || burst.x != 2 || burst.y != 65 || burst.z != 0 {
			t.Fatalf("burst=%+v", burst)
		}
		payload := (blast{radius: burst.radius, burst: true, breezeBurst: burst.breeze}).payload(0, 0, 0, 0, false)
		r := protocol.NewReader(payload)
		r.ReadF64()
		r.ReadF64()
		r.ReadF64()
		if r.ReadF32() != float32(tc.radius) {
			t.Fatal("incorrect wire radius")
		}
		r.ReadI32()
		r.ReadBool()
		if particle, sound := r.ReadVarInt(), r.ReadVarInt(); particle != tc.particle || sound != tc.sound {
			t.Fatalf("kind=%d particle=%d sound=%d", tc.kind, particle, sound)
		}
		if r.ReadVarInt() != 0 {
			t.Fatal("wind burst had block particles")
		}
	}
}
