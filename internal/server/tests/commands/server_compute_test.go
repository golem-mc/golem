package commands

import (
	"bytes"
	"path/filepath"
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

// TestComputeStoresItsValue checks that a constant provider answers its
// value to /execute store, and that an entity's score is readable through a
// provider and scaled and floored by the float form.
func TestComputeStoresItsValue(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("scoreboard objectives add x dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")

	c.ChatCommand("execute store result score @s x run compute default integer 7")
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

// TestComputeIntProviders checks the exact integer arithmetic of the inline
// providers in one world: results, Java's overflow and floor semantics and a
// storage read.
func TestComputeIntProviders(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand("data merge storage t:v {n:41}")
	servertest.FeedbackComponent(t, c)

	for _, tc := range []struct {
		name, provider string
		want           int64
		refusal        string
	}{
		{"pow", `{type:"minecraft:pow",base:2,exponent:10}`, 1024, ""},
		{"pow overflow", `{type:"minecraft:pow",base:2,exponent:31}`, 0, "integer overflow"},
		{"mul overflow", `{type:"minecraft:mul",inputs:[65536,65536]}`, 0, "Value 4294967296 can't be safely converted to int"},
		{"floor_mod negative left", `{type:"minecraft:floor_mod",left:-7,right:3}`, 2, ""},
		{"floor_mod negative right", `{type:"minecraft:floor_mod",left:7,right:-3}`, -2, ""},
		{"floor_div negative", `{type:"minecraft:floor_div",left:-7,right:2}`, -4, ""},
		{"storage", `{type:"minecraft:storage",storage:"t:v",path:"n"}`, 41, ""},
		{"conditional true", `{type:"minecraft:conditional",condition:{condition:"minecraft:random_chance",chance:1.0f},on_true:5,on_false:6}`, 5, ""},
		{"conditional false", `{type:"minecraft:conditional",condition:{condition:"minecraft:random_chance",chance:0.0f},on_true:5,on_false:6}`, 6, ""},
		{"conditional absent on_false", `{type:"minecraft:conditional",condition:{condition:"minecraft:random_chance",chance:0.0f},on_true:5}`, 0, ""},
		{"dispatcher first match", `{type:"minecraft:number_dispatcher",cases:[{condition:{condition:"minecraft:random_chance",chance:0.0f},value:1},{condition:{condition:"minecraft:random_chance",chance:1.0f},value:2},{condition:{condition:"minecraft:random_chance",chance:1.0f},value:3}],default:4}`, 2, ""},
		{"dispatcher default", `{type:"minecraft:number_dispatcher",cases:[{condition:{condition:"minecraft:random_chance",chance:0.0f},value:1}],default:4}`, 4, ""},
	} {
		c.ChatCommand("compute default integer " + tc.provider)
		if tc.refusal != "" {
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("command.compute.result.unnamed.invalid", tc.refusal))
			continue
		}
		with := expectCompute(t, c, "command.compute.result.unnamed.exact").With
		if len(with) != 1 || with[0].Number == nil || *with[0].Number != tc.want {
			t.Errorf("%s: feedback arguments = %+v, want %d", tc.name, with, tc.want)
		}
	}
	c.Disconnect()
}

// TestComputeFloatConditional checks the float forms of conditional and
// number_dispatcher, whose absent branch is 0.
func TestComputeFloatConditional(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand(`compute default float {type:"minecraft:conditional",condition:{condition:"minecraft:random_chance",chance:1.0f},on_true:3.0f}`)
	expectCompute(t, c, "command.compute.result.unnamed.exact")
	c.ChatCommand(`compute default float {type:"minecraft:number_dispatcher",cases:[],default:2.5f}`)
	with := expectCompute(t, c, "command.compute.result.unnamed.rounded").With
	if len(with) != 2 || with[0].Float == nil || *with[0].Float != 2.5 || with[1].Number == nil || *with[1].Number != 2 {
		t.Errorf("dispatcher feedback arguments = %+v, want 2.5 and 2", with)
	}
	c.Disconnect()
}

// TestComputeEnchantmentLevel checks that enchantment_level, a float-only
// type, reads its amount at level 0 and is an unknown key for integers.
func TestComputeEnchantmentLevel(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")

	c.ChatCommand(`compute default float {type:"minecraft:enchantment_level",amount:{type:"minecraft:linear",base:2.0f,per_level_above_first:0.5f}}`)
	with := expectCompute(t, c, "command.compute.result.unnamed.rounded").With
	if len(with) != 2 || with[0].Float == nil || *with[0].Float != 1.5 {
		t.Errorf("enchantment_level feedback arguments = %+v, want 1.5", with)
	}
	input := `compute default integer {type:"minecraft:enchantment_level",amount:1}`
	c.ChatCommand(input)
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse", "Failed to parse either. First: Not a number; Second: Unknown registry key in ResourceKey[minecraft:root / minecraft:context_int_provider_type]: minecraft:enchantment_level"),
		servertest.CommandErrorContextForTest(input, len(input)),
	)

	// A nested provider field is an either too, so it wraps twice.
	nested := `compute default integer {type:"minecraft:negate",input:{type:"minecraft:enchantment_level",amount:1}}`
	c.ChatCommand(nested)
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse", "Failed to parse either. First: Not a number; Second: Failed to parse either. First: Not a number; Second: Unknown registry key in ResourceKey[minecraft:root / minecraft:context_int_provider_type]: minecraft:enchantment_level"),
		servertest.CommandErrorContextForTest(nested, len(nested)),
	)
	c.Disconnect()
}

// TestComputeEnvironmentAttribute checks the environment_attribute
// provider against the values a vanilla 26.3 server computes: the day
// timeline's linear, eased and maximum tracks, a dimension type's constant,
// a settled thunderstorm's blend, and the refusals of an attribute that is not a float, of the integer form
// and of an unknown name.
func TestComputeEnvironmentAttribute(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	run := func(cmd string) {
		t.Helper()
		c.ChatCommand(cmd)
		servertest.FeedbackComponent(t, c)
	}
	run("gamerule advance_time false")
	run("weather clear")

	attr := func(name string) string {
		return `{type:"minecraft:environment_attribute",attribute:"minecraft:` + name + `"}`
	}
	expectFloat := func(cmd string, want float32) {
		t.Helper()
		c.ChatCommand(cmd)
		got := servertest.FeedbackComponent(t, c)
		var v float32
		switch with := got.With; {
		case len(with) > 0 && with[0].Float != nil:
			v = *with[0].Float
		case len(with) > 0 && with[0].Number != nil:
			v = float32(*with[0].Number)
		default:
			t.Errorf("%s: feedback = %+v, want %v", cmd, got, want)
			return
		}
		if v != want {
			t.Errorf("%s = %v, want %v", cmd, v, want)
		}
	}
	for _, tc := range []struct {
		time  string
		name  string
		where string
		want  float32
	}{
		{"18000", "gameplay/sky_light_level", "", 4},
		{"18000", "visual/sun_angle", "", 180},
		{"18000", "visual/moon_angle", "", 360},
		{"18000", "visual/star_brightness", "", 0.5},
		{"18000", "visual/sky_light_factor", "", 0.24},
		{"18000", "visual/cloud_height", "", 192.33},
		{"18000", "visual/sky_light_factor", "execute in minecraft:the_end run ", 0},
		{"18000", "gameplay/sky_light_level", "execute in minecraft:the_nether run ", 4},
		{"13000", "gameplay/sky_light_level", "", 8.087632},
		{"13000", "visual/sun_angle", "", 93.52133},
		{"13000", "visual/star_brightness", "", 0.38942683},
		{"1000", "visual/sun_angle", "", 297.54572},
		{"1000", "gameplay/turtle_egg_hatch_chance", "", 0.002},
		{"1000", "gameplay/surface_slime_spawn_chance", "", 0.5},
	} {
		run("time set " + tc.time)
		expectFloat(tc.where+"compute default float "+attr(tc.name), tc.want)
	}

	// Rain and thunder levels rise by 0.01 a tick in float, so 110 ticks
	// settle both clamped at 1, where only the thunder entries apply.
	run("time set 6000")
	servertest.HoldTicks(t, srv)
	run("weather thunder")
	servertest.RunTicks(t, srv, 110)
	c.Settle()
	expectFloat("compute default float "+attr("gameplay/sky_light_level"), 9.199219)
	expectFloat("compute default float "+attr("visual/sky_light_factor"), 0.5992187)
	expectFloat("compute default float "+attr("visual/star_brightness"), 0)

	for _, tc := range []struct{ input, msg string }{
		{"compute default float " + attr("visual/sky_color"), "minecraft:visual/sky_color cannot be converted to a float"},
		{"compute default integer " + attr("gameplay/sky_light_level"), "minecraft:gameplay/sky_light_level cannot be converted to an integer"},
		{"compute default float " + attr("nope"), "Unknown registry key in ResourceKey[minecraft:root / minecraft:environment_attribute]: minecraft:nope"},
	} {
		c.ChatCommand(tc.input)
		c.ExpectCommandError(
			servertest.CommandErrorMessageForTest("argument.resource_or_id.failed_to_parse", "Failed to parse either. First: Not a number; Second: "+tc.msg),
			servertest.CommandErrorContextForTest(tc.input, len(tc.input)),
		)
	}
	c.Disconnect()
}

// TestComputeRegistryEntries checks that an identifier names a built-in
// context provider entry, that a data pack's entry resolves, also as an
// element nested in another entry, and that entries referring to themselves
// end in a refusal. The pack layer is process-wide, so this test is not
// parallel and uses entries no other test names.
// vanilla: ContextIntProviders.CODEC, ContextFloatProviders.CODEC
func TestComputeRegistryEntries(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackContextProviders(nil, nil) })
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "pk")
	writeFile(t, filepath.Join(data, "context_int_provider", "five.json"), `5`)
	writeFile(t, filepath.Join(data, "context_int_provider", "sum.json"), `{"type":"minecraft:add","inputs":["pk:five",2]}`)
	writeFile(t, filepath.Join(data, "context_int_provider", "loop.json"), `{"type":"minecraft:add","inputs":["pk:loop"]}`)
	writeFile(t, filepath.Join(data, "context_float_provider", "half.json"), `0.5`)
	_, c := joinPacks(t, cfg)

	exact := func(command string, want float64) {
		t.Helper()
		c.ChatCommand(command)
		with := expectCompute(t, c, "command.compute.result.unnamed.exact").With
		switch {
		case len(with) != 1:
			t.Errorf("%s: feedback arguments = %+v", command, with)
		case with[0].Number != nil && float64(*with[0].Number) != want, with[0].Float != nil && float64(*with[0].Float) != want:
			t.Errorf("%s: feedback arguments = %+v, want %v", command, with, want)
		}
	}
	exact("compute default integer minecraft:brewing/uses_default", 20)
	exact("compute default float minecraft:cooking/normal_speed_multiplier", 1)
	exact("compute default float minecraft:cooking/speed_default", 1)
	exact("compute default integer pk:sum", 7)
	exact("compute default integer "+`{type:"minecraft:add",inputs:["pk:sum",1]}`, 8)

	c.ChatCommand("compute default float pk:half")
	with := expectCompute(t, c, "command.compute.result.unnamed.rounded").With
	if len(with) != 2 || with[0].Float == nil || *with[0].Float != 0.5 {
		t.Errorf("pack float feedback arguments = %+v, want 0.5", with)
	}

	c.ChatCommand("compute default integer pk:loop")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("command.compute.result.unnamed.invalid", "context provider pk:loop refers to itself"))
}
