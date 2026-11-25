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

// vanilla: SummonEntityEffect.apply
func TestPackSummonEntityBesideVictim(t *testing.T) {
	srv, _ := hitZombieWith(t, `{"type":"minecraft:summon_entity","entity":"minecraft:zombie"}`, nil)
	if n := zombieCount(srv); n != 2 {
		t.Fatalf("%d zombies after the hit, want 2", n)
	}
}
