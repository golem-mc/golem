package items

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// runPlain runs a console line and joins the plain text of its replies.
func runPlain(srv *server.Server, line string) string {
	var out []string
	for _, text := range srv.RunLine(line) {
		out = append(out, text.Plain())
	}
	return strings.Join(out, "\n")
}

func unqualifiedServer(t *testing.T) *server.Server {
	t.Helper()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Componenter")
	servertest.HoldTicks(t, srv)
	return srv
}

// TestSetblockChestReadsUnqualifiedComponents: component keys and the ids
// inside them default to the minecraft namespace, so the chest keeps both stacks.
// vanilla: DataComponentType.CODEC, Identifier.tryParse
func TestSetblockChestReadsUnqualifiedComponents(t *testing.T) {
	t.Parallel()
	srv := unqualifiedServer(t)
	srv.RunLine(`setblock 4 64 4 chest{Items:[{Slot:0b,id:"diamond_sword",count:1,components:{"enchantments":{"sharpness":1},"custom_name":"Blade"}},{Slot:1b,id:"potion",count:1,components:{"potion_contents":{potion:"swiftness"}}}]}`)
	got := runPlain(srv, "data get block 4 64 4 Items")
	for _, want := range []string{`"minecraft:sharpness": 1`, `potion: "minecraft:swiftness"`, `id: "minecraft:diamond_sword"`, `Slot: 1b`} {
		if !strings.Contains(got, want) {
			t.Errorf("chest Items = %s, missing %s", got, want)
		}
	}
}
