package commands

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// A pack's provider list keeps a literal {"":7} entry as a provider
// compound, whatever the list's length, so it is refused where an entry that
// is a number or a typed provider is summed. The tables are process-wide, so
// the test is not parallel.
// vanilla: ListCodec.decode, ContextIntProviders.DIRECT_CODEC, DispatcherProvider.mapCodec
func TestAPackProviderListRefusesLiteralEmptyKeyCompound(t *testing.T) {
	t.Cleanup(func() {
		registry.ApplyPackRegistries(nil)
		registry.ApplyPackContextProviders(nil, nil)
	})
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	cfg.LevelName = t.TempDir()
	const one = `{"type":"minecraft:constant","value":1}`
	for file, body := range map[string]string{
		"single":     `{"type":"minecraft:add","inputs":[{"":7}]}`,
		"multiple":   `{"type":"minecraft:add","inputs":[{"":7},` + one + `]}`,
		"dispatcher": `{"type":"minecraft:number_dispatcher","cases":[{"":7},{"condition":{"type":"minecraft:random_chance","chance":1},"value":1}]}`,
		"mixed":      `{"type":"minecraft:add","inputs":[7,` + one + `]}`,
	} {
		servertest.WritePackFile(t, cfg, "p", "p/context_int_provider/"+file+".json", body)
	}
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	for _, tc := range []struct{ name, want string }{
		{"single", `No key type in MapLike[{"":7}]`},
		{"multiple", `No key type in MapLike[{"":7}]`},
		{"dispatcher", `No key value in MapLike[{"":7}]; No key condition in MapLike[{"":7}]`},
	} {
		c.ChatCommand("compute default integer p:" + tc.name)
		got := feedback(t, srv, c)
		if len(got.Extra) != 1 {
			t.Fatalf("%s: feedback = %+v, want one extra component", tc.name, got)
		}
		got = got.Extra[0]
		if got.Translate != "command.compute.result.named.invalid" || len(got.With) != 2 || got.With[0].Text != "p:"+tc.name || !strings.Contains(got.With[1].Text, tc.want) {
			t.Errorf("%s: feedback = %+v, want an invalid result naming %q", tc.name, got, tc.want)
		}
	}
	c.ChatCommand("compute default integer p:mixed")
	if got := feedback(t, srv, c); got.Translate != "command.compute.result.named.exact" || len(got.With) != 2 || got.With[0].Text != "p:mixed" || got.With[1].Number == nil || *got.With[1].Number != 8 {
		t.Errorf("mixed list feedback = %+v, want the exact result", got)
	}
	c.Disconnect()
}

func feedback(t *testing.T, srv *server.Server, c *servertest.Client) protocol.Text {
	t.Helper()
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat))
	component, ok := protocol.ReadText(r)
	if !ok {
		t.Fatalf("decode system chat component: %v", r.Err())
	}
	return component
}
