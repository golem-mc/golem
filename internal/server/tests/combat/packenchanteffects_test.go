package combat

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// hitZombieWith has a sword enchanted by a pack post_attack effect strike a
// zombie that stands still beside the attacker, returning the server.
func hitZombieWith(t *testing.T, effect string, functions map[string]string, before ...func(*server.Server)) (*server.Server, int32) {
	t.Helper()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = postAttackPack(t, "mainhand", "#minecraft:enchantable/weapon", packPostAttack("attacker", "victim", effect))
	for name, body := range functions {
		path := filepath.Join(cfg.LevelName, "datapacks", "targets", "data", "pack", "function", name+".mcfunction")
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	const name = "Striker"
	srv, c, _ := servertest.JoinAs(t, cfg, name)
	c.MoveToGround(.5, 64, .5, true)
	servertest.NightWorld(srv, c)
	zombie := srv.SpawnMobSeeded("minecraft:zombie", 2.5, 64, .5, 4374)
	srv.SetMobNoAI(zombie)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	srv.GiveSlot(name, player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:iron_sword", "pk:targets", 1))
	servertest.HoldTicks(t, srv)
	for _, f := range before {
		f(srv)
	}
	servertest.AwaitFullSwing(t, srv, name)
	c.Attack(zombie)
	c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
	return srv, zombie
}

// executeHolds reports whether an `execute if entity` line passed.
func executeHolds(out []protocol.Text) bool {
	return strings.Contains(fmt.Sprint(out), "conditional.pass")
}

func zombieCount(srv *server.Server) int {
	n := 0
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:zombie" {
			n++
		}
	}
	return n
}

// vanilla: RunFunction.apply
func TestPackRunFunctionRunsAsVictim(t *testing.T) {
	srv, _ := hitZombieWith(t, `{"type":"minecraft:run_function","function":"pack:mark"}`,
		map[string]string{"mark": "tag @s add marked\n"})
	if !executeHolds(srv.RunLine("execute if entity @e[type=zombie,tag=marked]")) {
		t.Fatal("the hit zombie was not tagged by the function")
	}
}

// vanilla: ReplaceBlock.apply, BlockStateProvider.DIRECT_CODEC
func TestPackReplaceBlockUnderVictim(t *testing.T) {
	const state = `{"id":"minecraft:gold_block"}`
	for _, tc := range []struct{ name, provider string }{
		{"simple", `{"type":"minecraft:simple","state":` + state + `}`},
		{"bare_state", state},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := hitZombieWith(t, `{"type":"minecraft:replace_block","block_state":`+tc.provider+`,"offset":[0,-1,0]}`, nil)
			if got := registry.BlockOf(srv.BlockAt(2, 63, 0)); got != registry.BlockGoldBlock {
				t.Fatalf("block under the zombie is %s, want gold_block", got.Name())
			}
		})
	}
}

// vanilla: BlockStateProviderTypes.bootstrap, RegistryDataLoader.load
func TestPackRefusesLegacySimpleStateProvider(t *testing.T) {
	const provider = `{"type":"minecraft:simple_state_provider","state":{"id":"minecraft:gold_block"}}`
	for _, tc := range []struct{ name, effect string }{
		{"replace_block", `{"type":"minecraft:replace_block","block_state":` + provider + `}`},
		{"replace_disk", `{"type":"minecraft:replace_disk","radius":1,"height":1,"block_state":` + provider + `}`},
		{"nested_source", `{"type":"minecraft:replace_block","block_state":{
			"type":"minecraft:randomized_int","property":"age","values":1,"source":` + provider + `}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.BlockTestConfig()
			cfg.LevelType = server.TestFlatLevelType
			cfg.LevelName = postAttackPack(t, "mainhand", "minecraft:iron_sword", packPostAttack("attacker", "victim", tc.effect))
			srv, err := server.NewSeeded(cfg, 1)
			if err == nil {
				srv.Shutdown()
				t.Fatal("server started with an invalid enchantment block state provider")
			}
			for _, want := range []string{"pk:targets", "minecraft:simple_state_provider"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("startup error = %v, want it to name %q", err, want)
				}
			}
		})
	}
}

// vanilla: BlockStateProviderTypes.bootstrap, ReplaceBlock.apply, ReplaceDisk.apply
func TestPackReplaceWithCurrentStateProviders(t *testing.T) {
	const noise = `"seed":42,"noise":{"base_octave":-3},"scale":1`
	for _, tc := range []struct {
		kind, fields, block string
		props               map[string]string
	}{
		{"weighted", `"entries":[{"data":"minecraft:gold_block","weight":1}]`, "gold_block", nil},
		{"noise", noise + `,"states":["minecraft:gold_block"]`, "gold_block", nil},
		{"noise_threshold", noise + `,"threshold":0,"high_chance":1,"default_state":"minecraft:gold_block","low_states":["minecraft:gold_block"],"high_states":["minecraft:gold_block"]`, "gold_block", nil},
		{"dual_noise", noise + `,"slow_noise":{"base_octave":-4},"slow_scale":1,"variety":[1,1],"states":["minecraft:gold_block"]`, "gold_block", nil},
		{"rotated", `"state":{"type":"weighted","entries":[{"data":"minecraft:oak_log","weight":1}]},"direction":"east"`, "oak_log", map[string]string{"axis": "x"}},
		{"randomized_int", `"source":{"type":"simple","state":{"id":"minecraft:water_cauldron"}},"property":"level","values":2`, "water_cauldron", map[string]string{"level": "2"}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			effect := `"type":"minecraft:replace_block"`
			if tc.kind == "noise" {
				effect = `"type":"minecraft:replace_disk","radius":1,"height":1`
			}
			srv, _ := hitZombieWith(t, fmt.Sprintf(`{%s,"offset":[0,-1,0],"block_state":{"type":"minecraft:%s",%s}}`, effect, tc.kind, tc.fields), nil)
			want := registry.MustState("minecraft:"+tc.block, tc.props)
			if got := srv.BlockAt(2, 63, 0); got != want {
				t.Fatalf("block under the zombie is %s %v, want %s %v", registry.BlockOf(got).Name(), registry.StateProps(got), tc.block, tc.props)
			}
		})
	}
}

// vanilla: BlockStateProvider.TYPED_CODEC
func TestPackRefusesRetiredStateProviderIDs(t *testing.T) {
	for _, tc := range []struct {
		name, retired, provider string
	}{
		{name: "weighted", retired: "weighted_state_provider"},
		{name: "noise", retired: "noise_provider"},
		{name: "noise_threshold", retired: "noise_threshold_provider"},
		{name: "dual_noise", retired: "dual_noise_provider"},
		{name: "rotated", retired: "rotated_block_provider"},
		{name: "randomized_int", retired: "randomized_int_state_provider"},
		{name: "rule_based", retired: "rule_based_state_provider"},
		{"rotated_state", "weighted_state_provider", `{"type":"rotated","state":{"type":"weighted_state_provider"}}`},
		{"randomized_int_source", "noise_provider", `{"type":"randomized_int","source":{"type":"noise_provider"},"property":"level","values":1}`},
		{"copy_properties_source", "simple_state_provider", `{"type":"copy_properties","source":{"type":"simple_state_provider"}}`},
		{"rule_based_fallback", "dual_noise_provider", `{"type":"rule_based","rules":[],"fallback":{"type":"dual_noise_provider"}}`},
		{"rule_based_rule", "rotated_block_provider", `{"type":"rule_based","rules":[{"if_true":{"type":"true"},"then":{"type":"rotated_block_provider"}}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := tc.provider
			if provider == "" {
				provider = fmt.Sprintf(`{"type":"minecraft:%s"}`, tc.retired)
			}
			effect := fmt.Sprintf(`{"type":"minecraft:replace_disk","radius":1,"height":1,"block_state":%s}`, provider)
			cfg := servertest.BlockTestConfig()
			cfg.LevelType = server.TestFlatLevelType
			cfg.LevelName = postAttackPack(t, "mainhand", "minecraft:iron_sword", packPostAttack("attacker", "victim", effect))
			srv, err := server.NewSeeded(cfg, 1)
			if err == nil {
				srv.Shutdown()
				t.Fatal("world loaded with a retired block state provider ID")
			}
			if msg := err.Error(); !strings.Contains(msg, "pk:targets") || !strings.Contains(msg, "minecraft:"+tc.retired) {
				t.Fatalf("startup error %q should name the enchantment and provider", msg)
			}
		})
	}
}

// vanilla: SummonEntityEffect.apply
func TestPackSummonEntityBesideVictim(t *testing.T) {
	srv, _ := hitZombieWith(t, `{"type":"minecraft:summon_entity","entity":"minecraft:zombie"}`, nil)
	if n := zombieCount(srv); n != 2 {
		t.Fatalf("%d zombies after the hit, want 2", n)
	}
}
