package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestStackDamageTypeNamesTheHit hits a zombie with a stick that carries its
// own damage_type and with a plain stick.
// vanilla: ItemStack.getDamageSource
func TestStackDamageTypeNamesTheHit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ item, want string }{
		{`stick[damage_type="minecraft:magic"]`, "minecraft:magic"},
		{`stick`, "minecraft:player_attack"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			srv, c, attackerID := servertest.JoinAs(t, servertest.CombatTestConfig(), "Smith", servertest.OpsListing(t, "Smith"))
			servertest.HoldTicks(t, srv)
			c.MoveToGround(2.5, 64, 5.5, true)
			c.FacePlayer(5.5, 64, 5.5, 0, 0)
			c.Settle()
			c.ChatCommand("give @s " + tc.item)
			c.Settle()
			if srv.InventorySlot("Smith", player.HotbarSlot).Empty() {
				t.Fatalf("/give %s left the hand empty", tc.item)
			}
			mob := srv.SpawnMob("minecraft:zombie", 5.5, 64, 5.5)
			c.ExpectMobSpawn(protocol.EntityTypeZombie)
			chargeByHand(t, srv, "Smith", 20)
			c.Attack(mob)
			c.ExpectMobDamageEvent(mob, tc.want, attackerID+1)
		})
	}
}

// TestStackMinimumAttackChargeGatesTheJab jabs with a wooden spear that
// carries minimum_attack_charge=0 and with one that has it removed against
// the spear's own 1: a fresh jab lands only without the minimum.
// vanilla: Player.cannotAttackWithItem
func TestStackMinimumAttackChargeGatesTheJab(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item string
		hurt bool
	}{
		{`wooden_spear[minimum_attack_charge=0.0f]`, true},
		{`wooden_spear[!minimum_attack_charge]`, true},
		{`wooden_spear[minimum_attack_charge=1.0f]`, false},
		{`wooden_spear`, false},
	} {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Smith", servertest.OpsListing(t, "Smith"))
			servertest.HoldTicks(t, srv)
			c.FacePlayer(5.5, 64, 5.5, 0, 0)
			c.Settle()
			c.ChatCommand("give @s " + tc.item)
			c.Settle()
			if srv.InventorySlot("Smith", player.HotbarSlot).Empty() {
				t.Fatalf("/give %s left the hand empty", tc.item)
			}
			srv.TickSurvival()
			zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, 8.5)
			full := mobHealthNow(t, srv, zombie)
			c.Stab()
			c.Settle()
			if got := mobHealthNow(t, srv, zombie); (got < full) != tc.hurt {
				t.Errorf("a fresh jab left the zombie at %v of %v, hurt want %v", got, full, tc.hurt)
			}
		})
	}
}

// TestStackPiercingWeaponGatesTheJab jabs with a stick that carries its own
// piercing_weapon and with an iron spear that has it removed: only the stick
// hurts the zombie.
// vanilla: ItemStack.get(DataComponents.PIERCING_WEAPON), PiercingWeapon.attack
func TestStackPiercingWeaponGatesTheJab(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item string
		hurt bool
	}{
		{`stick[piercing_weapon={},minimum_attack_charge=0.0f]`, true},
		{`iron_spear[!piercing_weapon,minimum_attack_charge=0.0f]`, false},
		{`iron_spear[minimum_attack_charge=0.0f]`, true},
	} {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Smith", servertest.OpsListing(t, "Smith"))
			servertest.HoldTicks(t, srv)
			c.FacePlayer(5.5, 64, 5.5, 0, 0)
			c.Settle()
			c.ChatCommand("give @s " + tc.item)
			c.Settle()
			if srv.InventorySlot("Smith", player.HotbarSlot).Empty() {
				t.Fatalf("/give %s left the hand empty", tc.item)
			}
			srv.TickSurvival()
			zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, 7.5)
			full := mobHealthNow(t, srv, zombie)
			c.Stab()
			c.Settle()
			if got := mobHealthNow(t, srv, zombie); (got < full) != tc.hurt {
				t.Errorf("a jab left the zombie at %v of %v, hurt want %v", got, full, tc.hurt)
			}
		})
	}
}

// TestStackKineticWeaponGatesTheSpearCharge runs at a zombie holding a stick
// that carries its own kinetic_weapon and an iron spear that has it removed:
// only the stick's charge hurts.
// vanilla: ItemStack.get(DataComponents.KINETIC_WEAPON), KineticWeapon.damageEntities
func TestStackKineticWeaponGatesTheSpearCharge(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item string
		hurt bool
	}{
		{`stick[kinetic_weapon={damage_conditions:{max_duration_ticks:100,min_relative_speed:4.6f}}]`, true},
		{`iron_spear[!kinetic_weapon]`, false},
	} {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.JoinAs(t, servertest.CombatTestConfig(), "Smith", servertest.OpsListing(t, "Smith"))
			servertest.HoldTicks(t, srv)
			c.FacePlayer(5.5, 64, 5.5, 0, 0)
			c.Settle()
			c.ChatCommand("give @s " + tc.item)
			c.Settle()
			if srv.InventorySlot("Smith", player.HotbarSlot).Empty() {
				t.Fatalf("/give %s left the hand empty", tc.item)
			}
			srv.StopTick()
			srv.TickSurvival()
			zombie := srv.SpawnMob("minecraft:zombie", 5.5, 64, 12.5)
			full := mobHealthNow(t, srv, zombie)
			c.UseItemRightClicks(protocol.HandMain)
			c.Settle()
			z := 5.5
			for range 24 {
				z += 0.3
				c.MoveTo(5.5, 64, z, 0, 0)
				c.Settle()
				srv.TickSurvival()
			}
			if got := mobHealthNow(t, srv, zombie); (got < full) != tc.hurt {
				t.Errorf("a charge left the zombie at %v of %v, hurt want %v", got, full, tc.hurt)
			}
		})
	}
}
