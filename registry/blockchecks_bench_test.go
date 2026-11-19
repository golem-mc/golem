package registry

import "testing"

var blockCheckInt int
var blockCheckBool bool
var blockCheckState uint16

func BenchmarkFallingFireAndAnvilStates(b *testing.B) {
	states := [...]uint16{BlockAir.DefaultState(), BlockStone.DefaultState(), BlockFire.DefaultState(), BlockWater.DefaultState(), BlockOakPlanks.DefaultState(), MustState("minecraft:oak_fence", map[string]string{"waterlogged": "true"}), BlockAnvil.DefaultState(), BlockChippedAnvil.DefaultState()}
	b.Run("falling", func(b *testing.B) {
		i := 0
		for b.Loop() {
			blockCheckBool = CanFallThrough(states[i&7])
			i++
		}
	})
	b.Run("fire", func(b *testing.B) {
		i := 0
		for b.Loop() {
			blockCheckInt = FireIgniteOdds(states[i&7]) + FireBurnOdds(states[i&7])
			i++
		}
	})
	b.Run("anvil", func(b *testing.B) {
		states := [...]uint16{BlockAnvil.DefaultState(), BlockChippedAnvil.DefaultState(), BlockDamagedAnvil.DefaultState(), BlockStone.DefaultState()}
		i := 0
		for b.Loop() {
			blockCheckState, blockCheckBool = DamageAnvilState(states[i&3])
			i++
		}
	})
}
