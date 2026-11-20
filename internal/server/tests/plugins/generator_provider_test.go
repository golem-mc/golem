package plugins_test

import (
	"errors"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// genPlugin publishes the id "testgen" and nothing else.
type genPlugin struct {
	testPlugin
}

func (*genPlugin) Generator(world plugin.Key, id string) (plugin.Generator, error) {
	if id != "" && id != "testgen" {
		return nil, errors.New("unknown generator " + id)
	}
	return &flatGen{}, nil
}

func TestPluginGeneratorProvider(t *testing.T) {
	t.Parallel()
	var h plugin.Server
	gp := &genPlugin{}
	gp.enable = func(s plugin.Server) error { h = s; return nil }
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{
		{Name: "Gens", Plugin: gp},
		{Name: "plain", Plugin: &testPlugin{}},
	})
	servertest.HoldTicks(t, srv)
	var w plugin.World
	run := func(f func()) {
		t.Helper()
		ran := false
		h.Scheduler().RunTask(func() { f(); ran = true })
		servertest.RunTicks(t, srv, 1)
		if !ran {
			t.Fatal("plugin task did not complete")
		}
	}
	run(func() {
		for _, spec := range []string{"gens:testgen", "GENS:testgen", "gens", "GENS:"} {
			if g, err := h.Generator(flatKey, spec); err != nil || g == nil {
				t.Errorf("Generator(%q) = %v, %v", spec, g, err)
			}
		}
		for _, spec := range []string{"gens:nope", "plain", "plain:x", "missing:testgen", ""} {
			if g, err := h.Generator(flatKey, spec); err == nil {
				t.Errorf("Generator(%q) = %v, want an error", spec, g)
			}
		}
		g, _ := h.Generator(flatKey, "gens:testgen")
		var err error
		if w, err = h.CreateWorld(flatOptions(g)); err != nil {
			t.Errorf("CreateWorld: %v", err)
			return
		}
		w.LoadChunk(plugin.ChunkPos{}, true, func(plugin.Chunk) {})
		for _, info := range h.Plugins() {
			if info.Generators != (info.Name == "Gens") {
				t.Errorf("Generators of %s = %v", info.Name, info.Generators)
			}
		}
	})
	if w == nil {
		t.FailNow()
	}
	awaitPluginIO(t, srv, "generated chunk", func() bool { return srv.ChunkLoadedInDimension("test:flat", 0, 0) })
	if got := srv.LoadBlockInDimension("test:flat", 0, 64, 0); got != uint16(registry.BlockAir.DefaultState()) {
		t.Fatalf("(0,64,0) = %d, want air", got)
	}
}
