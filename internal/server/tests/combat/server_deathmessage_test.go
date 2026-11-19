package combat

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"

	"github.com/google/uuid"
)

// These tests check the killer named in a death message: a player by their
// own name, a mob by the entity.minecraft translation of its kind, and a named
// weapon in the killer's hand by the ".item" form that includes the weapon.
// vanilla: DamageSource.getLocalizedDeathMessage, Entity.getDisplayName

// expectFatalDeath watches a client for the Combat Death and the System Chat
// a fatal blow sends the body with the given entity ID, the traffic between
// them ignored, and checks both carry message.
func expectFatalDeath(srv *server.Server, c *servertest.Client, victimID int32, message protocol.Text) {
	c.T.Helper()
	var wantDeath protocol.Buffer
	wantDeath.PutVarInt(victimID)
	wantDeath.PutText(message)
	var wantChat protocol.Buffer
	wantChat.PutText(message)
	wantChat.PutBool(false) // chat, not the action bar

	deathScreen, announcement := false, false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, nil, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayCombatDeath:
			if !bytes.Equal(data, wantDeath.Bytes()) {
				c.T.Errorf("combat death = % X, want % X", data, wantDeath.Bytes())
			}
			deathScreen = true
		case protocol.IDPlaySystemChat:
			if !bytes.Equal(data, wantChat.Bytes()) {
				c.T.Errorf("system chat = % X, want % X", data, wantChat.Bytes())
			}
			announcement = true
		}
		return deathScreen && announcement
	})
	if !deathScreen {
		c.T.Error("the blow never put the victim on the respawn screen")
	}
	if !announcement {
		c.T.Error("the death was never announced in chat")
	}
}

// TestAPlayerKillNamesTheWeapon kills a player with a renamed diamond sword:
// the message is the player's own with the ".item" form, naming the victim,
// the killer and the sword.
func TestAPlayerKillNamesTheWeapon(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig())
	killer := connect()
	killer.JoinAs("Killer")
	killer.MoveToGround(0.5, 64, 0.5, true)

	victim := connect()
	victimID := victim.JoinAs("Victim")
	victim.MoveToGround(0.5, 64, 0.5, true)
	victim.Settle()

	sword := servertest.ItemStack(t, "minecraft:diamond_sword", 1)
	name := protocol.Text{Text: "Sharp Sword"}
	sword.CustomName = &name
	srv.GiveSlot("Killer", player.HotbarSlot, sword)
	killer.Settle()
	servertest.AwaitFullSwing(t, srv, "Killer")

	srv.SetHealth("Victim", 1)
	killer.Attack(victimID)

	expectFatalDeath(srv, victim, victimID, protocol.Text{
		Translate: "death.attack.player.item",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Victim"), servertest.PlayerDisplayComponent("Killer"), rules.ItemText(sword, 1)},
	})
}

// A zombie's fatal bite names it with its team formatting, UUID insertion
// and show-entity hover.
// vanilla: Entity.getDisplayName, DamageSource.getLocalizedDeathMessage
func TestAMobKillNamesTheMob(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		team, custom bool
	}{{name: "unaffiliated"}, {name: "red team", team: true}, {name: "named red team", team: true, custom: true}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c, playerID := servertest.JoinAs(t, servertest.HostileTestConfig(), "Prey", servertest.OpsListing(t, "Prey"))
			srv.SetDifficulty(3)
			srv.SetDayTime(18000) // midnight keeps the zombie from burning
			c.Settle()
			servertest.HoldTicks(t, srv)

			zombieID := srv.SpawnMob("minecraft:zombie", 1.5, 64, 0.5)
			identity, ok := srv.MobUUID(zombieID)
			if !ok {
				t.Fatal("zombie has no UUID")
			}
			zombie := servertest.EntityDisplayComponent("minecraft:zombie", "entity.minecraft.zombie", uuid.MustParse(identity))
			if tc.custom {
				c.ChatCommand("data merge entity " + identity + ` {CustomName:{text:"Bob"}}`)
				servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
				zombie = customNamed(zombie, "Bob")
			}
			if tc.team {
				c.ChatCommand("team add t")
				c.ChatCommand("team modify t color red")
				c.ChatCommand(`team modify t prefix {text:"[R] "}`)
				c.ChatCommand(`team modify t suffix {text:"!"}`)
				c.ChatCommand("team join t " + identity)
				servertest.AwaitSystemChatSaying(t, c, "commands.team.join.success.single")
				zombie.Translate = ""
				zombie.Color = "red"
				zombie.Text = ""
				zombie.Extra = []protocol.Text{{Text: "[R] "}, zombie.HoverEntity.Name, {Text: "!"}}
			}
			c.Settle()
			srv.SetHealth("Prey", 1)
			expectFatalDeath(srv, c, playerID, protocol.Text{
				Translate: "death.attack.mob",
				With:      []protocol.Text{servertest.PlayerDisplayComponent("Prey"), zombie},
			})
		})
	}
}

// TestAnArrowKillNamesTheBow shoots a player with their own arrow while a
// renamed bow sits in their hand: the message is the arrow's own with the
// ".item" form, naming the victim, the archer and the bow.
func TestAnArrowKillNamesTheBow(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig())
	archer := connect()
	archer.JoinAs("Archer")
	archer.MoveToGround(20.5, 64, 20.5, true)

	victim := connect()
	victimID := victim.JoinAs("Victim")
	victim.MoveToGround(0.5, 64, 0.5, true)
	victim.Settle()

	bow := servertest.ItemStack(t, "minecraft:bow", 1)
	name := protocol.Text{Text: "Long Shot"}
	bow.CustomName = &name
	srv.GiveSlot("Archer", player.HotbarSlot, bow)
	archer.Settle()

	srv.SetHealth("Victim", 1)
	srv.ShootPlayerArrow("Archer", 4.5, 65, 0.75, -2, 0, 0)

	expectFatalDeath(srv, victim, victimID, protocol.Text{
		Translate: "death.attack.arrow.item",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Victim"), servertest.PlayerDisplayComponent("Archer"), rules.ItemText(bow, 1)},
	})
}

// killCreditDeath has a zombie hit the op Prey, runs gap ticks, kills Prey
// with a sourceless burn through /damage, checks the death message and
// returns Prey's killed_by zombie statistic.
func killCreditDeath(t *testing.T, gap int, message func(victim, zombie protocol.Text) protocol.Text) int32 {
	t.Helper()
	srv, c, playerID := servertest.JoinAs(t, servertest.CombatTestConfig(), "Prey", servertest.OpsListing(t, "Prey"))
	srv.SetDifficulty(3)
	srv.SetDayTime(18000) // midnight, so the zombie does not burn
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	zombieID := srv.SpawnMob("minecraft:zombie", 8.5, 64, 0.5)
	srv.SetMobNoAI(zombieID)
	if !srv.HurtByEntity("Prey", 1, zombieID) {
		t.Fatal("the zombie's blow did not land")
	}
	servertest.RunTicks(t, srv, gap)
	c.ChatCommand("damage @s 100 minecraft:on_fire")
	identity, ok := srv.MobUUID(zombieID)
	if !ok {
		t.Fatal("zombie has no UUID")
	}
	expectFatalDeath(srv, c, playerID, message(servertest.PlayerDisplayComponent("Prey"),
		servertest.EntityDisplayComponent("minecraft:zombie", "entity.minecraft.zombie", uuid.MustParse(identity))))

	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandRequestStats)
	})
	typeID, keyID, ok := registry.StatID(registry.StatKilledBy, "minecraft:zombie")
	if !ok {
		t.Fatal("no killed_by zombie statistic")
	}
	var n int32
	c.ReadTicks(srv, servertest.HostileTicks, func(id int32, data []byte) bool {
		if id != protocol.IDPlayAwardStats {
			return false
		}
		r := protocol.NewReader(data)
		for range r.ReadVarInt() {
			if r.ReadVarInt() == typeID && r.ReadVarInt() == keyID {
				n = r.ReadVarInt()
			} else {
				r.ReadVarInt()
			}
		}
		if err := r.Err(); err != nil {
			t.Fatalf("award statistics: %v", err)
		}
		return true
	})
	return n
}

// TestAnAttackerOlderThanAHundredTicksGetsNoKillCredit burns a player to death
// 110 ticks after a zombie's blow: the zombie is forgotten, so the message is
// the plain burn and no killed_by is counted.
// vanilla: LivingEntity.getKillCredit, baseTick
func TestAnAttackerOlderThanAHundredTicksGetsNoKillCredit(t *testing.T) {
	t.Parallel()
	n := killCreditDeath(t, 110, func(victim, zombie protocol.Text) protocol.Text {
		return protocol.Text{Translate: "death.attack.onFire", With: []protocol.Text{victim}}
	})
	if n != 0 {
		t.Errorf("killed_by zombie = %d, want 0", n)
	}
}

// ownerlessUUID is the identity of a summoned projectile or TNT, UUID:[I;1,2,3,4].
var ownerlessUUID = uuid.UUID{0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3, 0, 0, 0, 4}

// customNamed is an entity's display component under a plain custom name.
func customNamed(component protocol.Text, name string) protocol.Text {
	component.Translate, component.Text = "", name
	component.HoverEntity.Name = protocol.Text{Text: name}
	return component
}

// TestADeathWithNoAttackerGivesTheKeyOnlyTheVictim kills a player with
// /damage and no "by" entity: the key gets one argument, whatever the type.
// vanilla: DamageSource.getLocalizedDeathMessage
func TestADeathWithNoAttackerGivesTheKeyOnlyTheVictim(t *testing.T) {
	for _, tc := range []struct{ damageType, key string }{
		{"minecraft:player_attack", "death.attack.player"},
		{"minecraft:player_explosion", "death.attack.explosion.player"},
	} {
		t.Run(tc.damageType, func(t *testing.T) {
			srv, c, playerID := servertest.JoinAs(t, servertest.CombatTestConfig(), "Victim", servertest.OpsListing(t, "Victim"))
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)
			c.ChatCommand("damage @s 1000 " + tc.damageType)
			expectFatalDeath(srv, c, playerID, protocol.Text{
				Translate: tc.key,
				With:      []protocol.Text{servertest.PlayerDisplayComponent("Victim")},
			})
		})
	}
}
