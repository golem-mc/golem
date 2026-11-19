package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/registry"
)

func TestMobMetadataRevisionUsesVisiblePredicates(t *testing.T) {
	m := NewMob(TypePiglin, 7, .5, 64, .5, 11)
	for _, test := range []struct {
		name                  string
		set                   func(int)
		first, next, boundary int
		group                 MobMetadataGroup
	}{
		{"baby", m.SetMetadataBabyAge, -10, -9, 0, MobMetadataBaby},
		{"fire", m.SetMetadataFireTicks, 10, 9, 0, MobMetadataParticles},
		{"allay", func(n int) { m.SetMetadataAllayCooldown(int64(n)) }, 10, 9, 0, MobMetadataAllayDancing},
		{"dance", m.SetMetadataBarterDance, 10, 9, 0, MobMetadataDancing},
		{"standing", m.SetMetadataHorseStanding, 10, 9, 0, MobMetadataChest},
		{"mouth", m.SetMetadataHorseMouthTicks, 10, 9, 0, MobMetadataChest},
		{"still", m.SetMetadataStillTimeout, 10, 9, 0, MobMetadataStaysStill},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.set(test.first)
			m.ClearMetadataDirty()
			before := m.MetadataRevision()
			test.set(test.next)
			if m.MetadataRevision() != before || m.MetadataDirty() != (MobMetadataMask{}) {
				t.Fatal("countdown dirtied an unchanged predicate")
			}
			test.set(test.boundary)
			if m.MetadataRevision() != before+1 || !m.MetadataDirty().Has(test.group) {
				t.Fatal("predicate boundary did not invalidate its group")
			}
		})
	}
	m.SetMetadataHealth(10)
	m.ClearMetadataDirty()
	before := m.MetadataRevision()
	m.SetMetadataHealth(10 + 1e-10)
	if m.Health == 10 || m.MetadataRevision() != before {
		t.Fatal("health setter must store double precision but compare the float32 projection")
	}
	for _, set := range []func(int){m.SetMetadataAirSupply, m.SetMetadataTicksFrozen, m.SetMetadataDolphinMoistness, m.SetMetadataDarkTicks} {
		set(10)
		m.ClearMetadataDirty()
		before := m.MetadataRevision()
		set(9)
		if m.MetadataRevision() != before+1 {
			t.Fatal("visible integer countdown suppressed")
		}
	}
}

func TestMobMetadataSharedSourcesUseIndependentKeys(t *testing.T) {
	m := NewMob(TypeFrog, 7, .5, 64, .5, 11)
	m.SetMetadataFrogTarget(17)
	m.SetMetadataFrogPhase(frogTongueEating)
	m.ClearMetadataDirty()
	before := m.MetadataRevision()
	m.SetMetadataFrogPhase(frogTongueCatching)
	if m.MetadataRevision() != before {
		t.Fatal("phase changed without a projected change")
	}
	m.SetMetadataFrogPhase(frogTongueMoving)
	if !m.MetadataDirty().Has(MobMetadataPose) || m.MetadataDirty().Has(MobMetadataFrogTongue) {
		t.Fatal("tongue pose key invalidated the wrong groups")
	}
	m.ClearMetadataDirty()
	m.SetMetadataFrogPhase(frogTongueIdle)
	if m.MetadataDirty().Has(MobMetadataPose) || !m.MetadataDirty().Has(MobMetadataFrogTongue) {
		t.Fatal("tongue target branch did not invalidate independently")
	}
	m.SetMetadataCamelLastPose(1)
	m.ClearMetadataDirty()
	m.SetMetadataCamelLastPose(2)
	if !m.MetadataDirty().Has(MobMetadataCamelDash) || m.MetadataDirty().Has(MobMetadataPose) {
		t.Fatal("camel timestamp and sitting predicate were conflated")
	}
	cow := NewMob(TypeCow, 8, .5, 64, .5, 11)
	before = cow.MetadataRevision()
	cow.SetMetadataGameTime(100)
	cow.SetMetadataAngerTicks(20)
	if cow.MetadataRevision() != before {
		t.Fatal("anger sources invalidated a kind outside the capture guard")
	}
}

func TestMobMetadataReconcilesEmbeddedEffectRevision(t *testing.T) {
	m := NewMob(TypeCow, 7, .5, 64, .5, 11)
	before := m.MetadataRevision()
	// Use the embedded adapter, bypassing Mob.AddEffect, as server adapters do.
	m.LivingEffects.AddEffect(effect.New(registry.MobEffectSpeed, 2, 0, false, true, true))
	if m.MetadataRevision() != before+1 || !m.MetadataDirty().Has(MobMetadataParticles) {
		t.Fatal("embedded effect mutation was not reconciled")
	}
	m.ClearMetadataDirty()
	before = m.MetadataRevision()
	m.effectsStep(nil)
	if m.MetadataRevision() != before || m.MetadataDirty() != (MobMetadataMask{}) {
		t.Fatal("duration-only tick invalidated effects")
	}
	m.effectsStep(nil)
	if m.MetadataRevision() != before+1 || !m.MetadataDirty().Has(MobMetadataParticles) {
		t.Fatal("expiry did not invalidate effects")
	}
	m.ClearMetadataDirty()
	before = m.MetadataRevision()
	if m.MetadataRevision() != before || m.MetadataDirty() != (MobMetadataMask{}) {
		t.Fatal("effect revision was consumed more than once")
	}
	m.RestoreEffects(effect.Set{})
	if m.MetadataRevision() != before+1 {
		t.Fatal("effect restore did not reconcile")
	}
	m.AddEffect(effect.New(registry.MobEffectSpeed, 10, 0, false, true, true))
	m.AddEffect(effect.New(registry.MobEffectSpeed, 1, 1, false, true, true))
	before = m.MetadataRevision()
	m.ClearMetadataDirty()
	m.effectsStep(nil)
	if m.MetadataRevision() != before+1 || !m.MetadataDirty().Has(MobMetadataParticles) {
		t.Fatal("hidden effect promotion did not invalidate effects")
	}
	m.ClearMetadataDirty()
	before = m.MetadataRevision()
	m.LivingEffects.RemoveEffect(registry.MobEffectSpeed)
	if m.MetadataRevision() != before+1 || !m.MetadataDirty().Has(MobMetadataParticles) {
		t.Fatal("embedded effect removal did not invalidate effects")
	}
}
