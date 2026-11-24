package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: RegistryDataLoader.RELOADABLE_REGISTRIES, ReloadCommand.reloadPacks
func TestInvalidContextProviderKeepsPreviousPackResources(t *testing.T) {
	// The context provider tables are process-wide.
	t.Cleanup(func() {
		registry.ApplyPackRegistries(nil)
		registry.ApplyPackContextProviders(nil, nil)
		registry.ApplyPackTags(nil)
	})
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	cfg.LevelName = t.TempDir()
	cfg.InitialDisabledPacks = "file/bad"

	const goodInt = "p/context_int_provider/base.json"
	const goodFloat = "p/context_float_provider/base.json"
	const badInt = "p/context_int_provider/new.json"
	const badFloat = "p/context_float_provider/new.json"
	servertest.WritePackFile(t, cfg, "good", goodInt, `7`)
	servertest.WritePackFile(t, cfg, "good", goodFloat, `2.5`)
	servertest.WritePackFile(t, cfg, "good", "p/function/loaded.mcfunction", "return 1\n")
	servertest.WritePackFile(t, cfg, "bad", badInt, `{"type":"minecraft:add","inputs":[{"":7}]}`)
	servertest.WritePackFile(t, cfg, "bad", badFloat, `4`)
	srv, c := joinPacks(t, cfg)

	reloadFailure := func() {
		t.Helper()
		got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
		if !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.reload.failure")) {
			t.Fatal("expected commands.reload.failure")
		}
	}
	editPack := func(input, key string) {
		t.Helper()
		c.ChatCommand(input)
		if got := servertest.FeedbackComponent(t, c); got.Translate != key {
			t.Fatalf("%s returned %q, want %q", input, got.Translate, key)
		}
		reloadFailure()
	}

	exact := func(want int64) {
		t.Helper()
		c.ChatCommand("compute default integer p:base")
		if with := expectCompute(t, c, "command.compute.result.named.exact").With; len(with) != 2 || with[0].Text != "p:base" || with[1].Number == nil || *with[1].Number != want {
			t.Errorf("p:base integer returned %+v, want %d", with, want)
		}
	}
	missing := func(kind, name, reg string) {
		t.Helper()
		input := "compute default " + kind + " " + name
		c.ChatCommand(input)
		c.ExpectCommandError(
			servertest.CommandErrorMessageForTest("argument.resource_or_id.no_such_element", name, reg),
			servertest.CommandErrorContextForTest(input, len(input)))
	}
	unchanged := func() {
		t.Helper()
		c.ChatCommand("datapack list enabled")
		expectPackList(t, c, "commands.datapack.list.enabled.success", "vanilla", "file/good")
		exact(7)
		c.ChatCommand("compute default float p:base")
		if with := expectCompute(t, c, "command.compute.result.named.rounded").With; len(with) != 3 || with[0].Text != "p:base" || with[1].Float == nil || *with[1].Float != 2.5 {
			t.Errorf("p:base float returned %+v, want 2.5", with)
		}
		missing("integer", "p:new", "minecraft:context_int_provider")
		missing("float", "p:new", "minecraft:context_float_provider")
		expectFunctionReturns(t, c, "p:loaded", 1)
	}

	editPack(`datapack enable "file/bad"`, "commands.datapack.modify.enable")
	unchanged()

	servertest.WritePackFile(t, cfg, "good", goodInt, `99`)
	servertest.WritePackFile(t, cfg, "good", goodFloat, `{"type":"minecraft:constant"}`)
	servertest.WritePackFile(t, cfg, "good", "p/function/loaded.mcfunction", "return 2\n")
	servertest.WritePackFile(t, cfg, "discovered", badInt, `10`)
	servertest.WritePackFile(t, cfg, "discovered", badFloat, `1`)
	c.ChatCommand("reload")
	c.ExpectCommandFeedback("commands.reload.success")
	reloadFailure()
	unchanged()

	servertest.WritePackFile(t, cfg, "good", goodFloat, `{"type":"minecraft:from_int","input":"p:new"}`)
	servertest.WritePackFile(t, cfg, "good", goodInt, `{"type":"minecraft:conditional","condition":{"type":"minecraft:random_chance","chance":"p:new"},"on_true":{"type":"minecraft:from_float","input":"p:base"}}`)
	servertest.WritePackFile(t, cfg, "discovered", badInt, `{"type":"minecraft:add","inputs":[7,{"type":"minecraft:constant","value":1}]}`)
	servertest.WritePackFile(t, cfg, "discovered", "p/tags/block/blocks.json", `{"values":["minecraft:stone"]}`)
	servertest.WritePackFile(t, cfg, "discovered", "p/context_int_provider/with_tag.json", `{"type":"minecraft:conditional","condition":{"type":"minecraft:match_block","blocks":"#p:blocks"},"on_true":1}`)
	c.ChatCommand("reload")
	c.ExpectCommandFeedback("commands.reload.success")
	c.ChatCommand("datapack list enabled")
	expectPackList(t, c, "commands.datapack.list.enabled.success", "vanilla", "file/good", "file/discovered")
	exact(8)
	expectFunctionReturns(t, c, "p:loaded", 2)

	// Removing a holder's target in the candidate must fail even though the
	// previous selection still has that target.
	editPack(`datapack disable "file/discovered"`, "commands.datapack.modify.disable")
	c.ChatCommand("datapack list enabled")
	expectPackList(t, c, "commands.datapack.list.enabled.success", "vanilla", "file/good", "file/discovered")
	exact(8)
}
