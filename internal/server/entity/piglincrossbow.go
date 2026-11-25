package entity

const (
	// piglinCrossbowRange is CrossbowItem.getDefaultProjectileRange: the
	// crossbow is used within it, and the piglin walks until within one less.
	// vanilla: BehaviorUtils.isWithinAttackRange
	piglinCrossbowRange = 8.0
	// piglinBackUpDistance and piglinBackUpSpeed are BackUpIfTooClose's
	// arguments in PiglinAi.initFightActivity.
	piglinBackUpDistance = 5.0
	piglinBackUpSpeed    = 0.75
)

// piglinCrossbowGoal is the fight activity of a piglin holding a crossbow:
// walk to within range, back away when closer than five blocks, and run
// CrossbowAttack's charge, wait and shoot while the target is seen in range.
// vanilla: PiglinAi.initFightActivity, CrossbowAttack, BackUpIfTooClose
type piglinCrossbowGoal struct {
	crossbowAttackGoal
}

func newPiglinCrossbowGoal(m *Mob, priority int) *piglinCrossbowGoal {
	return &piglinCrossbowGoal{crossbowAttackGoal{m: m, priority: priority}}
}

func (g *piglinCrossbowGoal) CanStart(t *tickView) bool {
	return g.holdsCrossbow() && g.m.targetIn(t) != nil
}

func (g *piglinCrossbowGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }

func (g *piglinCrossbowGoal) Start(*tickView) {}

// release is CrossbowAttack.stop: drop the draw and the loaded arrow.
func (g *piglinCrossbowGoal) release() {
	m := g.m
	if m.ChargingCrossbow {
		g.cancelCharge()
	} else if m.UsingItem {
		m.SetMetadataUsingItem(false)
	}
	g.state = crossbowUncharged
}

func (g *piglinCrossbowGoal) Stop(*tickView) {
	g.release()
	g.m.stopSeeking()
}

func (g *piglinCrossbowGoal) Tick(t *tickView) {
	m := g.m
	p := m.targetIn(t)
	if p == nil {
		return
	}
	sees := m.seesTarget(t.w, p)
	dist2 := Dist2(m.X, m.Y, m.Z, p.X, p.Y, p.Z)
	dx, dz := p.X-m.X, p.Z-m.Z
	// vanilla: SetWalkTargetFromAttackTargetIfTargetOutOfReach looks at the
	// target whenever it walks, CrossbowAttack.tick whenever it runs
	if dx != 0 || dz != 0 {
		m.HeadYaw = turnToward(m.HeadYaw, float32(yawToward(dx, dz)), headTurnSpeed)
	}
	m.Pitch = turnToward(m.Pitch,
		float32(pitchToward(dx, p.Y+p.eyeHeight()-(m.Y+m.eyeHeight()), dz)), headTurnSpeed)

	reach := piglinCrossbowRange - 1
	if sees && dist2 < reach*reach {
		m.stopSeeking()
	} else {
		g.pathDelay--
		if g.pathDelay <= 0 {
			m.seekTo(t.w, p.X, p.Y, p.Z, crossbowWalkFactor, navReach)
			g.pathDelay = crossbowPathDelayMinTicks + m.Rand.Intn(crossbowPathDelaySpread)
		}
	}
	if sees && dist2 < piglinBackUpDistance*piglinBackUpDistance {
		g.backUp(t.w)
	}

	if !sees || dist2 >= piglinCrossbowRange*piglinCrossbowRange {
		if g.state != crossbowUncharged || m.UsingItem {
			g.release()
		}
		return
	}
	g.stepCrossbow(t, p, sees, true)
}

// backUp walks straight backwards at BackUpIfTooClose's speed, the body
// turned to the head.
func (g *piglinCrossbowGoal) backUp(w World) {
	m := g.m.navOf()
	m.Yaw = g.m.HeadYaw
	m.strafeInput(w, -piglinBackUpSpeed, 0)
}
