package raid

import (
	"math/rand"
	"slices"

	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// Encode is raids.dat's data compound, without raiders: a saved raider
// carries CanJoinRaid and finds its raid again.
// vanilla: Raids.CODEC, Raid.MAP_CODEC
func (rs *Raids) Encode() nbt.Compound {
	list := nbt.List{Elem: nbt.TypeCompound}
	for _, id := range sortedIDs(rs.raids) {
		list.Items = append(list.Items, rs.raids[id].encode(id))
	}
	return nbt.Compound{"raids": list, "next_id": nbt.Int(rs.nextID), "tick": nbt.Int(rs.tick)}
}

func sortedIDs(m map[int]*Raid) []int {
	ids := make([]int, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func boolTag(b bool) nbt.Byte {
	if b {
		return 1
	}
	return 0
}

func (r *Raid) encode(id int) nbt.Compound {
	heroes := nbt.List{Elem: nbt.TypeIntArray}
	for _, h := range r.heroes {
		heroes.Items = append(heroes.Items, nbt.UUIDToIntArray(h))
	}
	return nbt.Compound{
		"id": nbt.Int(id), "started": boolTag(r.started), "active": boolTag(r.active),
		"ticks_active": nbt.Long(r.ticksActive), "raid_omen_level": nbt.Int(r.omenLevel),
		"groups_spawned": nbt.Int(r.groupsSpawned), "cooldown_ticks": nbt.Int(r.cooldown),
		"post_raid_ticks": nbt.Int(r.postRaid), "total_health": nbt.Float(r.totalHealth),
		"group_count": nbt.Int(r.numGroups), "status": nbt.String(r.status.String()),
		"center":                nbt.IntArray{int32(r.center.X), int32(r.center.Y), int32(r.center.Z)},
		"heroes_of_the_village": heroes,
	}
}

// Decode loads the raids of a raids.dat data compound; a raid that does not
// read is left out, as a failed codec drops the lot.
func (rs *Raids) Decode(data nbt.Compound) {
	list, _ := nbt.Get[nbt.List](data, "raids")
	for _, item := range list.Items {
		c, ok := item.(nbt.Compound)
		if !ok {
			continue
		}
		id, ok1 := numberOf(c, "id")
		r, ok2 := rs.decodeRaid(c)
		if ok1 && ok2 {
			r.id = int(id)
			rs.raids[int(id)] = r
		}
	}
	if n, ok := numberOf(data, "next_id"); ok {
		rs.nextID = int(n)
	}
	if n, ok := numberOf(data, "tick"); ok {
		rs.tick = int(n)
	}
	rs.dirty = false
}

func numberOf(c nbt.Compound, name string) (int64, bool) {
	switch v := c[name].(type) {
	case nbt.Int:
		return int64(v), true
	case nbt.Long:
		return int64(v), true
	case nbt.Byte:
		return int64(v), true
	case nbt.Short:
		return int64(v), true
	}
	return 0, false
}

func (rs *Raids) decodeRaid(c nbt.Compound) (*Raid, bool) {
	var n [8]int64
	for i, name := range []string{"ticks_active", "raid_omen_level", "groups_spawned", "cooldown_ticks", "post_raid_ticks", "group_count", "started", "active"} {
		v, ok := numberOf(c, name)
		if !ok {
			return nil, false
		}
		n[i] = v
	}
	health, ok := c["total_health"].(nbt.Float)
	if !ok {
		return nil, false
	}
	name, ok := c["status"].(nbt.String)
	status := slices.Index(statusNames, string(name))
	center, ok2 := c["center"].(nbt.IntArray)
	if !ok || status < 0 || !ok2 || len(center) != 3 {
		return nil, false
	}
	r := newRaid(rs.level, rs, Pos{int(center[0]), int(center[1]), int(center[2])}, rand.Int63())
	r.ticksActive, r.omenLevel, r.groupsSpawned = n[0], int(n[1]), int(n[2])
	r.cooldown, r.postRaid, r.numGroups = int(n[3]), int(n[4]), int(n[5])
	r.started, r.active = n[6] != 0, n[7] != 0
	r.totalHealth, r.status = float32(health), Status(status)
	heroes, _ := nbt.Get[nbt.List](c, "heroes_of_the_village")
	for _, h := range heroes.Items {
		if arr, ok := h.(nbt.IntArray); ok {
			if id, ok := nbt.UUIDFromIntArray(arr); ok && id != uuid.Nil {
				r.AddHero(id)
			}
		}
	}
	return r, true
}
