package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

var trackerVictim = protocol.Text{Text: "Alex"}

func TestTrackerFallLocationWordsAFall(t *testing.T) {
	var tr CombatTracker
	tr.Record(10, CombatEntry{DamageType: DamageTypeFall, Damage: 7, FallDistance: 10, Location: FallLocationLadder})
	wantArgs(t, tr.DeathMessage(trackerVictim), "death.fell.accident.ladder", "Alex")
	var plain CombatTracker
	plain.Record(10, CombatEntry{DamageType: DamageTypeFall, Damage: 7, FallDistance: 10})
	wantArgs(t, plain.DeathMessage(trackerVictim), "death.fell.accident.generic", "Alex")
	var short CombatTracker
	short.Record(10, CombatEntry{DamageType: DamageTypeFall, Damage: 4, FallDistance: 4, Location: FallLocationWater})
	wantArgs(t, short.DeathMessage(trackerVictim), "death.attack.fall", "Alex")
}

func TestTrackerAssistedFall(t *testing.T) {
	steve := &Killer{Name: protocol.Text{Text: "Steve"}}
	var tr CombatTracker
	tr.Record(1, CombatEntry{DamageType: DamageTypePlayerAttack, Attacker: steve, Damage: 1})
	tr.Record(5, CombatEntry{DamageType: DamageTypeFall, Damage: 7, FallDistance: 10})
	wantArgs(t, tr.DeathMessage(trackerVictim), "death.fell.assist", "Alex", "Steve")
	sword := protocol.Text{Text: "Sharp"}
	armed := &Killer{Name: steve.Name, Weapon: protocol.ItemStack{ItemID: 1, Count: 1, CustomName: &sword}}
	var withItem CombatTracker
	withItem.Record(1, CombatEntry{DamageType: DamageTypePlayerAttack, Attacker: armed, Damage: 1})
	withItem.Record(5, CombatEntry{DamageType: DamageTypeFall, Damage: 7, FallDistance: 10})
	item := withItem.DeathMessage(trackerVictim)
	wantArgs(t, item, "death.fell.assist.item", "Alex", "Steve", "")
	if item.With[2].Translate != "chat.square_brackets" || len(item.With[2].With) != 1 || len(item.With[2].With[0].Extra) != 1 || item.With[2].With[0].Extra[0].Text != "Sharp" {
		t.Errorf("item argument = %+v, want the bracketed name Sharp", item.With[2])
	}
	var finish CombatTracker
	finish.Record(1, CombatEntry{DamageType: DamageTypePlayerAttack, Attacker: steve, Damage: 1})
	finish.Record(5, CombatEntry{DamageType: DamageTypeEnderPearl, Attacker: steve, Damage: 7, FallDistance: 10})
	wantArgs(t, finish.DeathMessage(trackerVictim), "death.fell.finish", "Alex", "Steve")
	var killer CombatTracker
	killer.Record(1, CombatEntry{DamageType: DamageTypeGeneric, Damage: 6, Location: FallLocationWater})
	killer.Record(2, CombatEntry{DamageType: DamageTypeFall, Damage: 1, FallDistance: 1})
	wantArgs(t, killer.DeathMessage(trackerVictim), "death.fell.killer", "Alex")
}

func TestTrackerResetsAfterQuietTicks(t *testing.T) {
	var tr CombatTracker
	tr.Record(0, CombatEntry{DamageType: DamageTypeFall, Damage: 7, FallDistance: 10, Location: FallLocationLadder})
	tr.Record(100, CombatEntry{DamageType: DamageTypeFall, Damage: 7, FallDistance: 1})
	if len(tr.entries) != 2 {
		t.Errorf("entries after 100 quiet ticks = %d, want 2", len(tr.entries))
	}
	tr.Record(201, CombatEntry{DamageType: DamageTypeFall, Damage: 7, FallDistance: 1})
	if len(tr.entries) != 1 {
		t.Errorf("entries after 101 quiet ticks = %d, want 1", len(tr.entries))
	}
	var fight CombatTracker
	fight.Record(0, CombatEntry{DamageType: DamageTypePlayerAttack, Attacker: &Killer{}, Damage: 1})
	fight.Record(250, CombatEntry{DamageType: DamageTypeFall, Damage: 1})
	if len(fight.entries) != 2 {
		t.Errorf("a fight's entries after 250 ticks = %d, want 2", len(fight.entries))
	}
	fight.Record(551, CombatEntry{DamageType: DamageTypeFall, Damage: 1})
	if len(fight.entries) != 1 {
		t.Errorf("a fight's entries after 301 quiet ticks = %d, want 1", len(fight.entries))
	}
}

func TestBlockFallLocation(t *testing.T) {
	for name, want := range map[string]FallLocation{
		"minecraft:ladder": FallLocationLadder, "minecraft:vine": FallLocationVines,
		"minecraft:oak_trapdoor": FallLocationLadder, "minecraft:scaffolding": FallLocationScaffolding,
		"minecraft:weeping_vines_plant": FallLocationWeepingVines, "minecraft:twisting_vines": FallLocationTwistingVines,
		"minecraft:cave_vines": FallLocationOtherClimb,
	} {
		if got := BlockFallLocation(registry.MustState(name, nil)); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

// wantArgs checks a message's translation key and the text of its arguments.
func wantArgs(t *testing.T, got protocol.Text, key string, args ...string) {
	t.Helper()
	if got.Translate != key || len(got.With) != len(args) {
		t.Fatalf("message = %+v, want %s with %v", got, key, args)
	}
	for i, want := range args {
		if got.With[i].Text != want {
			t.Errorf("%s argument %d = %q, want %q", key, i, got.With[i].Text, want)
		}
	}
}

func TestTrackerKillCreditPlayerVariant(t *testing.T) {
	var tr CombatTracker
	tr.Record(0, CombatEntry{DamageType: DamageTypePlayerAttack, Attacker: &Killer{Name: protocol.Text{Text: "Bob"}}})
	tr.Record(5, CombatEntry{DamageType: DamageTypeLava})
	wantArgs(t, tr.DeathMessage(trackerVictim), "death.attack.lava.player", "Alex", "Bob")
}

func TestTrackerKillCreditFallWithoutSignificantFall(t *testing.T) {
	var tr CombatTracker
	tr.Record(0, CombatEntry{DamageType: DamageTypePlayerAttack, Attacker: &Killer{Name: protocol.Text{Text: "Bob"}}})
	tr.Record(5, CombatEntry{DamageType: DamageTypeFall})
	wantArgs(t, tr.DeathMessage(trackerVictim), "death.attack.fall.player", "Alex", "Bob")
}

func TestDeathMessageUnknownTypeUsesMessageID(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackDamageTypes(nil) })
	registry.ApplyPackDamageTypes(map[string]registry.DamageTypeInfo{"minecraft:made_up": {MessageID: "made_up"}})
	typ, _ := registry.DamageTypeByName("minecraft:made_up")
	got := DeathMessage(typ, protocol.Text{Text: "Alex"}, nil)
	if got.Translate != "death.attack.made_up" {
		t.Fatalf("got %q", got.Translate)
	}
}

func TestTrackerKillCreditPrefersPlayer(t *testing.T) {
	var tr CombatTracker
	tr.Record(0, CombatEntry{DamageType: DamageTypePlayerAttack, Attacker: &Killer{Name: protocol.Text{Text: "Bob"}}})
	tr.Record(2, CombatEntry{DamageType: DamageTypeMobAttack, Attacker: &Killer{Name: protocol.Text{Text: "Zombie"}}})
	tr.Record(5, CombatEntry{DamageType: DamageTypeLava})
	got := tr.DeathMessageAlive(trackerVictim, 5, &Killer{Name: protocol.Text{Text: "Bob"}}, true, nil)
	wantArgs(t, got, "death.attack.lava.player", "Alex", "Bob")
}

func TestTrackerKillCreditSkipsGoneAttacker(t *testing.T) {
	var tr CombatTracker
	tr.Record(0, CombatEntry{DamageType: DamageTypeMobAttack, AttackerID: 1, Attacker: &Killer{Name: protocol.Text{Text: "Husk"}}})
	tr.Record(2, CombatEntry{DamageType: DamageTypeMobAttack, AttackerID: 2, Attacker: &Killer{Name: protocol.Text{Text: "Zombie"}}})
	tr.Record(5, CombatEntry{DamageType: DamageTypeLava})
	got := tr.DeathMessageAlive(trackerVictim, 5, nil, false, func(id int32) bool { return id == 2 })
	wantArgs(t, got, "death.attack.lava.player", "Alex", "Zombie")
	got = tr.DeathMessageAlive(trackerVictim, 5, nil, false, func(id int32) bool { return id == 1 })
	wantArgs(t, got, "death.attack.lava", "Alex")
	got = tr.DeathMessageAlive(trackerVictim, 5, nil, false, func(int32) bool { return false })
	wantArgs(t, got, "death.attack.lava", "Alex")
	// A remembered player who is gone leaves no credit to the zombie.
	got = tr.DeathMessageAlive(trackerVictim, 5, nil, true, func(int32) bool { return true })
	wantArgs(t, got, "death.attack.lava", "Alex")
}

func TestKillCreditForgetsAfterAHundredTicks(t *testing.T) {
	var tr CombatTracker
	tr.Record(0, CombatEntry{DamageType: DamageTypePlayerAttack, AttackerID: 1, Attacker: &Killer{Name: protocol.Text{Text: "Bob"}}})
	tr.Record(90, CombatEntry{DamageType: DamageTypeMobAttack, AttackerID: 2, Attacker: &Killer{Name: protocol.Text{Text: "Zombie"}}})
	if e := tr.KillCredit(105, nil, false, nil); e == nil || e.AttackerID != 2 {
		t.Errorf("credit at 105 = %+v, want the zombie", e)
	}
	if e := tr.KillCredit(191, nil, false, nil); e != nil {
		t.Errorf("credit at 191 = %+v, want none", e)
	}

	var gone CombatTracker
	gone.Record(0, CombatEntry{DamageType: DamageTypeMobAttack, AttackerID: 1, Attacker: &Killer{Name: protocol.Text{Text: "Husk"}}})
	gone.Record(2, CombatEntry{DamageType: DamageTypeMobAttack, AttackerID: 2, Attacker: &Killer{Name: protocol.Text{Text: "Zombie"}}})
	gone.Record(5, CombatEntry{DamageType: DamageTypeLava})
	alive := func(id int32) bool { return id == 1 }
	if e := gone.KillCredit(5, nil, false, alive); e != nil {
		t.Errorf("credit with the latest mob gone = %+v, want none", e)
	}
	wantArgs(t, gone.DeathMessageAlive(trackerVictim, 5, nil, false, alive), "death.attack.lava", "Alex")
}
