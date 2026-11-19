package combat

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestHurtAnimationGoesToTheVictimWithTheBlowsBearing checks that only the hurt
// player gets Hurt Animation, with the direction of the blow relative to their
// yaw, and that a no_knockback hit sends none.
// vanilla: ServerPlayer.indicateDamage, LivingEntity.dealDefaultKnockback
func TestHurtAnimationGoesToTheVictimWithTheBlowsBearing(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig(), servertest.OpsListing(t, "Victim"))
	victim := connect()
	victimID := victim.JoinAs("Victim")
	watcher := connect()
	watcher.JoinAs("Watcher")
	victim.MoveToGround(0.5, 64, 0.5, true)
	victim.Settle()
	watcher.Settle()
	servertest.HoldTicks(t, srv)

	isAnim := servertest.PacketIs(protocol.IDPlayHurtAnimation)
	for _, tc := range []struct {
		at   string
		want float64
	}{{"5.5 64 0.5", 0}, {"0.5 64 5.5", 90}} {
		victim.ChatCommand("damage @s 1 minecraft:mob_attack at " + tc.at)
		r := protocol.NewReader(victim.ExpectWithinTicks(srv, 20, protocol.IDPlayHurtAnimation))
		if got := r.ReadVarInt(); got != victimID {
			t.Errorf("hurt animation for entity %d, want %d", got, victimID)
		}
		if got := float64(r.ReadF32()); math.Abs(got-tc.want) > 0.01 {
			t.Errorf("hurt animation yaw from %s = %v, want %v", tc.at, got, tc.want)
		}
		watcher.ExpectNoneWithinTicks(srv, 20, isAnim)
	}

	victim.ChatCommand("damage @s 1 minecraft:on_fire")
	victim.ExpectNoneWithinTicks(srv, 20, isAnim)

	// A harming cloud's blow comes from the cloud, not from the world origin.
	// vanilla: AreaEffectCloud.serverTick, DamageSource.getSourcePosition
	victim.ChatCommand(`summon area_effect_cloud 0.5 64 2.5 {Radius:3f,Duration:100,WaitTime:0,potion_contents:{potion:"minecraft:harming"}}`)
	r := protocol.NewReader(victim.ExpectWithinTicks(srv, 40, protocol.IDPlayHurtAnimation))
	if got := r.ReadVarInt(); got != victimID {
		t.Errorf("hurt animation for entity %d, want %d", got, victimID)
	}
	if got := float64(r.ReadF32()); math.Abs(got-90) > 0.01 {
		t.Errorf("hurt animation yaw from a cloud at +z = %v, want 90", got)
	}
}
