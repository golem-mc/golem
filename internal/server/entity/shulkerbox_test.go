package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// openShell asks for a fully open shell and steps the box n ticks.
func openShell(m *Mob, w testWorld, others []*Mob, n int) {
	m.shulker.peek = shulkerOpenPeek
	tv := tickView{w: w, others: others}
	for range n {
		m.stepShulkerPeek(w, &tv)
	}
}

func TestAScaledShulkerBoxFollowsItsAttachFace(t *testing.T) {
	for _, tc := range []struct {
		face uint8
		want AABB
	}{
		{faceDown, AABB{-0.5, 64, -0.5, 1.5, 68, 1.5}},
		{faceUp, AABB{-0.5, 62, -0.5, 1.5, 66, 1.5}},
		{faceNorth, AABB{-0.5, 64, -0.5, 1.5, 66, 3.5}},
		{faceSouth, AABB{-0.5, 64, -2.5, 1.5, 66, 1.5}},
		{faceWest, AABB{-0.5, 64, -0.5, 3.5, 66, 1.5}},
		{faceEast, AABB{-2.5, 64, -0.5, 1.5, 66, 1.5}},
	} {
		m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		m.attrs.SetBase(registry.AttributeScale, 2)
		m.shulker.face, m.shulker.peekNow = tc.face, 1
		if got := m.shulkerBox(); got != tc.want {
			t.Errorf("face %d: box %+v, want %+v", tc.face, got, tc.want)
		}
	}
}

// TestAShulkerBoxGrowsTowardItsOpeningAsTheShellOpens: the box reaches out
// of the face the shulker holds by the physical peek of the opening, which
// climbs 0.05 a tick, and is the plain unit box while shut.
// vanilla: Shulker.makeBoundingBox, getPhysicalPeek, updatePeekAmount
func TestAShulkerBoxGrowsTowardItsOpeningAsTheShellOpens(t *testing.T) {
	w := shulkerFloor(t)
	m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	if _, _, _, _, maxY, _ := m.BlastBox(); maxY != 65 {
		t.Fatalf("a shut shulker's box tops out at %v, want 65", maxY)
	}
	openShell(m, w, nil, 1)
	_, _, _, _, maxY, _ := m.BlastBox()
	if want := 65 + float64(physicalPeek(0.05)); maxY != want || maxY <= 65 {
		t.Errorf("after one tick the box tops out at %v, want %v", maxY, want)
	}
	openShell(m, w, nil, 30)
	minX, minY, minZ, maxX, maxY, maxZ := m.BlastBox()
	if math.Abs(maxY-66) > 1e-6 || minY != 64 || maxX-minX != 1 || maxZ-minZ != 1 {
		t.Errorf("the open box is %v..%v high and %v by %v wide, want 64..66 and 1 by 1", minY, maxY, maxX-minX, maxZ-minZ)
	}
}

// TestAnOpeningShellPushesTheMobInItsWay: a mob standing in the cell the
// shell opens into is lifted, one beside it is left alone.
// vanilla: Shulker.onPeekAmountChange
func TestAnOpeningShellPushesTheMobInItsWay(t *testing.T) {
	w := shulkerFloor(t)
	m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	above := NewMob(TypePig, 2, 0.5, 65, 0.5, 4)
	beside := NewMob(TypePig, 3, 3.5, 65, 0.5, 5)
	openShell(m, w, []*Mob{m, above, beside}, 10)
	if above.Y <= 65 {
		t.Errorf("the pig over the shell stands at %v, want it lifted", above.Y)
	}
	if beside.Y != 65 || beside.X != 3.5 {
		t.Errorf("the pig beside the shell moved to %v,%v", beside.X, beside.Y)
	}
}

// TestAShellShoveReadsShulkerHullsInHerdOrder: a pig shoved east toward a
// low shell, with a taller one just past it, steps onto the low one. The
// taller shell's grid cell comes first; read in that order, its top would
// stand as the obstacle and refuse the step.
// vanilla: Shulker.onPeekAmountChange, Entity.collide
func TestAShellShoveReadsShulkerHullsInHerdOrder(t *testing.T) {
	w := newTestWorld()
	opener := NewMob(TypeShulker, 1, 1.5, 63, 0.5, 3)
	opener.shulker.face, opener.shulker.peekNow = faceWest, 0.4
	// Both shells open west: the low one to X 2.5 (top 64), the tall one to
	// about X 2.55 (top 65). The pig straddles both on Z, 0.01 short of the
	// low one; the shove carries it 0.076.
	low := NewMob(TypeShulker, 2, 3.5, 63, 0.5, 4)
	low.shulker.face, low.shulker.peekNow = faceEast, 0.5
	tall := NewMob(TypeShulker, 3, 3.5, 64, -0.5, 5)
	tall.shulker.face, tall.shulker.peekNow = faceEast, 0.468
	pig := NewMob(TypePig, 4, 2.04, 63.6, 0, 6)
	pig.OnGround = true
	herd := []*Mob{opener, low, tall, pig}
	grid := takePushGrid(len(herd), nil)
	defer grid.release()
	grid.build(herd)
	if cellOf(tall.X, tall.Z) == cellOf(low.X, low.Z) {
		t.Fatal("the shells share a grid cell; the fixture no longer orders them apart")
	}
	for _, m := range herd {
		m.push = grid
	}
	opener.shulker.peek = shulkerOpenPeek
	tv := tickView{w: w, others: herd}
	opener.stepShulkerPeek(opener.shulkerWorld(&tv), &tv)
	if math.Abs(pig.Y-64) > 1e-9 {
		t.Errorf("the shoved pig stands at Y %v, want 64 on the low shell", pig.Y)
	}
}

// TestAShulkerKeepsItsColourInItsFile: Color is the dye id, 16 for none, and
// anything past 15 reads as undyed.
// vanilla: Shulker.readAdditionalSaveData, addAdditionalSaveData
func TestAShulkerKeepsItsColourInItsFile(t *testing.T) {
	m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	saved := nbt.Compound{}
	m.shulkerFields(saved, false)
	if got, _ := nbt.ByteValue(saved["Color"]); got != 16 {
		t.Errorf("an undyed shulker saved Color %d, want 16", got)
	}
	m.shulkerFields(nbt.Compound{"Color": nbt.Byte(3)}, true)
	if m.ShulkerColor() != 3 {
		t.Errorf("Color 3 loaded as %d, want 3", m.ShulkerColor())
	}
	saved = nbt.Compound{}
	m.shulkerFields(saved, false)
	if got, _ := nbt.ByteValue(saved["Color"]); got != 3 {
		t.Errorf("a dyed shulker saved Color %d, want 3", got)
	}
	m.shulkerFields(nbt.Compound{}, true)
	if m.ShulkerColor() != 16 {
		t.Errorf("a missing Color loaded as %d, want 16", m.ShulkerColor())
	}
	// A byte past 15 is kept in the file and on the wire but dyes no copy.
	m.shulkerFields(nbt.Compound{"Color": nbt.Byte(20)}, true)
	saved = nbt.Compound{}
	m.shulkerFields(saved, false)
	if got, _ := nbt.ByteValue(saved["Color"]); got != 20 || m.ShulkerColor() != 20 || m.dyeColor() != 16 {
		t.Errorf("Color 20 kept as %d (wire %d, copy %d), want 20, 20, 16", got, m.ShulkerColor(), m.dyeColor())
	}
	// A negative byte reads as the first colour, white.
	m.shulkerFields(nbt.Compound{"Color": nbt.Byte(-3)}, true)
	if m.ShulkerColor() != 253 || m.dyeColor() != 0 {
		t.Errorf("Color -3 kept as wire %d, copy %d, want 253, 0", m.ShulkerColor(), m.dyeColor())
	}
}

// vanilla: Shulker.setRawPeekAmount, LivingEntity.addAdditionalSaveData
func TestShulkerCoveredArmorSurvivesSaveAndReload(t *testing.T) {
	t.Parallel()
	for _, mapped := range []bool{false, true} {
		m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
		if mapped {
			m.SetAttributes(m.BuildAttributes(nbt.List{}))
			m.attrs.SetBase(registry.AttributeArmor, 5)
			m.attrs.AddModifier(registry.AttributeArmor, player.Modifier{ID: "test:kept", Amount: 2, Persistent: true})
		}
		tv := tickView{}
		m.setPeek(shulkerOpenPeek, &tv)
		m.setPeek(0, &tv)
		for _, covered := range []bool{true, false} {
			if !covered {
				m.setPeek(shulkerOpenPeek, &tv)
			}
			saved := nbt.Compound{}
			if mapped {
				m.SaveWith(saved, nil, nil, func() {
					if list := m.attrs.SaveInstances(nil, true); list != nil {
						saved.Set("attributes", *list)
					}
				}, nil, nil)
			} else {
				m.SaveTo(saved, nil)
				if m.Attributes() != nil {
					t.Fatal("native shell state installed an attribute map")
				}
			}
			back := NewMob(TypeShulker, 2, 0.5, 64, 0.5, 4)
			back.LoadFrom(saved, nil, nil)
			want := 0.0
			if covered {
				want = 20
			}
			if armor := back.baseArmor(); armor != want {
				t.Errorf("mapped %v, covered %v: reloaded base armor = %v, want %v", mapped, covered, armor, want)
			}
			if !mapped && back.Attributes() != nil {
				t.Error("native saved shell state installed an attribute map on reload")
			}
			// Peek alone could otherwise hide a missing saved modifier.
			attrs := back.BuildAttributes(saved["attributes"].(nbt.List))
			if mapped {
				want += 7 // command base and the unrelated permanent modifier
			}
			if armor := attrs.Value(registry.AttributeArmor); armor != want {
				t.Errorf("mapped %v, covered %v: saved armor attribute = %v, want %v", mapped, covered, armor, want)
			}
		}
	}
}
