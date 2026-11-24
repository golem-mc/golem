package commands

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
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

// Run serially because data pack providers are process-wide.
func TestComputeNamesRegistryProviders(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackContextProviders(nil, nil) })
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "compute")
	writeFile(t, filepath.Join(pack, "pack.mcmeta"), `{"pack":{"description":"Compute providers","min_format":121,"max_format":121}}`)
	const constant = `{"type":"minecraft:constant","value":17}`
	writeFile(t, filepath.Join(pack, "data", "review436", "context_int_provider", "keep.json"), constant)
	writeFile(t, filepath.Join(pack, "data", "review436", "context_float_provider", "keep.json"), constant)
	_, c := joinPacks(t, cfg)

	name := protocol.Text{Text: "review436:keep"}
	exact, rounded, original := int64(17), int64(8), float32(17)
	for _, tc := range []struct {
		input, key string
		with       []protocol.Text
	}{
		{"integer review436:keep", "command.compute.result.named.exact", []protocol.Text{name, {Number: &exact}}},
		{"float review436:keep", "command.compute.result.named.exact", []protocol.Text{name, {Number: &exact}}},
		{"float review436:keep 0.5", "command.compute.result.named.rounded", []protocol.Text{name, {Float: &original}, {Number: &rounded}}},
		{"integer " + constant, "command.compute.result.unnamed.exact", []protocol.Text{{Number: &exact}}},
		{"float " + constant + " 0.5", "command.compute.result.unnamed.rounded", []protocol.Text{{Float: &original}, {Number: &rounded}}},
	} {
		c.ChatCommand("compute default " + tc.input)
		c.ExpectCommandFeedbackText(tc.key, tc.with...)
	}
	c.Disconnect()
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

// Run serially because data pack providers are process-wide.
// vanilla: ComputeCommand.throwInvalidValue
func TestComputeInvalidProviderFeedback(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackContextProviders(nil, nil) })
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	const division = `{"type":"minecraft:div","left":1,"right":0}`
	servertest.WritePackFile(t, cfg, "compute", "pk/context_int_provider/div_zero.json", division)
	servertest.WritePackFile(t, cfg, "compute", "pk/context_float_provider/div_zero.json", division)
	srv, c := joinPacks(t, cfg)
	servertest.HoldTicks(t, srv)

	for _, tc := range []struct{ kind, reason string }{
		{"integer", "/ by zero"},
		{"float", "Infinity"},
	} {
		c.Run(t, tc.kind, func(t *testing.T) {
			command := "compute default " + tc.kind + " "
			c.ChatCommand(command + "pk:div_zero")
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("command.compute.result.named.invalid", "pk:div_zero", tc.reason))

			c.ChatCommand(command + division)
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("command.compute.result.unnamed.invalid", tc.reason))
		})
	}
	c.Disconnect()
}

// TestComputeRefusals checks the arithmetic a provider throws, the
// registry entry no pack defines and a provider the codec refuses: every
// missing key of a record is reported, in DFU's order (the reverse of the
// codec's group(...)).
// vanilla: RecordCodecBuilder, EitherCodec.decode, ListCodec.decode
func TestComputeRefusals(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand(`compute default integer {type:"minecraft:div",left:1,right:0}`)
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("command.compute.result.unnamed.invalid", "/ by zero"))

	c.ChatCommand(`compute default float {type:"minecraft:div",left:1,right:0}`)
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("command.compute.result.unnamed.invalid", "Infinity"))

	c.ChatCommand("compute default integer pk:none")
	if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got,
		servertest.CommandErrorMessageForTest("argument.resource_or_id.no_such_element", "pk:none", "minecraft:context_int_provider")) {
		t.Errorf("an unknown provider answered % X, want the no such element refusal", got)
	}
	c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)

	// Array and zero-weight lists the shared list decoder accepts; both forms
	// send an exact result as an integer.
	// vanilla: ComputeCommand.printExactOutput
	for _, tc := range []struct {
		command string
		want    int64
	}{
		{`compute default integer {type:"minecraft:add",inputs:[B;1b,2b]}`, 3},
		{`compute default float {type:"minecraft:add",inputs:[L;2L,3L]} 1`, 5},
		{`compute default float {type:"minecraft:add",inputs:[L;9007199791611905L,-9007199254740992L]} 1`, 1073741824},
		{`compute default integer {type:"minecraft:weighted_list",distribution:[{data:5,weight:1},{data:9,weight:0}]}`, 5},
	} {
		c.ChatCommand(tc.command)
		r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat))
		got, ok := protocol.ReadText(r)
		if !ok {
			t.Fatalf("%s: decode feedback: %v", tc.command, r.Err())
		}
		if got.Translate != "command.compute.result.unnamed.exact" || len(got.With) != 1 || got.With[0].Number == nil || *got.With[0].Number != tc.want {
			t.Errorf("%s: feedback = %+v, want exact %d", tc.command, got, tc.want)
		}
	}

	// A refusal's wording: the typed branch of the number-or-provider codec,
	// or the case list's own error where an element failed.
	const either = "Failed to parse either. First: Not a number; Second: "
	const invalidDefault = `{type:"minecraft:constant"}`
	missing := func(key, of string) string { return "No key " + key + " in MapLike[" + of + "]" }
	defaultErr := either + missing("value", invalidDefault)
	caseErr := missing("value", "{}") + "; " + missing("condition", "{}")
	for _, tc := range []struct{ command, failure string }{
		{`compute default float {type:"minecraft:uniform",min_inclusive:3,max_exclusive:3.0001} 1`,
			either + missing("max", `{max_exclusive:3.0001d,min_inclusive:3,type:"minecraft:uniform"}`) + "; " +
				missing("min", `{max_exclusive:3.0001d,min_inclusive:3,type:"minecraft:uniform"}`)},
		{`compute default integer {type:"minecraft:sub"}`,
			either + missing("right", `{type:"minecraft:sub"}`) + "; " + missing("left", `{type:"minecraft:sub"}`)},
		{`compute default integer {type:"minecraft:sub",left:{type:"minecraft:constant"}}`,
			either + missing("right", `{left:{type:"minecraft:constant"},type:"minecraft:sub"}`) + "; " + either +
				missing("value", `{type:"minecraft:constant"}`)},
		{`compute default integer {type:"minecraft:binomial"}`,
			either + missing("p", `{type:"minecraft:binomial"}`) + "; " + missing("n", `{type:"minecraft:binomial"}`)},
		{`compute default integer {type:"minecraft:score"}`,
			either + missing("score", `{type:"minecraft:score"}`) + "; " + missing("target", `{type:"minecraft:score"}`)},
		{`compute default integer {type:"minecraft:storage"}`,
			either + missing("path", `{type:"minecraft:storage"}`) + "; " + missing("storage", `{type:"minecraft:storage"}`)},
		{`compute default integer {type:"minecraft:number_dispatcher",cases:[{}],default:` + invalidDefault + `}`,
			defaultErr + "; " + caseErr},
		{`compute default integer {type:"minecraft:number_dispatcher",cases:[{},1],default:` + invalidDefault + `}`,
			defaultErr + "; Not a map: 1; " + caseErr},
		{`compute default float {type:"minecraft:number_dispatcher",cases:[{}],default:` + invalidDefault + `} 1`,
			defaultErr + "; " + caseErr},
		{`compute default integer {type:"minecraft:number_dispatcher",cases:[],default:` + invalidDefault + `}`,
			defaultErr},
		{`compute default integer {type:"minecraft:number_dispatcher",cases:1,default:` + invalidDefault + `}`,
			either + defaultErr + "; Not a list: 1"},
		{`compute default integer {type:"minecraft:conditional"}`,
			either + missing("on_true", `{type:"minecraft:conditional"}`) + "; " + missing("condition", `{type:"minecraft:conditional"}`)},
		{`compute default integer {type:"minecraft:number_dispatcher",cases:[{}]}`,
			missing("value", "{}") + "; " + missing("condition", "{}")},
		{`compute default integer {type:"minecraft:number_dispatcher",cases:[B;1b,2b]}`, "Not a map: 2b; Not a map: 1b"},
		{`compute default integer {type:"minecraft:weighted_list",distribution:[B;1b,2b]}`,
			either + "Not a map: 2b; Not a map: 1b; Weighted list must contain at least one entry with non-zero weight"},
		{`compute default integer {type:"minecraft:weighted_list",distribution:[L;]}`,
			either + "Weighted list must contain at least one entry with non-zero weight"},
		{`compute default integer {type:"minecraft:weighted_list",distribution:[1,{data:5,weight:0}]}`,
			either + "Not a map: 1; Weighted list must contain at least one entry with non-zero weight"},
		{`compute default integer {type:"minecraft:weighted_list",distribution:[1,{data:5,weight:1}]}`, "Not a map: 1"},
		{`compute default integer {type:"minecraft:weighted_list",distribution:[{data:9,weight:0},{data:5}]}`,
			either + missing("weight", "{data:5}") + "; Weighted list must contain at least one entry with non-zero weight"},
		{`compute default integer {type:"minecraft:weighted_list",distribution:[{data:5,weight:-1}]}`,
			either + "Value must be non-negative: -1; Weighted list must contain at least one entry with non-zero weight"},
		{`compute default integer {type:"minecraft:weighted_list",distribution:[{data:{type:"minecraft:number_dispatcher",cases:[B;1b,2b]},weight:1}]}`,
			"Not a map: 2b; Not a map: 1b"},
		{`compute default integer {type:"minecraft:number_dispatcher",cases:[L;1L,2L]}`, "Not a map: 2L; Not a map: 1L"},
		{`compute default integer {type:"minecraft:number_dispatcher",cases:[{},{}]}`,
			missing("value", "{}") + "; " + missing("condition", "{}") + "; " +
				missing("value", "{}") + "; " + missing("condition", "{}")},
		{`compute default integer {type:"minecraft:weighted_list",distribution:[{data:1,weight:"abc"}]}`,
			either + "Not a number; Weighted list must contain at least one entry with non-zero weight"},
		{`compute default integer {type:"minecraft:weighted_list",distribution:[{data:1,weight:"abc"},{data:2,weight:1}]}`,
			"Not a number"},
		{`compute default float {type:"minecraft:weighted_list",distribution:[{data:1,weight:"abc"},{data:2,weight:1}]} 1`,
			"Not a number"},
		{`compute default integer {type:"minecraft:weighted_list",distribution:[{data:1,weight:"abc"},{weight:1}]}`,
			either + missing("data", "{weight:1}") + "; Not a number; Weighted list must contain at least one entry with non-zero weight"},
		{`compute default integer {type:"minecraft:weighted_list",distribution:[{data:1,weight:"abc"},{data:2}]}`,
			either + missing("weight", "{data:2}") + "; Not a number; Weighted list must contain at least one entry with non-zero weight"},
		{`compute default integer {type:"minecraft:weighted_list",distribution:[{data:1,weight:"abc"},{data:{type:"minecraft:constant"},weight:1}]}`,
			either + either + missing("value", invalidDefault) + "; Not a number; Weighted list must contain at least one entry with non-zero weight"},
		{`compute default integer {type:"minecraft:number_dispatcher",cases:[{},1]}`,
			"Not a map: 1; " + missing("value", "{}") + "; " + missing("condition", "{}")},
	} {
		c.ChatCommand(tc.command)
		suffix := 0
		if strings.HasSuffix(tc.command, " 1") {
			suffix = 2
		}
		data := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
		if want := servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse", tc.failure); !bytes.Equal(data, want) {
			t.Errorf("%s: refusal = % X, want % X", tc.command, data, want)
		}
		data = c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
		if want := servertest.CommandErrorContextForTest(tc.command, len(tc.command)-suffix); !bytes.Equal(data, want) {
			t.Errorf("%s: context = % X, want % X", tc.command, data, want)
		}
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
// identifier and so a registry lookup that fails.
// vanilla: ResourceOrIdArgument.createGrammar
func TestComputeProviderThatReadsAsIdIsReference(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	for _, tc := range []struct {
		input, name, registry string
	}{
		{"compute default integer 5", "minecraft:5", "minecraft:context_int_provider"},
		{"compute default float 2.5", "minecraft:2.5", "minecraft:context_float_provider"},
		{"compute block 0 -61 0 integer 3", "minecraft:3", "minecraft:context_int_provider"},
		{"compute entity @s float 1.5", "minecraft:1.5", "minecraft:context_float_provider"},
	} {
		c.ChatCommand(tc.input)
		c.ExpectCommandError(
			servertest.CommandErrorMessageForTest("argument.resource_or_id.no_such_element", tc.name, tc.registry),
			servertest.CommandErrorContextForTest(tc.input, len(tc.input)))
	}
	c.Disconnect()
}
