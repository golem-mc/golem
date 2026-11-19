package entity

import (
	"maps"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/rules"
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

func TestAttributeSupplierCubeSize(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeSlime, 1, 0, 0, 0, 1)
	m.Size = 4
	s := m.AttributeSupplier()
	if got := s.Bases["minecraft:attack_damage"]; got != 4 {
		t.Errorf("size 4 slime attack_damage = %v, want 4", got)
	}
	if again := m.AttributeSupplier(); again != s {
		t.Error("supplier not cached per kind and size")
	}
}

func TestAttributeSupplierHorsesKeepOwnStats(t *testing.T) {
	t.Parallel()
	a := NewMob(TypeHorse, 1, 0, 0, 0, 1)
	b := NewMob(TypeHorse, 2, 0, 0, 0, 2)
	a.MaxHealth, b.MaxHealth = 20, 20
	a.Horse.Speed, a.Horse.Jump = 0.2, 0.6
	b.Horse.Speed, b.Horse.Jump = 0.3, 0.8
	sa, sb := a.AttributeSupplier(), b.AttributeSupplier()
	for name, want := range map[string][2]float64{
		"minecraft:movement_speed": {0.2, 0.3},
		"minecraft:jump_strength":  {0.6, 0.8},
	} {
		if sa.Bases[name] != want[0] || sb.Bases[name] != want[1] {
			t.Errorf("%s = %v, %v; want %v, %v", name, sa.Bases[name], sb.Bases[name], want[0], want[1])
		}
	}
}

// TestAnUneditedMapPacesLikeTheModel installs an untouched attribute map on
// every kind and checks the pace it reads is the model's own (the ghast's
// included, whose model pace its attribute does not carry): an edit must
// move a body off its kind's base, not to a different one.
func TestAnUneditedMapPacesLikeTheModel(t *testing.T) {
	t.Parallel()
	for i, k := range kinds {
		if k == nil {
			continue
		}
		typ := MobType(i)
		m := NewMob(typ, 1, 0.5, 64, 0.5, 1)
		model := m.pace()
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		if got := m.pace(); got != model {
			t.Errorf("%s: an unedited map paces %v, want the model's %v", typ, got, model)
		}
	}
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

// TestTheLeadersBonusComposesInTheMap builds a leader zombie's map: the
// supplier's max_health base holds the pre-bonus maximum and the bonus
// modifier folds it back, so an install leaves a healthy leader whole.
func TestTheLeadersBonusComposesInTheMap(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeZombie, 2, 0.5, 64, 0.5, 1)
	m.SetTraits(ZombieTraits{LeaderHealth: 0.5})
	a := m.BuildAttributes(nbt.List{})
	if got := a.Value(registry.AttributeMaxHealth); got != m.MaxHealth {
		t.Errorf("the leader's cap composes to %v, want the model's %v", got, m.MaxHealth)
	}
	if base := m.AttributeSupplier().Bases[attrMaxHealth]; base != m.MaxHealth/(1+m.zombie.LeaderHealth) {
		t.Errorf("the leader's supplier base is %v, want the pre-bonus %v",
			base, m.MaxHealth/(1+m.zombie.LeaderHealth))
	}
	m.Health = m.MaxHealth
	m.SetAttributes(a)
	m.OnAttributeUpdated()
	if m.Health != m.MaxHealth {
		t.Errorf("installing the map trimmed the leader to %v of %v", m.Health, m.MaxHealth)
	}
}

// TestARaisedCapLetsTheTameWolfEat feeds a tamed wolf at its model's full
// health whose map raises the cap: the meal a bare model refuses now heals
// past the model's maximum, to the map's.
func TestARaisedCapLetsTheTameWolfEat(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 1)
	m.Tamable.Tame = true
	food, ok := registry.ItemID("minecraft:cooked_beef")
	if !ok {
		t.Fatal("no cooked beef in the item registry")
	}
	a := m.BuildAttributes(nbt.List{})
	a.SetBase(registry.AttributeMaxHealth, m.MaxHealth+20)
	m.SetAttributes(a)
	m.Health = m.MaxHealth
	if !m.FeedWolf(protocol.ItemStack{ItemID: food, Count: 1}) {
		t.Fatal("the wolf refused a meal at the model's cap under a raised one")
	}
	if got := m.Health; got <= m.MaxHealth || got > m.healthCap() {
		t.Errorf("the meal healed to %v, want past the model's %v and under the cap %v",
			got, m.MaxHealth, m.healthCap())
	}
}

// TestAnUneditedMapPacesTheModelledVariants installs an untouched map on
// the bodies a supplier builds per mob — a leader, a baby, a rolled
// horse — and checks the pace matches the model's own.
func TestAnUneditedMapPacesTheModelledVariants(t *testing.T) {
	t.Parallel()
	variants := []struct {
		name  string
		typ   MobType
		setup func(*Mob)
	}{
		{"a leader zombie", TypeZombie, func(m *Mob) { m.SetTraits(ZombieTraits{LeaderHealth: 0.5}) }},
		{"a baby zombie", TypeZombie, func(m *Mob) { m.SetBaby(true) }},
		{"a rolled horse", TypeHorse, func(m *Mob) { m.Horse.Speed, m.Horse.Jump = 0.25, 0.6 }},
	}
	for _, v := range variants {
		m := NewMob(v.typ, 1, 0.5, 64, 0.5, 1)
		v.setup(m)
		model := m.pace()
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		if got := m.pace(); got != model {
			t.Errorf("%s: an unedited map paces %v, want the model's %v", v.name, got, model)
		}
	}
}

// TestTheCapFollowsAModelChangeOnAMappedMob changes the model's maximum
// after a map is installed and checks the cap moves with it.
func TestTheCapFollowsAModelChangeOnAMappedMob(t *testing.T) {
	t.Parallel()
	t.Run("slime size", func(t *testing.T) {
		t.Parallel()
		m := NewMob(TypeSlime, 1, 0.5, 64, 0.5, 1)
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		m.SetSize(4, true)
		if got := m.healthCap(); got != 16 {
			t.Errorf("cap after SetSize(4) = %v, want 16", got)
		}
	})
	t.Run("slime data merge", func(t *testing.T) {
		t.Parallel()
		m := NewMob(TypeSlime, 1, 0.5, 64, 0.5, 1)
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		m.LoadFrom(nbt.Compound{"Size": nbt.Int(3), "Health": nbt.Float(16)}, nil, func() int64 { return 0 })
		if m.Health != 16 {
			t.Errorf("health after merging Size 3 and Health 16 = %v, want 16", m.Health)
		}
	})
	t.Run("leader traits", func(t *testing.T) {
		t.Parallel()
		m := NewMob(TypeZombie, 1, 0.5, 64, 0.5, 1)
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		m.SetTraits(ZombieTraits{LeaderHealth: 1})
		if got := m.healthCap(); got != m.MaxHealth || got != 40 {
			t.Errorf("cap with leader bonus 1 = %v, model %v, want 40", got, m.MaxHealth)
		}
		m.SetTraits(ZombieTraits{})
		if got := m.healthCap(); got != 20 {
			t.Errorf("cap after dropping the bonus = %v, want 20", got)
		}
	})
	t.Run("horse roll", func(t *testing.T) {
		t.Parallel()
		m := NewMob(TypeHorse, 1, 0.5, 64, 0.5, 1)
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		m.finalizeHorse(GoDice{m.Rand}, nil)
		if got := m.healthCap(); got != m.MaxHealth {
			t.Errorf("cap after the roll = %v, want the model's %v", got, m.MaxHealth)
		}
	})
	t.Run("horse attribute merge", func(t *testing.T) {
		t.Parallel()
		m := NewMob(TypeHorse, 1, 0.5, 64, 0.5, 1)
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		attr := nbt.Compound{"id": nbt.String("minecraft:max_health"), "base": nbt.Double(30)}
		m.LoadFrom(nbt.Compound{"attributes": nbt.List{Items: []nbt.Tag{attr}}}, nil, func() int64 { return 0 })
		if got := m.healthCap(); got != 30 {
			t.Errorf("cap after merging a max_health base of 30 = %v, want 30", got)
		}
	})
}

// TestAWitchHealsAtTheMapsCap rolls the witch's drinks at the model's full
// health under a raised cap: the heal is only picked below the map's cap.
func TestAWitchHealsAtTheMapsCap(t *testing.T) {
	t.Parallel()
	pick := func(m *Mob) bool {
		for range 5000 {
			if m.witchPickPotion(nil) == "minecraft:healing" {
				return true
			}
		}
		return false
	}
	m := NewMob(TypeWitch, 1, 0.5, 64, 0.5, 1)
	m.Health = m.MaxHealth
	if pick(m) {
		t.Error("a witch at full health drank a healing potion")
	}
	a := m.BuildAttributes(nbt.List{})
	a.SetBase(registry.AttributeMaxHealth, m.MaxHealth+20)
	m.SetAttributes(a)
	if !pick(m) {
		t.Error("a witch under a raised cap never drank a healing potion")
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

// TestMappedMobWeaponCountsOnce checks that a weapon's damage reaches the
// blow once through the map.
func TestMappedMobWeaponCountsOnce(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeZombie, 1, 0, 0, 0, 3)
	m.SetEquipment(protocol.EquipMainHand, stackOf(t, "minecraft:iron_sword"))
	want := m.attackDamage()
	m.SetAttributes(m.BuildAttributes(nbt.List{}))
	m.SyncEquipmentModifiers(m.attrs)
	if got := m.attackDamage(); got != want {
		t.Errorf("mapped attackDamage = %v, want %v", got, want)
	}
}

// TestMappedZombieKeepsItsTraits checks that installing a map changes none
// of the values the spawn traits gave, and that a command's edit then does.
func TestMappedZombieKeepsItsTraits(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeZombie, 1, 0, 0, 0, 3)
	m.SetTraits(ZombieTraits{ReinforceBase: 0.05, LeaderBonus: 0.5, KnockbackResist: 0.04, FollowBonus: 0.3, LeaderHealth: 2})
	follow, knock, chance, health := m.followRange(), m.KnockbackResistance(), m.ReinforcementChance(), m.healthCap()
	m.SetAttributes(m.BuildAttributes(nbt.List{}))
	if got := m.followRange(); got != follow {
		t.Errorf("followRange = %v, want %v", got, follow)
	}
	if got := m.KnockbackResistance(); got != knock {
		t.Errorf("KnockbackResistance = %v, want %v", got, knock)
	}
	if got := m.ReinforcementChance(); got != chance {
		t.Errorf("ReinforcementChance = %v, want %v", got, chance)
	}
	if got := m.healthCap(); got != health {
		t.Errorf("healthCap = %v, want %v", got, health)
	}
	m.ChargeReinforcement()
	if got := m.ReinforcementChance(); got >= chance {
		t.Errorf("a charge left the chance at %v from %v", got, chance)
	}
	m.attrs.SetBase(registry.AttributeFollowRange, 10)
	if got, want := m.followRange(), 10*1.3; got != want {
		t.Errorf("edited followRange = %v, want %v", got, want)
	}
}

// TestMappedMobArmourFollowsItsEquipment checks that worn pieces reach the
// armour through the map the same as without one.
func TestMappedMobArmourFollowsItsEquipment(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeZombie, 1, 0, 0, 0, 3)
	m.SetEquipment(protocol.EquipHead, stackOf(t, "minecraft:diamond_helmet"))
	want := m.armoured(9)
	m.SetAttributes(m.BuildAttributes(nbt.List{}))
	m.SyncEquipmentModifiers(m.attrs)
	if got := m.armoured(9); got != want {
		t.Errorf("mapped armoured = %v, want %v", got, want)
	}
	m.SetEquipment(protocol.EquipHead, protocol.ItemStack{})
	m.SetEquipment(protocol.EquipChest, stackOf(t, "minecraft:iron_chestplate"))
	m.SyncEquipmentModifiers(m.attrs)
	if got, want := m.armoured(9), rules.DamageAfterArmour(9, 8, 0); got != want {
		t.Errorf("after the swap armoured = %v, want %v", got, want)
	}
}

// TestSpeedOnAMappedMobCountsOnce: Speed II is a modifier on the map, so the
// pace is the base times 1.4 and not 1.96.
func TestSpeedOnAMappedMobCountsOnce(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeCow, 1, 0.5, 64, 0.5, 1)
	m.SetAttributes(m.BuildAttributes(nbt.List{}))
	base := m.pace()
	m.AddEffect(effect.Instance{ID: registry.MobEffectSpeed, Amplifier: 1, Duration: 600})
	m.SyncSpeedModifiers(m.Attributes())
	if got, want := m.pace(), base*1.4; math.Abs(got-want) > 1e-6 {
		t.Errorf("pace under Speed II = %v, want %v", got, want)
	}
}

// TestSpeedScalesTheAddendsOfAMappedMob: the angry piglin's +0.05 is not on
// the map, and Speed II scales it with the rest.
func TestSpeedScalesTheAddendsOfAMappedMob(t *testing.T) {
	t.Parallel()
	m := NewMob(TypeZombifiedPiglin, 1, 0.5, 64, 0.5, 1)
	m.SetAttributes(m.BuildAttributes(nbt.List{}))
	base := m.pace()
	m.anger.ticks = 400
	m.AddEffect(effect.Instance{ID: registry.MobEffectSpeed, Amplifier: 1, Duration: 600})
	m.SyncSpeedModifiers(m.Attributes())
	if got, want := m.pace(), (base+0.05)*1.4; math.Abs(got-want) > 1e-6 {
		t.Errorf("angry pace under Speed II = %v, want %v", got, want)
	}
}

// TestAttrScaleKeepsTheKindsLimit checks that the dragon, the happy ghast and
// the shulker hold the scale attribute to their own limits.
func TestAttrScaleKeepsTheKindsLimit(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		typ  MobType
		want float64
	}{{TypeEnderDragon, 1}, {TypeHappyGhast, 1}, {TypeShulker, 3}, {TypeZombie, 4}} {
		m := NewMob(c.typ, 1, 0, 0, 0, 1)
		m.SetAttributes(m.BuildAttributes(nbt.List{}))
		m.attrs.SetBase(registry.AttributeScale, 4)
		if got := m.attrScale(); got != c.want {
			t.Errorf("type %d: attrScale = %v, want %v", c.typ, got, c.want)
		}
	}
}
