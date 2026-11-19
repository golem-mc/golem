package plugins_test

import (
	"math"
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

func TestPluginWorldCommandFailures(t *testing.T) {
	ready := false
	runs := 0
	cfg := servertest.BlockTestConfig()
	cfg.CommandSpamThresholdSeconds = 100
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "arguments", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		var children []plugin.Node
		for name, arg := range map[string]plugin.Argument{"pos": plugin.BlockPosArg("value"), "vec": plugin.Vec3Arg("value"), "block": plugin.BlockStateArg("value"), "item": plugin.ItemArg("value"), "text": plugin.TextArg("value"), "world": plugin.DimensionArg("value"), "players": plugin.PlayersArg("value", false), "player": plugin.PlayersArg("value", true)} {
			children = append(children, plugin.Node{Literal: name, Children: []plugin.Node{{Arg: arg, Run: func(*plugin.CommandContext) (int, error) { runs++; return 1, nil }}}})
		}
		// Completing an unresolved preceding argument must never emit a chat failure.
		children = append(children, plugin.Node{Literal: "after", Children: []plugin.Node{{Arg: plugin.DimensionArg("world"), Children: []plugin.Node{{Arg: plugin.WordArg("value"), Suggest: func(*plugin.CommandContext, string) []string {
			t.Error("completion ran after an invalid dimension")
			return nil
		}}}}}})
		if err := s.Commands().Register(plugin.Command{Name: "invalid", Children: children}); err != nil {
			return err
		}
		ready = true
		return nil
	}}}}, servertest.WriteOpsFile(t, servertest.OpRecord{UUID: protocol.OfflineUUID("Tester").String(), Name: "Tester", Level: 2}))
	if !ready {
		t.Fatal("advanced arguments were not registered")
	}
	c := registrationClient(t, srv)
	c.CommandSuggestion(40, "/invalid after nonexistent x")
	_, _, _, matches := registrationSuggestions(t, srv, c)
	if len(matches) != 0 {
		t.Errorf("invalid preceding argument suggestions = %v", matches)
	}
	for _, tc := range []struct {
		line, key string
		context   bool
	}{
		{"invalid pos 1 2", "argument.pos3d.incomplete", true},
		{"invalid pos 1.5 2 3", "parsing.int.invalid", true},
		{"invalid vec 1 ^ 3", "argument.pos.mixed", true},
		{"invalid block unknown_block", "argument.block.id.invalid", true},
		{"invalid block oak_log[axis=bad]", "argument.block.property.invalid", true},
		{"invalid item missing_item", "argument.item.id.invalid", true},
		{"invalid text 42", "argument.component.invalid", true},
		{"invalid world nowhere", "argument.dimension.invalid", false},
		{"invalid players Nobody", "argument.entity.notfound.player", false},
		{"invalid players @e", "argument.player.entities", true},
		{"invalid player @a", "argument.player.toomany", true},
	} {
		c.ChatCommand(tc.line)
		text, ok := protocol.ReadText(protocol.NewReader(c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)))
		if !ok || text.Color != protocol.ColorRed || !textHasTranslation(text, tc.key) {
			t.Fatalf("failure for %q = %+v, want %s", tc.line, text, tc.key)
		}
		if tc.context {
			text, ok = protocol.ReadText(protocol.NewReader(c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)))
			if !ok || !textHasTranslation(text, "command.context.here") {
				t.Fatalf("syntax context for %q = %+v", tc.line, text)
			}
		}
	}
	guest := servertest.DialServer(t, srv)
	guest.ViewAsk = 2
	guest.Join("Guest")
	guest.ChatCommand("invalid players @a")
	text, _ := protocol.ReadText(protocol.NewReader(guest.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)))
	if !textHasTranslation(text, "argument.entity.selector.not_allowed") {
		t.Errorf("non-op selector refusal = %+v", text)
	}
	guest.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)
	if out := srv.RunLine("list"); len(out) != 1 {
		t.Fatal("console synchronization failed")
	}
	if runs != 0 {
		t.Errorf("invalid arguments invoked handler %d times", runs)
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

func TestPluginWorldCommandExecuteContext(t *testing.T) {
	var host plugin.Server
	ready := false
	var context *plugin.CommandContext
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "arguments", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		host = s
		var children []plugin.Node
		for name, arg := range map[string]plugin.Argument{"pos": plugin.BlockPosArg("value"), "vec": plugin.Vec3Arg("value"), "text": plugin.TextArg("value"), "players": plugin.PlayersArg("value", true)} {
			children = append(children, plugin.Node{Literal: name, Children: []plugin.Node{{Arg: arg, Run: func(ctx *plugin.CommandContext) (int, error) { context = ctx; return 9, nil }}}})
		}
		if err := s.Commands().Register(plugin.Command{Name: "context", Children: children}); err != nil {
			return err
		}
		ready = true
		return nil
	}}}})
	if !ready {
		t.Fatal("advanced arguments were not registered")
	}
	registrationClient(t, srv)
	registrationTask(t, srv, host, func() {
		sender := &recordingSender{level: 4}
		for _, tc := range []struct {
			line string
			want any
		}{
			{"execute positioned 10 64 -20 run context pos ~-1.2 ~1 ~-0.6", plugin.BlockPos{X: 9, Y: 65, Z: -21}},
			{"execute positioned 10 64 -20 run context vec ~-1.2 ~1 ~-0.6", plugin.Vec3{X: 9.3, Y: 65, Z: -20.1}},
			{"execute positioned 10 64 -20 rotated 0 0 run context pos ^ ^ ^2", plugin.BlockPos{X: 10, Y: 64, Z: -18}},
			{"execute in the_end run context text {selector:'@a'}", "Tester"},
		} {
			context = nil
			_, err := host.Dispatch(sender, tc.line)
			if err != nil || context == nil {
				t.Errorf("context dispatch %q: %v", tc.line, err)
				continue
			}
			if got, ok := context.Args["value"].(plugin.Vec3); ok {
				want := tc.want.(plugin.Vec3)
				if math.Abs(got.X-want.X) > 1e-6 || math.Abs(got.Y-want.Y) > 1e-6 || math.Abs(got.Z-want.Z) > 1e-6 {
					t.Errorf("%q: vector=%+v, want %+v", tc.line, got, want)
				}
			} else if got, ok := context.Args["value"].(plugin.Text); ok {
				if got.PlainString() != tc.want {
					t.Errorf("resolved text=%#v, want %v", got, tc.want)
				}
			} else if !reflect.DeepEqual(context.Args["value"], tc.want) {
				t.Errorf("%q: argument=%#v, want %#v", tc.line, context.Args["value"], tc.want)
			}
			if context.Sender.Name() != sender.Name() {
				t.Errorf("execute changed original sender to %s", context.Sender.Name())
			}
			if strings.Contains(tc.line, "in the_end") && context.Location.World.Key() != plugin.MinecraftKey("the_end") {
				t.Errorf("execution world = %v", context.Location.World.Key())
			}
		}
		context = nil
		if _, err := host.Dispatch(sender, "execute as Tester at @s run context players @s"); err != nil || context == nil {
			t.Errorf("execute player dispatch: %v", err)
			return
		}
		players := plugin.Arg[[]plugin.Entity](context, "value")
		if len(players) != 1 || context.Entity == nil || players[0].UUID() != context.Entity.UUID() || context.Entity.UUID() != protocol.OfflineUUID("Tester") {
			t.Errorf("execute player context = %#v", context)
		}
	})
}
