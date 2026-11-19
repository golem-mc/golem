package entity

import (
	"maps"
	"math"
	"math/rand"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// SpawnerHost is what a spawner's tick needs of the server around it.
type SpawnerHost interface {
	// World is the dimension the spawner stands in.
	World() SpawnWorld
	// Rand is the level random.
	Rand() *rand.Rand
	// SpawnersWork is the spawner_blocks_work game rule.
	SpawnersWork() bool
	// Players are the living players of the dimension, spectators flagged.
	Players() []SpawnPlayer
	// Bodies are the boxes that obstruct a spawn.
	Bodies() []SpawnBody
	// MobBodies are the boxes of the mobs of the type, the corpses still
	// lying there included.
	MobBodies(t MobType) []SpawnBody
	// StandUp stands up a mob at the position facing yaw. A plain spawn is
	// finalized like a natural one; otherwise tag is its configuration. It
	// reports the entity ID, false when the mob could not be added. A non-nil
	// equip dresses the mob after it is finalized.
	StandUp(t MobType, x, y, z float64, yaw float32, tag nbt.Compound, plain bool, equip func(*Mob)) (int32, bool)
	// Loot rolls a loot table with no origin.
	Loot(table string) []protocol.ItemStack
	// SpawnDataRolled tells the clients the spawner's new SpawnData and delay.
	SpawnDataRolled(x, y, z int32)
	// DelayRolled tells the clients a new delay began.
	DelayRolled(x, y, z int32)
	// Spawned announces a mob stood up by the spawner at (x, y, z).
	Spawned(x, y, z int32, mobID int32)
}

// spawnerRoll is what a tick found to tell the clients once the block entity
// is no longer locked.
type spawnerRoll struct{ data, delay bool }

// SpawnerSpawnPos rolls one attempt's feet position around the spawner at
// (x, y, z): a centred spread of spawnRange on x and z, and one block down,
// level or up. The dice are thrown in vanilla's order, x's pair, y, z's pair.
// vanilla: BaseSpawner.serverTick
func SpawnerSpawnPos(rng *rand.Rand, x, y, z int32, spawnRange int) (float64, float64, float64) {
	r := float64(spawnRange)
	fx := float64(x) + (rng.Float64()-rng.Float64())*r + 0.5
	fy := float64(y + int32(rng.Intn(3)) - 1)
	fz := float64(z) + (rng.Float64()-rng.Float64())*r + 0.5
	return fx, fy, fz
}

// SpawnerAllows reports whether a spawner's attempt of type t at the feet
// position passes the checks made before the entity exists: the spawn box
// clear of blocks, then the type's spawn rule read for the SPAWNER reason.
// vanilla: BaseSpawner.serverTick, SpawnPlacements.checkSpawnRules
func SpawnerAllows(w SpawnWorld, rng *rand.Rand, t MobType, x, y, z float64) bool {
	if boxCollidesBlocks(w, t, x, y, z) {
		return false
	}
	bx, by, bz := int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
	if !AllowedAt(t, w.Difficulty()) {
		return false
	}
	return spawnerSpawnRules(w, rng, t, bx, by, bz, false)
}

// spawnerSpawnRules is each type's placement predicate for a spawner; only
// TRIAL_SPAWNER ignores light where the predicate explicitly allows it.
// vanilla: EntitySpawnReason.isSpawner, EntitySpawnReason.ignoresLightRequirements
func spawnerSpawnRules(w SpawnWorld, rng *rand.Rand, t MobType, x, y, z int32, ignoresLight bool) bool {
	switch {
	case t == TypeSlime, t == TypeMagmaCube:
		// vanilla: Slime.checkSlimeSpawnRules, MagmaCube.checkMagmaCubeSpawnRules
		return w.Difficulty() != rules.DifficultyPeaceful
	case t == TypeDrowned:
		// vanilla: Drowned.checkDrownedSpawnRules
		return w.Difficulty() != rules.DifficultyPeaceful && (ignoresLight || isDarkEnoughToSpawn(w, x, y, z, GoDice{rng}))
	case t == TypeSquid, t == TypeDolphin, t == TypeCod, t == TypeSalmon, t == TypePufferfish:
		// vanilla: AgeableWaterCreature.checkSurfaceAgeableWaterCreatureSpawnRules, WaterAnimal.checkSurfaceWaterAnimalSpawnRules
		sea := w.SeaLevel()
		return y >= sea-13 && y <= sea && rules.IsWater(w.BlockAt(x, y-1, z)) && rules.IsWaterBlock(w.BlockAt(x, y+1, z))
	case t == TypeGlowSquid:
		// vanilla: GlowSquid.checkGlowSquidSpawnRules
		return y <= w.SeaLevel()-33 && rawBrightness(w, x, y, z) == 0 && rules.IsWaterBlock(w.BlockAt(x, y, z))
	case t == TypeSilverfish:
		// vanilla: Silverfish.checkSilverfishSpawnRules asks no darkness;
		// its player-within-5 test is skipped for a spawner.
		return w.Difficulty() != rules.DifficultyPeaceful
	case golemKind(t), t == TypePhantom, t == TypeShulker, t == TypeEnderDragon:
		// vanilla: Mob.checkMobSpawnRules
		return true
	case t == TypeBlaze, t == TypeBreeze, t == TypeZoglin, t == TypeEndermite:
		// vanilla: Monster.checkAnyLightMonsterSpawnRules, Endermite.checkEndermiteSpawnRules
		return true
	case t == TypeGhast:
		// vanilla: Ghast.checkGhastSpawnRules
		return rng.Intn(20) == 0
	case t == TypeZombifiedPiglin, t == TypePiglin, t == TypeHoglin:
		// vanilla: ZombifiedPiglin.checkZombifiedPiglinSpawnRules, Piglin.checkPiglinSpawnRules, Hoglin.checkHoglinSpawnRules
		return registry.BlockOf(w.BlockAt(x, y-1, z)) != registry.BlockNetherWartBlock
	case t == TypePillager:
		// vanilla: PatrollingMonster.checkPatrollingMonsterSpawnRules
		return w.BlockLightAt(x, y, z) <= 8
	case t == TypeGuardian, t == TypeElderGuardian:
		// vanilla: Guardian.checkGuardianSpawnRules; a spawner skips water at pos.
		return (rng.Intn(20) == 0 || !seesSkyBelowWater(w, x, y, z)) && rules.IsWater(w.BlockAt(x, y-1, z))
	case t == TypeZombieNautilus:
		// vanilla: SpawnPlacements has no entry for the zombie nautilus.
		return true
	case MonsterKind(t):
		// vanilla: Monster.checkMonsterSpawnRules
		return ignoresLight || isDarkEnoughToSpawn(w, x, y, z, GoDice{rng})
	case t == TypeTurtle:
		// vanilla: Turtle.checkTurtleSpawnRules
		return turtleSpawnOk(w, x, y, z)
	case t == TypeSkeletonHorse:
		// vanilla: SkeletonHorse.checkSkeletonHorseSpawnRules skips ground for a spawner.
		return rawBrightness(w, x, y, z) > 8
	case t == TypeAxolotl:
		// vanilla: Axolotl.checkAxolotlSpawnRules
		return registry.BlockOf(w.BlockAt(x, y-1, z)).InTag(axolotlGround)
	case t == TypeTropicalFish:
		// vanilla: TropicalFish.checkTropicalFishSpawnRules
		sea := w.SeaLevel()
		return rules.IsWater(w.BlockAt(x, y-1, z)) && rules.IsWaterBlock(w.BlockAt(x, y+1, z)) &&
			(w.Biome(x, y, z).InTag(registry.BiomeTagIDAllowsTropicalFishSpawnsAtAnyHeight) || (y >= sea-13 && y <= sea))
	case t == TypeNautilus:
		// vanilla: AbstractNautilus.checkNautilusSpawnRules
		sea := w.SeaLevel()
		return y >= sea-25 && y <= sea-5 && rules.IsWater(w.BlockAt(x, y-1, z)) && rules.IsWaterBlock(w.BlockAt(x, y+1, z))
	case t == TypeBat:
		return batSpawnOk(w, GoDice{rng}, x, y, z)
	case t == TypeOcelot:
		// vanilla: Ocelot.checkOcelotSpawnRules; ground belongs to checkSpawnObstruction.
		return rng.Intn(3) != 0
	case t == TypeStrider:
		// vanilla: Strider.checkStriderSpawnRules; placement's lava-at-pos check is skipped.
		for y++; rules.IsLava(w.BlockAt(x, y, z)); y++ {
		}
		return rules.IsAir(w.BlockAt(x, y, z))
	case t == TypeChicken, t == TypeCow, t == TypeDonkey, t == TypeHorse,
		t == TypeLlama, t == TypeMule, t == TypePig, t == TypeSheep,
		t == TypeCat, t == TypePanda, t == TypeTraderLlama, t == TypeHappyGhast,
		t == TypeMooshroom, t == TypeGoat, t == TypeRabbit, t == TypeFox,
		t == TypeArmadillo, t == TypeCamel, t == TypeWolf, t == TypeFrog,
		t == TypeParrot, t == TypePolarBear:
		return creatureGroundSpawnOk(w, t, x, y, z)
	default:
		// vanilla: SpawnPlacements.checkSpawnRules passes an unregistered type.
		return true
	}
}

// rawBrightness is LevelReader.getRawBrightness with no darkening: the
// brighter of the sky and the block light at the cell.
func rawBrightness(w SpawnWorld, x, y, z int32) int {
	raw := int(w.SkyLightAt(x, y, z))
	if block := int(w.BlockLightAt(x, y, z)); block > raw {
		raw = block
	}
	return raw
}

// SpawnerMobFits is the spawner's check of the created mob before it joins
// the world: a PathfinderMob's walk-target value of at least 0 and the type's
// spawn obstruction.
// vanilla: BaseSpawner.serverTick, PathfinderMob.checkSpawnRules,
// Mob.checkSpawnObstruction
func SpawnerMobFits(w SpawnWorld, t MobType, x, y, z float64, bodies []SpawnBody) bool {
	bx, by, bz := int32(math.Floor(x)), int32(math.Floor(y)), int32(math.Floor(z))
	if pathfinderMobSpawn(t) && (&Mob{Type: t}).walkTargetValue(w, Waypoint{bx, by, bz}) < 0 {
		return false
	}
	return TrialMobFits(w, t, x, y, z, bodies)
}

// SpawnerDelay rolls the ticks a spawner waits before its next round: the
// low end of the range, or a uniform pick below the high end.
// vanilla: BaseSpawner.delay
func SpawnerDelay(rng *rand.Rand, minDelay, maxDelay int) int {
	if maxDelay <= minDelay {
		return minDelay
	}
	return minDelay + rng.Intn(maxDelay-minDelay)
}

// TickSpawner runs a spawner block's server tick: idle while no live
// non-spectator is within its required range or spawner_blocks_work is off;
// otherwise the delay counts down, and at zero up to SpawnCount attempts
// spawn its SpawnData's mob, stopping at MaxNearbyEntities of that type in
// range.
// vanilla: BaseSpawner.serverTick
func TickSpawner(h SpawnerHost, be *world.BlockEntity) {
	var x, y, z int32
	var requiredRange int
	be.UseLive(func(b *world.BlockEntity) {
		x, y, z, requiredRange = b.X, b.Y, b.Z, b.RequiredPlayerRange
	})
	if !h.SpawnersWork() || !spawnerPlayerNear(h.Players(), x, y, z, requiredRange) {
		return
	}
	rng := h.Rand()

	var (
		due                             bool
		spawnRange, count, nearbyCap    int
		spawnData, toSpawn, customRules nbt.Compound
		hasCustom                       bool
		roll                            spawnerRoll
	)
	be.UseLive(func(b *world.BlockEntity) {
		if b.Delay == -1 {
			roll = delaySpawner(rng, b)
		}
		if b.Delay > 0 {
			b.Delay--
			return
		}
		due = true
		if b.SpawnData == nil {
			// vanilla: BaseSpawner.getOrCreateNextSpawnData
			if b.RollSpawnPotential(rng.Intn) {
				roll.data = true
			}
			if b.SpawnData == nil {
				b.SpawnData = nbt.Compound{"entity": nbt.Compound{}}
				roll.data = true
			}
		}
		spawnData = nbt.PlainCopy(b.SpawnData).(nbt.Compound)
		toSpawn, _ = nbt.Get[nbt.Compound](spawnData, "entity")
		customRules, hasCustom = nbt.Get[nbt.Compound](spawnData, "custom_spawn_rules")
		spawnRange, count, nearbyCap = b.SpawnRange, b.SpawnCount, b.MaxNearbyEntities
	})
	tell := func(r spawnerRoll) {
		if r.data {
			h.SpawnDataRolled(x, y, z)
		}
		if r.delay {
			h.DelayRolled(x, y, z)
		}
	}
	tell(roll)
	if !due {
		return
	}
	// Ends the round on a new delay: vanilla's delay-and-return.
	giveUp := func() {
		var r spawnerRoll
		be.UseLive(func(b *world.BlockEntity) { r = delaySpawner(rng, b) })
		tell(r)
	}

	id, _ := nbt.Get[nbt.String](toSpawn, "id")
	kind := MobTypeOf(string(id))
	// A type no mob here is cannot be loaded, vanilla's null entity.
	if id == "" || !SpawnableKind(kind) {
		giveUp()
		return
	}
	// An entity compound naming only its type is finalized like a natural
	// spawn; any other field is configuration the spawn keeps instead.
	plain := len(toSpawn) == 1
	fixedPos, hasPos := nbt.Get[nbt.List](toSpawn, "Pos")
	w := h.World()

	var bodies, mobs []SpawnBody
	stale := true
	spawned := false
	for range count {
		var fx, fy, fz float64
		if hasPos && len(fixedPos.Items) == 3 {
			fx, fy, fz = listDouble(fixedPos, 0), listDouble(fixedPos, 1), listDouble(fixedPos, 2)
		} else {
			fx, fy, fz = SpawnerSpawnPos(rng, x, y, z, spawnRange)
		}
		// With custom_spawn_rules only the light limits decide, not the
		// type's spawn rule.
		// vanilla: BaseSpawner.serverTick
		if hasCustom {
			if boxCollidesBlocks(w, kind, fx, fy, fz) || !AllowedAt(kind, w.Difficulty()) ||
				!customSpawnRulesAllow(nbt.Compound{"custom_spawn_rules": customRules}, w,
					int32(math.Floor(fx)), int32(math.Floor(fy)), int32(math.Floor(fz))) {
				continue
			}
		} else if !SpawnerAllows(w, rng, kind, fx, fy, fz) {
			continue
		}
		if stale {
			bodies, mobs, stale = h.Bodies(), h.MobBodies(kind), false
		}
		if spawnerMobsNear(mobs, x, y, z, spawnRange) >= nearbyCap {
			giveUp()
			return
		}
		yaw := rng.Float32() * 360
		fits := SpawnerMobFits
		if hasCustom {
			// Mob.checkSpawnRules is skipped; the obstruction check stays.
			fits = TrialMobFits
		}
		if !fits(w, kind, fx, fy, fz, bodies) {
			continue
		}
		var tag nbt.Compound
		if !plain {
			tag = maps.Clone(toSpawn)
			delete(tag, "Pos")
		}
		// vanilla: BaseSpawner.serverTick, nextSpawnData.getEquipment().ifPresent(mob::equip)
		mobID, ok := h.StandUp(kind, fx, fy, fz, yaw, tag, plain, spawnEquipment(h.Loot, spawnData))
		if !ok {
			// vanilla: tryAddFreshEntityWithPassengers refusing
			giveUp()
			return
		}
		stale = true
		h.Spawned(x, y, z, mobID)
		spawned = true
	}
	if spawned {
		giveUp()
	}
}

// delaySpawner rolls the spawner's next delay and its next spawn from the
// potentials, and says what the clients must be told. Call it from inside
// Use.
// vanilla: BaseSpawner.delay
func delaySpawner(rng *rand.Rand, b *world.BlockEntity) spawnerRoll {
	b.Delay = SpawnerDelay(rng, b.MinSpawnDelay, b.MaxSpawnDelay)
	return spawnerRoll{data: b.RollSpawnPotential(rng.Intn), delay: true}
}

// spawnerPlayerNear reports whether a non-spectator stands within the range
// of the spawner block's centre, strictly; a negative range is unlimited.
// vanilla: EntityGetter.hasNearbyAlivePlayer
func spawnerPlayerNear(players []SpawnPlayer, x, y, z int32, requiredRange int) bool {
	cx, cy, cz := float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
	r := float64(requiredRange)
	for _, p := range players {
		if p.Spectator {
			continue
		}
		dx, dy, dz := p.X-cx, p.Y-cy, p.Z-cz
		if r < 0 || dx*dx+dy*dy+dz*dz < r*r {
			return true
		}
	}
	return false
}

// spawnerMobsNear counts the boxes that overlap the spawner's block grown by
// the spawn range on every side.
func spawnerMobsNear(bodies []SpawnBody, x, y, z int32, spawnRange int) int {
	r := float64(spawnRange)
	minX, minY, minZ := float64(x)-r, float64(y)-r, float64(z)-r
	maxX, maxY, maxZ := float64(x)+1+r, float64(y)+1+r, float64(z)+1+r
	n := 0
	for _, b := range bodies {
		if b.MinX < maxX && b.MaxX > minX &&
			b.MinY < maxY && b.MaxY > minY &&
			b.MinZ < maxZ && b.MaxZ > minZ {
			n++
		}
	}
	return n
}

// listDouble is the i-th double of an NBT list, 0 where it holds none.
func listDouble(list nbt.List, i int) float64 {
	v, _ := list.Items[i].(nbt.Double)
	return float64(v)
}
