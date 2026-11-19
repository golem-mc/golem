package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func newCreaking() *Mob { return NewMob(TypeCreaking, 1, 0.5, 64, 0.5, 5) }

func creakingFloor(t *testing.T) testWorld {
	w := newTestWorld()
	wideFloor(w, t)
	return w
}

// frozenAfterTick ticks once and reports whether the creaking holds still.
func frozenAfterTick(m *Mob, w testWorld, players ...Player) bool {
	m.Tick(w, players)
	return m.CreakingView().Frozen
}

// vanilla: Creaking.checkCanMove, LivingEntity.isLookingAtMe
func TestACreakingFreezesUnderAClearGazeOnly(t *testing.T) {
	w := creakingFloor(t)
	m := newCreaking()
	if !frozenAfterTick(m, w, starer(m, 8)) {
		t.Fatal("a clear gaze did not freeze the creaking")
	}
	m = newCreaking()
	for y := int32(64); y <= 68; y++ {
		w.set(4, y, 0, defaultState(t, "minecraft:stone"))
	}
	if frozenAfterTick(m, w, starer(m, 8)) {
		t.Error("a gaze through a wall froze the creaking")
	}
	if m.CreakingView().Active {
		t.Error("a gaze through a wall woke the creaking")
	}
}

// vanilla: Creaking.checkCanMove (Player.isCreative and isSpectator are skipped)
func TestACreakingIgnoresACreativeOrSpectatingGaze(t *testing.T) {
	w := creakingFloor(t)
	m := newCreaking()
	p := starer(m, 8)
	p.Huntable = false
	if frozenAfterTick(m, w, p) || m.CreakingView().Active {
		t.Error("a player who cannot be hunted froze or woke the creaking")
	}
	p.Huntable, p.Spectator = true, true
	if frozenAfterTick(m, w, p) || m.CreakingView().Active {
		t.Error("a spectator froze or woke the creaking")
	}
}

// vanilla: Creaking.checkCanMove, LivingEntity.PLAYER_NOT_WEARING_DISGUISE_ITEM
func TestAGazeDisguiseHidesOnlyFromAnAwakeCreaking(t *testing.T) {
	w := creakingFloor(t)
	pumpkin, ok := registry.ItemID("minecraft:carved_pumpkin")
	if !ok {
		t.Fatal("no carved pumpkin item")
	}
	m := newCreaking()
	p := starer(m, 8)
	p.Worn[wornHead] = protocol.ItemStack{ItemID: pumpkin, Count: 1}
	if !frozenAfterTick(m, w, p) {
		t.Fatal("a pumpkin spared a sleeping creaking from the gaze")
	}
	if !m.CreakingView().Active {
		t.Fatal("the gaze did not wake the creaking")
	}
	if frozenAfterTick(m, w, p) {
		t.Error("a pumpkin did not hide the player from an awake creaking")
	}
	p.Worn[wornHead] = protocol.ItemStack{}
	if !frozenAfterTick(m, w, p) {
		t.Error("the bare gaze did not hold the awake creaking")
	}
}

// vanilla: Creaking.tick
func TestABoundCreakingTheServerReadsOrphanedDies(t *testing.T) {
	w := creakingFloor(t)
	m := newCreaking()
	m.CreakingBind(0, 70, 0)
	m.Tick(w, nil)
	if !m.Alive() {
		t.Error("a creaking its heart holds died")
	}
	m.CreakingOrphaned(true)
	m.Tick(w, nil)
	if m.Alive() {
		t.Fatal("an orphaned creaking outlived a tick")
	}
	free := newCreaking()
	free.CreakingOrphaned(true)
	free.Tick(w, nil)
	if !free.Alive() {
		t.Error("a creaking bound to no heart died of the orphan reading")
	}
}

// vanilla: Creaking.getAmbientSound, getHurtSound
func TestACreakingSoundsFollowItsState(t *testing.T) {
	m := newCreaking()
	if s := m.Sounds(); s.Hurt.Name() != "minecraft:entity.generic.hurt" || s.Ambient.Name() != "minecraft:entity.creaking.ambient" {
		t.Errorf("unbound: hurt %q ambient %q", s.Hurt.Name(), s.Ambient.Name())
	}
	m.creaking.active = true
	if s := m.Sounds(); s.Ambient.Name() != "" {
		t.Errorf("an active creaking speaks %q", s.Ambient.Name())
	}
	m.CreakingBind(0, 70, 0)
	if s := m.Sounds(); s.Hurt.Name() != "minecraft:entity.creaking.sway" {
		t.Errorf("bound hurt = %q, want the sway", s.Hurt.Name())
	}
}

// vanilla: Creaking constructor setCanFloat, CreakingAi Swim; setTransient sets no home
func TestACreakingSwimsAndBindingSetsNoHome(t *testing.T) {
	m := newCreaking()
	m.CreakingBind(0, 70, 0)
	if !m.canFloat {
		t.Error("a creaking sinks: no float goal")
	}
	if m.home.set {
		t.Error("binding restricted the creaking to a home")
	}
}

func TestCreakingActionsRollTheirVoicePitch(t *testing.T) {
	w := creakingFloor(t)
	m := newCreaking()
	m.CreakingBind(0, 70, 0)
	p := starer(m, 8)
	events := m.Tick(w, []Player{p})
	events = append(events, m.Tick(w, nil)...)
	m.TakeDamage(1, p)
	events = append(events, m.Tick(w, nil)...)
	m.TargetID = p.EntityID
	p.X, p.Z = m.X+1, m.Z
	v := tickView{w: w, players: []Player{p}}
	for _, goal := range creakingGoals(m) {
		if melee, ok := goal.(*meleeAttackGoal); ok {
			melee.Tick(&v)
		}
	}
	events = append(events, v.events...)
	pending := map[string]bool{
		"minecraft:entity.creaking.activate":   true,
		"minecraft:entity.creaking.deactivate": true,
		"minecraft:entity.creaking.freeze":     true,
		"minecraft:entity.creaking.unfreeze":   true,
		"minecraft:entity.creaking.sway":       true,
		"minecraft:entity.creaking.attack":     true,
	}
	for _, ev := range events {
		if !pending[ev.SoundID.Name()] {
			continue
		}
		delete(pending, ev.SoundID.Name())
		if ev.Volume != 1 || ev.Pitch == 1 || ev.Pitch < 0.79 || ev.Pitch > 1.21 {
			t.Errorf("%s: volume=%v pitch=%v; want 1 and a rolled adult voice", ev.SoundID.Name(), ev.Volume, ev.Pitch)
		}
	}
	if len(pending) != 0 {
		t.Errorf("missing sounds: %v", pending)
	}
}
