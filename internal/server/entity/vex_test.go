package entity

import "testing"

// vexView builds the view a vex's goals run against.
func vexView(w World, players []Player, others []*Mob) *tickView {
	return &tickView{w: w, players: players, others: others}
}

// vexCharges counts the CanStart offers of a fresh charge goal out of n.
func vexCharges(m *Mob, v *tickView, n int) int {
	g := &vexChargeGoal{m: m}
	started := 0
	for range n {
		if g.CanStart(v) {
			started++
		}
	}
	return started
}

// TestVexChargeStartsOneTickInFourBeyondTwoBlocks: the charge rolls nextInt(reducedTickDelay(7)),
// one in four, per offer and refuses a target within 2 blocks.
// vanilla: Vex.VexChargeAttackGoal.canUse
func TestVexChargeStartsOneTickInFourBeyondTwoBlocks(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeVex, 1, 0.5, 64, 0.5, 5)
	m.TargetID = 2
	far := vexView(w, []Player{{X: 5.5, Y: 64, Z: 0.5, EntityID: 2, Huntable: true}}, nil)
	if n := vexCharges(m, far, 7000); n < 1600 || n > 1900 {
		t.Errorf("charges started = %d of 7000, want about 1750 (1 in 4)", n)
	}
	near := vexView(w, []Player{{X: 1.5, Y: 64, Z: 0.5, EntityID: 2, Huntable: true}}, nil)
	if n := vexCharges(m, near, 2000); n != 0 {
		t.Errorf("charges started = %d with the target 1 block away, want 0", n)
	}
}

// TestVexWandersAroundItsBoundBlock: a leg ends at the centre of an empty
// block within 7 across and 5 up or down of the bound block, not of the vex.
// vanilla: Vex.VexRandomMoveGoal.tick
func TestVexWandersAroundItsBoundBlock(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeVex, 1, 100.5, 64, 100.5, 5)
	m.SetVexBound(0, 70, 0)
	g := &vexWanderGoal{m: m}
	v := vexView(w, nil, nil)
	for range 200 {
		m.vex.wanted = false
		g.Tick(v)
		if !m.vex.wanted {
			t.Fatal("no leg set in an empty world")
		}
		x, y, z := m.vex.wx-0.5, m.vex.wy-0.5, m.vex.wz-0.5
		if x < -7 || x > 7 || y < 65 || y > 75 || z < -7 || z > 7 || x != float64(int(x)) {
			t.Fatalf("leg to (%v, %v, %v) leaves the box around the bound block", m.vex.wx, m.vex.wy, m.vex.wz)
		}
		if m.vex.pace != vexWanderPace {
			t.Fatalf("leg pace = %v, want %v", m.vex.pace, vexWanderPace)
		}
	}
	solid := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-8); x <= 8; x++ {
		for y := int32(64); y <= 76; y++ {
			for z := int32(-8); z <= 8; z++ {
				solid.set(x, y, z, stone)
			}
		}
	}
	m.vex.wanted = false
	g.Tick(vexView(solid, nil, nil))
	if m.vex.wanted {
		t.Error("a leg was set into solid blocks")
	}
}

// TestVexDragsThroughFluids: noPhysics skips only the collision, so water
// scales the velocity by 0.8 and lava by 0.5 where air leaves 0.91.
// vanilla: LivingEntity.travelInWater, LivingEntity.travelInLava
func TestVexDragsThroughFluids(t *testing.T) {
	for _, tc := range []struct {
		name        string
		water, lava float64
		want        float64
	}{
		{"water", 1, 0, flyWaterDrag},
		{"lava", 0, 1, flyLavaDrag},
		{"air", 0, 0, airDrag},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := NewMob(TypeVex, 1, 0.5, 64, 0.5, 5)
			m.VX = 1
			m.waterFluid.height, m.lavaFluid.height = tc.water, tc.lava
			m.vexMoveStep(vexView(newTestWorld(), nil, nil))
			if !closeEnough(m.VX, tc.want) {
				t.Errorf("velocity after the step = %v, want %v", m.VX, tc.want)
			}
		})
	}
}

// TestVexFloatsInDeepWater: the float goal is wired.
// vanilla: Vex.registerGoals
func TestVexFloatsInDeepWater(t *testing.T) {
	m := NewMob(TypeVex, 1, 0.5, 64, 0.5, 5)
	for _, g := range vexGoals(m) {
		if _, ok := g.(*floatGoal); ok {
			return
		}
	}
	t.Error("the vex has no float goal")
}

// TestVexIsNeitherPushingNorPushed: Entity.push returns while the vex's tick
// holds noPhysics.
// vanilla: Entity.push
func TestVexIsNeitherPushingNorPushed(t *testing.T) {
	w := newTestWorld()
	vex := NewMob(TypeVex, 1, 0.5, 64, 0.5, 5)
	zombie := NewMob(TypeZombie, 2, 0.6, 64, 0.5, 5)
	vex.pushStep(w, nil, []*Mob{zombie}, nil)
	if vex.VX != 0 || zombie.VX != 0 {
		t.Errorf("velocities after the vex's push = %v and %v, want 0 and 0", vex.VX, zombie.VX)
	}
	zombie.pushStep(w, nil, []*Mob{vex}, nil)
	if zombie.VX == 0 {
		t.Error("the zombie's own push moved nothing, so the test overlap is wrong")
	}
}

// TestVexWithoutOwnerCopiesNothing: an ownerless vex does not look up entity
// ID 0 in the herd.
// vanilla: Vex.VexCopyOwnerTargetGoal.canUse
func TestVexWithoutOwnerCopiesNothing(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeVex, 1, 0.5, 64, 0.5, 5)
	stray := NewMob(TypeZombie, 0, 3.5, 64, 0.5, 5)
	stray.TargetID = 2
	v := vexView(w, []Player{{X: 3.5, Y: 64, Z: 0.5, EntityID: 2, Huntable: true}}, []*Mob{stray})
	if (&vexCopyOwnerTargetGoal{m: m}).CanStart(v) {
		t.Error("an ownerless vex took the target of entity 0")
	}
}
