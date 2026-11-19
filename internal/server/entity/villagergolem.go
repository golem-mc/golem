package entity

import "math"

// The golem memory and the number of villagers that must agree.
// vanilla: GolemSensor, Villager.spawnGolemIfNeeded, VillagerPanicTrigger
const (
	golemScanRate        = 200 // ticks between sensor scans
	golemMemory          = 599 // ticks the memory lasts
	golemVillagerBox     = 10  // blocks the villager search box grows by
	golemSleepWindow     = 24000
	golemGossipVillagers = 5
	golemPanicVillagers  = 3
	golemPanicEvery      = 100
)

// golemStep is the golem sensor and the panic trigger: an iron golem whose
// box meets the villager's grown by its follow range sets the memory, and a hurt or threatened villager asks for one
// every 100 ticks.
// vanilla: GolemSensor.doTick, NearestLivingEntitySensor.doTick, VillagerPanicTrigger.tick
func (m *Mob) golemStep(t *tickView) {
	vb := &m.vb
	if vb.golemScan--; vb.golemScan <= 0 {
		vb.golemScan = golemScanRate
		for _, o := range t.others {
			if o.Type == TypeIronGolem && o.Alive() && m.NearBox(o, m.followRange()) {
				m.GolemDetected(m.GameTime)
				break
			}
		}
	}
	if m.GameTime%golemPanicEvery == 0 && (vb.danger.hurt || vb.danger.hostile != nil) {
		m.spawnGolemIfNeeded(t, golemPanicVillagers)
	}
}

// GolemDetected sets the golem-seen memory at game time now.
// vanilla: GolemSensor.golemDetected
func (m *Mob) GolemDetected(now int64) { m.vb.golemUntil = now + golemMemory }

// WantsGolem reports whether the villager slept within the day and has not
// seen a golem lately.
// vanilla: Villager.wantsToSpawnGolem, golemSpawnConditionsMet
func (m *Mob) WantsGolem() bool {
	slept := m.vb.lastSlept
	return slept != 0 && m.GameTime-slept < golemSleepWindow && m.GameTime >= m.vb.golemUntil
}

// spawnGolemIfNeeded asks the server for a golem when this villager and
// enough others (counting up to five) around it want one.
// vanilla: Villager.spawnGolemIfNeeded
func (m *Mob) spawnGolemIfNeeded(t *tickView, needed int) {
	if !m.WantsGolem() {
		return
	}
	wanting := 1
	for _, o := range t.others {
		if o != m && o.Type == TypeVillager && o.Alive() && o.WantsGolem() && o.NearBox(m, golemVillagerBox) {
			wanting++
		}
	}
	if min(wanting, 5) >= needed {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobWantsGolem})
	}
}

// NearBox reports whether the mob's box meets the other's grown by r.
func (m *Mob) NearBox(o *Mob, r float64) bool {
	mw, mh := KindDims(m.Type)
	ow, oh := KindDims(o.Type)
	return math.Abs(m.X-o.X) < (mw+ow)/2+r && math.Abs(m.Z-o.Z) < (mw+ow)/2+r &&
		m.Y < o.Y+oh+r && m.Y+mh > o.Y-r
}
