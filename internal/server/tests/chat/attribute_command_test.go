package chat

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
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
