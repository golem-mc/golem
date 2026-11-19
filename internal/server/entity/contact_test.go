package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// TestContactTableMatchesRules checks every state's contact bits against the
// predicates the contact steps read, so the table cannot drift from them.
func TestContactTableMatchesRules(t *testing.T) {
	for s := range uint16(registry.StateCount) {
		got := contactOf[registry.BlockOf(s)]
		_, campfire := rules.CampfireDamage(s)
		_, column := rules.BubbleColumnDrag(s)
		for _, c := range []struct {
			name string
			bit  contactSet
			is   bool
		}{
			{"fire", contactFire, campfire || rules.IsFire(s)},
			{"cactus", contactCactus, rules.IsCactus(s)},
			{"berry bush", contactBerryBush, rules.IsSweetBerryBush(s)},
			{"lava cauldron", contactLavaCauldron, rules.IsLavaCauldron(s)},
			{"powder snow", contactPowderSnow, rules.IsPowderSnow(s)},
			{"layered cauldron", contactLayeredCauldron, rules.IsLayeredCauldron(s)},
			{"wither rose", contactWitherRose, rules.IsWitherRose(s)},
			{"eyeblossom", contactEyeblossom, registry.BlockOf(s) == registry.BlockOpenEyeblossom},
			{"crop", contactCrop, rules.IsTrampledCrop(s)},
			{"bubble column", contactBubbleColumn, column},
		} {
			if c.is && got&c.bit == 0 {
				t.Fatalf("state %d: %s predicate holds but the table lacks its bit", s, c.name)
			}
		}
	}
}

// TestBaseTickContactEvents steps a mob through baseTick inside each contact
// block and checks the event it reports.
func TestBaseTickContactEvents(t *testing.T) {
	for _, c := range []struct {
		block string
		burn  bool
		want  MobEventKind
	}{
		{"minecraft:fire", false, MobBurned},
		{"minecraft:cactus", false, MobBurned},
		{"minecraft:lava_cauldron", false, MobBurned},
		{"minecraft:powder_snow", true, MobMeltedSnow},
		{"minecraft:water_cauldron", true, MobDousedCauldron},
		{"minecraft:wither_rose", false, MobEffectChanged},
		{"minecraft:bubble_column", false, MobBubbleSurface},
		{"minecraft:grass_block", false, 0},
	} {
		t.Run(c.block, func(t *testing.T) {
			w := newTestWorld()
			w.set(0, 64, 0, defaultState(t, c.block))
			m := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
			m.NoAI = true
			if c.burn {
				m.FireTicks = 100
			}
			events, _ := m.baseTick(w, new(contactScan), nil, nil)
			found := false
			for _, e := range events {
				found = found || e.Kind == c.want
			}
			if c.want != 0 && !found {
				t.Errorf("events %v lack kind %v", events, c.want)
			}
			if c.want == 0 && len(events) != 0 {
				t.Errorf("plain grass gave events %v", events)
			}
		})
	}
}
