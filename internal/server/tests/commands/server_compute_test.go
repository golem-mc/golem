package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The /compute command, black-box: the provider kinds the commands tree
// offers, the result a provider's value stores and the feedback and
// refusals around it. vanilla: ComputeCommand

// expectCompute reads the feedback of a /compute and checks its key.
func expectCompute(t *testing.T, c *servertest.Client, key string) protocol.Text {
	t.Helper()
	got := servertest.FeedbackComponent(t, c)
	if got.Translate != key {
		t.Fatalf("feedback = %+v, want %s", got, key)
	}
	return got
}

// TestComputeStoresItsValue checks that a constant provider answers its
// value to /execute store, and that an entity's score is readable through a
// provider and scaled and floored by the float form.
func TestComputeStoresItsValue(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("scoreboard objectives add x dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")

	c.ChatCommand("execute store result score @s x run compute default integer {type:\"minecraft:constant\",value:7}")
	if with := expectCompute(t, c, "command.compute.result.unnamed.exact").With; len(with) != 1 || with[0].Number == nil || *with[0].Number != 7 {
		t.Errorf("exact feedback arguments = %+v, want 7", with)
	}
	expectScore := func(want int64) {
		t.Helper()
		c.ChatCommand("scoreboard players get @s x")
		with := expectScoreboardFeedback(t, c, "commands.scoreboard.players.get.success")
		if len(with) != 3 || number(with[1]) != want {
			t.Errorf("score %+v, want %d", with, want)
		}
	}
	expectScore(7)

	const read = `{type:"minecraft:from_int",input:{type:"minecraft:score",target:"target_entity",score:"x"}}`
	c.ChatCommand("execute store result score @s x run compute entity @s float " + read + " 2")
	expectCompute(t, c, "command.compute.result.unnamed.rounded")
	expectScore(14)

	// 14 * 0.5 floors to 7 and differs from the 14 the provider read.
	c.ChatCommand("compute entity @s float " + read + " 0.5")
	with := expectCompute(t, c, "command.compute.result.unnamed.rounded").With
	if len(with) != 2 || with[0].Float == nil || *with[0].Float != 14 || with[1].Number == nil || *with[1].Number != 7 {
		t.Errorf("rounded feedback arguments = %+v, want 14 and 7", with)
	}

	c.Disconnect()
}

// TestComputeRefusals checks the arithmetic a provider throws, the
// registry entry no pack defines and a provider the codec refuses.
func TestComputeRefusals(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand(`compute default integer {type:"minecraft:div",left:1,right:0}`)
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("command.compute.result.unnamed.invalid", "/ by zero"))

	c.ChatCommand(`compute default float {type:"minecraft:div",left:1,right:0}`)
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("command.compute.result.unnamed.invalid", "Infinity"))

	c.ChatCommand("compute default integer pk:none")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got,
		servertest.CommandErrorMessageForTest("argument.resource_or_id.no_such_element", "pk:none", "minecraft:context_int_provider")) {
		t.Errorf("an unknown provider answered % X, want the no such element refusal", got)
	}
	c.Disconnect()
}

// TestComputeTreeIsSent checks that an operator's command tree carries
// /compute with its three context sources.
func TestComputeTreeIsSent(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()

	var nodes []servertest.TestCommandNode
	for packetID, data := range c.Stream() {
		if packetID == protocol.IDPlayCommands {
			nodes = servertest.DecodeCommands(t, data)
			break
		}
	}
	if nodes == nil {
		t.Fatal("no command tree arrived")
	}
	for _, n := range nodes {
		if n.Typ == 1 && n.Name == "compute" {
			var names []string
			for _, child := range n.Children {
				names = append(names, nodes[child].Name)
			}
			if len(names) != 3 || names[0] != "default" || names[1] != "block" || names[2] != "entity" {
				t.Errorf("compute's children = %v, want default, block, entity", names)
			}
			return
		}
	}
	t.Error("the command tree carries no compute node")
}

// TestComputeProviderThatReadsAsIdIsReference checks that a bare number is an
// identifier and so a registry lookup that fails, and that an inline uniform
// provider reports every missing key.
// vanilla: ResourceOrIdArgument.createGrammar
func TestComputeProviderThatReadsAsIdIsReference(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	const uniform = `{type:"minecraft:uniform",min_inclusive:3,max_exclusive:3.0001}`
	for _, tc := range []struct {
		input, name, registry string
		failure               string
	}{
		{"compute default integer 5", "minecraft:5", "minecraft:context_int_provider", ""},
		{"compute default float 2.5", "minecraft:2.5", "minecraft:context_float_provider", ""},
		{"compute block 0 -61 0 integer 3", "minecraft:3", "minecraft:context_int_provider", ""},
		{"compute entity @s float 1.5", "minecraft:1.5", "minecraft:context_float_provider", ""},
		{"compute default float " + uniform + " 1", "", "",
			"Failed to parse either. First: Not a number; Second: No key max in MapLike[" +
				"{max_exclusive:3.0001d,min_inclusive:3,type:\"minecraft:uniform\"}]; No key min in MapLike[" +
				"{max_exclusive:3.0001d,min_inclusive:3,type:\"minecraft:uniform\"}]"},
	} {
		c.ChatCommand(tc.input)
		if tc.failure != "" {
			c.ExpectCommandError(
				servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse", tc.failure),
				servertest.CommandErrorContextForTest(tc.input, len(tc.input)-2))
			continue
		}
		c.ExpectCommandError(
			servertest.CommandErrorMessageForTest("argument.resource_or_id.no_such_element", tc.name, tc.registry),
			servertest.CommandErrorContextForTest(tc.input, len(tc.input)))
	}
	c.Disconnect()
}
