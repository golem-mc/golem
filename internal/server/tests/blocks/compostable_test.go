package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestCompostableStackUse(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "survival"
	srv, actor := servertest.OpenWindowedOps(t, cfg)
	servertest.HoldTicks(t, srv)
	level := func(n int) uint16 {
		s, _ := registry.WithInt(registry.BlockComposter.DefaultState(), registry.PropLevel, n)
		return s
	}
	for _, tc := range []struct {
		item, component string
		start, want     int
		consumed        bool
	}{
		{"apple", "compostable={layers:7}", 0, 7, true},
		{"stick", "compostable={layers:2}", 1, 3, true},
		{"apple", "!compostable", 0, 0, false},
		{"apple", "compostable={layers:0}", 0, 0, true},
		{"apple", "compostable={layers:-2}", 2, 2, true},
		{"stick", "compostable={layers:100}", 6, 7, true},
		{"stick", "compostable={layers:2}", 7, 7, false},
		{"stick", `compostable={layers:"minecraft:compostable/always_add_one"}`, 0, 1, true},
	} {
		t.Run(tc.item+"/"+tc.component, func(t *testing.T) {
			srv.GiveSlot("Testificate", player.HotbarSlot, protocol.ItemStack{})
			actor.ChatCommand("give @s minecraft:" + tc.item + "[" + tc.component + "] 2")
			actor.Settle()
			if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != 2 {
				t.Fatalf("give produced %+v, want two items", got)
			}
			srv.SetBlock(3, 64, 0, level(tc.start))
			actor.UseBlock(3, 64, 0)
			actor.Settle()
			if got := srv.BlockAt(3, 64, 0); got != level(tc.want) {
				t.Fatalf("state = %d, want level %d", got, tc.want)
			}
			wantCount := int32(2)
			if tc.consumed {
				wantCount--
			}
			if got := srv.InventorySlot("Testificate", player.HotbarSlot); got.Count != wantCount {
				t.Errorf("remaining = %d, want %d", got.Count, wantCount)
			}
			if tc.want == 7 && tc.start < 7 {
				servertest.RunTicks(t, srv, 19)
				if srv.BlockAt(3, 64, 0) != level(7) {
					t.Fatal("ready before 20 ticks")
				}
				servertest.RunTicks(t, srv, 1)
				if srv.BlockAt(3, 64, 0) != level(8) {
					t.Fatal("not ready after 20 ticks")
				}
			}
		})
	}
}

// The pack layer is process-wide; this test runs before parallel tests start.
func TestCompostableBlockInteractionProvider(t *testing.T) {
	srv, actor := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	servertest.HoldTicks(t, srv)
	registry.ApplyPackContextProviders(map[string][]byte{
		"pk:layers": []byte(`{"type":"minecraft:number_dispatcher","cases":[{"condition":{"type":"minecraft:all_of","terms":[{"type":"minecraft:match_block","blocks":"minecraft:composter","state":{"level":"2"}},{"type":"minecraft:entity_properties","entity":"interacting_entity","predicate":{"minecraft:entity_type":"minecraft:player"}}]},"value":3}],"default":0}`),
	}, nil)
	t.Cleanup(func() { registry.ApplyPackContextProviders(nil, nil) })
	actor.ChatCommand(`give @s stick[compostable={layers:"pk:layers"}] 2`)
	actor.Settle()
	level := func(n int) uint16 {
		s, _ := registry.WithInt(registry.BlockComposter.DefaultState(), registry.PropLevel, n)
		return s
	}
	srv.SetBlock(3, 64, 0, level(2))
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if srv.BlockAt(3, 64, 0) != level(5) {
		t.Fatal("provider did not see the block state and interacting player")
	}
	actor.UseBlock(3, 64, 0)
	actor.Settle()
	if srv.BlockAt(3, 64, 0) != level(5) {
		t.Fatal("provider did not see the updated state")
	}
}
