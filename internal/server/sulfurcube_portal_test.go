package server

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestSulfurPortalCooldownBlastPreservesPortalWithoutStoppingRays(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName, cfg.LevelType = t.TempDir(), TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Shutdown()
	dim := s.allDims()[0]
	w := dim.w
	w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	portal := [3]int32{8, 70, 8}
	dirt := [3]int32{9, 70, 8}
	w.SetBlock(dirt[0], dirt[1], dirt[2], registry.MustState("minecraft:dirt", nil))
	for _, cooldown := range []int{0, 1} {
		id := s.spawnMobInDim(dim, entity.TypeSulfurCube, 8.5, 70, 8.5, 1, nil)
		ent := s.mobByEntityIDLocked(w, id)
		ent.mob.Body = protocol.ItemStack{ItemID: registry.MustItem("minecraft:tnt"), Count: 1}
		ent.portal.Cooldown = cooldown
		fx := newMobEffects(s)
		tick := mobDimTick{s: s, dim: dim, h: dim.mobs, w: w}
		w.SetBlock(portal[0], portal[1], portal[2], 0)
		tick.crossPortals(&fx)
		if ent.portal.Cooldown != 0 {
			t.Fatalf("cooldown did not decrement from %d", cooldown)
		}
		tick.creeperExploded(&fx, entity.MobEvent{Mob: ent.mob, X: 8.5, Y: 70.06125, Z: 8.5, Radius: 3})
		if len(fx.blasts) != 1 {
			t.Fatal("missing sulfur blast")
		}
		blast := fx.blasts[0]
		w.SetBlock(portal[0], portal[1], portal[2], registry.MustState("minecraft:nether_portal", nil))
		blast.rng.Seed(1)
		got := s.blastInputs(blast).destroyed
		if slices.Contains(got, portal) != (cooldown == 0) {
			t.Fatalf("cooldown=%d portal destruction=%v", cooldown, slices.Contains(got, portal))
		}
		if !slices.Contains(got, dirt) {
			t.Fatalf("cooldown=%d portal blocked blast rays", cooldown)
		}
		if cooldown > 0 {
			w.SetBlock(portal[0], portal[1], portal[2], 0)
			blast.rng.Seed(1)
			throughAir := s.blastInputs(blast).destroyed
			throughAir = slices.DeleteFunc(throughAir, func(c [3]int32) bool { return c == portal })
			if !slices.Equal(got, throughAir) {
				t.Fatal("portal changed ray propagation compared with air")
			}
		}
	}
}
