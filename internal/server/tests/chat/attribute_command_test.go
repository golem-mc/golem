package chat

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// movementSpeedID is minecraft:movement_speed in the attribute registry.
const movementSpeedID = 26

func attributeName(path string) protocol.Text {
	return protocol.Text{Translate: "attribute.name." + path}
}

func attributeRunner(t *testing.T) *servertest.Client {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	return c
}

// TestAttributeBaseSetSendsUpdateAttributes sets a base, which the client is
// told of, reads it back and resets it.
func TestAttributeBaseSetSendsUpdateAttributes(t *testing.T) {
	t.Parallel()
	c := attributeRunner(t)
	who := servertest.PlayerDisplayComponent("Testificate")
	speed := attributeName("movement_speed")

	c.ChatCommand("attribute @s minecraft:movement_speed base set 0.2")
	c.ExpectCommandFeedbackText("commands.attribute.base_value.set.success", speed, who, protocol.Text{Text: "0.2"})
	c.AwaitAttributes(c.PlayerEntityID, movementSpeedID, func(e servertest.AttrEntry) bool { return e.Base == 0.2 })

	c.ChatCommand("attribute @s movement_speed base get")
	c.ExpectCommandFeedbackText("commands.attribute.base_value.get.success", speed, who, protocol.Text{Text: "0.2"})

	c.ChatCommand("attribute @s movement_speed get")
	c.ExpectCommandFeedbackText("commands.attribute.value.get.success", speed, who, protocol.Text{Text: "0.2"})

	c.ChatCommand("attribute @s movement_speed base reset")
	c.ExpectCommandFeedbackText("commands.attribute.base_value.reset.success", speed, who, protocol.Text{Text: "0.10000000149011612"})
	c.AwaitAttributes(c.PlayerEntityID, movementSpeedID, func(e servertest.AttrEntry) bool { return e.Base == float64(float32(0.1)) })
	c.Disconnect()
}

// TestAttributeModifiers adds a modifier, which the client is told of and
// which changes the value, refuses a second of the same ID, reads it and
// removes it.
func TestAttributeModifiers(t *testing.T) {
	t.Parallel()
	c := attributeRunner(t)
	who := servertest.PlayerDisplayComponent("Testificate")
	armor := attributeName("armor")
	id := protocol.Text{Text: "minecraft:test"}

	c.ChatCommand("attribute @s minecraft:armor modifier add minecraft:test 4 add_value")
	c.ExpectCommandFeedbackText("commands.attribute.modifier.add.success", id, armor, who)
	c.AwaitAttributes(c.PlayerEntityID, 1, func(e servertest.AttrEntry) bool { return e.Value() == 4 })

	c.ChatCommand("attribute @s minecraft:armor modifier add minecraft:test 4 add_value")
	c.ExpectComponentError("commands.attribute.failed.modifier_already_present", id, armor, who)

	c.ChatCommand("attribute @s minecraft:armor modifier value get minecraft:test")
	c.ExpectCommandFeedbackText("commands.attribute.modifier.value.get.success", id, armor, who, protocol.Text{Text: "4.0"})

	c.ChatCommand("attribute @s minecraft:armor modifier remove minecraft:test")
	c.ExpectCommandFeedbackText("commands.attribute.modifier.remove.success", id, armor, who)
	c.AwaitAttributes(c.PlayerEntityID, 1, func(e servertest.AttrEntry) bool { return len(e.Modifiers) == 0 })

	c.ChatCommand("attribute @s minecraft:armor modifier remove minecraft:test")
	c.ExpectComponentError("commands.attribute.failed.no_modifier", armor, who, id)
	c.Disconnect()
}

// TestAttributeRefusals refuses an attribute a player does not carry.
func TestAttributeRefusals(t *testing.T) {
	t.Parallel()
	c := attributeRunner(t)
	c.ChatCommand("attribute @s minecraft:flying_speed get")
	c.ExpectComponentError("commands.attribute.failed.no_attribute", servertest.PlayerDisplayComponent("Testificate"), attributeName("flying_speed"))
	c.Disconnect()
}

// TestAttributeScaleAndStore scales a read and stores it: the result of a
// get is the value times the scale, truncated to an int.
func TestAttributeScaleAndStore(t *testing.T) {
	t.Parallel()
	c := attributeRunner(t)
	who := servertest.PlayerDisplayComponent("Testificate")
	speed := attributeName("movement_speed")

	c.ChatCommand("attribute @s movement_speed base set 0.25")
	c.ExpectCommandFeedbackText("commands.attribute.base_value.set.success", speed, who, protocol.Text{Text: "0.25"})
	c.ChatCommand("execute store result storage test:r x int 1 run attribute @s movement_speed get 100")
	c.ExpectCommandFeedbackText("commands.attribute.value.get.success", speed, who, protocol.Text{Text: "0.25"})
	c.ChatCommand("execute if data storage test:r {x:25}")
	one := int64(1)
	c.ExpectCommandFeedbackText("commands.execute.conditional.pass_count", protocol.Text{Number: &one})

	// A smaller scale truncates: 0.25 * 10 is 2.
	c.ChatCommand("execute store result storage test:r y int 1 run attribute @s movement_speed get 10")
	c.ExpectCommandFeedbackText("commands.attribute.value.get.success", speed, who, protocol.Text{Text: "0.25"})
	c.ChatCommand("execute if data storage test:r {y:2}")
	c.ExpectCommandFeedbackText("commands.execute.conditional.pass_count", protocol.Text{Number: &one})
	c.Disconnect()
}

// TestAttributeFeedbackStaysWithSender checks the feedback of /attribute is
// not echoed to the other operators.
func TestAttributeFeedbackStaysWithSender(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice", "Bob"))
	alice := connect().Join("Alice")
	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.ChatCommand("attribute @s movement_speed base set 0.2")
	alice.ExpectCommandFeedbackText("commands.attribute.base_value.set.success",
		attributeName("movement_speed"), servertest.PlayerDisplayComponent("Alice"), protocol.Text{Text: "0.2"})
	bob.ExpectNothing(srv)
	alice.Disconnect()
	bob.Disconnect()
}

// TestEffectModifierAppliesWithinTheTick gives Luck and reads the luck
// attribute with no tick in between, then clears it and reads again: the
// effect's modifier goes on and comes off with the effect, not on the
// player's next tick.
// vanilla: LivingEntity.onEffectAdded, onEffectsRemoved
func TestEffectModifierAppliesWithinTheTick(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, _ := servertest.Join(t, cfg, "Vaulter")
	servertest.HoldTicks(t, srv)

	luck := func() string {
		t.Helper()
		for _, text := range srv.RunLine("attribute Vaulter minecraft:luck get") {
			if text.Translate == "commands.attribute.value.get.success" && len(text.With) == 3 {
				return text.With[2].Text
			}
		}
		t.Fatal("attribute get gave no value")
		return ""
	}
	srv.RunLine("effect give Vaulter minecraft:luck 100 0")
	if got := luck(); got != "1.0" {
		t.Errorf("luck right after effect give = %s, want 1.0", got)
	}
	srv.RunLine("effect clear Vaulter minecraft:luck")
	if got := luck(); got != "0.0" {
		t.Errorf("luck right after effect clear = %s, want 0.0", got)
	}
}

// attributeMobRunner joins an operator and stops the tick, with a zombie
// well clear of the player.
func attributeMobRunner(t *testing.T) (*server.Server, *servertest.Client, int32, string) {
	t.Helper()
	cfg := servertest.HostileTestConfig()
	cfg.OnlineMode = false
	srv, c, _ := servertest.JoinAs(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(0.5, 64, 10.5, true)
	c.Settle()
	srv.StopTick()
	id := srv.SpawnMob("minecraft:zombie", 6.5, 64, 0.5)
	uuid, ok := srv.MobUUID(id)
	if !ok {
		t.Fatal("the zombie is not in the herd")
	}
	return srv, c, id, uuid
}

// TestAttributeBurningTimeScalesAMobsIgnition sets a zombie's burning_time
// base to 0, which leaves no burn.
// vanilla: LivingEntity.igniteForTicks
func TestAttributeBurningTimeScalesAMobsIgnition(t *testing.T) {
	t.Parallel()
	srv, c, id, uuid := attributeMobRunner(t)
	c.ChatCommand("attribute " + uuid + " minecraft:burning_time base set 0")
	c.Settle()
	srv.IgniteMob(id, 160)
	if got := srv.MobFireTicks(id); got != 0 {
		t.Errorf("a zombie with burning_time 0 burns for %d ticks, want 0", got)
	}
	c.ChatCommand("attribute " + uuid + " minecraft:burning_time base set 1")
	c.Settle()
	srv.IgniteMob(id, 160)
	if got := srv.MobFireTicks(id); got != 160 {
		t.Errorf("a zombie with burning_time 1 burns for %d ticks, want 160", got)
	}
}

// TestAttributeExplosionKnockbackResistanceStopsAMobsBlastPush sets a
// zombie's explosion_knockback_resistance base to 1, which leaves its
// velocity unchanged by a blast.
// vanilla: ServerExplosion.hurtEntities
func TestAttributeExplosionKnockbackResistanceStopsAMobsBlastPush(t *testing.T) {
	t.Parallel()
	srv, c, id, uuid := attributeMobRunner(t)
	c.ChatCommand("attribute " + uuid + " minecraft:explosion_knockback_resistance base set 1")
	c.Settle()
	before, _, _, _ := srv.MobVelocity(id)
	srv.BlastAt(0.5, 64, 0.5, 4)
	after, _, _, ok := srv.MobVelocity(id)
	if !ok {
		t.Fatal("the zombie is not in the herd after the blast")
	}
	if after != before {
		t.Errorf("a zombie with full explosion_knockback_resistance went from vx %v to %v", before, after)
	}
}
