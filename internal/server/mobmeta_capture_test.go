package server

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

func TestMobMetadataPartialCaptureMatchesFullProjection(t *testing.T) {
	// Seed composite flags/pose so a partial capture must replace, rather than accumulate.
	seed := mobMetaDefaults
	seed.baseFlags, seed.mobFlags = 0xff, 0xff
	seed.pose = protocol.PoseSleeping
	seed.sleepingPos = sleepSpot{true, 1, 2, 3}
	seed.particles = []effect.Particle{{Type: "minecraft:entity_effect", Color: 7}}
	typ := reflect.TypeFor[mobMetadataState]()
	type column struct {
		name  string
		index []int
	}
	var columns []column
	for _, field := range reflect.VisibleFields(typ) {
		if field.Name != "mobMetadataFields" && field.Name != "particles" {
			columns = append(columns, column{field.Name, field.Index})
		}
	}
	for kind := range entity.MobType(registry.EntityTypeCount) {
		if !entity.KnownMobKind(kind) {
			continue
		}
		m := entity.NewMob(kind, 7, .5, 64, .5, 11)
		full := mobMetadataStateOf(m)
		groupIndex := 0
		for _, group := range mobMetadataSchema {
			if group.fields == "" {
				continue
			}
			fields := strings.Fields(group.fields)
			var mask mobMetadataMask
			mask[groupIndex/64] = 1 << uint(groupIndex%64)
			got := seed
			captureMobMetadata(m, &got, mask)
			for _, field := range columns {
				want := reflect.ValueOf(seed).FieldByIndex(field.index)
				if slices.Contains(fields, field.name) {
					want = reflect.ValueOf(full).FieldByIndex(field.index)
				}
				if !reflect.ValueOf(got).FieldByIndex(field.index).Equal(want) {
					t.Fatalf("%v group %s: incorrect %s", kind, group.fields, field.name)
				}
			}
			particles := seed.particles
			if slices.Contains(fields, "particles") {
				particles = full.particles
			}
			if !slices.Equal(got.particles, particles) {
				t.Fatalf("%v group %s: incorrect particles", kind, group.fields)
			}
			groupIndex++
		}
	}
}

func TestMobMetadataPartialCaptureKeepsOtherGroups(t *testing.T) {
	m := entity.NewMob(entity.TypeCow, 7, .5, 64, .5, 11)
	m.AddEffect(effect.Instance{ID: 1, Duration: 80, Visible: true})
	before := mobMetadataStateOf(m)
	m.Health--
	m.Crouching = true
	m.ClearEffects()
	var mask mobMetadataMask
	for i, group := range mobMetadataSchema {
		if group.fields == "health" {
			mask[i/64] = 1 << uint(i%64)
			break
		}
	}
	got := before
	captureMobMetadata(m, &got, mask)
	if got.health != float32(m.Health) || got.pose != before.pose || &got.particles[0] != &before.particles[0] {
		t.Fatal("health capture read or replaced another group's state")
	}
	var clean mobMetadataMask
	captureMobMetadata(m, &got, clean)
	if got.health != float32(m.Health) || &got.particles[0] != &before.particles[0] {
		t.Fatal("empty mask changed the snapshot")
	}
}
