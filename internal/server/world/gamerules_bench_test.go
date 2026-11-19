package world

import "testing"

var ruleReadBool bool
var ruleReadInt int32

func BenchmarkGameplayRuleReads(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	b.Run("MonstersBurn", func(b *testing.B) {
		for b.Loop() {
			ruleReadBool = w.MonstersBurn()
		}
	})
	b.Run("BlastMobGriefing", func(b *testing.B) {
		for b.Loop() {
			ruleReadBool = w.BoolRule(RuleMobGriefing)
		}
	})
	b.Run("PlayerTick", func(b *testing.B) {
		for b.Loop() {
			ruleReadBool = w.BoolRule(RulePlayerMovementCheck)
			ruleReadBool = w.BoolRule(RuleNaturalRegen)
			ruleReadBool = w.BoolRule(RuleDrowningDamage)
			ruleReadInt = w.MaxEntityCramming()
		}
	})
}
