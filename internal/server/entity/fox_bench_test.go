package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkFoxFinishMeal measures resolving and applying a swallowed stack's consumable.
func BenchmarkFoxFinishMeal(b *testing.B) {
	for _, meal := range []string{"default", "empty effects", "custom", "play_sound"} {
		b.Run(meal, func(b *testing.B) {
			fox := NewMob(TypeFox, 1, 0.5, 64, 0.5, 1)
			stack := protocol.ItemStack{ItemID: int32(registry.ItemGoldenApple), Count: 1}
			cons := protocol.ConsumableComponent{Seconds: 1.6, Sound: protocol.SoundHolder{Name: "minecraft:entity.generic.eat"}}
			if meal == "custom" {
				cons.Animation = protocol.ItemUseAnimationDrink
				cons.Sound.Name = "minecraft:entity.experience_orb.pickup"
				cons.Effects = []protocol.ConsumeEffectComponent{
					{Type: protocol.ConsumeEffectApplyEffects, Probability: 1,
						Effects: []protocol.PotionEffectSpec{{ID: registry.MustMobEffect("minecraft:speed"), Duration: 200}}},
				}
			}
			if meal == "play_sound" {
				fox.Silent = true
				cons.Effects = []protocol.ConsumeEffectComponent{
					{Type: protocol.ConsumeEffectPlaySound, Sound: registry.SoundBlockNoteBlockPling.Holder()},
				}
			}
			if meal != "default" {
				stack.SetConsumable(cons)
			}
			var events []MobEvent
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				fox.MainHand = stack
				events = fox.foxFinishMeal(events[:0])
			}
		})
	}
}

// BenchmarkFoxFoodTick measures the mouth's component check in Fox.aiStep.
func BenchmarkFoxFoodTick(b *testing.B) {
	for _, meal := range []string{"empty", "default", "patched", "removed"} {
		b.Run(meal, func(b *testing.B) {
			w := newTestWorld()
			fox := NewMob(TypeFox, 1, 0.5, 64, 0.5, 1)
			if meal != "empty" {
				fox.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemSweetBerries), Count: 1}
			}
			if meal == "patched" {
				fox.MainHand.SetFood(protocol.FoodComponent{Nutrition: 10, Saturation: 1})
			}
			if meal == "removed" {
				fox.MainHand.Remove(protocol.DataComponentConsumable)
			}
			fox.OnGround = true
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				fox.Fox.TicksSinceEaten = 0 // Keep the meal before its chewing and swallowing delay.
				fox.foxStep(w, nil)
			}
		})
	}
}

// BenchmarkFoxFollowParent measures navigation replanning by a kit following an adult.
func BenchmarkFoxFollowParent(b *testing.B) {
	w := newTestWorld()
	floor(w, -2, -2, 12, 2, 63, defaultState(b, "minecraft:stone"))
	kit := NewMob(TypeFox, 1, 0.5, 64, 0.5, 1)
	kit.BabyAge = -24000
	parent := NewMob(TypeFox, 2, 8.5, 64, 0.5, 2)
	view := &tickView{w: w, others: []*Mob{kit, parent}}
	goal := &followParentGoal{m: kit}
	if !goal.CanStart(view) {
		b.Fatal("kit did not select its parent")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		kit.stopSeeking()
		goal.Start(view)
		goal.Tick(view)
	}
}
