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

func TestOrdinaryMobCeilingBounceDoesNotCacheSulfur(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeCow, 1, 0, 64, 0, 1)
	m.VY = 0.5
	if got := m.bounceOffCeiling(0.1); got != 0 || m.sulfur.prof != nil {
		t.Fatalf("bounce %g, profile %+v", got, m.sulfur.prof)
	}
}

// vanilla: SulfurCube.hurtServer, LivingEntity.dealDefaultKnockback
func TestSulfurImmuneProjectileShovesUseShooterStance(t *testing.T) {
	t.Parallel()
	src := HitSource{X: 0, Y: 64, Z: 0, Eye: 64.49, Yaw: -90}
	for _, kind := range []string{"thrown", "wind", "spit", "bullet", "firework"} {
		t.Run(kind, func(t *testing.T) {
			m := NewMob(TypeSulfurCube, 1, 1, 64, 0, 1)
			m.Body = protocol.ItemStack{ItemID: registry.MustItem("minecraft:oak_planks"), Count: 1}
			var landed, soft bool
			switch kind {
			case "thrown":
				landed, soft = m.HurtByThrown(4, 1, 0, 2, true, false, &src)
			case "wind":
				landed, soft = m.HurtByWindCharge(4, 1, 0, 2, true, false, &src)
			case "spit":
				landed, soft = m.HurtByProjectile(4, registry.DamageTypeSpit, 1, 0, 2, true, false, &src)
			case "bullet":
				landed, soft = m.HurtByShulkerBullet(4, 1, 0, 2, true, false, &src)
			case "firework":
				landed, soft = m.HurtByFirework(4, registry.DamageTypeFireworks, 2, true, false, 0, 0, &src)
			}
			// sqrt(4) * bouncy horizontal/vertical power * (1 - (-2)).
			if !landed || !soft || m.Health != 8 || math.Abs(m.VX-float64(float32(0.4125)*2*3*0.4)) > 1e-6 || math.Abs(m.VY-float64(float32(0.105)*2*3)*1.2) > 1e-6 {
				t.Fatalf("immune %s: landed=%v soft=%v health=%g velocity=(%g,%g,%g)", kind, landed, soft, m.Health, m.VX, m.VY, m.VZ)
			}
			if !m.sulfur.hitSound || !m.Impulse {
				t.Fatal("shove did not queue sound/motion")
			}
		})
	}
}

func TestSulfurSourceLessImmuneDamageNudgesButRespectsNoKnockback(t *testing.T) {
	t.Parallel()
	for _, damageType := range []registry.DamageType{registry.DamageTypeCactus, registry.DamageTypeFallingBlock, registry.DamageTypeFall} {
		m := NewMob(TypeSulfurCube, 1, 0, 64, 0, 1)
		m.Body = protocol.ItemStack{ItemID: registry.MustItem("minecraft:oak_planks"), Count: 1}
		m.OnGround = true
		landed, _ := m.HurtByWorld(4, damageType)
		if !landed || m.Health != 8 {
			t.Fatalf("%s: landed=%v health=%g", damageType, landed, m.Health)
		}
		want := 0.0
		if !damageType.InTag(registry.DamageTypeTagIDNoKnockback) {
			want = 1.2
		}
		if math.Abs(math.Hypot(m.VX, m.VZ)-want) > 1e-6 {
			t.Fatalf("%s nudge=(%g,%g), want length %g", damageType, m.VX, m.VZ, want)
		}
	}
}

func TestSulfurFallingEntityUsesItsStanceWithoutDamageAnimation(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeSulfurCube, 1, 1, 64, 0, 1)
	m.Body = protocol.ItemStack{ItemID: registry.MustItem("minecraft:oak_planks"), Count: 1}
	// A falling entity has a source position and look; generic /damage with
	// the same type has neither and uses the random default shove instead.
	src := HitSource{X: 0, Y: 64, Z: 0, Eye: 64.49, Yaw: -90}
	if m.HurtByFallFrom(4, registry.DamageTypeFallingBlock, 0, 0, &src) {
		t.Fatal("immune fall requested a damage animation")
	}
	if m.Health != 8 || math.Abs(m.VX-float64(float32(0.4125)*2*3*0.4)) > 1e-6 || math.Abs(m.VY-float64(float32(0.105)*2*3)*1.2) > 1e-6 {
		t.Fatalf("falling-entity hit: health=%g velocity=(%g,%g,%g)", m.Health, m.VX, m.VY, m.VZ)
	}
}
