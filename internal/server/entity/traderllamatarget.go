package entity

var traderLlamaZombieKinds = []MobType{TypeZombie, TypeHusk, TypeDrowned, TypeZombieVillager}

// withTraderLlamaTargets adds TraderLlama.registerGoals' goals to the
// llama's: a panic at 2.0 and defend the trader at 1, zombies and
// illagers at 2 (not the zombified piglin), each after the goals already at
// its priority.
// vanilla: TraderLlama.registerGoals
func withTraderLlamaTargets(m *Mob, goals []Goal) []Goal {
	extra := []Goal{
		&panicGoal{m: m, priority: 1, pace: 2},
		&traderDefendGoal{m: m},
		&mobTargetGoal{m: m, priority: priorityBreed, kinds: traderLlamaZombieKinds},
		&mobTargetGoal{m: m, priority: priorityBreed, kinds: illagerKinds},
	}
	for _, g := range extra {
		i := 0
		for i < len(goals) && goals[i].Priority() <= g.Priority() {
			i++
		}
		goals = append(goals[:i], append([]Goal{g}, goals[i:]...)...)
	}
	return goals
}

// traderGrudgeTicks is how long a trader's attacker stays its last hurt-by.
// vanilla: LivingEntity.getLastHurtByMob
const traderGrudgeTicks = 100

// traderDefendGoal targets whoever last hurt the wandering trader holding the
// llama's lead, once per hurt.
// vanilla: TraderLlama.TraderLlamaDefendWanderingTraderGoal
type traderDefendGoal struct {
	m *Mob
	// stamp is the trader's lastDamageStamp of the grudge last taken, valid
	// while taken.
	stamp int
	taken bool
	// attacker and nextStamp are what CanStart found for Start.
	attacker  int32
	nextStamp int
}

func (g *traderDefendGoal) Priority() int    { return priorityHurtBy }
func (g *traderDefendGoal) Flags() GoalFlags { return GoalTarget }

func (g *traderDefendGoal) CanStart(t *tickView) bool {
	if g.m.LeashHolderID == 0 {
		return false
	}
	trader := t.mobByID(g.m.LeashHolderID)
	if trader == nil || trader.Type != TypeWanderingTrader || trader.retaliate == 0 ||
		trader.Age-trader.lastDamageStamp > traderGrudgeTicks || g.taken && trader.lastDamageStamp == g.stamp {
		return false
	}
	p := g.m.targetByID(t, trader.retaliate)
	if p == nil || !p.Huntable || t.playerByID(trader.retaliate) != nil && !t.canAttackPlayer(p) {
		return false
	}
	g.attacker, g.nextStamp = trader.retaliate, trader.lastDamageStamp
	return true
}

func (g *traderDefendGoal) Start(*tickView) {
	g.m.SetMetadataTargetID(g.attacker)
	g.stamp, g.taken = g.nextStamp, true
}

func (g *traderDefendGoal) ShouldContinue(t *tickView) bool {
	p := g.m.targetIn(t)
	return p != nil && g.m.inFollowRange(p)
}

func (g *traderDefendGoal) Tick(*tickView) {}

func (g *traderDefendGoal) Stop(*tickView) { g.m.SetMetadataTargetID(0) }

func (g *traderDefendGoal) RequiresEveryTick() bool { return false }
