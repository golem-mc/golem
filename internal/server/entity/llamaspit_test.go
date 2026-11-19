package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
	"math"
	"testing"
)

// TestLlamaSpitFliesThenFalls: the tick moves by the velocity it began with,
// then drags and drops it.
// vanilla: LlamaSpit.tick
func TestLlamaSpitFliesThenFalls(t *testing.T) {
	w := newTestWorld()
	spit := NewThrown(1, ThrownLlamaSpit, 0, 64, 0, 0, 0, 1.5, 0, 3)
	spit.Step(w, nil, nil)
	if !closeEnough(spit.Z, 1.5) {
		t.Errorf("z = %v after one tick, want the undragged 1.5", spit.Z)
	}
	if !closeEnough(spit.VZ, 1.5*llamaSpitDrag) || !closeEnough(spit.VY, -llamaSpitGravity) {
		t.Errorf("velocity z %v y %v, want 1.5 dragged and 0.06 down", spit.VZ, spit.VY)
	}
	if spit.Dead {
		t.Error("spit in open air ended")
	}
}

// TestLlamaSpitEndsOnABlock: a block in the way ends the spit.
func TestLlamaSpitEndsOnABlock(t *testing.T) {
	w := newTestWorld()
	w.set(0, 64, 2, defaultState(t, "minecraft:stone"))
	spit := NewThrown(1, ThrownLlamaSpit, 0.5, 64.5, 0.5, 0, 0, 1.5, 0, 3)
	var events []ThrownEvent
	for range 3 {
		events = append(events, spit.Step(w, nil, nil)...)
	}
	if !spit.Dead || len(events) != 1 || events[0].Kind != ThrownBreak {
		t.Fatalf("dead %v events %v, want one break", spit.Dead, events)
	}
}

// TestLlamaSpitHurtsABodyAndFliesOn: a hit reports 1 damage and the spit
// is not discarded by it.
// vanilla: LlamaSpit.onHitEntity
func TestLlamaSpitHurtsABodyAndFliesOn(t *testing.T) {
	w := newTestWorld()
	sheep := NewMob(TypeSheep, 7, 0.5, 64, 2, 1)
	target := MobTarget(7, 0, sheep)
	spit := NewThrown(1, ThrownLlamaSpit, 0.5, 64.5, 0.5, 0, 0, 1.5, 3, 3)
	spit.TickCount = 10
	events := spit.Step(w, nil, []ArrowTarget{target})
	if len(events) != 1 || events[0].Kind != ThrownHitTarget || events[0].Damage != LlamaSpitDamage {
		t.Fatalf("events %v, want one hit for %v", events, LlamaSpitDamage)
	}
	if spit.Dead || spit.Z < 1.9 {
		t.Errorf("dead %v z %v, want it flying past the body", spit.Dead, spit.Z)
	}
}

// TestLlamaSpitsAtItsTargetEveryFortyTicks: in range and in sight the llama
// stops, spits along the target's line with a lift, and lets go of the grudge.
// vanilla: RangedAttackGoal.tick, Llama.spit
func TestLlamaSpitsAtItsTargetEveryFortyTicks(t *testing.T) {
	w := newTestWorld()
	llama := NewMob(TypeLlama, 1, 0.5, 64, 0.5, 1)
	wolf := NewMob(TypeWolf, 2, 0.5, 64, 8.5, 2)
	llama.TargetID = wolf.ID
	llama.Yaw = 0
	goal := newSpitGoal(llama)
	view := &tickView{w: w, others: []*Mob{llama, wolf}}
	if !goal.CanStart(view) {
		t.Fatal("goal does not start on a live target")
	}
	goal.Start(view)
	var shots []MobEvent
	for range 90 {
		view.events = view.events[:0]
		goal.Tick(view)
		for _, ev := range view.events {
			if ev.Kind == MobSpat {
				shots = append(shots, ev)
			}
		}
	}
	if len(shots) != 2 {
		t.Fatalf("%d spits in 90 ticks, want 2", len(shots))
	}
	ev := shots[0]
	if ev.VZ < 1 || math.Abs(ev.VX) > 0.5 || ev.VY <= 0 {
		t.Errorf("spit velocity %v %v %v, want forward and lifted", ev.VX, ev.VY, ev.VZ)
	}
	if !llama.takeDidSpit() || llama.takeDidSpit() {
		t.Error("didSpit is not set once and cleared on read")
	}
}

// TestLlamaSpitEntityTypeIsTheRegistrys pins the wire number to the registry.
func TestLlamaSpitEntityTypeIsTheRegistrys(t *testing.T) {
	if got := registry.EntityTypes[protocol.EntityTypeLlamaSpit]; got != "minecraft:llama_spit" {
		t.Errorf("entity type %d is %q, want minecraft:llama_spit", protocol.EntityTypeLlamaSpit, got)
	}
}
