package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// A pack's provider list keeps a literal {"":7} entry as a provider
// compound, whatever the list's length, so it is refused where an entry that
// is a number or a typed provider is summed. The tables are process-wide, so
// the test is not parallel.
// vanilla: ListCodec.decode, ContextIntProviders.DIRECT_CODEC, DispatcherProvider.mapCodec
func TestAPackProviderListRefusesLiteralEmptyKeyCompound(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRegistries(nil) })
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	cfg.LevelName = t.TempDir()
	const one = `{"type":"minecraft:constant","value":1}`
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	for file, body := range map[string]string{
		"pack.mcmeta": `{"pack":{"description":"p"}}`,
		"data/p/context_int_provider/single.json":     `{"type":"minecraft:add","inputs":[{"":7}]}`,
		"data/p/context_int_provider/multiple.json":   `{"type":"minecraft:add","inputs":[{"":7},` + one + `]}`,
		"data/p/context_int_provider/dispatcher.json": `{"type":"minecraft:number_dispatcher","cases":[{"":7},{"condition":{"type":"minecraft:random_chance","chance":1},"value":1}]}`,
		"data/p/context_int_provider/mixed.json":      `{"type":"minecraft:add","inputs":[7,` + one + `]}`,
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	for _, tc := range []struct{ name, want string }{
		{"single", `No key type in MapLike[{"":7}]`},
		{"multiple", `No key type in MapLike[{"":7}]`},
		{"dispatcher", `No key value in MapLike[{"":7}]; No key condition in MapLike[{"":7}]`},
	} {
		c.ChatCommand("compute default integer p:" + tc.name)
		got := servertest.FeedbackComponent(t, c)
		if len(got.Extra) != 1 {
			t.Fatalf("%s: feedback = %+v, want one extra component", tc.name, got)
		}
		got = got.Extra[0]
		if got.Translate != "command.compute.result.unnamed.invalid" || len(got.With) != 1 || !strings.Contains(got.With[0].Text, tc.want) {
			t.Errorf("%s: feedback = %+v, want an invalid result naming %q", tc.name, got, tc.want)
		}
	}
	c.ChatCommand("compute default integer p:mixed")
	if got := servertest.FeedbackComponent(t, c); got.Translate != "command.compute.result.unnamed.exact" || len(got.With) != 1 || got.With[0].Number == nil || *got.With[0].Number != 8 {
		t.Errorf("mixed list feedback = %+v, want the exact result", got)
	}
	c.Disconnect()
}
