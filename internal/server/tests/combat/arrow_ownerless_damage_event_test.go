package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: AbstractArrow.onHitEntity, ClientboundDamageEventPacket
func TestAnOwnerlessArrowIsBothCauseAndDirectOfThePlayersDamageEvent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Target", func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	c.MoveTo(0.5, 64, 0.5, 0, 0)
	servertest.RunTicks(t, srv, 1)
	srv.ShootArrow(0.5, 64.5, -1.5, 0, 0, 1)
	arrows := srv.ArrowsInDimension(world.DimensionName)
	if len(arrows) != 1 {
		t.Fatalf("arrows = %v, want the one shot", arrows)
	}
	want := arrows[0].ID + 1
	var cause, direct int32
	found := c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
		if id != protocol.IDPlayDamageEvent {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt() // victim
		r.ReadVarInt() // type
		cause, direct = r.ReadVarInt(), r.ReadVarInt()
		return true
	})
	if !found {
		t.Fatal("the arrow never hurt the player")
	}
	if cause != want || direct != want {
		t.Errorf("damage event cause, direct = %d, %d; want the arrow's %d for both", cause, direct, want)
	}
}

// A worn damage_immunity requiring is_direct spares the wearer an ownerless
// arrow, which is its own direct, but not an owned one.
// vanilla: AbstractArrow.onHitEntity, DamageSource.isDirect, DamageSourcePredicate
func TestDirectImmunityCoversAnOwnerlessArrowOnly(t *testing.T) {
	const name = "Wearer"
	files := map[string]string{"data/pk/enchantment/deflect.json": `{"anvil_cost":1,"description":{"text":"Deflect"},"max_level":1,"weight":1,
		"min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":50,"per_level_above_first":0},
		"slots":["head"],"supported_items":"#minecraft:enchantable/head_armor",
		"effects":{"minecraft:damage_immunity":[{"effect":{},"requirements":{"type":"minecraft:damage_source_properties",
		"predicate":{"is_direct":true}}}]}}`}
	for _, owned := range []bool{false, true} {
		t.Run(map[bool]string{false: "ownerless", true: "owned"}[owned], func(t *testing.T) {
			cfg := servertest.BowTestConfig()
			cfg.LevelName = servertest.PackEnchantments(t, files)
			srv, c, _ := servertest.JoinAs(t, cfg, name)
			servertest.HoldTicks(t, srv)
			srv.GiveSlot(name, player.ArmorSlot, servertest.EnchantedStack(t, "minecraft:iron_helmet", "pk:deflect", 1))
			c.MoveTo(0.5, 64, 0.5, 0, 0)
			servertest.RunTicks(t, srv, 1)
			if owned {
				cow := srv.SpawnMob("minecraft:cow", 0.5, 64, -6.5)
				srv.ShootMobArrow(cow, 0.5, 64.5, -1.5, 0, 0, 1)
			} else {
				srv.ShootArrow(0.5, 64.5, -1.5, 0, 0, 1)
			}
			servertest.RunTicks(t, srv, 5)
			if got, hurt := srv.PlayerHealth(name), owned; (got < 20) != hurt {
				t.Errorf("health = %v with owned=%v, want hurt=%v", got, owned, hurt)
			}
		})
	}
}
