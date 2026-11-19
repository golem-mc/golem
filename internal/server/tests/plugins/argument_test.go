package plugins_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BlockPosArgument, Vec3Argument, BlockStateArgument, ItemArgument,
// ComponentArgument, DimensionArgument, EntityArgument.
func TestPluginWorldCommandArguments(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		arg   plugin.Argument
		input string
		check func(*testing.T, any)
	}{
		{"blockpos", plugin.BlockPosArg("value"), "-2 64 3", func(t *testing.T, value any) {
			if value != (plugin.BlockPos{X: -2, Y: 64, Z: 3}) {
				t.Errorf("block position = %#v", value)
			}
		}},
		{"vec", plugin.Vec3Arg("value"), "-2 64 3", func(t *testing.T, value any) {
			if value != (plugin.Vec3{X: -1.5, Y: 64, Z: 3.5}) {
				t.Errorf("vector = %#v", value)
			}
		}},
		{"block", plugin.BlockStateArg("value"), `oak_log[axis = x]`, func(t *testing.T, value any) {
			state, ok := value.(plugin.BlockState)
			axis, _ := state.Prop(registry.PropAxis)
			if !ok || state.Block() != registry.BlockOakLog || axis != "x" {
				t.Errorf("block state = %#v, axis=%s", value, axis)
			}
		}},
		{"item", plugin.ItemArg("value"), `diamond_sword[custom_name={text:'two words'},damage=3]`, func(t *testing.T, value any) {
			stack, ok := value.(plugin.ItemStack)
			if !ok || stack.Item != registry.ItemDiamondSword || stack.Count != 1 || stack.Patch.Damage == nil || *stack.Patch.Damage != 3 || stack.Patch.CustomName == nil || stack.Patch.CustomName.PlainString() != "two words" {
				t.Errorf("item = %#v", value)
			}
		}},
		{"text", plugin.TextArg("value"), `{text:'two words',bold:true,extra:[{text:'!'}]}`, func(t *testing.T, value any) {
			text, ok := value.(plugin.Text)
			if !ok || text.PlainString() != "two words!" || text.Style.Bold == nil || !*text.Style.Bold {
				t.Errorf("text = %#v", value)
			}
		}},
		{"dimension", plugin.DimensionArg("value"), "the_end", func(t *testing.T, value any) {
			w, ok := value.(plugin.World)
			if !ok || w.Key() != plugin.MinecraftKey("the_end") {
				t.Errorf("dimension = %#v", value)
			}
		}},
		{"players", plugin.PlayersArg("value", false), "@a[name=Tester]", func(t *testing.T, value any) {
			entities, ok := value.([]plugin.Entity)
			if !ok || len(entities) != 1 {
				t.Errorf("players = %#v", value)
				return
			}
			p, ok := entities[0].(plugin.Player)
			if !ok || p.Name() != "Tester" || !p.IsValid() {
				t.Errorf("player = %#v", entities[0])
			}
		}},
		{"player", plugin.PlayersArg("value", true), "Tester", func(t *testing.T, value any) {
			entities, ok := value.([]plugin.Entity)
			if !ok || len(entities) != 1 || entities[0].UUID() != protocol.OfflineUUID("Tester") {
				t.Errorf("player = %#v", value)
			}
		}},
	}
	ready := false
	cfg := servertest.BlockTestConfig()
	cfg.CommandSpamThresholdSeconds = 100
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "arguments", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		var children []plugin.Node
		for _, tc := range cases {
			children = append(children, plugin.Node{Literal: tc.name, Children: []plugin.Node{{Arg: tc.arg, Children: []plugin.Node{{Arg: plugin.IntArg("tail", 1, 1), Run: func(ctx *plugin.CommandContext) (int, error) {
				s.Players()
				tc.check(t, ctx.Args["value"])
				if len(ctx.Args) != 2 || plugin.Arg[int32](ctx, "tail") != 1 {
					t.Errorf("%s arguments = %#v", tc.name, ctx.Args)
				}
				ctx.Sender.SendMessage(plugin.Plain("parsed " + tc.name))
				return 1, nil
			}}}}}})
		}
		if err := s.Commands().Register(plugin.Command{Name: "typed", Aliases: []string{"ty"}, Children: children}); err != nil {
			t.Errorf("register advanced command: %v", err)
			return err
		}
		ready = true
		return nil
	}}}}, servertest.WriteOpsFile(t, servertest.OpRecord{UUID: protocol.OfflineUUID("Tester").String(), Name: "Tester", Level: 2}))
	if !ready {
		t.Fatal("advanced arguments were not registered")
	}
	c := registrationClient(t, srv)
	for _, tc := range cases {
		for _, name := range []string{"typed", "arguments:typed", "ty", "arguments:ty"} {
			c.ChatCommand(name + " " + tc.name + " " + tc.input + " 1")
			registrationFeedback(t, srv, c, "parsed "+tc.name)
		}
	}
}

func TestPluginWorldCommandSuggestions(t *testing.T) {
	t.Parallel()
	var host plugin.Server
	ready := false
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "arguments", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		host = s
		var children []plugin.Node
		for name, arg := range map[string]plugin.Argument{"pos": plugin.BlockPosArg("value"), "vec": plugin.Vec3Arg("value"), "block": plugin.BlockStateArg("value"), "item": plugin.ItemArg("value"), "world": plugin.DimensionArg("value"), "players": plugin.PlayersArg("value", false)} {
			children = append(children, plugin.Node{Literal: name, Children: []plugin.Node{{Arg: arg}}})
		}
		if err := s.Commands().Register(plugin.Command{Name: "complete", Children: children}); err != nil {
			return err
		}
		ready = true
		return nil
	}}}})
	if !ready {
		t.Fatal("advanced suggestions were not registered")
	}
	c := registrationClient(t, srv)
	for _, tc := range []struct {
		input   string
		start   int32
		matches []string
	}{
		{"/complete block oak_log[ax", 24, []string{"axis="}},
		{"/complete block oak_log[axis=", 29, []string{"x", "y", "z"}},
		{"/complete item diamond_sw", 15, []string{"minecraft:diamond_sword"}},
		{"/complete item stone", 20, []string{"["}},
		{"/complete item stone[custom_na", 21, []string{"!", "minecraft:custom_name="}},
		{"/complete item stone[!custom_na", 22, []string{"minecraft:custom_name"}},
		{"/complete item stone[damage", 27, []string{"="}},
		{"/complete item stone[damage=1", 29, []string{",", "]"}},
		{"/complete item stone[custom_name='two words',dam", 45, []string{"!", "minecraft:damage=", "minecraft:damage_resistant=", "minecraft:damage_type=", "minecraft:max_damage="}},
		{"/complete world the_", 16, []string{"minecraft:the_end", "minecraft:the_nether"}},
		{"/complete players Tes", 18, []string{"Tester"}},
		{"/complete players @", 0, nil},
		{"/complete pos ", 14, []string{"~", "~ ~", "~ ~ ~"}},
		{"/complete vec ^", 14, []string{"^ ^", "^ ^ ^"}},
		{"/complete vec ~ ~", 14, []string{"~ ~ ~"}},
	} {
		c.CommandSuggestion(33, tc.input)
		id, start, length, matches := registrationSuggestions(t, srv, c)
		wantLength := int32(len(tc.input)) - tc.start
		if len(tc.matches) == 0 {
			wantLength = 0
		}
		if id != 33 || start != tc.start || length != wantLength || !slices.Equal(matches, tc.matches) {
			t.Errorf("suggest %q: %d %d %d %v, want at %d: %v", tc.input, id, start, length, matches, tc.start, tc.matches)
		}
	}
	registrationTask(t, srv, host, func() {
		for _, arg := range []plugin.Argument{plugin.BlockPosArg("first"), plugin.Vec3Arg("first"), plugin.ItemArg("first"), plugin.TextArg("first"), plugin.DimensionArg("first"), plugin.PlayersArg("first", false)} {
			name := strings.ReplaceAll(arg.Parser.Path, "_", "")
			if err := host.Commands().Register(plugin.Command{Name: "after" + name, Children: []plugin.Node{{Arg: arg, Children: []plugin.Node{{Arg: plugin.WordArg("last"), Suggest: func(ctx *plugin.CommandContext, partial string) []string {
				if len(ctx.Args) != 1 || reflect.TypeOf(ctx.Args["first"]) == nil || partial != "ok" {
					t.Errorf("advanced suggestion context = %#v, partial=%q", ctx.Args, partial)
				}
				host.Players()
				return []string{"okay"}
			}}}}}}); err != nil {
				t.Error(err)
			}
		}
	})
	for _, input := range []string{"afterblockpos 1 2 3 ok", "aftervec3 1 2 3 ok", `afteritemstack stone[custom_name='two words'] ok`, `aftercomponent {text:'two words'} ok`, "afterdimension overworld ok", "afterentity Tester ok"} {
		c.CommandSuggestion(34, "/"+input)
		_, start, _, matches := registrationSuggestions(t, srv, c)
		if start != int32(len(input)-1) || !slices.Equal(matches, []string{"okay"}) {
			t.Errorf("suggest context %q: start=%d matches=%v", input, start, matches)
		}
	}
}

func textHasTranslation(text protocol.Text, key string) bool {
	if text.Translate == key {
		return true
	}
	for _, child := range text.Extra {
		if textHasTranslation(child, key) {
			return true
		}
	}
	return false
}
