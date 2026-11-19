package entity

import (
	"maps"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestAttributeSupplierPerKind pins each kind's set and bases against the
// vanilla AttributeSupplier of its createAttributes chain.
func TestAttributeSupplierPerKind(t *testing.T) {
	t.Parallel()
	type want struct {
		has  []string
		lack []string
		base map[string]float64
	}
	animal := want{has: []string{"follow_range", "tempt_range"}, lack: []string{"attack_damage", "flying_speed"},
		base: map[string]float64{"follow_range": 16, "tempt_range": 10}}
	monster := func(attack float64, extra map[string]float64) want {
		b := map[string]float64{"follow_range": 16, "attack_damage": attack}
		maps.Copy(b, extra)
		return want{has: []string{"follow_range", "attack_damage"}, lack: []string{"tempt_range"}, base: b}
	}
	tests := map[MobType]want{
		TypeAxolotl:   {has: []string{"tempt_range", "attack_damage"}, base: map[string]float64{"follow_range": 16, "tempt_range": 10, "step_height": 1, "movement_speed": 1, "max_health": 14, "attack_damage": 2}},
		TypeCow:       animal,
		TypeMooshroom: animal,
		TypePig:       animal,
		TypeSheep:     animal,
		TypeChicken:   animal,
		TypeWolf:      {has: []string{"tempt_range"}, base: map[string]float64{"tempt_range": 10}},
		TypeFrog:      {has: []string{"tempt_range"}, base: map[string]float64{"tempt_range": 10}},
		TypeTadpole:   {has: []string{"tempt_range"}, base: map[string]float64{"tempt_range": 10}},
		TypeHorse:     {has: []string{"tempt_range"}, base: map[string]float64{"tempt_range": 10}},
		TypeLlama:     {has: []string{"tempt_range"}, base: map[string]float64{"tempt_range": 10}},
		TypeCamel:     {has: []string{"tempt_range"}, base: map[string]float64{"tempt_range": 10}},
		TypeTurtle:    {has: []string{"tempt_range"}, base: map[string]float64{"follow_range": 16, "tempt_range": 10, "step_height": 1}},
		TypeHappyGhast: {has: []string{"tempt_range", "flying_speed"}, base: map[string]float64{
			"follow_range": 16, "tempt_range": 16, "flying_speed": 0.05, "movement_speed": 0.05, "camera_distance": 8}},
		TypeBee: {has: []string{"tempt_range", "flying_speed", "attack_damage"}, base: map[string]float64{
			"flying_speed": float64(float32(0.6)), "movement_speed": float64(float32(0.3)), "max_health": 10, "attack_damage": 2}},
		TypeParrot: {has: []string{"tempt_range", "flying_speed", "attack_damage"}, base: map[string]float64{
			"flying_speed": float64(float32(0.4)), "movement_speed": float64(float32(0.2)), "attack_damage": 3}},
		TypeAllay: {has: []string{"flying_speed", "attack_damage"}, lack: []string{"tempt_range"}, base: map[string]float64{
			"flying_speed": float64(float32(0.1)), "movement_speed": float64(float32(0.1)), "max_health": 20}},
		TypeWither: {has: []string{"flying_speed"}, base: map[string]float64{
			"flying_speed": float64(float32(0.6)), "movement_speed": float64(float32(0.6)), "follow_range": 40, "armor": 4}},
		TypeCat:           {base: map[string]float64{"movement_speed": float64(float32(0.3))}},
		TypeOcelot:        {base: map[string]float64{"movement_speed": float64(float32(0.3))}},
		TypeFox:           {base: map[string]float64{"movement_speed": float64(float32(0.3)), "follow_range": 32}},
		TypeElderGuardian: {base: map[string]float64{"movement_speed": float64(float32(0.3))}},
		TypeHoglin:        {base: map[string]float64{"movement_speed": float64(float32(0.3)), "knockback_resistance": float64(float32(0.6))}},
		TypeZoglin:        {base: map[string]float64{"movement_speed": float64(float32(0.3)), "knockback_resistance": float64(float32(0.6))}},
		TypePiglin:        {base: map[string]float64{"movement_speed": float64(float32(0.35))}},
		TypePiglinBrute:   {base: map[string]float64{"movement_speed": float64(float32(0.35))}},
		TypePillager:      {base: map[string]float64{"movement_speed": float64(float32(0.35))}},
		TypeVindicator:    {base: map[string]float64{"movement_speed": float64(float32(0.35))}},
		TypeSquid:         {lack: []string{"attack_damage", "tempt_range"}, base: map[string]float64{"follow_range": 16}},
		TypeGlowSquid:     {lack: []string{"attack_damage", "tempt_range"}, base: map[string]float64{"follow_range": 16}},
		TypeSnowGolem:     {lack: []string{"attack_damage", "tempt_range"}, base: map[string]float64{"follow_range": 16}},
		TypeIronGolem: {has: []string{"attack_damage"}, lack: []string{"tempt_range"},
			base: map[string]float64{"follow_range": 16, "attack_damage": 15, "step_height": 1, "knockback_resistance": 1}},
		TypeCreeper:   monster(2, nil),
		TypeSpider:    monster(2, nil),
		TypeSkeleton:  monster(2, nil),
		TypeZombie:    monster(3, map[string]float64{"follow_range": 35}),
		TypeDrowned:   monster(3, map[string]float64{"follow_range": 35, "step_height": 1}),
		TypePhantom:   monster(2, nil),
		TypeSlime:     monster(1, nil),
		TypeMagmaCube: monster(1, map[string]float64{"armor": 3}),
	}
	for typ, w := range tests {
		m := NewMob(typ, 1, 0, 0, 0, 1)
		if m.Size == 0 && kinds[typ].cube {
			m.Size = 1
		}
		s := m.AttributeSupplier()
		for _, n := range w.has {
			if !s.Names["minecraft:"+n] {
				t.Errorf("%s lacks %s", typ, n)
			}
		}
		for _, n := range w.lack {
			if s.Names["minecraft:"+n] {
				t.Errorf("%s carries %s", typ, n)
			}
		}
		for n, v := range w.base {
			got, ok := s.Bases["minecraft:"+n]
			if !ok {
				got = registryDefault(n)
			}
			if got != v {
				t.Errorf("%s %s base = %v, want %v", typ, n, got, v)
			}
		}
	}
}

func registryDefault(name string) float64 {
	a, _ := registry.AttributeByName("minecraft:" + name)
	return a.Default
}

// TestALoadedEditListWakesTheMap reads a compound whose attributes list a
// command made: the list belongs to the map, so a load installs it and the
// health clipped. A list the kind's own save writes — the zombie's traits,
// the horse's rolls — stays the model's and wakes nothing.
func TestALoadedEditListWakesTheMap(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 0 }
	m := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	m.LoadFrom(nbt.Compound{
		"Health": nbt.Float(10),
		"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"id": nbt.String("minecraft:max_health"), "base": nbt.Double(4)},
		}},
	}, nil, now)
	if m.Attributes() == nil {
		t.Fatal("a load of a command-made list left the mob without its map")
	}
	if got := m.healthCap(); got != 4 {
		t.Errorf("the loaded cow's health cap = %v, want the edited 4", got)
	}
	if m.Health != 4 {
		t.Errorf("the loaded cow's health = %v, want the file's 10 clipped to the 4 cap", m.Health)
	}

	zombie := NewMob(TypeZombie, 2, 0.5, 64, 0.5, 2)
	zombie.LoadFrom(nbt.Compound{
		"Health": nbt.Float(20),
		"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"id": nbt.String("minecraft:spawn_reinforcements"), "base": nbt.Double(0)},
		}},
	}, nil, now)
	if zombie.Attributes() != nil {
		t.Error("a load of the zombie's trait list woke the map, want the traits to stay modelled")
	}
	if zombie.MaxHealth != 20 || zombie.Health != 20 {
		t.Errorf("the trait-loaded zombie is %v of %v health, want 20 of 20", zombie.Health, zombie.MaxHealth)
	}

	horse := NewMob(TypeHorse, 3, 0.5, 64, 0.5, 3)
	horse.LoadFrom(nbt.Compound{
		"Health": nbt.Float(18),
		"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"id": nbt.String("minecraft:max_health"), "base": nbt.Double(21)},
			nbt.Compound{"id": nbt.String("minecraft:movement_speed"), "base": nbt.Double(0.25)},
			nbt.Compound{"id": nbt.String("minecraft:jump_strength"), "base": nbt.Double(0.5)},
		}},
	}, nil, now)
	if horse.Attributes() != nil {
		t.Error("a load of the horse's rolled list woke the map, want the rolls to stay modelled")
	}
	if horse.MaxHealth != 21 || horse.Horse.Speed != 0.25 || horse.Horse.Jump != 0.5 {
		t.Errorf("the horse's rolls came back %v %v %v, want 21 health, 0.25 pace, 0.5 jump",
			horse.MaxHealth, horse.Horse.Speed, horse.Horse.Jump)
	}
	if horse.Health != 18 {
		t.Errorf("the rolled horse's health = %v, want the file's 18 under the read rolls", horse.Health)
	}
}

// TestMobReadsCombatAttributes checks that an edit to the map reaches the
// follow range, the blow and the knockback resistance.
func TestMobReadsCombatAttributes(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeSpider, 1, 0, 0, 0, 1)
	m.SetAttributes(m.BuildAttributes(nbt.List{}))
	m.attrs.SetBase(registry.AttributeFollowRange, 40)
	m.attrs.SetBase(registry.AttributeAttackDamage, 9)
	m.attrs.SetBase(registry.AttributeKnockbackResistance, 0.5)
	if got := m.followRange(); got != 40 {
		t.Errorf("followRange = %v, want 40", got)
	}
	if got := m.attackDamage(); got != 9 {
		t.Errorf("attackDamage = %v, want 9", got)
	}
	if got := m.KnockbackResistance(); got != 0.5 {
		t.Errorf("KnockbackResistance = %v, want 0.5", got)
	}
	m.attrs.SetBase(registry.AttributeAttackKnockback, 2)
	if _, k := m.BlowEnchantments(0, 1); k <= 0 {
		t.Errorf("attack knockback ignored: %v", k)
	}
}
