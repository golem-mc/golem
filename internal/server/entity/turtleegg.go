package entity

import "github.com/golem-mc/golem/registry"

// Kinds that are turtle-egg exceptions in vanilla but are not spawned mobs here.
const typeBat = registry.EntityTypeBat

// BreaksTurtleEggsStepping reports whether a mob of kind t may break a
// turtle egg it walks over, the mob griefing rule aside.
// vanilla: TurtleEggBlock.canDestroyEgg
func BreaksTurtleEggsStepping(t MobType) bool {
	return t != TypeTurtle && t != typeBat
}

// BreaksTurtleEggsLanding reports whether a mob of kind t may break a
// turtle egg it lands on: zombies are spared on top of the stepping rule.
// vanilla: TurtleEggBlock.fallOn
func BreaksTurtleEggsLanding(t MobType) bool {
	switch t {
	case TypeZombie, TypeHusk, TypeDrowned, TypeZombieVillager, TypeZombifiedPiglin:
		return false
	}
	return BreaksTurtleEggsStepping(t)
}
