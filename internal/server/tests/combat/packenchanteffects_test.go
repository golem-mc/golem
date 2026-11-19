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

// vanilla: SetBlockProperties.apply
func TestPackSetBlockPropertiesAtVictim(t *testing.T) {
	furnace, _ := registry.BlockByName("minecraft:furnace")
	unlit, _ := registry.FindState("minecraft:furnace", map[string]string{"facing": "north", "lit": "false"})
	srv, _ := hitZombieWith(t, `{"type":"minecraft:set_block_properties","properties":{"lit":"true","bogus":"x"},"offset":[0,-1,0]}`, nil,
		func(srv *server.Server) { srv.SetBlock(2, 63, 0, unlit) })
	state := srv.BlockAt(2, 63, 0)
	props := registry.StateProps(state)
	if registry.BlockOf(state) != furnace || props["lit"] != "true" || props["facing"] != "north" {
		t.Fatalf("block under the zombie is %v, want a lit north-facing furnace", props)
	}
}

// vanilla: ReplaceDisk.apply
func TestPackReplaceDiskAroundVictim(t *testing.T) {
	srv, _ := hitZombieWith(t, `{"type":"minecraft:replace_disk","radius":2,"height":1,"offset":[0,-1,0],"predicate":{"type":"minecraft:matching_block_tag","tag":"minecraft:dirt"},"block_state":"minecraft:gold_block"}`, nil,
		func(srv *server.Server) {
			dirt, _ := registry.BlockByName("minecraft:dirt")
			for x := int32(-1); x <= 5; x++ {
				srv.SetBlock(x, 63, 0, dirt.DefaultState())
			}
		})
	gold, _ := registry.BlockByName("minecraft:gold_block")
	for x, want := range map[int32]bool{0: false, 1: true, 2: true, 3: true, 4: false} {
		if got := registry.BlockOf(srv.BlockAt(x, 63, 0)) == gold; got != want {
			t.Errorf("block %d,63,0 gold = %v, want %v", x, got, want)
		}
	}
}

// vanilla: SummonEntityEffect.apply
func TestPackSummonEntityJoinsVictimsTeam(t *testing.T) {
	srv, _ := hitZombieWith(t, `{"type":"minecraft:summon_entity","entity":"minecraft:zombie","join_team":true}`, nil,
		func(srv *server.Server) {
			srv.RunLine("team add red")
			srv.RunLine("team join red @e[type=zombie]")
		})
	if n := zombieCount(srv); n != 2 {
		t.Fatalf("%d zombies after the hit, want 2", n)
	}
	if executeHolds(srv.RunLine("execute if entity @e[type=zombie,team=]")) {
		t.Fatal("the summoned zombie did not join the victim's team")
	}
}

// vanilla: WeightedStateProvider.getState, ReplaceBlock.apply
func TestPackReplaceBlockWithWeightedProvider(t *testing.T) {
	srv, _ := hitZombieWith(t, `{"type":"minecraft:replace_block","offset":[0,-1,0],"block_state":{"type":"minecraft:weighted_state_provider",
		"entries":[{"data":{"Name":"minecraft:gold_block"},"weight":1}]}}`, nil)
	gold, _ := registry.BlockByName("minecraft:gold_block")
	if got := registry.BlockOf(srv.BlockAt(2, 63, 0)); got != gold {
		t.Fatalf("block under the zombie is %s, want gold_block", got.Name())
	}
}

// vanilla: ReplaceBlock.apply, SolidPredicate, MatchingFluidsPredicate
func TestPackReplaceBlockPredicateKinds(t *testing.T) {
	for predicate, replaced := range map[string]bool{
		`{"type":"minecraft:solid","offset":[0,-1,0]}`:                                      true,
		`{"type":"minecraft:not","predicate":{"type":"minecraft:solid","offset":[0,-1,0]}}`: false,
		`{"type":"minecraft:matching_fluids","fluids":"minecraft:water","offset":[0,-1,0]}`: false,
		`{"type":"minecraft:has_sturdy_face","direction":"up","offset":[0,-1,0]}`:           true,
	} {
		srv, _ := hitZombieWith(t, `{"type":"minecraft:replace_block","offset":[0,-1,0],"predicate":`+predicate+`,"block_state":"minecraft:gold_block"}`, nil)
		gold, _ := registry.BlockByName("minecraft:gold_block")
		if got := registry.BlockOf(srv.BlockAt(2, 63, 0)) == gold; got != replaced {
			t.Errorf("predicate %s: replaced = %v, want %v", predicate, got, replaced)
		}
	}
}
