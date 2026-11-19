package combat

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// bowPack writes a data pack with one bow enchantment whose effects component
// is body (the inside of the "effects" object) and the given functions, and
// returns the level directory. The registry tables are process-wide, so these
// tests must not run in parallel.
func bowPack(t *testing.T, body string, functions map[string]string) string {
	t.Helper()
	t.Cleanup(func() { registry.ApplyPackRegistries(nil); registry.ApplyPackEnchantments(nil) })
	dir := t.TempDir()
	pack := filepath.Join(dir, "datapacks", "targets")
	files := map[string]string{
		"pack.mcmeta": `{"pack":{"description":"targets"}}`,
		"data/pk/enchantment/targets.json": fmt.Sprintf(`{
   "anvil_cost":1,"description":{"text":"Targets"},"max_level":1,"weight":1,
   "min_cost":{"base":1,"per_level_above_first":0},"max_cost":{"base":1,"per_level_above_first":0},
   "slots":["mainhand"],"supported_items":"minecraft:bow","effects":{%s}}`, body),
	}
	for name, text := range functions {
		files["data/pack/function/"+name+".mcfunction"] = text
	}
	for name, text := range files {
		path := filepath.Join(pack, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func joinArcher(t *testing.T, level string) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.BowTestConfig()
	cfg.LevelName = level
	srv, c, _ := servertest.JoinAs(t, cfg, "Archer")
	servertest.GiveBowAndArrows(srv, "Archer")
	srv.GiveSlot("Archer", 36, servertest.EnchantedStack(t, "minecraft:bow", "pk:targets", 1))
	return srv, c
}

func shootEast(t *testing.T, srv *server.Server, c *servertest.Client) servertest.SpawnedArrow {
	t.Helper()
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	servertest.HoldDraw(t, srv, "Archer", 24)
	servertest.ReleaseBow(c)
	return servertest.ExpectArrowSpawn(srv, c)
}

// vanilla: EnchantmentHelper.onHitBlock, RunFunction.apply
func TestPackHitBlockRunsFunctionAsArrow(t *testing.T) {
	srv, c := joinArcher(t, bowPack(t,
		`"minecraft:hit_block":[{"effect":{"type":"minecraft:run_function","function":"pack:mark"}}]`,
		map[string]string{"mark": "tag @s add marked\n"}))
	stone, _ := registry.FindState("minecraft:stone", nil)
	for y := int32(64); y <= 66; y++ {
		srv.SetBlock(4, y, 0, stone)
	}
	arrow := shootEast(t, srv, c)
	c.ReadTicks(srv, servertest.SkeletonTicks, func(id int32, data []byte) bool {
		return id == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == arrow.EntityID &&
			servertest.ReadArrowMetadata(c, data).InGround
	})
	if !executeHolds(srv.RunLine("execute if entity @e[type=arrow,tag=marked]")) {
		t.Fatal("the arrow that hit the wall was not tagged by the hit_block function")
	}
}

// vanilla: EnchantmentHelper.onProjectileSpawned, ExplodeEffect.apply
func TestPackProjectileSpawnedExplodeBreaksBlock(t *testing.T) {
	srv, c := joinArcher(t, bowPack(t, `"minecraft:projectile_spawned":[{"effect":{"type":"minecraft:explode","radius":3,
		"block_interaction":"tnt","small_particle":{"type":"minecraft:explosion"},"large_particle":{"type":"minecraft:explosion_emitter"},
		"sound":"minecraft:entity.generic.explode"}}]`, nil))
	dirt, _ := registry.BlockByName("minecraft:dirt")
	srv.SetBlock(1, 65, 0, dirt.DefaultState())
	shootEast(t, srv, c)
	if got := srv.BlockAt(1, 65, 0); got != 0 {
		t.Fatalf("block beside the bow's arrow is %s, want it blown away", registry.BlockOf(got).Name())
	}
}

// vanilla: ExplodeEffect.apply, ClientboundExplodePacket
func TestPackExplodeSendsItsOwnParticleAndSound(t *testing.T) {
	srv, c := joinArcher(t, bowPack(t, `"minecraft:projectile_spawned":[{"effect":{"type":"minecraft:explode","radius":1,
		"block_interaction":"none","small_particle":{"type":"minecraft:flame"},"large_particle":{"type":"minecraft:flame"},
		"sound":"minecraft:block.anvil.land"}}]`, nil))
	var particle, sound, blocks int32 = -1, -1, -1
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	servertest.HoldDraw(t, srv, "Archer", 24)
	servertest.ReleaseBow(c)
	c.ReadTicks(srv, servertest.SkeletonTicks, func(id int32, data []byte) bool {
		if id != protocol.IDPlayExplosion {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadF64()
		r.ReadF64()
		r.ReadF64()
		r.ReadF32()
		r.ReadI32()
		if r.ReadBool() {
			r.ReadF64()
			r.ReadF64()
			r.ReadF64()
		}
		particle, sound, blocks = r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()
		return true
	})
	flame, _, _ := registry.ParticleID("minecraft:flame")
	anvil, _ := registry.SoundEventID("minecraft:block.anvil.land")
	if particle != flame || sound != anvil+1 || blocks != 0 {
		t.Fatalf("explosion sent particle %d sound %d block particles %d, want %d, %d, 0", particle, sound, blocks, flame, anvil+1)
	}
}
