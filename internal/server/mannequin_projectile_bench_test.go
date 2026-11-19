package server

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkMannequinProjectiles includes the snapshot, selection and deferred
// application in each full tick. Each operation resets the recipients' effects
// and attributes so repeated applications still build metadata and attribute packets.
func BenchmarkMannequinProjectiles(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() { slog.SetDefault(logger) })
	for _, kind := range []string{"selection", "arrow", "splash", "cloud"} {
		for _, recipients := range []int{1, 64} {
			b.Run(fmt.Sprintf("%s/recipients=%d", kind, recipients), func(b *testing.B) {
				benchMannequinProjectiles(b, kind, recipients)
			})
		}
	}
}

func benchMannequinProjectiles(b *testing.B, kind string, recipients int) {
	s, w := newTrackerBenchServer(b)
	y := float64(world.GroundLevel)
	for i := range recipients {
		// All recipients fit inside one splash/cloud; arrow lanes do not overlap.
		x, z := float64(i%8)*0.25, float64(i/8)*0.25
		if kind == "arrow" || kind == "selection" {
			x, z = trackerBenchSpot(i)
		}
		m := entity.NewMannequin(s.nextEntity(), x, y, z)
		m.Immovable = true
		s.registerMannequin(&mannequinEntity{mannequin: m, w: w})
	}
	contents := protocol.PotionContents{Potion: protocol.NoPotion, CustomEffects: []protocol.PotionEffectSpec{
		{ID: registry.MustMobEffect("minecraft:health_boost"), Duration: 200},
		{ID: registry.MustMobEffect("minecraft:instant_health"), Duration: 1},
	}}
	var flights []*arrowEntity
	var starts []entity.Arrow
	var thrown *thrownEntity
	var thrownStart entity.Thrown
	switch kind {
	case "selection", "arrow":
		stack := protocol.ItemStack{ItemID: int32(registry.ItemTippedArrow), Count: 1}
		stack.SetPotionContents(contents)
		for _, ent := range s.mannequins {
			m := ent.mannequin
			it := newArrowEntity(s, w, m.X, y+1, m.Z-1.5, 0, 0, 2, 0, 0,
				false, entity.ArrowBaseDamage, entity.PickupDisallowed, 0, uuid.Nil, stack, 0)
			it.arrow.Effect = pickupEffect(stack)
			it.arrow.NoGravity = true
			if kind == "selection" {
				it.arrow.Y, it.arrow.VZ, it.arrow.Life = y+10, 0, -1<<30
			}
			flights = append(flights, it)
			starts = append(starts, *it.arrow)
			s.registerArrow(it)
		}
	case "splash":
		stack := protocol.ItemStack{ItemID: int32(registry.ItemSplashPotion), Count: 1}
		stack.SetPotionContents(contents)
		s.standThrown(entity.ThrownSplashPotion, w, 0.875, y+1, 0.875, 0, -1, 0, 0, 0, 0, uuid.Nil,
			1, entity.NewRolls(1), stack)
		thrown = s.thrown[0]
		thrownStart = *thrown.throwable
	case "cloud":
		s.spawnCloud(w, 0.875, y, 0.875, rules.PotionInstances(contents), 0, contents, uuid.Nil)
		it := s.clouds[0]
		it.fresh = false
		it.cloud.Duration, it.cloud.WaitTime, it.cloud.Reapplication, it.cloud.RadiusOnUse = -1, 0, 1, 0
		it.cloud.Restore(0, 3, 0)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, ent := range s.mannequins {
			m := ent.mannequin
			m.ClearEffects()
			rules.UpdateEffectAttributes(m.Attributes, m.AllEffects())
			m.Attributes.TakeDirty()
			m.Health, m.InvulnerableTime = 10, 0
		}
		switch kind {
		case "selection", "arrow":
			s.arrows = s.arrows[:0]
			for i, it := range flights {
				*it.arrow = starts[i]
				s.arrows = append(s.arrows, it)
			}
			s.tickArrows()
		case "splash":
			*thrown.throwable = thrownStart
			s.thrown = append(s.thrown[:0], thrown)
			s.tickThrowables()
		case "cloud":
			// Force every measured tick to be an application tick, with expired victims.
			s.clouds[0].cloud.TickCount += entity.CloudApplicationFlip - 1
			s.tickClouds()
		}
	}
	b.StopTimer()
	if kind != "selection" {
		var affected int
		for _, ent := range s.mannequins {
			if ent.mannequin.HasEffect(registry.MustMobEffect("minecraft:health_boost")) {
				affected++
			}
		}
		b.ReportMetric(float64(affected), "recipients/op")
	}
}
