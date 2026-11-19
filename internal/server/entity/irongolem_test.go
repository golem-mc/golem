package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/registry"
)

func TestIronGolemVillageDefenseSelection(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Mob, *Mob, *Player, testWorld)
		want   bool
	}{
		{"threshold", nil, true},
		{"above threshold", func(_, v *Mob, _ *Player, _ testWorld) { v.vg.gossips[gossipA] = [5]int32{0, 99} }, false},
		{"player created", func(m, _ *Mob, _ *Player, _ testWorld) { m.PlayerCreated = true }, false},
		{"creative", func(_, _ *Mob, p *Player, _ testWorld) { p.Huntable = false }, false},
		{"player team", func(m, _ *Mob, p *Player, _ testWorld) { m.Team, p.Team = "red", "red" }, false},
		{"villager team", func(m, v *Mob, _ *Player, _ testWorld) { m.Team, v.Team = "red", "red" }, false},
		{"dead villager", func(_, v *Mob, _ *Player, _ testWorld) { v.Dead = true }, false},
		{"invulnerable villager", func(_, v *Mob, _ *Player, _ testWorld) { v.Invulnerable = true }, false},
		{"player inside box", func(_, _ *Mob, p *Player, _ testWorld) { p.X = 10.69 }, true},
		{"player outside box", func(_, _ *Mob, p *Player, _ testWorld) { p.X = 10.71 }, false},
		{"player feet outside body inside", func(_, _ *Mob, p *Player, _ testWorld) { p.X = 10.9 }, false},
		{"player lower boundary", func(_, _ *Mob, p *Player, _ testWorld) { p.X = -10.7 }, true},
		{"player upper boundary", func(_, _ *Mob, p *Player, _ testWorld) { p.X = 10.7 }, false},
		{"player below box", func(_, _ *Mob, p *Player, _ testWorld) { p.Y = 55.9 }, false},
		{"player bottom boundary", func(_, _ *Mob, p *Player, _ testWorld) { p.Y = 56 }, true},
		{"villager inside box", func(_, v *Mob, _ *Player, _ testWorld) { v.X = 10.99 }, true},
		{"villager outside box", func(_, v *Mob, _ *Player, _ testWorld) { v.X = 11.01 }, false},
		// The corner is farther than the golem's 16-block follow range; selection uses 64.
		{"box corner", func(_, v *Mob, _ *Player, _ testWorld) { v.X, v.Y, v.Z = 10.9, 72, 10.9 }, true},
		{"player invisible", func(_, _ *Mob, p *Player, _ testWorld) { p.X, p.Invisible = 5, true }, false},
		{"player invisible nearby", func(_, _ *Mob, p *Player, _ testWorld) { p.X, p.Invisible = 4, true }, true},
		{"villager invisible", func(_, v *Mob, _ *Player, _ testWorld) {
			v.Z = 5
			v.AddEffect(effect.New(mobEffectInvisibility, 100, 0, false, false, false))
		}, false},
		{"villager hidden", func(_, _ *Mob, _ *Player, w testWorld) {
			for y := int32(64); y < 69; y++ {
				w.set(0, y, 2, defaultState(t, "minecraft:stone"))
			}
		}, false},
		{"player hidden", func(_, _ *Mob, p *Player, w testWorld) {
			p.X = 5
			for y := int32(64); y < 69; y++ {
				w.set(2, y, 0, defaultState(t, "minecraft:stone"))
			}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			m := NewMob(TypeIronGolem, 1, 0, 64, 0, 1)
			v := NewMob(TypeVillager, 2, 0, 64, 4, 2)
			v.vg.gossips = Gossips{gossipA: {0, 100}}
			p := Player{EntityID: 7, UUID: gossipA, X: 2, Y: 64, Huntable: true}
			if tc.change != nil {
				tc.change(m, v, &p, w)
			}
			tick := &tickView{w: w, players: []Player{p}, others: []*Mob{v}}
			g := &defendVillageGoal{m: m}
			if got := g.CanStart(tick); got != tc.want {
				t.Fatalf("CanStart = %v, want %v", got, tc.want)
			}
			if tc.want {
				g.Start(tick)
				if m.TargetID != p.EntityID {
					t.Fatalf("target = %d, want %d", m.TargetID, p.EntityID)
				}
			}
		})
	}
}

func TestIronGolemRetaliationAndHostileTargets(t *testing.T) {
	for _, built := range []bool{false, true} {
		for _, typ := range []MobType{registry.EntityTypePlayer, TypeZombie, TypeCreeper, TypeCow} {
			t.Run(typ.Name()+map[bool]string{true: "/built", false: "/natural"}[built], func(t *testing.T) {
				m := NewMob(TypeIronGolem, 1, 0.5, 64, 0.5, 1)
				m.PlayerCreated = built
				tick := &tickView{w: flatWorld(t)}
				if typ == registry.EntityTypePlayer {
					tick.players = []Player{{EntityID: 7, UUID: gossipA, X: 3, Y: 64, Huntable: true}}
				} else {
					tick.others = []*Mob{NewMob(typ, 7, 3, 64, 0.5, 7)}
				}
				m.retaliate = 7
				var g *hurtByGoal
				for _, goal := range ironGolemGoals(m) {
					if h, ok := goal.(*hurtByGoal); ok {
						g = h
					}
				}
				want := typ != TypeCreeper && (typ != registry.EntityTypePlayer || !built)
				if g.CanStart(tick) != want {
					t.Fatalf("retaliation allowed = %v, want %v", !want, want)
				}
				if want {
					g.Start(tick)
					if m.TargetID != 7 || !g.ShouldContinue(tick) {
						t.Fatal("retaliation did not keep its attacker")
					}
				}
			})
		}
	}
	w := flatWorld(t)
	for _, typ := range []MobType{TypeZombie, TypeCreeper, TypeCow} {
		m := NewMob(TypeIronGolem, 1, 0.5, 64, 0.5, 1)
		o := NewMob(typ, 2, 4.5, 64, 0.5, 2)
		for range 40 {
			m.tick(w, nil, []*Mob{o})
		}
		if got, want := m.TargetID == o.ID, typ == TypeZombie; got != want {
			t.Errorf("autonomous target %s = %v, want %v", typ, got, want)
		}
	}
}

func TestIronGolemMeleeRollAndCooldown(t *testing.T) {
	m := NewMob(TypeIronGolem, 1, 0.5, 64, 0.5, 1)
	p := Player{EntityID: 7, UUID: gossipA, X: 1.5, Y: 64, Z: 0.5, Huntable: true}
	m.TargetID = p.EntityID
	tick := &tickView{w: flatWorld(t), players: []Player{p}}
	g := &meleeAttackGoal{m: m, priority: 1, followUnseen: true, modifySwing: m.golemBlow,
		swingStatus: protocol.EntityStatusIronGolemAttack, swingSound: registry.SoundEntityIronGolemAttack}
	if !g.CanStart(tick) {
		t.Fatal("no melee path to adjacent player")
	}
	g.Start(tick)
	for n := range 41 {
		tick.events = nil
		g.Tick(tick)
		attacks, statuses, sounds := 0, 0, 0
		for _, ev := range tick.events {
			switch ev.Kind {
			case MobAttackedPlayer:
				attacks++
				if ev.Player.EntityID != p.EntityID || ev.Damage < 7.5 || ev.Damage > 21.5 {
					t.Fatalf("bad blow: %+v", ev)
				}
			case MobStatus:
				if ev.Status == protocol.EntityStatusIronGolemAttack {
					statuses++
				}
			case MobSounded:
				if ev.SoundID.Name() == "minecraft:entity.iron_golem.attack" {
					sounds++
				}
			}
		}
		want := 0
		if n%20 == 0 {
			want = 1
		}
		if attacks != want || statuses != want || sounds != want {
			t.Fatalf("tick %d: attacks/statuses/sounds = %d/%d/%d, want %d", n, attacks, statuses, sounds, want)
		}
	}
	seen := map[float64]bool{}
	for range 1000 {
		ev := MobEvent{Damage: m.attackDamage()}
		m.golemBlow(&ev)
		seen[ev.Damage] = true
	}
	for i := range 15 {
		if !seen[7.5+float64(i)] {
			t.Errorf("damage roll never produced %v", 7.5+float64(i))
		}
	}
}
