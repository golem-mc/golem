package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestSulfurProfileFollowsTheAbsorbedBlock(t *testing.T) {
	t.Parallel()
	id := func(name string) protocol.ItemStack {
		item, ok := registry.ItemID(name)
		if !ok {
			t.Fatalf("no item %s", name)
		}
		return protocol.ItemStack{ItemID: item, Count: 1}
	}
	cases := []struct {
		item                         string
		resist, bounce, drag, kbVert float64
		buoyant                      bool
	}{
		{"minecraft:oak_planks", -2, 0.9, 0.01, 0.105, true},
		{"minecraft:stone", 0.4, 0.6, 0.05, 0.24, false},
		{"minecraft:magma_block", -1, 0.5, 0.1, 0.09, true},
		{"minecraft:honeycomb_block", -2, 0, 0.01, 0.09, false},
	}
	for _, c := range cases {
		m := &Mob{Type: TypeSulfurCube, Body: id(c.item)}
		p := m.sulfurProf()
		if float32(p.knockbackResist) != float32(c.resist) || float32(p.bounciness) != float32(c.bounce) || float32(p.drag) != float32(c.drag) ||
			p.kbVertical != float32(c.kbVert) || p.buoyant != c.buoyant {
			t.Errorf("%s: profile %+v", c.item, *p)
		}
	}
	if p := (&Mob{Type: TypeSulfurCube}).sulfurProf(); p.bounciness != 0 || p.friction != 1 || p.kbHorizontal != 0.33 {
		t.Errorf("an empty cube's profile = %+v", *p)
	}
}

func TestSulfurShovesWithAndWithoutAttributes(t *testing.T) {
	t.Parallel()
	planks, _ := registry.ItemID("minecraft:oak_planks")
	for _, installed := range []bool{false, true} {
		m := NewMob(TypeSulfurCube, 1, 1, 64, 0, 1)
		m.Body = protocol.ItemStack{ItemID: planks, Count: 1}
		if installed {
			m.SetAttributes(m.BuildAttributes(nbt.List{}))
		}
		if got := m.KnockbackResistance(); got != -2 {
			t.Fatalf("attributes=%v: resistance %g", installed, got)
		}
		m.OnGround = true
		p := Player{X: 0, Y: 64, Z: 0, KnownVX: 0.1}
		m.sulfurTouches([]Player{p}, nil)
		speed := 0.1 * 2 * float64(float32(0.3))
		if math.Abs(m.VX-3*speed) > 1e-8 || math.Abs(m.VY-3*float64(float32(0.3))*speed) > 1e-8 {
			t.Fatalf("attributes=%v: player push = (%g, %g)", installed, m.VX, m.VY)
		}
		m.VX, m.VY, m.VZ = 0, 0, 0
		src := HitSource{X: 0, Y: 64, Z: 0, Eye: 64 + m.height()/2, Yaw: -90}
		m.hitKnockback(&src, -1, 0, 1)
		if math.Abs(m.VX-float64(float32(0.4125)*3*0.4)) > 1e-6 || math.Abs(m.VY-float64(float32(0.105)*3)*1.2) > 1e-6 {
			t.Fatalf("attributes=%v: hit shove = (%g, %g, %g)", installed, m.VX, m.VY, m.VZ)
		}
	}
}
