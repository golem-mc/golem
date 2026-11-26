package combat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BlockStateProviderTypes.bootstrap, ReplaceBlock.apply, ReplaceDisk.apply
func TestPackReplaceRotatedAndRandomizedInt(t *testing.T) {
	t.Run("rotated", func(t *testing.T) {
		srv, _ := hitZombieWith(t, replaceBlockWith(`{
			"type":"minecraft:rotated","direction":"east",
			"state":{"type":"minecraft:simple","state":{"id":"minecraft:oak_log","properties":{"axis":"y"}}}
		}`), nil)
		want, _ := registry.FindState("minecraft:oak_log", map[string]string{"axis": "x"})
		if got := srv.BlockAt(2, 63, 0); got != want {
			t.Fatalf("block under the zombie = %s %v, want east-west oak log", registry.BlockOf(got).Name(), registry.StateProps(got))
		}
	})
	t.Run("randomized_int", func(t *testing.T) {
		srv, _ := hitZombieWith(t, `{"type":"minecraft:replace_disk","offset":[0,-1,0],"radius":2,"height":1,
			"block_state":{"type":"minecraft:randomized_int","property":"charges",
			"values":{"type":"minecraft:uniform","min_inclusive":2,"max_inclusive":3},
			"source":{"id":"minecraft:respawn_anchor","properties":{"charges":"0"}}}}`, nil)
		for _, x := range []int32{2, 3} {
			got := srv.BlockAt(x, 63, 0)
			charges, ok := registry.StateInt(got, registry.PropCharges)
			if registry.BlockOf(got) != registry.BlockRespawnAnchor || !ok || charges < 2 || charges > 3 {
				t.Errorf("disk block at x=%d = %s %v, want respawn anchor with 2 or 3 charges", x, registry.BlockOf(got).Name(), registry.StateProps(got))
			}
		}
	})
}

func TestPackReplaceRejectsRetiredRotatedAndRandomizedInt(t *testing.T) {
	const (
		rotated    = `{"type":"minecraft:rotated_block_provider","state":{"id":"minecraft:oak_log"}}`
		randomized = `{"type":"minecraft:randomized_int_state_provider","source":{"id":"minecraft:respawn_anchor"},"property":"charges","values":3}`
	)
	for _, tc := range []struct {
		name     string
		id       string
		provider string
	}{
		{"rotated", "rotated_block_provider", rotated},
		{"randomized_int", "randomized_int_state_provider", randomized},
		{"rotated_source", "rotated_block_provider", `{"type":"minecraft:randomized_int","property":"charges","values":3,"source":` + rotated + `}`},
		{"randomized_int_state", "randomized_int_state_provider", `{"type":"minecraft:rotated","direction":"east","state":` + randomized + `}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.MobTestConfig()
			cfg.LevelType = server.TestFlatLevelType
			cfg.LevelName = postAttackPack(t, "mainhand", "minecraft:iron_sword", packPostAttack("attacker", "victim", replaceBlockWith(tc.provider)))
			srv, err := server.New(cfg)
			if err == nil {
				srv.Shutdown()
				t.Fatalf("world loaded with retired provider %s", tc.id)
			}
			if !strings.Contains(err.Error(), "pk:targets") || !strings.Contains(err.Error(), "minecraft:"+tc.id) {
				t.Errorf("world load error = %v, want the enchantment and retired provider IDs", err)
			}
		})
	}
}

func replaceBlockWith(provider string) string {
	return fmt.Sprintf(`{"type":"minecraft:replace_block","offset":[0,-1,0],"block_state":%s}`, provider)
}

// vanilla: CopyPropertiesProvider.getState, RuleBasedStateProvider.getState, RandomBlockProvider.getOptionalState
func TestPackReplaceWorldReadingProviders(t *testing.T) {
	logX, _ := registry.FindState("minecraft:oak_log", map[string]string{"axis": "x"})
	setLog := func(srv *server.Server) { srv.SetBlock(2, 63, 0, logX) }
	t.Run("copy_properties", func(t *testing.T) {
		srv, _ := hitZombieWith(t, replaceBlockWith(`{"type":"minecraft:copy_properties",
			"source":{"id":"minecraft:oak_log","properties":{"axis":"y"}}}`), nil, setLog)
		if got := srv.BlockAt(2, 63, 0); got != logX {
			t.Fatalf("block = %s %v, want the log's east-west axis kept", registry.BlockOf(got).Name(), registry.StateProps(got))
		}
	})
	t.Run("rule_based", func(t *testing.T) {
		srv, _ := hitZombieWith(t, `{"type":"minecraft:replace_disk","offset":[0,-1,0],"radius":2,"height":1,
			"block_state":{"type":"minecraft:rule_based","fallback":{"id":"minecraft:emerald_block"},
			"rules":[{"if_true":{"type":"minecraft:matching_blocks","blocks":"minecraft:oak_log"},"then":{"id":"minecraft:diamond_block"}}]}}`, nil, setLog)
		for x, want := range map[int32]registry.Block{2: registry.BlockDiamondBlock, 3: registry.BlockEmeraldBlock} {
			if got := registry.BlockOf(srv.BlockAt(x, 63, 0)); got != want {
				t.Errorf("block at x=%d = %s, want %s", x, got.Name(), want.Name())
			}
		}
	})
	t.Run("random_block_empty", func(t *testing.T) {
		srv, _ := hitZombieWith(t, replaceBlockWith(`{"type":"minecraft:random_block","blocks":[]}`), nil, setLog)
		if got := srv.BlockAt(2, 63, 0); got != logX {
			t.Fatalf("block = %s %v, want the existing state kept", registry.BlockOf(got).Name(), registry.StateProps(got))
		}
	})
}
