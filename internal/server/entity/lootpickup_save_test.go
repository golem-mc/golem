package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestMobSavesEffectiveLootPickup(t *testing.T) {
	t.Parallel()
	for _, kind := range []MobType{TypeAllay, TypeSulfurCube} {
		t.Run(kind.Name(), func(t *testing.T) {
			m := NewMob(kind, 1, 0.5, 64, 0.5, 1)
			stack := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
			for _, occupied := range []bool{false, true} {
				m.MainHand, m.Body = protocol.ItemStack{}, protocol.ItemStack{}
				if occupied {
					m.MainHand, m.Body = stack, stack
				}
				for _, cooldown := range []struct {
					ticks   int
					present bool
				}{{0, false}, {0, true}, {20, true}} {
					m.allay.pickupWait = cooldown.ticks
					m.allay.pickupWaitSet = cooldown.present
					want := !occupied
					if kind == TypeAllay {
						want = occupied && !cooldown.present
					}
					m.CanPickUpLoot = !want
					tag := nbt.Compound{}
					m.SaveTo(tag, func() int64 { return 0 })
					if got, ok := nbt.ByteValue(tag["CanPickUpLoot"]); !ok || (got != 0) != want {
						t.Errorf("occupied=%v cooldown=%+v: saved %v, want %v", occupied, cooldown, tag["CanPickUpLoot"], want)
					}
					if m.CanPickUpLoot != !want {
						t.Error("saving changed the raw pickup flag")
					}
				}
			}
		})
	}
}
