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

func TestAShulkerBoxUsesSanitizedScale(t *testing.T) {
	for _, tc := range []struct{ scale, size float64 }{{0.5, 0.5}, {4, 3}} {
		m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		m.attrs.SetBase(registry.AttributeScale, tc.scale)
		box := m.shulkerBox()
		if box.maxX-box.minX != tc.size || box.maxY-box.minY != tc.size || box.maxZ-box.minZ != tc.size {
			t.Errorf("scale %v: box %+v, want size %v", tc.scale, box, tc.size)
		}
	}
}

func TestAScaledShulkerNeedsRoomForItsWholeShell(t *testing.T) {
	w := shulkerFloor(t)
	w.set(1, 64, 0, defaultState(t, "minecraft:stone"))
	m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	if !m.canStayAt(w, m.cell(), faceDown) {
		t.Fatal("unit shulker cannot attach beside the stone")
	}
	m.SetAttributes(m.BuildAttributes(nbt.List{}))
	m.attrs.SetBase(registry.AttributeScale, 2)
	if m.canStayAt(w, m.cell(), faceDown) {
		t.Error("scaled shulker attached with its shell inside the stone")
	}
}

func TestAScaledShulkerCollidesBeyondTheUnitShell(t *testing.T) {
	m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	m.SetAttributes(m.BuildAttributes(nbt.List{}))
	m.attrs.SetBase(registry.AttributeScale, 2)
	m.shulker.face, m.shulker.peekNow = faceNorth, 1
	mover := NewMob(TypeShulker, 2, 0.5, 65.5, 3.5, 4)
	w := shulkerWorld{World: shulkerFloor(t), self: mover, others: []*Mob{m}}
	box := AABB{0, 65.5, 3, 1, 66.5, 4}
	if got := w.BodyBoxes(box); len(got) != 1 || got[0] != m.shulkerBox() {
		t.Errorf("collision boxes %+v, want the scaled open shell", got)
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

// TestAShulkerBoxGrowsAwayFromTheFaceItHolds: a ceiling shulker opens down,
// a wall shulker sideways.
// vanilla: Shulker.getProgressAabb
func TestAShulkerBoxGrowsAwayFromTheFaceItHolds(t *testing.T) {
	w := shulkerFloor(t)
	m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	m.shulker.face = faceUp
	openShell(m, w, nil, 30)
	if _, minY, _, _, maxY, _ := m.BlastBox(); math.Abs(minY-63) > 1e-6 || maxY != 65 {
		t.Errorf("a ceiling shulker's box spans %v..%v, want 63..65", minY, maxY)
	}
	m = NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	m.shulker.face = faceNorth
	openShell(m, w, nil, 30)
	if _, _, minZ, _, _, maxZ := m.BlastBox(); minZ != 0 || math.Abs(maxZ-2) > 1e-6 {
		t.Errorf("a north-wall shulker's box spans z %v..%v, want 0..2", minZ, maxZ)
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

// TestAShutShellPushesNobody: closing moves the box back and shoves nothing.
// vanilla: Shulker.onPeekAmountChange
func TestAShutShellPushesNobody(t *testing.T) {
	w := shulkerFloor(t)
	m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	openShell(m, w, nil, 30)
	pig := NewMob(TypePig, 2, 0.5, 65, 0.5, 4)
	m.shulker.peek = 0
	tv := tickView{w: w, others: []*Mob{m, pig}}
	m.stepShulkerPeek(w, &tv)
	if pig.Y != 65 {
		t.Errorf("a closing shell moved the pig to %v", pig.Y)
	}
	for _, ev := range tv.events {
		if ev.Kind == MobShulkerShoved {
			t.Errorf("a closing shell told a shove %+v", ev)
		}
	}
}

// TestAShulkerTeleportsOnlyInsideTheWorldBorder: a cell outside the border
// is never taken.
// vanilla: Shulker.teleportSomewhere, WorldBorder.isWithinBounds
func TestAShulkerTeleportsOnlyInsideTheWorldBorder(t *testing.T) {
	w := shulkerFloor(t)
	landed := func(inside func(x, z int32) bool) int {
		w.inBorder = inside
		n := 0
		for seed := int64(1); seed <= 200; seed++ {
			m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, seed)
			if m.teleportShulker(w, &tickView{w: w}) {
				n++
			}
		}
		return n
	}
	if landed(nil) == 0 {
		t.Fatal("no teleport landed with the border open")
	}
	if n := landed(func(int32, int32) bool { return false }); n != 0 {
		t.Errorf("%d teleports landed outside the border", n)
	}
}

// TestAShulkerTellsItsTeleportFromTheCellItLeft: the event carries the old
// place, where the sound and the game event belong.
// vanilla: Shulker.teleportSomewhere
func TestAShulkerTellsItsTeleportFromTheCellItLeft(t *testing.T) {
	w := shulkerFloor(t)
	for seed := int64(1); seed <= 200; seed++ {
		m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, seed)
		tv := tickView{w: w}
		if !m.teleportShulker(w, &tv) {
			continue
		}
		var told []MobEvent
		for _, ev := range tv.events {
			if ev.Kind == MobShulkerTeleported {
				told = append(told, ev)
			}
		}
		if len(told) != 1 || told[0].X != 0.5 || told[0].Y != 64 || told[0].Z != 0.5 {
			t.Fatalf("teleport events %+v, want one from 0.5,64,0.5", told)
		}
		if m.X == 0.5 && m.Y == 64 && m.Z == 0.5 {
			t.Fatal("the shulker did not move")
		}
		return
	}
	t.Fatal("no teleport landed in 200 seeds")
}

// TestTheShellTellsEveryOpeningAndClosingEvenWhenSilent: the sound is the
// server's to mute, the game event is not.
// vanilla: Shulker.setRawPeekAmount
func TestTheShellTellsEveryOpeningAndClosingEvenWhenSilent(t *testing.T) {
	m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	m.Silent = true
	tv := tickView{w: shulkerFloor(t)}
	m.setPeek(shulkerOpenPeek, &tv)
	m.setPeek(0, &tv)
	if len(tv.events) != 2 || tv.events[0].Kind != MobShulkerShell || tv.events[0].Status != shulkerOpenPeek ||
		tv.events[1].Kind != MobShulkerShell || tv.events[1].Status != 0 {
		t.Errorf("shell events %+v, want an opening then a closing", tv.events)
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

// TestAShulkerCopyIsDyedLikeTheOriginal: the copy event carries the colour.
// vanilla: Shulker.hitByShulkerBullet
func TestAShulkerCopyIsDyedLikeTheOriginal(t *testing.T) {
	w := shulkerFloor(t)
	for seed := int64(1); seed <= 200; seed++ {
		m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, seed)
		m.SetShulkerColor(3)
		m.shulker.peek = shulkerOpenPeek
		m.shulker.splitFrom = m.mobBox()
		tv := tickView{w: w}
		m.copyShulker(w, &tv)
		for _, ev := range tv.events {
			if ev.Kind == MobShulkerCopied {
				if ev.Status != 3 {
					t.Fatalf("the copy event carries colour %d, want 3", ev.Status)
				}
				return
			}
		}
	}
	t.Fatal("no copy was made in 200 seeds")
}

// TestAShulkerBulletsBoxIsTheSizeOfItsType: a bullet is hit and blasted as
// 0.3125 wide and high, a snowball's box stays a quarter block.
// vanilla: EntityTypes.SHULKER_BULLET
func TestAShulkerBulletsBoxIsTheSizeOfItsType(t *testing.T) {
	b := NewThrown(1, ThrownShulkerBullet, 0, 64, 0, 0, 0, 0, 0, 1)
	minX, minY, _, maxX, maxY, _ := b.BlastBox()
	if maxX-minX != 0.3125 || maxY-minY != 0.3125 {
		t.Errorf("the bullet's box is %v by %v, want 0.3125 square", maxX-minX, maxY-minY)
	}
	s := NewThrown(2, ThrownSnowball, 0, 64, 0, 0, 0, 0, 0, 1)
	minX, minY, _, maxX, maxY, _ = s.BlastBox()
	if maxX-minX != 0.25 || maxY-minY != 0.25 {
		t.Errorf("the snowball's box is %v by %v, want 0.25 square", maxX-minX, maxY-minY)
	}
}

// TestAShulkerKeepsItsFaceAndPeekInItsFile: AttachFace and Peek round-trip, a
// faces use the legacy direction codec, and Peek grants no armour on load.
func TestAShulkerKeepsItsFaceAndPeekInItsFile(t *testing.T) {
	m := &Mob{Type: TypeShulker}
	m.shulkerFields(nbt.Compound{"AttachFace": nbt.Byte(4), "Peek": nbt.Byte(30)}, true)
	if m.ShulkerFace() != 4 || m.ShulkerPeek() != 30 || m.shulker.covered {
		t.Errorf("loaded face %d peek %d covered %v, want 4, 30, false", m.ShulkerFace(), m.ShulkerPeek(), m.shulker.covered)
	}
	saved := nbt.Compound{}
	m.shulkerFields(saved, false)
	if f, _ := nbt.ByteValue(saved["AttachFace"]); f != 4 {
		t.Errorf("saved AttachFace %d, want 4", f)
	}
	if p, _ := nbt.ByteValue(saved["Peek"]); p != 30 {
		t.Errorf("saved Peek %d, want 30", p)
	}
	m.shulkerFields(nbt.Compound{"AttachFace": nbt.Byte(9)}, true)
	if m.ShulkerFace() != 3 || m.ShulkerPeek() != 0 || m.shulker.covered {
		t.Errorf("bad face loaded %d peek %d covered %v, want 3, 0, false", m.ShulkerFace(), m.ShulkerPeek(), m.shulker.covered)
	}
	for _, tc := range []struct {
		value nbt.Tag
		want  uint8
	}{
		{nbt.Byte(-9), 3}, {nbt.Double(-9.9), 3}, {nbt.Double(9.9), 3},
		{nbt.Int(255), 1}, {nbt.Int(256), 0}, {nbt.String("south"), 0}, {nil, 0},
	} {
		m.shulkerFields(nbt.Compound{"AttachFace": tc.value, "Peek": nbt.Double(-1.5), "Color": nbt.Double(-2.5)}, true)
		if m.ShulkerFace() != tc.want || m.ShulkerPeek() != 254 || m.ShulkerColor() != 253 {
			t.Errorf("load %v: face %d peek %d color %d, want %d/254/253", tc.value, m.ShulkerFace(), m.ShulkerPeek(), m.ShulkerColor(), tc.want)
		}
	}

}

// TestAGrowingShellTellsItsSlab: the shove the server applies to the other
// bodies is the swept slab with the delta the mobs were moved by.
// vanilla: Shulker.onPeekAmountChange
func TestAGrowingShellTellsItsSlab(t *testing.T) {
	w := shulkerFloor(t)
	m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	tv := tickView{w: w, others: []*Mob{m}}
	m.shulker.peek = 100
	m.stepShulkerPeek(w, &tv)
	for _, ev := range tv.events {
		if ev.Kind == MobShulkerShoved {
			if ev.VY <= 0 || ev.VX != 0 || ev.Slab[4] <= ev.Slab[1] {
				t.Errorf("shove %+v, want a rise over a slab with height", ev)
			}
			return
		}
	}
	t.Error("a growing shell told no shove")
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

// vanilla: Shulker.readAdditionalSaveData, LivingEntity.readAdditionalSaveData
func TestShulkerLoadsCoveredArmorIndependentlyOfPeek(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		snbt string
		want float64
	}{
		{"fresh closed", `{Peek:0b}`, 0},
		{"fresh open", `{Peek:100b}`, 0},
		{"covered closed", `{Peek:0b,attributes:[{id:"minecraft:armor",base:0d,modifiers:[{id:"minecraft:covered",amount:20d,operation:"add_value"}]}]}`, 20},
		{"covered open", `{Peek:100b,attributes:[{id:"armor",base:0d,modifiers:[{id:"covered",amount:20d,operation:"add_value"}]}]}`, 20},
		{"zero modifier", `{Peek:0b,attributes:[{id:"armor",base:0d,modifiers:[{id:"covered",amount:0d,operation:"add_value"}]}]}`, 20},
		{"other attribute", `{Peek:0b,attributes:[{id:"attack_damage",base:2d,modifiers:[{id:"minecraft:covered",amount:20d,operation:"add_value"}]}]}`, 0},
		{"other modifier", `{Peek:0b,attributes:[{id:"armor",base:0d,modifiers:[{id:"test:other",amount:20d,operation:"add_value"}]}]}`, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tag, err := nbt.ParseSNBT(tc.snbt)
			if err != nil {
				t.Fatal(err)
			}
			m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
			m.LoadFrom(tag, nil, nil)
			if armor := m.baseArmor(); armor != tc.want {
				t.Errorf("loaded base armor = %v, want %v", armor, tc.want)
			}
		})
	}
}

// vanilla: Shulker.setRawPeekAmount
func TestShulkerPairingUsesCurrentShellArmor(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeShulker, 1, 0.5, 64, 0.5, 3)
	m.setPeek(0, &tickView{})
	saved := nbt.Compound{}
	m.SaveTo(saved, nil)
	back := NewMob(TypeShulker, 2, 0.5, 64, 0.5, 4)
	back.LoadFrom(saved, nil, nil)
	back.setPeek(shulkerOpenPeek, &tickView{})
	paired := back.BuildAttributes(saved["attributes"].(nbt.List))
	if got := paired.Value(registry.AttributeArmor); got != 0 {
		t.Errorf("an opened shell paired with saved covered armor = %v, want 0", got)
	}
	back.setPeek(0, &tickView{})
	paired = back.BuildAttributes(nbt.List{})
	if got := paired.Value(registry.AttributeArmor); got != 20 {
		t.Errorf("a shut shell paired with armor = %v, want 20", got)
	}
}
