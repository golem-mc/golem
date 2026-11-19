package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
)

// vanilla: EntityArgument, EntitySelector, CommandSourceStack.getEntity
func TestPluginEntitiesArgument(t *testing.T) {
	var many, single []plugin.Entity
	var asEntity plugin.Entity
	ready := false
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "entities", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		reply := func(ctx *plugin.CommandContext) (int, error) {
			ctx.Sender.SendMessage(plugin.Plain("ran"))
			return 1, nil
		}
		err := s.Commands().Register(plugin.Command{Name: "many", Children: []plugin.Node{{Arg: plugin.EntitiesArg("targets", false), Run: func(ctx *plugin.CommandContext) (int, error) {
			many, _ = ctx.Args["targets"].([]plugin.Entity)
			return reply(ctx)
		}}}})
		if err == nil {
			err = s.Commands().Register(plugin.Command{Name: "one", Children: []plugin.Node{{Arg: plugin.EntitiesArg("target", true), Run: func(ctx *plugin.CommandContext) (int, error) {
				single, _ = ctx.Args["target"].([]plugin.Entity)
				return reply(ctx)
			}}}})
		}
		if err == nil {
			err = s.Commands().Register(plugin.Command{Name: "who", Run: func(ctx *plugin.CommandContext) (int, error) {
				asEntity = ctx.Entity
				return reply(ctx)
			}})
		}
		if err != nil {
			t.Errorf("register: %v", err)
			return err
		}
		ready = true
		return nil
	}}}}, servertest.WriteOpsFile(t, servertest.OpRecord{UUID: protocol.OfflineUUID("Tester").String(), Name: "Tester", Level: 2}))
	if !ready {
		t.Fatal("commands were not registered")
	}
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{X: 0, Z: 0})
	w.InhabitTime([]world.ChunkPos{{X: 0, Z: 0}})
	srv.RunLine("summon minecraft:cow 8.5 80 8.5 {NoAI:1b}")
	srv.RunLine("summon minecraft:cow 9.5 80 8.5 {NoAI:1b}")
	c := registrationClient(t, srv)

	c.ChatCommand("many @e[type=cow]")
	registrationFeedback(t, srv, c, "ran")
	if len(many) != 2 {
		t.Fatalf("cows = %v", many)
	}
	for _, e := range many {
		if _, ok := e.(plugin.Mob); !ok || !e.IsValid() {
			t.Errorf("handle %T valid=%v", e, e.IsValid())
		}
	}
	c.ChatCommand("many @a")
	registrationFeedback(t, srv, c, "ran")
	if len(many) != 1 || many[0].UUID() != protocol.OfflineUUID("Tester") {
		t.Errorf("players = %v", many)
	}
	c.ChatCommand("one @e[type=cow,limit=1]")
	registrationFeedback(t, srv, c, "ran")
	if len(single) != 1 {
		t.Errorf("single = %v", single)
	}
	c.ChatCommand("one @e[type=cow]")
	entitiesError(t, srv, c, "argument.entity.toomany")

	c.ChatCommand("who")
	registrationFeedback(t, srv, c, "ran")
	if asEntity == nil || asEntity.UUID() != protocol.OfflineUUID("Tester") {
		t.Errorf("entity = %v", asEntity)
	}
	c.ChatCommand("execute as @e[type=cow,limit=1] run who")
	registrationFeedback(t, srv, c, "ran")
	if _, player := asEntity.(plugin.Player); asEntity == nil || player || !asEntity.IsValid() {
		t.Errorf("execute as entity = %T", asEntity)
	}
}

// entitiesError expects a command failure carrying the translation key,
// followed by its context line.
func entitiesError(t *testing.T, srv *server.Server, c *servertest.Client, key string) {
	t.Helper()
	first, _ := protocol.ReadText(protocol.NewReader(c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)))
	if !textHasTranslation(first, key) {
		second, _ := protocol.ReadText(protocol.NewReader(c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)))
		t.Fatalf("feedback has no %s: %q then %q", key, first.Plain(), second.Plain())
	}
	c.ExpectWithinTicks(srv, 4, protocol.IDPlaySystemChat)
}

// hoverHasTranslation is textHasTranslation through hover texts, where a
// syntax error carries its message.
func hoverHasTranslation(text protocol.Text, key string) bool {
	if textHasTranslation(text, key) || text.HoverText != nil && hoverHasTranslation(*text.HoverText, key) {
		return true
	}
	for _, child := range text.Extra {
		if hoverHasTranslation(child, key) {
			return true
		}
	}
	return false
}
