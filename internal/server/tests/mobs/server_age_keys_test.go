package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: AgeableMob.canBeABaby, Animal.addAdditionalSaveData
func TestAgeKeysFollowCanBeABaby(t *testing.T) {
	t.Parallel()
	const name = "Keeper"
	cfg := servertest.HostileTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c, _ := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	kinds := []string{"fox", "squid", "glow_squid", "wandering_trader", "parrot", "villager", "dolphin", "frog", "zombie_nautilus"}
	for i, kind := range kinds {
		c.ChatCommand(`summon minecraft:` + kind + ` 0.5 64 ` + string(rune('0'+i)) + `.5 {NoAI:1b,Age:-24000,Tags:["k_` + kind + `"]}`)
	}
	c.Settle()
	has := func(tag nbt.Compound, key string) bool { _, ok := tag[key]; return ok }
	for i, kind := range kinds {
		tag := entityCompound(t, c, "@e[tag=k_"+kind+",limit=1]")
		age, hasAge := nbt.IntField(tag, "Age")
		wantAge := i < 4 || kind == "villager" || kind == "dolphin"
		if hasAge != wantAge || (wantAge && age != -24000) {
			t.Errorf("%s: Age = %d, present %v; want present %v at -24000", kind, age, hasAge, wantAge)
		}
		wantLove := kind == "fox" || kind == "parrot" || kind == "frog" || kind == "zombie_nautilus"
		if has(tag, "InLove") != wantLove {
			t.Errorf("%s: InLove present = %v, want %v", kind, has(tag, "InLove"), wantLove)
		}
	}
	babies := map[string]bool{}
	for _, m := range srv.Mobs() {
		babies[m.Kind] = m.Baby
	}
	for _, kind := range kinds[:4] {
		if !babies["minecraft:"+kind] && !babies[kind] {
			t.Errorf("%s is not a baby", kind)
		}
	}
	if babies["minecraft:zombie_nautilus"] || babies["zombie_nautilus"] {
		t.Error("zombie_nautilus is a baby")
	}
}
