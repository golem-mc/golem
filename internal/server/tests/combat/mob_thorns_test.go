package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAMobsThornsHurtsThePlayerWhoPunchesIt punches a seeded zombie wearing a
// Thorns III chestplate forty times: some punches cost the puncher health,
// each told by a thorns damage event the zombie caused, and every
// retaliation costs the chestplate two durability.
// vanilla: Player.itemAttackInteraction, EnchantmentHelper.doPostAttackEffects
func TestAMobsThornsHurtsThePlayerWhoPunchesIt(t *testing.T) {
	t.Parallel()
	const name = "Brute"
	srv, c, playerID := servertest.JoinAs(t, servertest.MobTestConfig(), name)
	c.MoveToGround(0.5, 64, 0.5, true)
	servertest.NightWorld(srv, c)

	zombieID := srv.SpawnMobSeeded("minecraft:zombie", 2.5, 64, 0.5, 4374)
	srv.SetMobNoAI(zombieID)
	srv.SetMobWorn(zombieID, 2, servertest.EnchantedStack(t, "minecraft:diamond_chestplate", "minecraft:thorns", 3))
	thorns := registry.MustID("minecraft:damage_type", "minecraft:thorns")
	servertest.HoldTicks(t, srv)

	retaliations := 0
	for range 40 {
		// Full health on both sides, so neither the zombie nor the puncher
		// dies of the run.
		srv.SetMobHealth(zombieID, 20)
		srv.SetHealth(name, 20)
		servertest.AwaitFullSwing(t, srv, name)
		c.Attack(zombieID)
		// The swing's recovery outlasts the hurt windows the punch opens.
		thornsHits := 0
		c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
			if id != protocol.IDPlayDamageEvent {
				return false
			}
			r := protocol.NewReader(data)
			victim, kind, cause, direct := r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()
			if victim != playerID {
				return false
			}
			if kind != thorns || cause != zombieID+1 || direct != zombieID+1 {
				t.Errorf("the puncher's damage event names type %d by %d/%d, want thorns (%d) by the zombie (%d)",
					kind, cause, direct, thorns, zombieID+1)
			}
			thornsHits++
			return false
		})
		if lost := 20 - srv.PlayerHealth(name); (lost > 0) != (thornsHits > 0) {
			t.Errorf("a punch cost the puncher %v health with %d thorns damage events", lost, thornsHits)
		}
		retaliations += thornsHits
		if wear := srv.MobEquipment(zombieID, "chest").Damage; wear != int32(2*retaliations) {
			t.Fatalf("the chestplate has %d wear after %d retaliations, want %d", wear, retaliations, 2*retaliations)
		}
	}
	if retaliations == 0 {
		t.Error("forty punches at a Thorns III zombie never drew a retaliation")
	}
}
