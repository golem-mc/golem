package plugins_test

import (
	"slices"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: NearestAttackableTargetGoal, Mob.setTarget
func TestPluginEntityTargetEvent(t *testing.T) {
	t.Parallel()
	cancel, asked := true, 0
	var reason plugin.TargetReason
	srv, c := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityTargetEvent) {
			asked++
			reason = e.Reason
			_ = e.Entity.Health() // reads the mob: no lock may be held
			if e.Target == nil || e.Target.Type() != registry.EntityTypePlayer {
				t.Errorf("target %v", e.Target)
			}
			e.SetCancelled(cancel)
		})
	})
	id := srv.SpawnMob("minecraft:zombie", 3.5, 64, 3.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	for range 60 {
		srv.RunTick()
	}
	if asked == 0 || srv.MobTargetID(id) != 0 {
		t.Fatalf("refused: asked %d, target %d", asked, srv.MobTargetID(id))
	}
	if reason != plugin.TargetClosestPlayer {
		t.Errorf("reason %v", reason)
	}
	cancel = false
	for range 60 {
		srv.RunTick()
	}
	if srv.MobTargetID(id) == 0 {
		t.Error("an allowed target was not set")
	}
}

// vanilla: LivingEntity.addEffect
func TestPluginEntityPotionEffectEvent(t *testing.T) {
	t.Parallel()
	cancel, asked := true, 0
	var cause plugin.EffectCause
	srv, c := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityPotionEffectEvent) {
			asked++
			cause = e.Cause
			_ = e.Entity.Type() // reads the body: no lock may be held
			if e.New != nil {
				e.New.Amplifier = 1
			}
			e.SetCancelled(cancel)
		})
	})
	id := srv.SpawnMob("minecraft:cow", 3.5, 64, 3.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.GiveEffect("Victim", "minecraft:speed", 200, 0, true)
	if _, _, ok := srv.PlayerEffect("Victim", "minecraft:speed"); ok || asked != 1 {
		t.Fatalf("a refused effect landed or was not asked: asked %d", asked)
	}
	srv.GiveMobEffect(id, "minecraft:speed", 200, 0)
	if asked != 2 || cause != plugin.EffectOther {
		t.Fatalf("mob effect asked %d, cause %v", asked, cause)
	}
	cancel = false
	srv.GiveEffect("Victim", "minecraft:speed", 200, 0, true)
	if _, amp, ok := srv.PlayerEffect("Victim", "minecraft:speed"); !ok || amp != 1 {
		t.Errorf("an allowed effect: amplifier %d, present %v (the listener raised it to 1)", amp, ok)
	}
}

// vanilla: SummonCommand, ServerLevel.addFreshEntity
func TestPluginEntitySpawnEvent(t *testing.T) {
	t.Parallel()
	cancel, asked := true, 0
	var reason plugin.SpawnReason
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntitySpawnEvent) {
			asked++
			reason = e.Reason
			_ = e.Entity.Type() // reads the new body: no lock may be held
			e.SetCancelled(cancel)
		})
	})
	srv.SpawnMob("minecraft:cow", 3.5, 64, 3.5)
	if asked != 1 || len(srv.MobIDsOfKind("minecraft:cow")) != 0 {
		t.Fatalf("refused: asked %d, cows %d", asked, len(srv.MobIDsOfKind("minecraft:cow")))
	}
	cancel = false
	srv.SpawnMob("minecraft:cow", 3.5, 64, 3.5)
	if asked != 2 || len(srv.MobIDsOfKind("minecraft:cow")) != 1 || reason != plugin.SpawnDefault {
		t.Errorf("allowed: asked %d, cows %d, reason %v", asked, len(srv.MobIDsOfKind("minecraft:cow")), reason)
	}
}

// vanilla: FarmlandBlock.fallOn
func TestPluginEntityInteractEvent(t *testing.T) {
	t.Parallel()
	cancel, asked := true, 0
	srv, c := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityInteractEvent) {
			asked++
			_ = e.Entity.Type() // reads the mob: no lock may be held
			e.SetCancelled(cancel)
		})
	})
	farmland := servertest.BlockState(t, "minecraft:farmland")
	for _, x := range []int32{4, 5, 6} {
		for _, z := range []int32{4, 5, 6} {
			srv.SetBlock(x, 63, z, farmland)
		}
	}
	srv.SpawnMob("minecraft:cow", 5.5, 74, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	for range 40 {
		srv.RunTick()
	}
	if asked == 0 || srv.BlockAt(5, 63, 5) != farmland {
		t.Fatalf("refused: asked %d, block %d", asked, srv.BlockAt(5, 63, 5))
	}
	cancel = false
	srv.SpawnMob("minecraft:cow", 5.5, 74, 5.5)
	for range 40 {
		srv.RunTick()
	}
	if srv.BlockAt(5, 63, 5) == farmland {
		t.Error("an allowed landing left the farmland")
	}
}

// vanilla: SpawnEggItem.useOn, ThrownEgg.onHit, BaseSpawner.serverTick, TrialSpawner.spawnMob, Animal.spawnChildFromBreeding
func TestPluginEntitySpawnReasonsAndPersistence(t *testing.T) {
	t.Parallel()
	type spawnRecord struct {
		kind        registry.EntityType
		id          plugin.UUID
		eventReason plugin.SpawnReason
		entityProp  plugin.SpawnReason
	}
	var (
		mu           sync.Mutex
		spawns       []spawnRecord
		cancelSheep  bool
		cancelSpider bool
		host         plugin.Server
	)
	cfg := servertest.BlockTestConfig()
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{
		Name: "spawnreasons",
		Plugin: &testPlugin{enable: func(s plugin.Server) error {
			host = s
			plugin.Listen(s, func(e *plugin.PlayerEggThrowEvent) {
				e.Hatching, e.Hatches = true, 1
			})
			plugin.Listen(s, func(e *plugin.EntitySpawnEvent) {
				mu.Lock()
				spawns = append(spawns, spawnRecord{
					kind:        e.Entity.Type(),
					id:          e.Entity.UUID(),
					eventReason: e.Reason,
					entityProp:  e.Entity.SpawnReason(),
				})
				cancel := (cancelSheep && e.Entity.Type() == registry.EntityTypeSheep) ||
					(cancelSpider && e.Entity.Type() == registry.EntityTypeSpider)
				mu.Unlock()
				if cancel {
					e.SetCancelled(true)
				}
			})
			return nil
		}},
	}})
	takeLast := func(kind registry.EntityType) (spawnRecord, bool) {
		mu.Lock()
		defer mu.Unlock()
		for _, spawn := range slices.Backward(spawns) {
			if spawn.kind == kind {
				return spawn, true
			}
		}
		return spawnRecord{}, false
	}
	c := servertest.DialServer(t, srv)
	c.JoinAs("Rancher")

	// 1. Spawn egg used on a block -> SpawnSpawnerEgg (before HoldTicks so the
	// client goroutine's EntitySpawnEvent runs on the tick loop).
	srv.RunLine("give Rancher minecraft:cow_spawn_egg 1")
	c.Settle()
	c.PlaceBlock(2, 63, 3, protocol.FaceTop)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ExpectBlockAck()
	eggCow, ok := takeLast(registry.EntityTypeCow)
	if !ok || eggCow.eventReason != plugin.SpawnSpawnerEgg || eggCow.entityProp != plugin.SpawnSpawnerEgg {
		t.Fatalf("spawn-egg cow: ok=%v event=%v entity=%v, want SpawnSpawnerEgg", ok, eggCow.eventReason, eggCow.entityProp)
	}

	// 2. Thrown egg chick -> SpawnEgg (aim downward so it lands in chunk (0,0)
	// and settle so the throw packet is processed before HoldTicks).
	srv.RunLine("item replace entity Rancher hotbar.0 with minecraft:egg 1")
	c.SendRotation(0, 45, true)
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()

	servertest.HoldTicks(t, srv)
	servertest.UntilTicks(t, srv, 80, "thrown egg hatches", func() bool {
		_, found := takeLast(registry.EntityTypeChicken)
		return found
	})
	eggChick, ok := takeLast(registry.EntityTypeChicken)
	if !ok || eggChick.eventReason != plugin.SpawnEgg || eggChick.entityProp != plugin.SpawnEgg {
		t.Fatalf("thrown-egg chick: ok=%v event=%v entity=%v, want SpawnEgg", ok, eggChick.eventReason, eggChick.entityProp)
	}
	run := func(f func()) {
		t.Helper()
		ran := false
		host.Scheduler().RunTask(func() { f(); ran = true })
		servertest.RunTicks(t, srv, 1)
		if !ran {
			t.Fatal("plugin task did not complete")
		}
	}

	// 3. Monster spawner -> SpawnSpawner.
	srv.RunLine("time set noon")
	srv.RunLine("fill 2 63 2 6 63 6 minecraft:grass_block")
	srv.RunLine(`setblock 4 64 2 minecraft:spawner{Delay:0s,MinSpawnDelay:1s,MaxSpawnDelay:2s,SpawnCount:1s,SpawnData:{entity:{id:"minecraft:pig"}}}`)
	servertest.UntilTicks(t, srv, 40, "pig spawner spawns", func() bool {
		_, found := takeLast(registry.EntityTypePig)
		return found
	})
	spawnerPig, _ := takeLast(registry.EntityTypePig)
	if spawnerPig.eventReason != plugin.SpawnSpawner || spawnerPig.entityProp != plugin.SpawnSpawner {
		t.Fatalf("spawner pig: event=%v entity=%v, want SpawnSpawner", spawnerPig.eventReason, spawnerPig.entityProp)
	}
	srv.RunLine("setblock 4 64 2 minecraft:air")

	// 4. Trial spawner -> SpawnTrialSpawner (requires a survival player in range).
	srv.RunLine("gamemode survival Rancher")
	srv.SetDifficulty(2)
	srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:trial_spawner", map[string]string{"trial_spawner_state": "inactive"}))
	srv.SetBlockEntityTagAt(4, 64, 4, "minecraft:trial_spawner", nbt.Compound{
		"target_cooldown_length": nbt.Int(100),
		"normal_config": nbt.Compound{
			"total_mobs":                         nbt.Float(1),
			"simultaneous_mobs":                  nbt.Float(1),
			"total_mobs_added_per_player":        nbt.Float(0),
			"simultaneous_mobs_added_per_player": nbt.Float(0),
			"ticks_between_spawn":                nbt.Int(2),
			"spawn_potentials": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
				"weight": nbt.Int(1),
				"data":   nbt.Compound{"entity": nbt.Compound{"id": nbt.String("minecraft:husk")}},
			}}},
		},
	})
	servertest.UntilTicks(t, srv, 120, "trial spawner spawns husk", func() bool {
		_, found := takeLast(registry.EntityTypeHusk)
		return found
	})
	trialHusk, _ := takeLast(registry.EntityTypeHusk)
	if trialHusk.eventReason != plugin.SpawnTrialSpawner || trialHusk.entityProp != plugin.SpawnTrialSpawner {
		t.Fatalf("trial spawner husk: event=%v entity=%v, want SpawnTrialSpawner", trialHusk.eventReason, trialHusk.entityProp)
	}
	srv.RunLine("setblock 4 64 4 minecraft:air")
	srv.RunLine("gamemode creative Rancher")

	// 5. Dispenser spawn egg -> SpawnDispenser.
	srv.RunLine("setblock 4 64 6 minecraft:dispenser[facing=south]")
	if !srv.SetInventoryAt(4, 64, 6, 0, servertest.ItemStack(t, "minecraft:rabbit_spawn_egg", 1)) {
		t.Fatal("dispenser has no block entity to fill")
	}
	run(func() { srv.Dispense(4, 64, 6, protocol.FaceSouth) })
	dispensedRabbit, ok := takeLast(registry.EntityTypeRabbit)
	if !ok || dispensedRabbit.eventReason != plugin.SpawnDispenser || dispensedRabbit.entityProp != plugin.SpawnDispenser {
		t.Fatalf("dispensed rabbit: ok=%v event=%v entity=%v, want SpawnDispenser", ok, dispensedRabbit.eventReason, dispensedRabbit.entityProp)
	}
	srv.RunLine("setblock 4 64 6 minecraft:air")

	// 6. Cancelled tagged /summon with passengers -> SpawnCommand, mob and riders removed.
	run(func() {
		mu.Lock()
		cancelSpider = true
		mu.Unlock()
		srv.RunLine(`summon minecraft:spider 5.5 64 3.5 {Passengers:[{id:"minecraft:skeleton"}]}`)
		mu.Lock()
		cancelSpider = false
		mu.Unlock()
	})
	cancelledSpider, ok := takeLast(registry.EntityTypeSpider)
	if !ok || cancelledSpider.eventReason != plugin.SpawnCommand || cancelledSpider.entityProp != plugin.SpawnCommand {
		t.Fatalf("cancelled tagged summon spider: ok=%v event=%v entity=%v, want SpawnCommand", ok, cancelledSpider.eventReason, cancelledSpider.entityProp)
	}
	if spiders, skeletons := len(srv.MobIDsOfKind("minecraft:spider")), len(srv.MobIDsOfKind("minecraft:skeleton")); spiders != 0 || skeletons != 0 {
		t.Fatalf("cancelled tagged summon left mobs behind: spiders=%d skeletons=%d", spiders, skeletons)
	}

	// 7. Breeding -> SpawnBreeding.
	var cowA, cowB int32
	run(func() {
		cowA = srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
		cowB = srv.SpawnMob("minecraft:cow", 6.0, 64, 5.5)
	})
	srv.SetMobInLove(cowA, 600)
	srv.SetMobInLove(cowB, 600)
	beforeCows := len(srv.MobIDsOfKind("minecraft:cow"))
	servertest.UntilTicks(t, srv, 120, "calf is born", func() bool {
		return len(srv.MobIDsOfKind("minecraft:cow")) > beforeCows
	})
	bredCalf, ok := takeLast(registry.EntityTypeCow)
	if !ok || bredCalf.eventReason != plugin.SpawnBreeding || bredCalf.entityProp != plugin.SpawnBreeding {
		t.Fatalf("bred calf: ok=%v event=%v entity=%v, want SpawnBreeding", ok, bredCalf.eventReason, bredCalf.entityProp)
	}

	// 8. World.Spawn (mob cancel + allow, plus boat, minecart, armor stand) and summoned item.
	var sheepID, boatID, cartID, standID, itemID plugin.UUID
	run(func() {
		w := host.Overworld()
		loc := plugin.Location{World: w, Pos: plugin.Vec3{X: 3.5, Y: 64, Z: 5.5}}
		mu.Lock()
		cancelSheep = true
		mu.Unlock()
		if refused, err := w.Spawn(registry.EntityTypeSheep, loc, nil); err == nil || refused != nil {
			t.Fatalf("cancelled World.Spawn succeeded: entity=%v err=%v", refused, err)
		}
		mu.Lock()
		cancelSheep = false
		mu.Unlock()
		sheep, err := w.Spawn(registry.EntityTypeSheep, loc, nil)
		if err != nil || sheep == nil {
			t.Fatalf("World.Spawn sheep: %v", err)
		}
		sheepID = sheep.UUID()
		pluginSheep, ok := takeLast(registry.EntityTypeSheep)
		if !ok || pluginSheep.eventReason != plugin.SpawnPlugin || sheep.SpawnReason() != plugin.SpawnPlugin {
			t.Fatalf("World.Spawn sheep: ok=%v event=%v entity=%v, want SpawnPlugin", ok, pluginSheep.eventReason, sheep.SpawnReason())
		}

		for _, nb := range []struct {
			name string
			kind registry.EntityType
			dst  *plugin.UUID
		}{
			{"boat", registry.EntityTypeOakBoat, &boatID},
			{"minecart", registry.EntityTypeMinecart, &cartID},
			{"armor stand", registry.EntityTypeArmorStand, &standID},
		} {
			ent, err := w.Spawn(nb.kind, loc, nil)
			if err != nil || ent == nil {
				t.Fatalf("World.Spawn %s: %v", nb.name, err)
			}
			*nb.dst = ent.UUID()
			rec, ok := takeLast(nb.kind)
			if !ok || rec.eventReason != plugin.SpawnPlugin || rec.entityProp != plugin.SpawnPlugin || ent.SpawnReason() != plugin.SpawnPlugin {
				t.Fatalf("World.Spawn %s: ok=%v event=%v onEvent=%v live=%v, want SpawnPlugin", nb.name, ok, rec.eventReason, rec.entityProp, ent.SpawnReason())
			}
		}

		srv.RunLine(`summon minecraft:item 3.5 64 5.5 {PickupDelay:32767s,Item:{id:"minecraft:stone",count:1}}`)
		summonedItem, ok := takeLast(registry.EntityTypeItem)
		if !ok || summonedItem.eventReason != plugin.SpawnCommand || summonedItem.entityProp != plugin.SpawnCommand {
			t.Fatalf("summoned item: ok=%v event=%v entity=%v, want SpawnCommand", ok, summonedItem.eventReason, summonedItem.entityProp)
		}
		itemID = summonedItem.id
	})

	// 9. Persistence across chunk unload and reload.
	c.Disconnect()
	servertest.UntilTicks(t, srv, 100, "chunks unload", func() bool { return srv.MobCount() == 0 })
	servertest.ReleaseTicks(t, srv)
	c2 := servertest.DialServer(t, srv)
	c2.JoinAs("Rancher")
	servertest.HoldTicks(t, srv)
	servertest.UntilTicks(t, srv, 40, "mobs reload", func() bool { return srv.MobCount() > 0 })

	run(func() {
		for _, tc := range []struct {
			name string
			id   plugin.UUID
			want plugin.SpawnReason
		}{
			{"spawn-egg cow", eggCow.id, plugin.SpawnSpawnerEgg},
			{"thrown-egg chick", eggChick.id, plugin.SpawnEgg},
			{"spawner pig", spawnerPig.id, plugin.SpawnSpawner},
			{"trial spawner husk", trialHusk.id, plugin.SpawnTrialSpawner},
			{"dispensed rabbit", dispensedRabbit.id, plugin.SpawnDispenser},
			{"bred calf", bredCalf.id, plugin.SpawnBreeding},
			{"plugin sheep", sheepID, plugin.SpawnPlugin},
			{"plugin boat", boatID, plugin.SpawnPlugin},
			{"plugin minecart", cartID, plugin.SpawnPlugin},
			{"plugin armor stand", standID, plugin.SpawnPlugin},
			{"summoned item", itemID, plugin.SpawnCommand},
		} {
			ent, found := host.Overworld().Entity(tc.id)
			if !found {
				t.Errorf("%s not found after chunk reload", tc.name)
				continue
			}
			if got := ent.SpawnReason(); got != tc.want {
				t.Errorf("%s SpawnReason() after reload = %v, want %v", tc.name, got, tc.want)
			}
		}
	})
}
