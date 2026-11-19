package entity

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Dolphin.PlayWithItemsGoal
type dolphinPlayGoal struct {
	m        *Mob
	cooldown int
}

func (g *dolphinPlayGoal) Priority() int           { return 8 }
func (g *dolphinPlayGoal) Flags() GoalFlags        { return GoalMove }
func (g *dolphinPlayGoal) RequiresEveryTick() bool { return false }
func (g *dolphinPlayGoal) item(t *tickView) (FoxItem, bool) {
	m := g.m
	box := m.mobBox()
	for _, it := range m.DolphinItems {
		if it.X >= box.minX-8 && it.X <= box.maxX+8 && it.Y >= box.minY-8 && it.Y <= box.maxY+8 && it.Z >= box.minZ-8 && it.Z <= box.maxZ+8 {
			at := cellAt(it.X, it.Y, it.Z)
			if rules.IsWater(t.w.BlockAt(at.X, at.Y, at.Z)) {
				return it, true
			}
		}
	}
	return FoxItem{}, false
}
func (g *dolphinPlayGoal) CanStart(t *tickView) bool {
	if g.cooldown > g.m.Age {
		return false
	}
	_, ok := g.item(t)
	return ok || !g.m.MainHand.Empty()
}
func (g *dolphinPlayGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }
func (g *dolphinPlayGoal) Start(t *tickView) {
	if it, ok := g.item(t); ok {
		g.m.swimTo(t.w, it.X, it.Y, it.Z, float64(float32(1.2)))
		t.events = append(t.events, MobEvent{Mob: g.m, Kind: MobSounded, SoundID: registry.SoundEntityDolphinPlay, Volume: 1, Pitch: 1})
	}
	g.cooldown = 0
}
func (g *dolphinPlayGoal) Stop(t *tickView) {
	if !g.m.MainHand.Empty() {
		g.drop(t)
		g.cooldown = g.m.Age + g.m.Rand.Intn(100)
	}
}
func (g *dolphinPlayGoal) Tick(t *tickView) {
	if !g.m.MainHand.Empty() {
		g.drop(t)
	} else if it, ok := g.item(t); ok {
		g.m.swimTo(t.w, it.X, it.Y, it.Z, float64(float32(1.2)))
	}
}
func (g *dolphinPlayGoal) drop(t *tickView) {
	m := g.m
	yaw, pitch := float64(m.Yaw)*math.Pi/180, float64(m.Pitch)*math.Pi/180
	dir, power := float64(m.Rand.Float32())*math.Pi*2, float64(0.02*m.Rand.Float32())
	t.events = append(t.events, MobEvent{Mob: m, Kind: MobThrewItem, Stack: m.MainHand, Owned: true, X: m.X, Y: m.Y + m.eyeHeight() - float64(float32(0.3)), Z: m.Z,
		VX: float64(float32(0.3))*-math.Sin(yaw)*math.Cos(pitch) + math.Cos(dir)*power,
		VY: float64(float32(0.3)) * math.Sin(pitch) * 1.5,
		VZ: float64(float32(0.3))*math.Cos(yaw)*math.Cos(pitch) + math.Sin(dir)*power})
	m.SetEquipment(protocol.EquipMainHand, protocol.ItemStack{})
}
