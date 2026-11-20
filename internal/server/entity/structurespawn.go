package entity

import (
	"encoding/binary"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// structureSpawnWorld reads structure membership without generating terrain.
type structureSpawnWorld interface {
	StructureContains(x, y, z int32, id string, piece bool) bool
}

// structureReferenceWorld also names the structures a position's chunk
// references, so a lookup per override can be skipped.
type structureReferenceWorld interface {
	StructureReferences(x, z int32) []string
}

// vanilla: worldgen/structure/{swamp_hut,monument,pillager_outpost,ancient_city,trial_chambers}.json, StructureSpawnOverride
var overworldSpawnOverrides = []struct {
	id       string
	category SpawnCategory
	all      bool // every category
	piece    bool
	spawns   []spawnData
}{
	{"minecraft:swamp_hut", CategoryMonster, false, true, []spawnData{{TypeWitch, 1, 1, 1}}},
	{"minecraft:swamp_hut", CategoryCreature, false, true, []spawnData{{TypeCat, 1, 1, 1}}},
	{"minecraft:monument", CategoryMonster, false, false, []spawnData{{TypeGuardian, 1, 2, 4}}},
	{"minecraft:monument", CategoryAxolotls, false, false, nil},
	{"minecraft:monument", CategoryUndergroundWater, false, false, nil},
	{"minecraft:pillager_outpost", CategoryMonster, false, false, []spawnData{{TypePillager, 1, 1, 1}}},
	{"minecraft:ancient_city", 0, true, false, nil},
	{"minecraft:trial_chambers", 0, true, true, nil},
}

// vanilla: LevelReader.canSeeSkyFromBelowWater
func seesSkyBelowWater(w SpawnWorld, x, y, z int32) bool {
	sea := w.SeaLevel()
	if y >= sea {
		return w.SkyLightAt(x, y, z) == 15
	}
	if w.SkyLightAt(x, sea, z) != 15 {
		return false
	}
	for scan := sea - 1; scan > y; scan-- {
		state := w.BlockAt(x, scan, z)
		if registry.LightDampening(state) > 0 && registry.BlockOf(state) != registry.BlockWater && registry.BlockOf(state) != registry.BlockLava {
			return false
		}
	}
	return true
}

// CatInHut reports the piece-based condition shared by cat spawns and variants.
func CatInHut(w any, x, y, z int32) bool {
	sw, ok := w.(structureSpawnWorld)
	return ok && sw.StructureContains(x, y, z, "minecraft:swamp_hut", true)
}

// StructurePredicate returns a predicate reporting whether (x, y, z) is inside
// a piece of a named structure, or nil when w does not track structures.
// vanilla: StructureManager.getStructureWithPieceAt
func StructurePredicate(w any, x, y, z int32) func(string) bool {
	sw, ok := w.(structureSpawnWorld)
	if !ok {
		return nil
	}
	return func(id string) bool { return sw.StructureContains(x, y, z, id, true) }
}

// StructureNautilusKey marks a structure drowned in a chunk an older build of
// this server generated; a load spawns its zombie nautilus under it.
const StructureNautilusKey = "golem:structure_nautilus"

// FinalizeStructureTag finalizes a structure mob's saved tag in place on
// dice, as Server.finalizeMobSpawn does a structure mob, and rewrites the
// mob's fields; keys the mob does not own are kept. dim names the dimension
// it generates in. It returns the compound to place: the tag, or for a
// drowned that rolled its zombie nautilus, the nautilus carrying it. A tag
// of a kind this package does not simulate is left alone.
// vanilla: StructureTemplate.placeEntities, Mob.finalizeSpawn(STRUCTURE)
func FinalizeStructureTag(tag nbt.Compound, dim string, biome registry.Biome, blackCat bool, local rules.LocalDifficulty, dice Dice) nbt.Compound {
	id, _ := nbt.Get[nbt.String](tag, "id")
	kind := MobTypeOf(string(id))
	if !KnownMobKind(kind) {
		return tag
	}
	var pos [3]float64
	if list, ok := nbt.Get[nbt.List](tag, "Pos"); ok && len(list.Items) == 3 {
		for i, item := range list.Items {
			if v, ok := item.(nbt.Double); ok {
				pos[i] = float64(v)
			}
		}
	}
	var rot [2]float32
	if list, ok := nbt.Get[nbt.List](tag, "Rotation"); ok && len(list.Items) == 2 {
		for i, item := range list.Items {
			if v, ok := item.(nbt.Float); ok {
				rot[i] = float32(v)
			}
		}
	}
	ids, _ := nbt.Get[nbt.IntArray](tag, "UUID")
	u, _ := nbt.UUIDFromIntArray(ids)
	m := NewMob(kind, 0, pos[0], pos[1], pos[2], uuidSeed(u))
	m.Yaw, m.Pitch, m.HeadYaw = rot[0], rot[1], rot[0]
	m.LoadFrom(tag, nil, func() int64 { return 0 })
	m.FinalizeStructureSpawn(dice, biome, blackCat, local)
	m.InitBruteHome(dim)
	m.SetSpawnSize(local.Special)
	saveStructureMob(m, tag)
	if IsRaiderType(kind) {
		// vanilla: Raider.finalizeSpawn(STRUCTURE)
		tag["CanJoinRaid"] = nbt.Byte(1)
	}
	if m.Jockey != JockeyStructureNautilus {
		return tag
	}
	// vanilla: Drowned.finalizeSpawn, ZombieNautilus.finalizeSpawn; the
	// UUID is derived from the drowned's so regeneration repeats it.
	nu := uuid.NewSHA1(u, []byte("zombie_nautilus"))
	n := NewMob(TypeZombieNautilus, 0, pos[0], pos[1], pos[2], uuidSeed(nu))
	n.Yaw, n.HeadYaw = rot[0], rot[0]
	n.Persistence = true
	n.FinalizeStructureSpawn(dice, biome, false, local)
	vehicle := nbt.Compound{"id": nbt.String("minecraft:zombie_nautilus"), "UUID": nbt.UUIDToIntArray(nu),
		"Pos":        nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(pos[0]), nbt.Double(pos[1]), nbt.Double(pos[2])}},
		"Passengers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{tag}}}
	saveStructureMob(n, vehicle)
	return vehicle
}

// uuidSeed keys a structure mob's own stream by its UUID, so regeneration
// repeats it; 0 for no UUID.
func uuidSeed(u uuid.UUID) int64 {
	return int64(binary.BigEndian.Uint64(u[:8]) ^ binary.BigEndian.Uint64(u[8:]))
}

// saveStructureMob writes m's fields over tag, dropping the ones it owns first.
func saveStructureMob(m *Mob, tag nbt.Compound) {
	for _, key := range MobSavedKeys {
		delete(tag, key)
	}
	tag["Rotation"] = nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(m.Yaw), nbt.Float(m.Pitch)}}
	m.SaveTo(tag, func() int64 { return 0 })
}
