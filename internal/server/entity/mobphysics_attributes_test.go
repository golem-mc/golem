package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestMobAirTravelAttributes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                         string
		ground                       bool
		friction, drag, wantX, wantY float64
	}{
		{"air", false, 1, 2, 0.82, -0.0096},
		{"no air drag", false, 1, 0, 1, -0.01},
		{"no ground friction", true, 0, 1, 0.91, -0.0098},
		{"double ground friction", true, 2, 1, 0.182, -0.0098},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			floor(w, -2, -2, 4, 4, 63, defaultState(t, "minecraft:stone"))
			y := 70.0
			if tc.ground {
				y = 64
			}
			m := NewMob(TypeCow, 1, 0.5, y, 0.5, 1)
			m.SetAttributes(m.BuildAttributes(nbt.List{}))
			m.attrs.SetBase(registry.AttributeGravity, 0.01)
			m.attrs.SetBase(registry.AttributeFrictionModifier, tc.friction)
			m.attrs.SetBase(registry.AttributeAirDragModifier, tc.drag)
			m.OnGround = tc.ground
			m.VX = 1
			m.step(w, nil)
			if math.Abs(m.VX-tc.wantX) > 1e-6 || math.Abs(m.VY-tc.wantY) > 1e-6 {
				t.Fatalf("velocity=(%g,%g), want (%g,%g)", m.VX, m.VY, tc.wantX, tc.wantY)
			}
			m.VX, m.VY = 0, 0
			m.seatedTravel()
			if math.Abs(m.VY-tc.wantY) > 1e-6 {
				t.Errorf("seated vertical velocity=%g, want %g", m.VY, tc.wantY)
			}
			m.NoGravity = true
			if m.gravity() != 0 {
				t.Fatal("NoGravity did not override the gravity attribute")
			}
		})
	}
}

func TestMobCollisionBouncinessAttribute(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, block         string
		x, y, z, vx, vy, vz float64
		axis                string
		bounce              bool
	}{
		{"floor", "stone", 0.5, 64.5, 0.5, 0, -1, 0, "y", true},
		{"suppressed floor", "honey_block", 0.5, 64.5, 0.5, 0, -1, 0, "y", false},
		{"resting floor", "stone", 0.5, 64, 0.5, 0, -0.08, 0, "y", false},
		{"ceiling", "stone", 0.5, 65, 0.5, 0, 1, 0, "y", true},
		{"east wall", "stone", 0.5, 64, 0.5, 2, 0, 0, "x", true},
		{"south wall", "stone", 0.5, 64, 0.5, 0, 0, 2, "z", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			block := defaultState(t, "minecraft:"+tc.block)
			floor(w, -2, -2, 4, 4, 63, block)
			floor(w, -2, -2, 4, 4, 67, block)
			for y := int32(64); y < 67; y++ {
				w.set(2, y, 0, block)
				w.set(0, y, 2, block)
			}
			m := NewMob(TypeCow, 1, tc.x, tc.y, tc.z, 1)
			m.SetAttributes(m.BuildAttributes(nbt.List{}))
			m.attrs.SetBase(registry.AttributeBounciness, 0.5)
			m.VX, m.VY, m.VZ = tc.vx, tc.vy, tc.vz
			m.move(w, m.VX, m.VY, m.VZ)
			velocity := m.VY
			if tc.axis == "x" {
				velocity = m.VX
			}
			if tc.axis == "z" {
				velocity = m.VZ
			}
			if tc.bounce && velocity == 0 || !tc.bounce && velocity != 0 {
				t.Fatalf("bounce=%v: velocity=%g", tc.bounce, velocity)
			}
			if tc.bounce && (!m.Impulse || !m.bouncedOff) {
				t.Fatal("bounce did not request movement sync and its game event")
			}
		})
	}
}

func TestMobFallDamageAttributesAndJumpBoost(t *testing.T) {
	t.Parallel()
	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 63, defaultState(t, "minecraft:stone"))
	for _, boosted := range []bool{false, true} {
		m := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
		if boosted {
			m.AddEffect(effect.New(registry.MobEffectJumpBoost, 100, 1, false, true, true))
		}
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		if boosted {
			m.AddEffect(effect.New(registry.MobEffectJumpBoost, 100, 2, false, true, true))
		}
		m.attrs.SetBase(registry.AttributeSafeFallDistance, 6)
		m.attrs.SetBase(registry.AttributeFallDamageMultiplier, 0.5)
		want := 4.0
		if boosted {
			want = 2
		}
		damage, _, _, _ := m.fallBlow(w, 14)
		if damage != want {
			t.Errorf("boosted=%v: fourteen-block fall damage=%g, want %g", boosted, damage, want)
		}
		m.ClearEffects()
		if damage, _, _, _ := m.fallBlow(w, 14); damage != 4 {
			t.Errorf("fall damage after clearing effects=%g, want 4", damage)
		}
	}
	for _, kind := range []MobType{TypeHorse, TypeCamel, TypeFox} {
		m := NewMob(kind, 1, 0.5, 64, 0.5, 1)
		directSafe, directMultiplier := m.fallParams()
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		safe, multiplier := m.fallParams()
		if safe != directSafe || multiplier != directMultiplier {
			t.Errorf("%s: installing attributes changed fall parameters", kind.Name())
		}
	}
}

func TestCubeJumpStrengthAttribute(t *testing.T) {
	t.Parallel()
	for _, kind := range []MobType{TypeSlime, TypeMagmaCube, TypeSulfurCube} {
		m := NewMob(kind, 1, 0, 64, 0, 1)
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		m.attrs.SetBase(registry.AttributeJumpStrength, 0.8)
		m.jumpFromGround(0.5)
		want := float64(float32(0.8)*float32(0.5)) + m.cubeJumpBonus()
		if math.Abs(m.VY-want) > 1e-6 {
			t.Errorf("%s jump=%g, want %g", kind.Name(), m.VY, want)
		}
	}
}
