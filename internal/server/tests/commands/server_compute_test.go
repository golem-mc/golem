package commands

import (
	"bytes"
	"math"
	"path/filepath"
	"strconv"
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

func TestComputeNamesRegistryProviders(t *testing.T) {
	t.Parallel()
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
		{`compute default integer {type:"minecraft:number_dispatcher",cases:[{},{}]}`,
			missing("value", "{}") + "; " + missing("condition", "{}") + "; " +
				missing("value", "{}") + "; " + missing("condition", "{}")},
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

	// A weighted list whose total weight overflows an int is thrown from the
	// list's constructor, so the command parse fails with no codec wrapping.
	// vanilla: WeightedRandom.getTotalWeight, CommandDispatcher.parseNodes
	weighted := func(w int32) string {
		return `{type:"minecraft:weighted_list",distribution:[{data:1,weight:` + strconv.Itoa(int(w)) + `},{data:2,weight:1}]}`
	}
	// Both entries give 5, so the result at the limit is no draw.
	weightedSame := func(w int32) string {
		return `{type:"minecraft:weighted_list",distribution:[{data:5,weight:` + strconv.Itoa(int(w)) + `},{data:5,weight:1}]}`
	}
	for _, kind := range []string{"integer", "float"} {
		suffix := ""
		if kind == "float" {
			suffix = " 1"
		}
		overflow := "compute default " + kind + " " + weighted(math.MaxInt32) + suffix
		c.ChatCommand(overflow)
		data := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
		if want := servertest.CommandErrorMessageForTest("command.exception", "Sum of weights must be <= 2147483647"); !bytes.Equal(data, want) {
			t.Errorf("%s: refusal = % X, want % X", overflow, data, want)
		}
		data = c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
		if want := servertest.CommandErrorContextForTest(overflow, len(overflow)-len(suffix)); !bytes.Equal(data, want) {
			t.Errorf("%s: context = % X, want % X", overflow, data, want)
		}

		// The exact limit parses and runs.
		c.ChatCommand("compute default " + kind + " " + weightedSame(math.MaxInt32-1) + suffix)
		servertest.CheckCommandFeedback(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat),
			"command.compute.result.unnamed.exact", protocol.Text{Number: new(int64(5))})
	}

	// Only a genuine overflow is thrown: a constant provider missing its value
	// whose extra key spells the message is an ordinary codec refusal.
	// vanilla: MapCodec.decode
	spoof := `compute default integer {type:"minecraft:constant",note:"Sum of weights must be <= 2147483647"}`
	c.ChatCommand(spoof)
	if got, want := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), servertest.CommandErrorMessageForTest("command.exception", "Sum of weights must be <= 2147483647"); bytes.Equal(got, want) {
		t.Errorf("%s: answered the overflow", spoof)
	}
	c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)

	// A table with an overflowing weighted list is no codec error either.
	const loot = `{pools:[{rolls:1,entries:[{type:"minecraft:item",name:"minecraft:stick",modifier:{type:"minecraft:set_count",count:`
	inline := "loot give @s loot " + loot + `{type:"minecraft:weighted_list",distribution:[{data:1,weight:2147483647},{data:2,weight:1}]}}}]}]}`
	c.ChatCommand(inline)
	data := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
	if want := servertest.CommandErrorMessageForTest("command.exception", "Sum of weights must be <= 2147483647"); !bytes.Equal(data, want) {
		t.Errorf("inline loot overflow = % X, want % X", data, want)
	}
	c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)

	// The overflow also fails a provider inside a condition, an inline item
	// modifier (one function or a list), a predicate argument, and a list
	// whose other entry has a codec error: the sum covers the entries kept.
	// vanilla: ConditionalProvider.mapCodec, SetItemCountFunction.CODEC, IntValueCheck.CODEC,
	// ListCodec.decode, WeightedList.entryToListCodec
	w := weighted(math.MaxInt32)
	for _, command := range []string{
		`compute default integer {type:"weighted_list",distribution:[{data:1,weight:2147483647},{data:2,weight:1},{weight:1}]}`,
		`compute default integer {type:"minecraft:conditional",condition:{type:"minecraft:int_value_check",value:` + w + `,test:1},on_true:1}`,
		`item modify entity @s hotbar.0 {type:"minecraft:set_count",count:` + w + `}`,
		`item modify entity @s hotbar.0 [{type:"minecraft:set_count",count:` + w + `}]`,
		`execute if predicate {type:"minecraft:int_value_check",value:` + w + `,test:1}`,
	} {
		c.ChatCommand(command)
		data := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
		if want := servertest.CommandErrorMessageForTest("command.exception", "Sum of weights must be <= 2147483647"); !bytes.Equal(data, want) {
			t.Errorf("%s: refusal = % X, want % X", command, data, want)
		}
		data = c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
		if want := servertest.CommandErrorContextForTest(command, len(command)); !bytes.Equal(data, want) {
			t.Errorf("%s: context = % X, want % X", command, data, want)
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
