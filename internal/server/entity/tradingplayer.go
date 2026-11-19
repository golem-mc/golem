package entity

// Distances to the player a merchant trades with.
// vanilla: VillagerGoalPackages.getCorePackage (LookAndFollowTradingPlayerSink),
// TradeWithPlayerGoal
const (
	villagerFollowTradeRange = 4 // blocks within which the villager follows
	villagerTradeCloseEnough = 2 // blocks it walks to
	traderHoldRange2         = 16
)

// tradingPlayerGoal keeps a merchant with a window open at its trading
// player: a villager walks to within two blocks of them within four and looks
// at them, a wandering trader stands still on the ground within four blocks
// and looks at them.
// vanilla: LookAndFollowTradingPlayerSink, TradeWithPlayerGoal,
// LookAtTradingPlayerGoal
type tradingPlayerGoal struct {
	m      *Mob
	trader bool
}

func (g *tradingPlayerGoal) Priority() int           { return priorityPanic }
func (g *tradingPlayerGoal) Flags() GoalFlags        { return GoalMove | GoalLook }
func (g *tradingPlayerGoal) RequiresEveryTick() bool { return true }
func (g *tradingPlayerGoal) Start(*tickView)         {}

// Stop ends the trade of a wandering trader whose goal fails; a villager's
// window is closed by its own checks.
// vanilla: TradeWithPlayerGoal.stop
func (g *tradingPlayerGoal) Stop(*tickView) {
	if g.trader {
		g.m.Trading = false
	}
}
func (g *tradingPlayerGoal) CanStart(t *tickView) bool { return g.player(t) != nil }
func (g *tradingPlayerGoal) ShouldContinue(t *tickView) bool {
	return g.player(t) != nil
}

// player is the trading player while the goal applies.
func (g *tradingPlayerGoal) player(t *tickView) *Player {
	m := g.m
	if !m.Trading || m.Dead {
		return nil
	}
	for i := range t.players {
		p := &t.players[i]
		if p.UUID != m.TradingPlayer {
			continue
		}
		d2 := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z)
		if g.trader {
			if !m.OnGround || m.InWater() || m.InvulnTicks > hurtCooldown || d2 > traderHoldRange2 {
				return nil
			}
		} else if d2 > villagerFollowTradeRange*villagerFollowTradeRange {
			return nil
		}
		return p
	}
	return nil
}

func (g *tradingPlayerGoal) Tick(t *tickView) {
	p := g.player(t)
	if p == nil {
		return
	}
	m := g.m
	dx, dz := p.X-m.X, p.Z-m.Z
	m.HeadYaw = float32(yawToward(dx, dz))
	m.Pitch = float32(pitchToward(dx, p.Y+p.eyeHeight()-(m.Y+m.height()*0.85), dz))
	if g.trader {
		m.stopSeeking()
		return
	}
	if dx*dx+dz*dz > villagerTradeCloseEnough*villagerTradeCloseEnough {
		m.seekTo(t.w, p.X, p.Y, p.Z, villagerWalkPace, navReach)
	} else {
		m.stopSeeking()
	}
}
