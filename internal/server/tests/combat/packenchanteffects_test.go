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

// vanilla: ReplaceBlock.apply
func TestPackReplaceBlockUnderVictim(t *testing.T) {
	srv, _ := hitZombieWith(t, `{"type":"minecraft:replace_block","block_state":{"type":"minecraft:simple_state_provider","state":{"id":"minecraft:gold_block"}},"offset":[0,-1,0]}`, nil)
	gold, _ := registry.BlockByName("minecraft:gold_block")
	if got := registry.BlockOf(srv.BlockAt(2, 63, 0)); got != gold {
		t.Fatalf("block under the zombie is %s, want gold_block", got.Name())
	}
}

// vanilla: SummonEntityEffect.apply
func TestPackSummonEntityBesideVictim(t *testing.T) {
	srv, _ := hitZombieWith(t, `{"type":"minecraft:summon_entity","entity":"minecraft:zombie"}`, nil)
	if n := zombieCount(srv); n != 2 {
		t.Fatalf("%d zombies after the hit, want 2", n)
	}
}
