package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

type patrolBenchWorld struct{ spawnTestWorld }

func (w patrolBenchWorld) NoLeavesHeight(x, z int32) int32 { return 63 }

// BenchmarkMobsTickPatrol steps a captain and two followers for 200 ticks,
// restarting the group so navigation stays within the loaded flat terrain.
func BenchmarkMobsTickPatrol(b *testing.B) {
	w := patrolBenchWorld{newSpawnTestWorld()}
	stone, _ := registry.FindState("minecraft:stone", nil)
	for x := int32(-32); x <= 64; x++ {
		for z := int32(-32); z <= 32; z++ {
			w.set(x, 63, z, stone)
		}
	}
	for b.Loop() {
		var mobs []*Mob
		for i := range 3 {
			m := NewMob(TypePillager, int32(i+1), float64(i*2), 64, 0, int64(i+1))
			tag := nbt.Compound{"Patrolling": nbt.Byte(1), "PersistenceRequired": nbt.Byte(1)}
			if i == 0 {
				tag["PatrolLeader"] = nbt.Byte(1)
				tag["patrol_target"] = nbt.IntArray{200, 64, 0}
			}
			m.LoadFrom(tag, w, func() int64 { return 0 })
			mobs = append(mobs, m)
		}
		for range 200 {
			for _, m := range mobs {
				m.tick(w, nil, mobs)
			}
		}
	}
}
