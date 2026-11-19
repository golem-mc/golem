package storage

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// restartAround runs fill against a first server with its ticks held, saves
// and stops it, and starts a second server on the same level with a player
// in the spawn chunk and its ticks held too.
func restartAround(t *testing.T, fill func(srv *server.Server), saved func(level string)) (again *server.Server, level string) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	c := connect().Join("Keeper")
	servertest.HoldTicks(t, srv)
	fill(srv)
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()
	if saved != nil {
		saved(cfg.LevelName)
	}

	restarted := *cfg
	again, reconnect := servertest.Start(t, &restarted)
	// The saved bodies wake on the first held tick, not during the join.
	servertest.FreezeUntilHeld(t, again)
	reconnect().Join("Keeper")
	servertest.HoldTicks(t, again)
	return again, cfg.LevelName
}

// TestThrownBodiesSurviveARestart saves one of each thrown kind a chunk file
// keeps, and finds them again, with the owner of a pearl, after a restart.
// vanilla: ThrowableItemProjectile.addAdditionalSaveData
func TestThrownBodiesSurviveARestart(t *testing.T) {
	t.Parallel()
	owner := uuid.New()
	want := map[string]string{}
	again, _ := restartAround(t, func(srv *server.Server) {
		srv.ThrowThrown(entity.ThrownSnowball, 3.5, 100, 3.5, 0, 0, 0, 1)
		srv.ThrowThrown(entity.ThrownEgg, 4.5, 100, 3.5, 0, 0, 0, 1)
		srv.ThrowThrown(entity.ThrownExperienceBottle, 5.5, 100, 3.5, 0, 0, 0, 1)
		srv.ThrowEnderPearlInDimension("minecraft:overworld", 6.5, 100, 3.5, 0, 0, 0, owner, 1)
		poison, _ := registry.PotionID("minecraft:poison")
		srv.ThrowPotion(entity.ThrownSplashPotion, poison, 7.5, 100, 3.5, 0, 0, 0, 1)
		srv.ThrowPotion(entity.ThrownLingeringPotion, poison, 8.5, 100, 3.5, 0, 0, 0, 1)
		for _, it := range srv.ThrownInDimension("minecraft:overworld") {
			want[it.UUID] = it.Kind
		}
	}, func(level string) {
		servertest.EntityFileIs(t, level, world.ChunkPos{X: 0, Z: 0}, pearlEntityID, func(c nbt.Compound) {
			words, _ := c["Owner"].(nbt.IntArray)
			if got, _ := nbt.UUIDFromIntArray(words); got != owner {
				t.Errorf("the pearl's saved Owner = %v, want %v", got, owner)
			}
		})
	})
	if len(want) != 6 {
		t.Fatalf("the first server held %d thrown bodies, want 6", len(want))
	}
	servertest.UntilTicks(t, again, 40, "the thrown bodies woke", func() bool {
		return len(again.ThrownInDimension("minecraft:overworld")) >= len(want)
	})
	got := map[string]string{}
	for _, it := range again.ThrownInDimension("minecraft:overworld") {
		got[it.UUID] = it.Kind
	}
	for id, kind := range want {
		if got[id] != kind {
			t.Errorf("thrown %s is %q after the restart, want %q", id, got[id], kind)
		}
	}
}

const pearlEntityID = "minecraft:ender_pearl"

// TestACloudSurvivesARestart saves an aged cloud and finds it again with its
// age, its shrunk radius and its potion contents.
// vanilla: AreaEffectCloud.addAdditionalSaveData
func TestACloudSurvivesARestart(t *testing.T) {
	t.Parallel()
	var id uuid.UUID
	var age int
	var radius float32
	again, level := restartAround(t, func(srv *server.Server) {
		body := srv.SpawnCloudEntity(0.5, 64, 3.5)
		id = body.UUID
		servertest.RunTicks(t, srv, 30)
		age, radius, _ = srv.CloudState(id)
	}, nil)
	if age < 25 || radius >= entity.CloudStartRadius {
		t.Fatalf("the cloud was %d ticks old with radius %v, want it aged and shrunk", age, radius)
	}
	servertest.UntilTicks(t, again, 40, "the cloud woke", func() bool { return again.CloudCount() == 1 })
	gotAge, gotRadius, ok := again.CloudState(id)
	if !ok || gotAge < age || gotAge > age+40 || gotRadius > radius {
		t.Errorf("the cloud after the restart = age %d radius %v (%v), want age from %d and radius at most %v",
			gotAge, gotRadius, ok, age, radius)
	}
	again.FlushMobs()
	again.Shutdown()
	servertest.EntityFileIs(t, level, world.ChunkPos{X: 0, Z: 0}, "minecraft:area_effect_cloud", func(c nbt.Compound) {
		contents, ok := nbt.Get[nbt.Compound](c, "potion_contents")
		if !ok || contents["potion"] != nbt.Tag(nbt.String("minecraft:poison")) {
			t.Errorf("the reloaded cloud's potion_contents = %v, want poison", c["potion_contents"])
		}
	})
}

// TestACloudKeepsItsTimingsAcrossARestart saves a cloud with every timing
// field and the particle off their defaults, and finds them unchanged in the
// file the reloaded cloud is saved to.
// vanilla: AreaEffectCloud.readAdditionalSaveData
func TestACloudKeepsItsTimingsAcrossARestart(t *testing.T) {
	t.Parallel()
	particle := nbt.Compound{"type": nbt.String("minecraft:flame")}
	again, level := restartAround(t, func(srv *server.Server) {
		body := srv.SpawnCloudEntity(0.5, 64, 3.5)
		srv.TuneCloud(body.UUID, 100000, 5, 10, 3, -0.2, particle)
	}, nil)
	servertest.UntilTicks(t, again, 40, "the cloud woke", func() bool { return again.CloudCount() == 1 })
	again.FlushMobs()
	again.Shutdown()
	servertest.EntityFileIs(t, level, world.ChunkPos{X: 0, Z: 0}, "minecraft:area_effect_cloud", func(c nbt.Compound) {
		for key, want := range map[string]int32{"Duration": 100000, "WaitTime": 5, "ReapplicationDelay": 10, "DurationOnUse": 3} {
			if got, _ := c[key].(nbt.Int); int32(got) != want {
				t.Errorf("the reloaded cloud's %s = %v, want %d", key, c[key], want)
			}
		}
		if got, _ := c["RadiusOnUse"].(nbt.Float); float32(got) != -0.2 {
			t.Errorf("the reloaded cloud's RadiusOnUse = %v, want -0.2", c["RadiusOnUse"])
		}
		if got, _ := nbt.Get[nbt.Compound](c, "custom_particle"); got["type"] != nbt.Tag(nbt.String("minecraft:flame")) {
			t.Errorf("the reloaded cloud's custom_particle = %v, want flame", c["custom_particle"])
		}
	})
}

// TestARocketAndAnEyeSurviveARestart saves a rocket mid-flight and an eye of
// ender in the air, and finds both again.
// vanilla: FireworkRocketEntity.addAdditionalSaveData, EyeOfEnder.addAdditionalSaveData
func TestARocketAndAnEyeSurviveARestart(t *testing.T) {
	t.Parallel()
	var rocket uuid.UUID
	var life, lifetime int
	again, _ := restartAround(t, func(srv *server.Server) {
		item, _ := registry.ItemID("minecraft:firework_rocket")
		rocket = srv.SpawnFireworkStackEntity(protocol.ItemStack{ItemID: item, Count: 1}, 2.5, 100, 2.5).UUID
		srv.SpawnEyeEntity(4.5, 100, 2.5, 0, 0, 0)
		servertest.RunTicks(t, srv, 3)
		life, lifetime, _ = srv.FireworkState(rocket)
	}, nil)
	if life < 3 {
		t.Fatalf("the rocket had flown %d ticks, want at least 3", life)
	}
	servertest.UntilTicks(t, again, 40, "the rocket and the eye woke", func() bool {
		return again.FireworkCount() == 1 && again.EyeCount() == 1
	})
	gotLife, gotLifetime, ok := again.FireworkState(rocket)
	if !ok || gotLifetime != lifetime || gotLife < life {
		t.Errorf("the rocket after the restart = life %d of %d (%v), want life from %d of %d",
			gotLife, gotLifetime, ok, life, lifetime)
	}
}

func pos3(x, y, z float64) nbt.List {
	return nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(x), nbt.Double(y), nbt.Double(z)}}
}

// TestPlantedBodiesWithoutAnItemLoadTheirDefault wakes a snowball with no
// Item and a rocket with no FireworksItem from a hand-written file, and
// finds both flying instead of dropped.
// vanilla: ThrowableItemProjectile.getDefaultItem, FireworkRocketEntity.getDefaultItem
func TestPlantedBodiesWithoutAnItemLoadTheirDefault(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	snowball := servertest.FixtureUUID(2, 6, 4, 7)
	rocket := servertest.FixtureUUID(2, 6, 4, 8)
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0},
		nbt.Compound{"id": nbt.String("minecraft:snowball"), "UUID": servertest.UuidWords(snowball), "Pos": pos3(3.5, 100, 3.5)},
		nbt.Compound{"id": nbt.String("minecraft:firework_rocket"), "UUID": servertest.UuidWords(rocket), "Pos": pos3(5.5, 100, 3.5),
			"Life": nbt.Int(0), "LifeTime": nbt.Int(100000)})
	srv, connect := servertest.Start(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	connect().Join("Keeper")
	servertest.HoldTicks(t, srv)
	servertest.UntilTicks(t, srv, 40, "the planted bodies woke", func() bool {
		return srv.FireworkCount() == 1 && len(srv.ThrownInDimension("minecraft:overworld")) == 1
	})
	if got := srv.ThrownInDimension("minecraft:overworld")[0]; got.Kind != "minecraft:snowball" {
		t.Errorf("the planted snowball woke as %q", got.Kind)
	}
	if _, lifetime, ok := srv.FireworkState(rocket); !ok || lifetime != 100000 {
		t.Errorf("the planted rocket = lifetime %d (%v), want the saved 100000", lifetime, ok)
	}
	srv.FlushMobs()
	srv.Shutdown()
	for id, field := range map[string]string{"minecraft:snowball": "Item", "minecraft:firework_rocket": "FireworksItem"} {
		servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, id, func(f nbt.Compound) {
			stack, _ := nbt.Get[nbt.Compound](f, field)
			if stack["id"] != nbt.Tag(nbt.String(id)) {
				t.Errorf("the %s is saved carrying %v in %s, want its default item", id, stack["id"], field)
			}
		})
	}
}

// TestALoadedCloudExpiresOnItsSavedTimings wakes a cloud saved with
// Duration 60 and WaitTime 20 and finds it gone once its age passes
// WaitTime+Duration, where the defaults would keep it for 610 ticks.
// vanilla: AreaEffectCloud.tick
func TestALoadedCloudExpiresOnItsSavedTimings(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cloud := servertest.FixtureUUID(2, 6, 4, 9)
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id": nbt.String("minecraft:area_effect_cloud"), "UUID": servertest.UuidWords(cloud), "Pos": pos3(0.5, 64, 3.5),
		"Age": nbt.Int(0), "Radius": nbt.Float(3), "Duration": nbt.Int(60), "WaitTime": nbt.Int(20),
	})
	srv, connect := servertest.Start(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	connect().Join("Keeper")
	servertest.HoldTicks(t, srv)
	servertest.UntilTicks(t, srv, 40, "the planted cloud woke", func() bool { return srv.CloudCount() == 1 })
	last := -1
	for range 100 {
		age, _, ok := srv.CloudState(cloud)
		if !ok {
			break
		}
		last = age
		servertest.RunTicks(t, srv, 1)
	}
	if last != 79 {
		t.Errorf("the cloud was last seen at age %d, want 79 (WaitTime 20 + Duration 60 ends it on tick 80)", last)
	}
}

// TestACloudAndARocketKeepTheirFieldsThroughAnUnload lets the chunk holding
// a tuned cloud and a rocket unload when the player leaves, and finds both
// back, with the cloud's timings and particle, when the player returns.
// vanilla: AreaEffectCloud.readAdditionalSaveData
func TestACloudAndARocketKeepTheirFieldsThroughAnUnload(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Keeper")
	// Held from the spawn to the return, so the rocket flies only the ticks
	// the unload and the wake take and is still in the air for both.
	servertest.HoldTicks(t, srv)
	cloud := srv.SpawnCloudEntity(0.5, 64, 3.5).UUID
	srv.TuneCloud(cloud, 100000, 5000, 10, 3, -0.2, nbt.Compound{"type": nbt.String("minecraft:flame")})
	item, _ := registry.ItemID("minecraft:firework_rocket")
	rocket := srv.SpawnFireworkStackEntity(protocol.ItemStack{ItemID: item, Count: 1}, 2.5, 100, 2.5).UUID
	_, lifetime, _ := srv.FireworkState(rocket)

	c.Disconnect()
	servertest.WaitFor(t, srv, 200, func() bool { return srv.CloudCount() == 0 && srv.FireworkCount() == 0 })
	srv.FlushMobs()
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	c = connect()
	c.Join("Keeper")
	servertest.HoldTicks(t, srv)
	servertest.WaitFor(t, srv, 200, func() bool { return srv.CloudCount() == 1 && srv.FireworkCount() == 1 })
	if _, got, ok := srv.FireworkState(rocket); !ok || got != lifetime {
		t.Errorf("the rocket came back with lifetime %d (%v), want %d", got, ok, lifetime)
	}
	srv.FlushMobs()
	srv.Shutdown()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:area_effect_cloud", func(f nbt.Compound) {
		for key, want := range map[string]int32{"Duration": 100000, "WaitTime": 5000, "ReapplicationDelay": 10, "DurationOnUse": 3} {
			if got, _ := f[key].(nbt.Int); int32(got) != want {
				t.Errorf("the cloud's %s after the unload = %v, want %d", key, f[key], want)
			}
		}
		if got, _ := nbt.Get[nbt.Compound](f, "custom_particle"); got["type"] != nbt.Tag(nbt.String("minecraft:flame")) {
			t.Errorf("the cloud's custom_particle after the unload = %v, want flame", f["custom_particle"])
		}
	})
}
