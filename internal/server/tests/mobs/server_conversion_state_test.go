package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func TestMobConversionsCarryVanillaState(t *testing.T) {
	t.Parallel()
	const operator = "Observer"
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), operator,
		servertest.OpsListing(t, operator), func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.ChatCommand("difficulty hard")
	c.ChatCommand("time set midnight")
	c.Settle()

	t.Run("nether mobs", func(t *testing.T) {
		c.ChatCommand(`summon hoglin 4.5 64 0.5 {TimeInOverworld:299,CanPickUpLoot:1b,equipment:{body:{id:"minecraft:iron_horse_armor",count:1}},drop_chances:{body:2f}}`)
		c.ChatCommand(`summon piglin 4.5 64 4.5 {TimeInOverworld:299}`)
		c.Settle()
		servertest.RunTicks(t, srv, 2)

		const zoglin = "@e[type=zoglin,limit=1] "
		got := entityData(t, c, zoglin+"CanPickUpLoot")
		if got != "0b" {
			t.Errorf("zoglin CanPickUpLoot = %s, want 0b", got)
		}
		body := entityCompound(t, c, zoglin+"equipment.body")
		if got := body["id"]; got != nbt.String("minecraft:iron_horse_armor") {
			t.Errorf("zoglin body equipment = %v, want iron horse armor", got)
		}
		if got := entityCompound(t, c, zoglin+"drop_chances")["body"]; got != nbt.Float(2) {
			t.Errorf("zoglin body drop chance = %v, want 2", got)
		}
		nausea := entityCompound(t, c, `@e[type=zombified_piglin,limit=1] active_effects[{id:"minecraft:nausea"}]`)
		if got := nausea["show_particles"]; got != nil && got != nbt.Byte(1) {
			t.Errorf("nausea show_particles = %v, want absent or true", got)
		}
		if got := nausea["show_icon"]; got != nbt.Byte(1) {
			t.Errorf("nausea show_icon = %v, want true", got)
		}
	})

	t.Run("infection", func(t *testing.T) {
		c.ChatCommand(`summon zombie 10.5 64 0.5 {NoAI:1b,Tags:[infector]}`)
		for i := range 10 {
			c.ChatCommand(fmt.Sprintf(`summon villager 10.5 64 3.5 {NoAI:1b,CanPickUpLoot:1b,LeftHanded:1b,Tags:[infection%d]}`, i))
			c.ChatCommand(fmt.Sprintf(`damage @e[tag=infection%d,limit=1] 1000 minecraft:mob_attack by @e[tag=infector,limit=1]`, i))
		}
		c.Settle()
		servertest.RunTicks(t, srv, 1)

		rightHanded := 0
		for i := range 10 {
			tag := entityCompound(t, c, fmt.Sprintf("@e[type=zombie_villager,tag=infection%d,limit=1]", i))
			if got := tag["CanPickUpLoot"]; got != nbt.Byte(1) {
				t.Errorf("zombie villager %d CanPickUpLoot = %v, want true", i, got)
			}
			if tag["LeftHanded"] == nbt.Byte(0) {
				rightHanded++
			}
		}
		if rightHanded < 8 {
			t.Errorf("%d of 10 infected villagers became right-handed with the test seed, want at least 8", rightHanded)
		}
	})

	t.Run("lightning", func(t *testing.T) {
		for i := range 10 {
			c.ChatCommand(fmt.Sprintf(`summon villager 0.5 64 8.5 {NoAI:1b,LeftHanded:1b,Tags:[struck%d]}`, i))
		}
		c.ChatCommand("summon lightning_bolt 0.5 64 8.5")
		c.Settle()
		servertest.RunTicks(t, srv, 1)

		rightHanded := 0
		for i := range 10 {
			if entityData(t, c, fmt.Sprintf("@e[type=witch,tag=struck%d,limit=1] LeftHanded", i)) == "0b" {
				rightHanded++
			}
		}
		if rightHanded < 8 {
			t.Errorf("%d of 10 witches became right-handed with the test seed, want at least 8", rightHanded)
		}
	})
}
