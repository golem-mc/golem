package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestCommandDataMergeSameCompoundTwice checks that a merge argument
// parsed once still writes its own copy into each target, and that a refused
// argument refuses the same way on its second run.
// vanilla: DataCommands.mergeData
func TestCommandDataMergeSameCompoundTwice(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")
	disp := map[string]protocol.Text{}
	sel := map[string]string{
		"a": "@e[type=minecraft:item_display,x=2.5,y=64,z=2.5,sort=nearest,limit=1]",
		"b": "@e[type=minecraft:item_display,x=6.5,y=64,z=2.5,sort=nearest,limit=1]",
	}
	for _, tag := range []string{"a", "b"} {
		c.ChatCommand(`summon minecraft:item_display ` + map[string]string{"a": "2.5", "b": "6.5"}[tag] + ` 64 2.5 {item:{id:"minecraft:diamond",count:1}}`)
		d := c.AwaitSpawn(protocol.EntityTypeItemDisplay)
		disp[tag] = servertest.EntityDisplayComponent("minecraft:item_display", "entity.minecraft.item_display", d.Uuid)
		c.ExpectCommandFeedbackText("commands.summon.success", disp[tag])
	}
	same := `{transformation:{scale:[2f,2f,2f]}}`
	for _, tag := range []string{"a", "b"} {
		c.ChatCommand("data merge entity " + sel[tag] + " " + same)
		c.ExpectCommandFeedbackText("commands.data.entity.modified", disp[tag])
	}
	c.ChatCommand("data merge entity " + sel["a"] + " {transformation:{scale:[3f,3f,3f]}}")
	c.ExpectCommandFeedbackText("commands.data.entity.modified", disp["a"])
	for tag, want := range map[string]string{"a": "[3.0f, 3.0f, 3.0f]", "b": "[2.0f, 2.0f, 2.0f]"} {
		c.ChatCommand("data get entity " + sel[tag] + " transformation.scale")
		c.ExpectCommandFeedbackText("commands.data.entity.query", disp[tag], servertest.NBTArg(want))
	}
	var refusals [][]byte
	for range 2 {
		c.ChatCommand("data merge entity " + sel["b"] + " [1,2]")
		// the message, then the context line under it
		refusals = append(refusals, append(c.ExpectPlay(protocol.IDPlaySystemChat), c.ExpectPlay(protocol.IDPlaySystemChat)...))
	}
	if !bytes.Equal(refusals[0], refusals[1]) || !bytes.Contains(refusals[0], []byte("argument.nbt.expected.compound")) {
		t.Errorf("refusals differ or are not the compound refusal:\n% X\n% X", refusals[0], refusals[1])
	}
	c.Disconnect()
}
