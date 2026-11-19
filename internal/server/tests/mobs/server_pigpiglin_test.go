package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

const piglinKind = "minecraft:zombified_piglin"

func kindsNear(srv *server.Server) map[string]int {
	counts := map[string]int{}
	for _, m := range srv.Mobs() {
		counts[m.Kind]++
	}
	return counts
}

// TestLightningTurnsAPigIntoAnArmedZombifiedPiglin: a bolt on a pig outside
// peaceful replaces it with a zombified piglin holding a golden sword or
// spear; on peaceful the pig stays.
// vanilla: Pig.thunderHit
func TestLightningTurnsAPigIntoAnArmedZombifiedPiglin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		difficulty uint8
		converts   bool
	}{{"normal", 2, true}, {"peaceful", 0, false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), "Struck")
			srv.StopTick()
			srv.SetDifficulty(tc.difficulty)
			pos := srv.PlayerPosition("Struck")
			pig := srv.SpawnMob("minecraft:pig", pos.X+1, pos.Y, pos.Z)

			srv.StrikeLightning(pos.X+1, pos.Y, pos.Z)
			srv.TickBolts(4)

			counts := kindsNear(srv)
			if tc.converts {
				if counts["minecraft:pig"] != 0 || counts[piglinKind] != 1 {
					t.Fatalf("mobs after the bolt = %v, want one zombified piglin and no pig", counts)
				}
				for _, m := range srv.Mobs() {
					if m.Kind != piglinKind {
						continue
					}
					item := srv.MobHand(m.ID).ItemID
					sword, _ := registry.ItemID("minecraft:golden_sword")
					spear, _ := registry.ItemID("minecraft:golden_spear")
					if item != sword && item != spear {
						t.Errorf("the piglin holds item %d, want a golden sword or spear", item)
					}
				}
				return
			}
			if counts["minecraft:pig"] != 1 || counts[piglinKind] != 0 {
				t.Fatalf("mobs after a peaceful bolt = %v, want the pig alone", counts)
			}
			_ = pig
		})
	}
}

// portalColumn stands a two-block nether portal column on stone at (x, 64, z).
func portalColumn(srv *server.Server, x, z int32) {
	srv.SetBlock(x, 63, z, registry.BlockStates["minecraft:stone"])
	portal := registry.MustState("minecraft:nether_portal", map[string]string{"axis": "x"})
	srv.SetBlock(x, 64, z, portal)
	srv.SetBlock(x, 65, z, portal)
}

func portalTicks(t *testing.T, mutate func(*server.Server), passes int) (map[string]int, *server.Server) {
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Camper")
	srv.StopTick()
	servertest.SeedPhysics(t, srv, driedGhastSeed)
	srv.SetLevelSeed(5)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), driedGhastTickets)
	srv.SetDifficulty(3)
	pos := srv.PlayerPosition("Camper")
	portalColumn(srv, int32(pos.X)+3, int32(pos.Z))
	mutate(srv)
	for range passes {
		srv.TickRandomTicks(1)
		if kindsNear(srv)[piglinKind] > 0 {
			break
		}
	}
	return kindsNear(srv), srv
}

// TestAPortalSpawnsAPiglinOnACarvedPumpkin: a carved pumpkin is always a
// valid spawn block even though it is not a sturdy full block.
// vanilla: Blocks.CARVED_PUMPKIN
func TestAPortalSpawnsAPiglinOnACarvedPumpkin(t *testing.T) {
	t.Parallel()
	counts, _ := portalTicks(t, func(srv *server.Server) {
		pos := srv.PlayerPosition("Camper")
		srv.SetBlock(int32(pos.X)+3, 63, int32(pos.Z),
			registry.MustState("minecraft:carved_pumpkin", map[string]string{"facing": "north"}))
	}, 3000)
	if counts[piglinKind] == 0 {
		t.Error("a portal over a carved pumpkin spawned no piglin")
	}
}

// TestSpawnableBelowNeedsASturdyTop: the default isValidSpawn wants a sturdy
// top face, which a carpet, a lily pad, a sea pickle, a pitcher crop and a
// bottom slab lack although they have a collision shape.
// vanilla: BlockBehaviour.Properties.isValidSpawn
func TestSpawnableBelowNeedsASturdyTop(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		block string
		props map[string]string
		want  bool
	}{
		{"minecraft:stone", nil, true},
		{"minecraft:tube_coral_block", nil, true},
		{"minecraft:white_carpet", nil, false},
		{"minecraft:lily_pad", nil, false},
		{"minecraft:sea_pickle", nil, false},
		{"minecraft:pitcher_crop", nil, false},
		{"minecraft:stone_slab", map[string]string{"type": "bottom"}, false},
		{"minecraft:bush", nil, false},
	} {
		state := registry.MustState(tc.block, tc.props)
		if got := entity.SpawnableBelow(state, registry.EntityTypeZombie); got != tc.want {
			t.Errorf("SpawnableBelow(%s %v) = %v, want %v", tc.block, tc.props, got, tc.want)
		}
	}
}
