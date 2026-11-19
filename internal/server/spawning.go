package server

import (
	"cmp"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// The server half of natural spawning: each tick, every dimension with a
// player gathers the chunks and mob counts entity.SpawnNaturally reads, then spawns their requests through the
// normal mob path. vanilla: ServerChunkCache.tickChunks, ChunkMap

// spawnReachChunks is the chunk radius around each player that the global
// cap's chunk count covers. vanilla: NaturalSpawner.SPAWN_DISTANCE_CHUNK
const spawnReachChunks = 8

// tickNaturalSpawning runs a tick of natural spawning in every dimension
// with a player. Nearby chunks first gain inhabited time, ungated;
// spawn_mobs off disables all else (water categories and creatures too),
// the latched spawnEnemies flag gates only monsters.
// vanilla: ServerChunkCache.tickChunks, getFilteredSpawningCategories
func (s *Server) tickNaturalSpawning() {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()

	s.tickPhantomSpawners(clients)
	s.tickCatSpawner(clients)
	s.tickTraderSpawner(clients)
	s.tickPatrolSpawner(clients)
	s.tickVillageSiege(clients)

	for _, dim := range s.allDims() {
		players := spawnPlayersOf(clients, dim.w)
		if len(players) == 0 {
			continue
		}
		w := dim.w
		if w.IsDebug() {
			continue
		}

		var spawning []world.ChunkPos
		for _, pos := range w.LoadedChunkPositions() {
			if chunkIsSpawning(players, pos) {
				spawning = append(spawning, pos)
			}
		}
		// The loaded set is a map, so its order changes per call; a fixed order
		// keeps a seeded pass over mixed biomes reproducible.
		slices.SortFunc(spawning, func(a, b world.ChunkPos) int {
			if a.X != b.X {
				return int(a.X) - int(b.X)
			}
			return int(a.Z) - int(b.Z)
		})
		w.InhabitTime(spawning)
		if s.spawnDisabled.Load() || !s.world.BoolRule(world.RuleSpawnMobs) {
			continue
		}
		monsters := w.SpawnEnemies()

		var chunks []entity.ChunkPos
		for _, pos := range spawning {
			// A chunk whose light is still building reads as pitch dark, so wait
			// (vanilla walks only FULL-status chunks).
			// vanilla: ServerLevel.canSpawnEntitiesInChunk
			if w.ChunkLightReady(pos) && entity.ChunkInBorder(w, entity.ChunkPos{X: pos.X, Z: pos.Z}) {
				chunks = append(chunks, entity.ChunkPos{X: pos.X, Z: pos.Z})
			}
		}
		if len(chunks) == 0 {
			continue
		}

		// Creatures join every 400 game ticks, with their own cap of 10 and
		// their own grass-and-light rule.
		worldAge, _ := w.Time()
		creatures := worldAge%400 == 0

		// Each mob spawns and is finalized as the pass emits it, before the
		// walk's next roll: the pass reads what finalizing wrote to the pack's
		// group (a solitary tropical fish ends its pack) and shares its dice.
		// vanilla: NaturalSpawner.spawnCategoryForPosition
		spawn := func(r entity.SpawnRequest) { s.spawnMobInDim(dim, r.Type, r.X, r.Y, r.Z, 0, r.Group) }
		spawnable := spawnableChunkCount(players)
		bodies := s.spawnBodies(dim, players)
		counts, potential := s.spawnCountsAll(dim, players)
		// MobCategory order. The water categories are friendly but not
		// persistent: they spawn every tick, not every 400th, and ignore
		// spawn_monsters, so squid spawn in peaceful like animals.
		var passes []entity.SpawnPass
		if monsters {
			passes = append(passes, entity.MonsterPass(counts[entity.CategoryMonster]))
		}
		if creatures {
			passes = append(passes, entity.CreaturePass(counts[entity.CategoryCreature]))
		}
		passes = append(passes,
			entity.AmbientPass(counts[entity.CategoryAmbient]),
			entity.AxolotlPass(counts[entity.CategoryAxolotls]),
			entity.UndergroundWaterPass(counts[entity.CategoryUndergroundWater]),
			entity.WaterCreaturePass(counts[entity.CategoryWaterCreature]),
			entity.WaterAmbientPass(counts[entity.CategoryWaterAmbient]))
		entity.SpawnNaturally(w, s.spawnRng, chunks, players, bodies, potential, passes, spawnable, spawn)
	}
}

// spawnPlayersOf lists w's players with position and spectator flag.
func spawnPlayersOf(clients []*client, w *world.World) []entity.SpawnPlayer {
	var players []entity.SpawnPlayer
	for _, c := range clients {
		if c.theWorld() != w {
			continue
		}
		pos := c.player.Position()
		d := c.dims(c.player.Appearance().Pose)
		players = append(players, entity.SpawnPlayer{
			EntityID:  c.player.EntityID,
			Width:     d.Width,
			Height:    d.Height,
			X:         pos.X,
			Y:         pos.Y,
			Z:         pos.Z,
			Spectator: c.player.GameMode() == protocol.GameModeSpectator,
			Dead:      c.player.Vitals().Dead,
		})
	}
	return players
}

// chunkIsSpawning reports whether a chunk's centre is within
// 128 blocks of a non-spectator. A centre within 128 blocks is never
// outside the 8-chunk reach, so this one check suffices.
// vanilla: ChunkMap.anyPlayerCloseEnoughForSpawning
func chunkIsSpawning(players []entity.SpawnPlayer, pos world.ChunkPos) bool {
	for i := range players {
		if players[i].CloseForSpawning(entity.ChunkPos(pos)) {
			return true
		}
	}
	return false
}

// spawnableChunkCount counts chunk positions within spawnReachChunks of any
// player, loaded or not, as vanilla's tracker does; it scales the global cap
// (70 monsters per 17x17 chunks).
func spawnableChunkCount(players []entity.SpawnPlayer) int {
	// All spans have the same width, so the preceding span in a sorted row
	// also has the furthest covered endpoint.
	type span struct{ z, x int32 }
	const width = 2*spawnReachChunks + 1
	spans := make([]span, 0, len(players)*width)
	for i := range players {
		px, pz := world.ChunkOf(players[i].X), world.ChunkOf(players[i].Z)
		for dz := int32(-spawnReachChunks); dz <= spawnReachChunks; dz++ {
			spans = append(spans, span{z: pz + dz, x: px - spawnReachChunks})
		}
	}
	slices.SortFunc(spans, func(a, b span) int {
		if a.z != b.z {
			return cmp.Compare(a.z, b.z)
		}
		return cmp.Compare(a.x, b.x)
	})
	count := 0
	for i, sp := range spans {
		if i > 0 && spans[i-1].z == sp.z && int64(sp.x)-int64(spans[i-1].x) < width {
			count += int(sp.x - spans[i-1].x)
		} else {
			count += width
		}
	}
	return count
}

// spawnCountsAll counts every category's non-persistent mobs in one walk of
// the herd, globally and per player whose range covers the mob's chunk.
// vanilla: NaturalSpawner.createState
func (s *Server) spawnCountsAll(dim *dimension, players []entity.SpawnPlayer) ([entity.SpawnCategoryCount]*entity.SpawnCounts, *entity.SpawnPotential) {
	var counts [entity.SpawnCategoryCount]*entity.SpawnCounts
	potential := &entity.SpawnPotential{}
	for i := range counts {
		counts[i] = &entity.SpawnCounts{}
	}
	// near[category*len(players)+i] counts the category's mobs close to player
	// i; the maps are built from it once the walk ends.
	near := make([]int, entity.SpawnCategoryCount*len(players))
	// The players close to each chunk seen in this walk, by index. A herd
	// stands in runs of one chunk, so the last chunk skips the map.
	nearOf := map[entity.ChunkPos][]int{}
	var last entity.ChunkPos
	var lastNear []int
	haveLast := false
	s.lockTick(&s.mobsMu)
	for _, m := range dim.mobs.herd {
		category, ok := entity.SpawnCategoryOf(m.Type)
		if !ok || !m.CountsForSpawnCap() {
			continue
		}
		counts[category].Global++
		if entity.HasSpawnCost(m.Type) {
			bx, by, bz := rules.BlockOf(m.X), rules.BlockOf(m.Y), rules.BlockOf(m.Z)
			if charge, ok := entity.SpawnChargeOf(dim.w.Biome(bx, by, bz), m.Type); ok {
				potential.Add(bx, by, bz, charge)
			}
		}
		at := entity.ChunkPos{X: world.ChunkOf(m.X), Z: world.ChunkOf(m.Z)}
		if !haveLast || at != last {
			var seen bool
			if lastNear, seen = nearOf[at]; !seen {
				lastNear = nil
				for i := range players {
					if players[i].CloseForSpawning(at) {
						lastNear = append(lastNear, i)
					}
				}
				nearOf[at] = lastNear
			}
			last, haveLast = at, true
		}
		row := near[int(category)*len(players):]
		for _, i := range lastNear {
			row[i]++
		}
	}
	s.unlockTick(&s.mobsMu)
	for category, c := range counts {
		for i, n := range near[category*len(players) : (category+1)*len(players)] {
			if n == 0 {
				continue
			}
			if c.PerPlayer == nil {
				c.PerPlayer = make(map[int32]int, len(players))
			}
			c.PerPlayer[players[i].EntityID] += n
		}
	}
	return counts, potential
}

// spawnBodies gathers every blocksBuilding body the obstruction test checks
// a new mob against: non-spectator players, herd and corpses, primed TNT,
// falling blocks, boats, minecarts and end crystals. Each list is read
// under its own lock alone, the server's usual lock order.
func (s *Server) spawnBodies(dim *dimension, players []entity.SpawnPlayer) []entity.SpawnBody {
	var bodies []entity.SpawnBody
	for i := range players {
		p := &players[i]
		if p.Spectator {
			continue
		}
		hw := p.Width / 2
		bodies = append(bodies, entity.SpawnBody{
			MinX: p.X - hw, MinY: p.Y, MinZ: p.Z - hw,
			MaxX: p.X + hw, MaxY: p.Y + p.Height, MaxZ: p.Z + hw,
		})
	}
	mobBox := func(m *entity.Mob) entity.SpawnBody {
		hw := m.BbWidth() / 2
		return entity.SpawnBody{
			MinX: m.X - hw, MinY: m.Y, MinZ: m.Z - hw,
			MaxX: m.X + hw, MaxY: m.Y + m.BbHeight(), MaxZ: m.Z + hw,
		}
	}
	lockedDo(&s.mobsMu, func() {
		for _, m := range dim.mobs.herd {
			bodies = append(bodies, mobBox(m))
		}
		// A corpse keeps its box for the twenty ticks its death animation runs.
		for _, corpse := range dim.mobs.dying {
			bodies = append(bodies, mobBox(corpse.ent.mob))
		}
	})

	lockedDo(&s.tntMu, func() {
		for _, it := range s.tnts {
			if it.w != dim.w || it.tnt.Dead {
				continue
			}
			bodies = append(bodies, entity.SpawnBody{
				MinX: it.tnt.X - entity.TNTHalfWidth, MinY: it.tnt.Y, MinZ: it.tnt.Z - entity.TNTHalfWidth,
				MaxX: it.tnt.X + entity.TNTHalfWidth, MaxY: it.tnt.Y + entity.TNTHeight, MaxZ: it.tnt.Z + entity.TNTHalfWidth,
			})
		}
	})

	lockedDo(&s.fallingMu, func() {
		for _, it := range s.fallings {
			if it.w != dim.w || it.fb.Dead {
				continue
			}
			bodies = append(bodies, entity.SpawnBody{
				MinX: it.fb.X - entity.FallingHalfWidth, MinY: it.fb.Y, MinZ: it.fb.Z - entity.FallingHalfWidth,
				MaxX: it.fb.X + entity.FallingHalfWidth, MaxY: it.fb.Y + entity.FallingHeight, MaxZ: it.fb.Z + entity.FallingHalfWidth,
			})
		}
	})

	lockedDo(&s.boatsMu, func() {
		for _, it := range s.boats {
			if it.w != dim.w || it.boat.Dead {
				continue
			}
			bodies = append(bodies, entity.SpawnBody{
				MinX: it.boat.X - entity.BoatHalfWidth, MinY: it.boat.Y, MinZ: it.boat.Z - entity.BoatHalfWidth,
				MaxX: it.boat.X + entity.BoatHalfWidth, MaxY: it.boat.Y + entity.BoatHeight, MaxZ: it.boat.Z + entity.BoatHalfWidth,
			})
		}
	})

	lockedDo(&s.minecartsMu, func() {
		for _, it := range s.minecarts {
			if it.w != dim.w || it.cart.Dead {
				continue
			}
			minX, minY, minZ, maxX, maxY, maxZ := it.cart.Box()
			bodies = append(bodies, entity.SpawnBody{
				MinX: minX, MinY: minY, MinZ: minZ,
				MaxX: maxX, MaxY: maxY, MaxZ: maxZ,
			})
		}
	})

	lockedDo(&s.endCrystalsMu, func() {
		for _, ent := range s.endCrystals {
			if ent.w != dim.w || ent.crystal.Dead {
				continue
			}
			minX, minY, minZ, maxX, maxY, maxZ := ent.crystal.BlastBox()
			bodies = append(bodies, entity.SpawnBody{
				MinX: minX, MinY: minY, MinZ: minZ,
				MaxX: maxX, MaxY: maxY, MaxZ: maxZ,
			})
		}
	})
	return bodies
}
