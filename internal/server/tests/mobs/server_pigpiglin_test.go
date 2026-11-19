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

// TestANetherPortalSpawnsAZombifiedPiglinBelowItself: random ticks on the
// portal column stand a zombified piglin on the stone under it.
// vanilla: NetherPortalBlock.randomTick
func TestANetherPortalSpawnsAZombifiedPiglinBelowItself(t *testing.T) {
	t.Parallel()
	counts, srv := portalTicks(t, func(*server.Server) {}, 3000)
	if counts[piglinKind] == 0 {
		t.Fatal("no zombified piglin came out of the portal")
	}
	for _, m := range srv.Mobs() {
		if m.Kind == piglinKind && m.Y != 64 {
			t.Errorf("the piglin stands at y=%v, want 64 (on the stone under the portal)", m.Y)
		}
	}
}

// TestANetherPortalSpawnsNoPiglinOnPeacefulOrWithMonstersOff: peaceful and
// the spawn_monsters rule each stop it.
func TestANetherPortalSpawnsNoPiglinOnPeacefulOrWithMonstersOff(t *testing.T) {
	t.Parallel()
	counts, _ := portalTicks(t, func(srv *server.Server) { srv.SetDifficulty(0) }, 3000)
	if counts[piglinKind] != 0 {
		t.Errorf("a portal on peaceful spawned %d piglins", counts[piglinKind])
	}
	counts, _ = portalTicks(t, func(srv *server.Server) { srv.SetGameRule(world.RuleSpawnMonsters.String(), false) }, 3000)
	if counts[piglinKind] != 0 {
		t.Errorf("a portal with spawn_monsters off spawned %d piglins", counts[piglinKind])
	}
}

// TestAStruckPigKeepsItsNameBabyFlagAndLeash: the zombified piglin that
// replaces a struck pig carries the pig's custom name, baby age and leash.
// vanilla: Pig.thunderHit, ConversionType.convertCommon
func TestAStruckPigKeepsItsNameBabyFlagAndLeash(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), "Struck")
	srv.StopTick()
	srv.SetDifficulty(2)
	pos := srv.PlayerPosition("Struck")
	pig := srv.SpawnMob("minecraft:pig", pos.X+1, pos.Y, pos.Z)
	holder := srv.SpawnMob("minecraft:cow", pos.X-12, pos.Y, pos.Z)
	srv.SetMobBaby(pig, true)
	srv.SetMobNamed(pig, "Porky", true)
	srv.SetMobLeashHolder(pig, holder)

	srv.StrikeLightning(pos.X+1, pos.Y, pos.Z)
	srv.TickBolts(4)

	var found bool
	for _, m := range srv.Mobs() {
		if m.Kind != piglinKind {
			continue
		}
		found = true
		if m.Name != "Porky" || !m.Baby {
			t.Errorf("piglin name = %q baby = %v, want Porky and a baby", m.Name, m.Baby)
		}
		if got, _ := srv.MobLeashHolder(m.ID); got != holder {
			t.Errorf("piglin leash holder = %d, want %d", got, holder)
		}
	}
	if !found {
		t.Fatal("the struck pig did not become a zombified piglin")
	}
}

// TestANetherPortalSetsThePiglinsPortalCooldown: the piglin that comes out
// of a portal is not sent back through it at once.
// vanilla: NetherPortalBlock.randomTick, Entity.setPortalCooldown
func TestANetherPortalSetsThePiglinsPortalCooldown(t *testing.T) {
	t.Parallel()
	_, srv := portalTicks(t, func(*server.Server) {}, 3000)
	for _, m := range srv.Mobs() {
		if m.Kind == piglinKind {
			if got := srv.MobPortalCooldown(m.ID); got != 300 {
				t.Errorf("portal cooldown = %d, want 300", got)
			}
			return
		}
	}
	t.Fatal("no zombified piglin came out of the portal")
}

// TestANetherPortalInTheNetherSpawnsNoPiglin: only the overworld's portals
// spawn piglins.
// vanilla: EnvironmentAttributes.NETHER_PORTAL_SPAWNS_PIGLINS
func TestANetherPortalInTheNetherSpawnsNoPiglin(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.CrossingTestConfig(t), "Camper")
	srv.StopTick()
	servertest.SeedPhysics(t, srv, driedGhastSeed)
	srv.SetLevelSeed(5)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), driedGhastTickets)
	srv.SetDifficulty(3)
	servertest.CarryIntoNether(t, srv, c, "Camper", 0, 0)
	portal := registry.MustState("minecraft:nether_portal", map[string]string{"axis": "x"})
	if !srv.SetBlockInDimension(world.NetherName, 3, 63, 0, registry.BlockStates["minecraft:stone"]) {
		t.Fatal("the nether did not take the stone")
	}
	srv.SetBlockInDimension(world.NetherName, 3, 64, 0, portal)
	srv.SetBlockInDimension(world.NetherName, 3, 65, 0, portal)
	// Keep all 3000 passes on the portal's chunk: unrelated Nether fire,
	// vegetation and lava in the surrounding eight chunks cannot spawn it.
	srv.TickRandomTicks(3000, world.ChunkPos{X: 0, Z: 0})
	for _, m := range srv.MobsInDimension(world.NetherName) {
		if m.Kind == piglinKind {
			t.Fatal("a portal in the nether spawned a zombified piglin")
		}
	}
}

// TestAPortalSpawnsNoPiglinOnGlass: the block under the portal must pass its
// own isValidSpawn, which glass refuses.
// vanilla: BlockBehaviour.Properties.isValidSpawn
func TestAPortalSpawnsNoPiglinOnGlass(t *testing.T) {
	t.Parallel()
	counts, _ := portalTicks(t, func(srv *server.Server) {
		pos := srv.PlayerPosition("Camper")
		srv.SetBlock(int32(pos.X)+3, 63, int32(pos.Z), registry.BlockStates["minecraft:glass"])
	}, 3000)
	if counts[piglinKind] != 0 {
		t.Errorf("a portal over glass spawned %d piglins", counts[piglinKind])
	}
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

// TestAPortalSpawnsAPiglinOnMudAndMagmaButNotOnACopperGrate: mud is always
// valid, magma only for a fire-immune type (the piglin is one), a copper grate
// never.
// vanilla: Blocks.MUD, Blocks.MAGMA_BLOCK, Blocks.COPPER_GRATE
func TestAPortalSpawnsAPiglinOnMudAndMagmaButNotOnACopperGrate(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		block string
		want  bool
	}{
		{"minecraft:mud", true},
		{"minecraft:magma_block", true},
		{"minecraft:copper_grate", false},
		{"minecraft:waxed_weathered_copper_grate", false},
	} {
		t.Run(tc.block, func(t *testing.T) {
			t.Parallel()
			counts, _ := portalTicks(t, func(srv *server.Server) {
				pos := srv.PlayerPosition("Camper")
				srv.SetBlock(int32(pos.X)+3, 63, int32(pos.Z), registry.BlockStates[tc.block])
			}, 3000)
			if got := counts[piglinKind] != 0; got != tc.want {
				t.Errorf("a portal over %s spawned a piglin: %v, want %v", tc.block, got, tc.want)
			}
		})
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

// TestSpawnableBelowTakesTheMobType: ice admits only a polar bear, leaves an
// ocelot or a parrot, magma a fire-immune type.
// vanilla: Blocks.ICE, Blocks.FROSTED_ICE, Blocks.OAK_LEAVES, Blocks.MAGMA_BLOCK
func TestSpawnableBelowTakesTheMobType(t *testing.T) {
	t.Parallel()
	const (
		bear   = registry.EntityTypePolarBear
		parrot = registry.EntityTypeParrot
		zombie = registry.EntityTypeZombie
	)
	for _, tc := range []struct {
		block string
		mob   entity.MobType
		want  bool
	}{
		{"minecraft:ice", bear, true},
		{"minecraft:ice", zombie, false},
		{"minecraft:frosted_ice", bear, true},
		{"minecraft:frosted_ice", entity.TypeZombifiedPiglin, false},
		{"minecraft:oak_leaves", entity.TypeOcelot, true},
		{"minecraft:oak_leaves", parrot, true},
		{"minecraft:oak_leaves", zombie, false},
		{"minecraft:magma_block", entity.TypeZombifiedPiglin, true},
		{"minecraft:magma_block", zombie, false},
	} {
		state := registry.BlockStates[tc.block]
		switch tc.block {
		case "minecraft:oak_leaves":
			state = registry.MustState(tc.block, map[string]string{"distance": "7", "persistent": "true", "waterlogged": "false"})
		case "minecraft:frosted_ice":
			state = registry.MustState(tc.block, map[string]string{"age": "0"})
		}
		if got := entity.SpawnableBelow(state, tc.mob); got != tc.want {
			t.Errorf("SpawnableBelow(%s, %s) = %v, want %v", tc.block, tc.mob, got, tc.want)
		}
	}
}
