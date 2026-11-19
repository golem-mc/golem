package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The panda's flag byte and counters are synched as MetadataIndexPandaFlags,
// MetadataIndexPandaSneezeCounter and MetadataIndexPandaEatCounter.
func (m *Mob) PandaFlags() uint8       { return m.panda.flags }
func (m *Mob) PandaSneezeCounter() int { return m.panda.sneeze }
func (m *Mob) PandaEatCounter() int    { return m.panda.eat }

// ForcePandaSneeze makes the sneeze goal's next check start.
func (m *Mob) ForcePandaSneeze() { m.panda.forceSneeze = true }

// SetPandaItems is the tick's view of the pickable items on the ground.
func (m *Mob) SetPandaItems(items []FoxItem) { m.panda.ground = items }

func (m *Mob) pandaFlag(f uint8) bool { return m.panda.flags&f != 0 }

func (m *Mob) pandaSetFlag(f uint8, on bool) {
	if on {
		m.SetMetadataPandaFlags(m.panda.flags | (f))
	} else {
		m.SetMetadataPandaFlags(m.panda.flags &^ (f))
	}
}

func (m *Mob) pandaEating() bool { return m.panda.eat > 0 }

// pandaCanAct is Panda.canPerformAction.
func (m *Mob) pandaCanAct() bool {
	return !m.pandaFlag(pandaFlagOnBack|pandaFlagRoll|pandaFlagSit) && !m.panda.scared && !m.pandaEating()
}

func (m *Mob) pandaNotSitting() bool {
	if m.pandaFlag(pandaFlagSit) {
		m.stopSeeking()
		return false
	}
	return true
}

// pandaTrySit is Panda.tryToSit.
func (m *Mob) pandaTrySit() {
	if !m.InWater() {
		m.walk.speed = 0
		m.stopSeeking()
		m.pandaSetFlag(pandaFlagSit, true)
	}
}

// pandaEatsFromGround is Panda.canPickUpAndEat's item test.
func pandaEatsFromGround(item int32) bool {
	return registry.Item(item).InTag(registry.ItemTagIDPandaEatsFromGround)
}

// takePanda is Panda.pickUpItem: a panda with an empty main hand takes the
// whole stack of something it eats.
func (s *stagedSlots) takePanda(stack protocol.ItemStack) (slot string, count int32, ok bool) {
	if !s.main.Empty() || !pandaEatsFromGround(stack.ItemID) {
		return "", 0, false
	}
	s.main = stack
	return "mainhand", stack.Count, true
}

// PandaSnack is the part of Panda.mobInteract that feeds a panda that cannot
// breed: it sits and eats the one item, and drops what it held.
func (m *Mob) PandaSnack(item int32) (held protocol.ItemStack, ok bool) {
	if m.panda.scared || m.pandaFlag(pandaFlagSit) || m.InWater() {
		return protocol.ItemStack{}, false
	}
	m.pandaTrySit()
	m.SetMetadataPandaEat(1)
	held = m.MainHand
	m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{ItemID: item, Count: 1})
	return held, true
}

// PandaScared is Panda.isScared: a worried panda in thunder.
func (m *Mob) PandaScared() bool { return m.panda.scared }

// PandaStandUp is Panda.mobInteract on a panda lying on its back.
func (m *Mob) PandaStandUp() bool {
	if m.panda.scared || !m.pandaFlag(pandaFlagOnBack) {
		return false
	}
	m.pandaSetFlag(pandaFlagOnBack, false)
	return true
}

// pandaTick is Panda.tick after the goals: the worried panda's shelter, the
// unhappy, sneeze and roll counters, and eating.
func (m *Mob) pandaTick(t *tickView) {
	p := &m.panda
	thundering := t.w.IsThundering()
	p.scared = m.pandaVariant() == GeneWorried && thundering
	if m.pandaVariant() == GeneWorried {
		if thundering && !m.InWater() {
			m.pandaSetFlag(pandaFlagSit, true)
			m.SetMetadataPandaEat(0)
		} else if !m.pandaEating() {
			m.pandaSetFlag(pandaFlagSit, false)
		}
	}
	if m.TargetID == 0 {
		p.gotBamboo, p.didBite = false, false
	}
	if p.unhappy > 0 {
		if p.unhappy == 29 || p.unhappy == 14 {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityPandaCantBreed, Volume: 1, Pitch: 1})
		}
		m.SetMetadataPandaUnhappy(p.unhappy - 1)
	}
	if m.pandaFlag(pandaFlagSneeze) {
		m.SetMetadataPandaSneeze(p.sneeze + 1)
		if p.sneeze > 20 {
			m.pandaSetFlag(pandaFlagSneeze, false)
			m.SetMetadataPandaSneeze(0)
			m.pandaAfterSneeze(t)
		} else if p.sneeze == 1 {
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityPandaPreSneeze, Volume: 1, Pitch: 1})
		}
	}
	if m.pandaFlag(pandaFlagRoll) {
		m.pandaRoll()
	} else {
		p.rollCount = 0
	}
	if m.pandaFlag(pandaFlagSit) {
		m.Pitch = 0
	}
	m.pandaEat(t)
	if !m.pandaCanAct() {
		// vanilla: PandaMoveControl.tick
		m.walk.speed = 0
	}
}

// pandaEat is Panda.handleEating and addEatingParticles' sound.
func (m *Mob) pandaEat(t *tickView) {
	p := &m.panda
	held := !m.MainHand.Empty()
	sitting := m.pandaFlag(pandaFlagSit)
	if !m.pandaEating() && sitting && !p.scared && held && m.Rand.Intn(80) == 1 {
		m.SetMetadataPandaEat(1)
	} else if !held || !sitting {
		m.SetMetadataPandaEat(0)
	}
	if !m.pandaEating() {
		return
	}
	if p.eat%5 == 0 {
		// vanilla: Panda.addEatingParticles evaluates volume before pitch.
		volume := 0.5 + 0.5*float32(m.Rand.Intn(2))
		pitch := (m.Rand.Float32()-m.Rand.Float32())*0.2 + 1
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityPandaEat,
			Volume: volume, Pitch: pitch})
	}
	if p.eat > 80 && m.Rand.Intn(20) == 1 {
		if p.eat > 100 && held && pandaEatsFromGround(m.MainHand.ItemID) {
			m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{})
			t.events = append(t.events, MobEvent{Mob: m, Kind: MobPandaAte})
			m.pandaSetFlag(pandaFlagSit, false)
		}
		m.SetMetadataPandaEat(0)
		return
	}
	m.SetMetadataPandaEat(p.eat + 1)
}

// pandaRoll is Panda.handleRoll: 32 steps, a hop at 7, 15 and 23.
func (m *Mob) pandaRoll() {
	p := &m.panda
	p.rollCount++
	if p.rollCount > 32 {
		m.pandaSetFlag(pandaFlagRoll, false)
		return
	}
	switch p.rollCount {
	case 1:
		angle := float64(float32(m.Yaw) * (math.Pi / 180))
		scale := 0.2
		if m.IsBaby() {
			scale = 0.1
		}
		scale = float64(float32(scale))
		p.rollX = m.VX + -float64(float32(math.Sin(angle)))*scale
		p.rollZ = m.VZ + float64(float32(math.Cos(angle)))*scale
		m.VX, m.VY, m.VZ = p.rollX, 0.27, p.rollZ
	case 7, 15, 23:
		m.VX, m.VZ = 0, 0
		if m.OnGround {
			m.VY = 0.27
		}
	default:
		m.VX, m.VZ = p.rollX, p.rollZ
	}
}

// pandaAfterSneeze is Panda.afterSneeze: the sneeze sound, a hop for the
// grown pandas within ten blocks, and the gift when mob drops are on.
func (m *Mob) pandaAfterSneeze(t *tickView) {
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobSounded, SoundID: registry.SoundEntityPandaSneeze, Volume: 1, Pitch: 1})
	for _, o := range t.others {
		if o.Type != TypePanda || o.IsBaby() || !o.OnGround || o.InWater() || !o.pandaCanAct() ||
			!boxesOverlap(m.X, m.Y-10, m.Z, m.width()+20, m.height()+20, o.X, o.Y, o.Z, o.width(), o.height()) {
			continue
		}
		o.jumpFromGround(blockJumpFactor(t.w, o.X, o.Y, o.Z))
	}
	if !t.w.BoolRule(world.RuleMobDrops) {
		return
	}
	ctx := rules.LootContext{This: m.LootEntity(), Level: m.lootLevel(t.w)}
	for _, stack := range rules.RollLoot("minecraft:gameplay/panda_sneeze", &ctx, m.Rand) {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobDroppedItem, Stack: stack, X: m.X, Y: m.Y, Z: m.Z})
	}
}

// pandaItemsWithin lists the item stacks the panda eats from the ground
// within the box grown by r around it.
func (m *Mob) pandaItemsWithin(r float64) []FoxItem {
	var found []FoxItem
	for _, it := range m.panda.ground {
		if pandaEatsFromGround(it.ItemID) && math.Abs(it.X-m.X) <= r+m.width()/2+0.125 &&
			math.Abs(it.Z-m.Z) <= r+m.width()/2+0.125 && it.Y+0.25 >= m.Y-r && it.Y <= m.Y+m.height()+r {
			found = append(found, it)
		}
	}
	return found
}

// pandaSitGoal is Panda.PandaSitGoal.
type pandaSitGoal struct{ m *Mob }

func (g *pandaSitGoal) Priority() int           { return pandaPrioritySit }
func (g *pandaSitGoal) Flags() GoalFlags        { return GoalMove }
func (g *pandaSitGoal) RequiresEveryTick() bool { return false }

func (g *pandaSitGoal) CanStart(*tickView) bool {
	m := g.m
	if m.panda.sitAfter > m.Age || m.IsBaby() || m.InWater() || !m.pandaCanAct() || m.panda.unhappy > 0 {
		return false
	}
	return !m.MainHand.Empty() || len(m.pandaItemsWithin(6)) > 0
}

func (g *pandaSitGoal) ShouldContinue(*tickView) bool {
	m := g.m
	if m.InWater() || m.pandaVariant() != GeneLazy && m.Rand.Intn(reducedTickDelay(600)) == 1 {
		return false
	}
	return m.Rand.Intn(reducedTickDelay(2000)) != 1
}

func (g *pandaSitGoal) Tick(*tickView) {
	if m := g.m; !m.pandaFlag(pandaFlagSit) && !m.MainHand.Empty() {
		m.pandaTrySit()
	}
}

func (g *pandaSitGoal) Start(t *tickView) {
	m := g.m
	if !m.MainHand.Empty() {
		m.pandaTrySit()
	} else if items := m.pandaItemsWithin(8); len(items) > 0 {
		m.seekTo(t.w, items[0].X, items[0].Y, items[0].Z, float64(float32(1.2)), navReach)
	}
	m.panda.sitAfter = 0
}

// Stop drops what the panda held and waits before the next sit.
func (g *pandaSitGoal) Stop(t *tickView) {
	m := g.m
	if held := m.MainHand; !held.Empty() {
		t.events = append(t.events, MobEvent{Mob: m, Kind: MobDroppedItem, Stack: held, X: m.X, Y: m.Y, Z: m.Z})
		m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{})
		seconds := m.Rand.Intn(150) + 10
		if m.pandaVariant() == GeneLazy {
			seconds = m.Rand.Intn(50) + 10
		}
		m.panda.sitAfter = m.Age + seconds*20
	}
	m.pandaSetFlag(pandaFlagSit, false)
}

// pandaLieGoal is Panda.PandaLieOnBackGoal.
type pandaLieGoal struct{ m *Mob }

func (g *pandaLieGoal) Priority() int           { return pandaPriorityLie }
func (g *pandaLieGoal) Flags() GoalFlags        { return 0 }
func (g *pandaLieGoal) RequiresEveryTick() bool { return false }

func (g *pandaLieGoal) CanStart(*tickView) bool {
	m := g.m
	return m.panda.backAfter < m.Age && m.pandaVariant() == GeneLazy && m.pandaCanAct() &&
		m.Rand.Intn(reducedTickDelay(400)) == 1
}

func (g *pandaLieGoal) ShouldContinue(*tickView) bool {
	m := g.m
	if m.InWater() || m.pandaVariant() != GeneLazy && m.Rand.Intn(reducedTickDelay(600)) == 1 {
		return false
	}
	return m.Rand.Intn(reducedTickDelay(2000)) != 1
}

func (g *pandaLieGoal) Start(*tickView) {
	g.m.pandaSetFlag(pandaFlagOnBack, true)
	g.m.panda.backAfter = 0
}
func (g *pandaLieGoal) Tick(*tickView) {}
func (g *pandaLieGoal) Stop(*tickView) {
	g.m.pandaSetFlag(pandaFlagOnBack, false)
	g.m.panda.backAfter = g.m.Age + 200
}

// pandaGoalBase is the shared half of the one-shot goals that never continue.
type pandaGoalBase struct{ m *Mob }

func (pandaGoalBase) Flags() GoalFlags              { return 0 }
func (pandaGoalBase) RequiresEveryTick() bool       { return false }
func (pandaGoalBase) ShouldContinue(*tickView) bool { return false }
func (pandaGoalBase) Tick(*tickView)                {}
func (pandaGoalBase) Stop(*tickView)                {}

// pandaSneezeGoal is Panda.PandaSneezeGoal: a baby's sneeze.
type pandaSneezeGoal struct{ pandaGoalBase }

func (g *pandaSneezeGoal) Priority() int { return pandaPrioritySneeze }

func (g *pandaSneezeGoal) CanStart(*tickView) bool {
	m := g.m
	if !m.IsBaby() || !m.pandaCanAct() {
		return false
	}
	if m.panda.forceSneeze {
		m.panda.forceSneeze = false
		return true
	}
	if m.pandaVariant() == GeneWeak && m.Rand.Intn(reducedTickDelay(500)) == 1 {
		return true
	}
	return m.Rand.Intn(reducedTickDelay(6000)) == 1
}

func (g *pandaSneezeGoal) Start(*tickView) { g.m.pandaSetFlag(pandaFlagSneeze, true) }

// pandaRollGoal is Panda.PandaRollGoal: babies and playful pandas roll,
// at a ledge or at random.
type pandaRollGoal struct{ pandaGoalBase }

func (g *pandaRollGoal) Priority() int         { return pandaPriorityRoll }
func (g *pandaRollGoal) Flags() GoalFlags      { return GoalMove | GoalLook | GoalJump }
func (g *pandaRollGoal) Uninterruptible() bool { return true }

func (g *pandaRollGoal) CanStart(t *tickView) bool {
	m := g.m
	if !m.IsBaby() && m.pandaVariant() != GenePlayful || !m.OnGround || !m.pandaCanAct() {
		return false
	}
	angle := float64(float32(m.Yaw) * (math.Pi / 180))
	step := func(v float64) int32 {
		if math.Abs(v) > 0.5 {
			if v > 0 {
				return 1
			}
			return -1
		}
		return 0
	}
	xs, zs := step(-float64(float32(math.Sin(angle)))), step(float64(float32(math.Cos(angle))))
	switch registry.BlockOf(t.w.BlockAt(floorI(m.X)+xs, floorI(m.Y)-1, floorI(m.Z)+zs)) {
	case registry.BlockAir, registry.BlockCaveAir, registry.BlockVoidAir:
		return true
	}
	if m.pandaVariant() == GenePlayful && m.Rand.Intn(reducedTickDelay(60)) == 1 {
		return true
	}
	return m.Rand.Intn(reducedTickDelay(500)) == 1
}

func (g *pandaRollGoal) Start(*tickView) { g.m.pandaSetFlag(pandaFlagRoll, true) }
