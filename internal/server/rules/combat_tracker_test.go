package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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

func TestTrackerKillCreditPrefersPlayer(t *testing.T) {
	var tr CombatTracker
	tr.Record(0, CombatEntry{DamageType: DamageTypePlayerAttack, Attacker: &Killer{Name: protocol.Text{Text: "Bob"}}})
	tr.Record(2, CombatEntry{DamageType: DamageTypeMobAttack, Attacker: &Killer{Name: protocol.Text{Text: "Zombie"}}})
	tr.Record(5, CombatEntry{DamageType: DamageTypeLava})
	got := tr.DeathMessageAlive(trackerVictim, 5, &Killer{Name: protocol.Text{Text: "Bob"}}, true, nil)
	wantArgs(t, got, "death.attack.lava.player", "Alex", "Bob")
}
