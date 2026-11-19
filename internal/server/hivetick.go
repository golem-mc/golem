package server

import (
	"maps"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// hiveWorkChance is the per-tick chance that a hive holding bees buzzes.
// vanilla: BeehiveBlockEntity.serverTick
const hiveWorkChance = 0.005

// tickHives advances stored bees' clocks and releases those whose stay is
// up; a nectar-carrying bee raises the honey level.
// vanilla: BeehiveBlockEntity.serverTick, tickOccupants
func (s *Server) tickHives(w *world.World, ticks func(world.ChunkPos) bool) {
	for _, hive := range w.Beehives() {
		if ticks != nil && !ticks(blockChunk(hive.X, hive.Z)) {
			continue
		}
		state := w.BlockAt(hive.X, hive.Y, hive.Z)
		if !registry.BlockOf(state).InTag(registry.TagIDBeehives) {
			continue
		}
		list, _ := nbt.Get[nbt.List](hive.Tag, "bees")
		dx, dz := rules.HiveFacingStep(registry.StateValue(state, registry.PropFacing))
		stays := w.BeesStayInHive()
		frontBlocked := len(world.CollisionBoxes(w.BlockAt(hive.X+dx, hive.Y, hive.Z+dz))) > 0
		var gone []int
		// Every release reads the state from before this tick.
		// vanilla: BeehiveBlockEntity.tickOccupants
		base, level, honey := hiveHoney(state), hiveHoney(state), false
		for i, item := range list.Items {
			occupant, ok := nbt.CompoundView(item)
			if !ok {
				continue
			}
			ticks, _ := nbt.IntValue(occupant["ticks_in_hive"])
			minTicks, _ := nbt.IntValue(occupant["min_ticks_in_hive"])
			next, due := rules.HiveOccupantDue(int(ticks), int(minTicks))
			if due && !stays && !frontBlocked {
				entry := nbt.ShallowCopyCompound(item.(nbt.CompoundTag))
				entry.Set("ticks_in_hive", nbt.Int(next))
				data, _ := nbt.Get[nbt.Compound](occupant, "entity_data")
				nectar := nbt.BoolField(data, "HasNectar")
				if s.spawnOccupant(w, hive.X, hive.Y, hive.Z, dx, dz, frontBlocked, entry, nectar, nil) {
					if nectar {
						honey = true
						level = rules.HoneyAfterDelivery(base, s.levelRng.Intn(100))
					}
					gone = append(gone, i)
				}
			}
		}
		// Clocks advance in place; the chunk is marked only when a bee leaves.
		w.AdvanceHiveClocks(hive.X, hive.Y, hive.Z)
		if len(gone) > 0 {
			// Release from the advanced list, not the one read before the clocks
			// moved.
			current, _ := w.BlockEntityTag(hive.X, hive.Y, hive.Z)
			advanced, _ := nbt.Get[nbt.List](current, "bees")
			// A changed length means the occupants changed since the read, so gone's
			// indexes no longer match.
			if len(advanced.Items) != len(list.Items) {
				advanced = list
			}
			kept := slices.Clone(advanced.Items)
			for n, i := range gone {
				kept = slices.Delete(kept, i-n, i-n+1)
			}
			tag := maps.Clone(hive.Tag)
			tag["bees"] = nbt.List{Elem: nbt.TypeCompound, Items: kept}
			w.ReplaceCarried(hive.X, hive.Y, hive.Z, tag)
			s.emptyHiveNearFire(w, hive.X, hive.Y, hive.Z)
		}
		// Only an occupied hive rolls, so empty ones draw nothing from the RNG.
		if len(list.Items) > len(gone) && s.levelRng.Float64() < hiveWorkChance {
			s.broadcastSoundID(w, registry.SoundBlockBeehiveWork, protocol.SoundCategoryBlock,
				float64(hive.X)+0.5, float64(hive.Y), float64(hive.Z)+0.5, 1, 1)
		}
		if honey && level != hiveHoney(state) {
			if next, ok := registry.WithInt(state, registry.PropHoneyLevel, level); ok {
				s.setBlockAndTell(w, hive.X, hive.Y, hive.Z, next)
			}
		}
	}
}

func hiveHoney(state uint16) int {
	n, _ := registry.StateInt(state, registry.PropHoneyLevel)
	return n
}
