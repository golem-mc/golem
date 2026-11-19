package entity

import (
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
)

func breezeScene(t testing.TB) (*Mob, *tickView) {
	t.Helper()
	w := newTestWorld()
	floor(w, -16, -16, 16, 16, 63, defaultState(t, "minecraft:stone"))
	m := NewMob(TypeBreeze, 1, 0.5, 64, 0.5, 3)
	m.OnGround = true
	return m, &tickView{w: w, players: []Player{survivor(9, 10.5)}}
}

// vanilla: Shoot.start, tick, stop
func TestBreezeChargesFifteenTicksThenRecoversAndCoolsDown(t *testing.T) {
	m, view := breezeScene(t)
	m.breeze.shoot = 60
	m.breezeStep(view)
	if m.BreezePose() != protocol.PoseShooting || m.breeze.charge != 15 {
		t.Fatal("did not begin charging")
	}
	for tick := 1; tick <= 29; tick++ {
		view.events = nil
		m.breezeStep(view)
		shots := 0
		for _, ev := range view.events {
			if ev.Kind != MobShotBreezeCharge {
				continue
			}
			shots++
			if tick != 15 {
				t.Fatalf("shot at tick %d, want 15", tick)
			}
			if ev.Y != m.Y+m.height()/2+0.3 {
				t.Errorf("firing y=%v", ev.Y)
			}
			if speed := math.Sqrt(ev.VX*ev.VX + ev.VY*ev.VY + ev.VZ*ev.VZ); speed < 0.6 || speed > 0.8 {
				t.Errorf("shot speed=%v", speed)
			}
		}
		if tick == 15 && shots != 1 {
			t.Fatalf("shot count=%d, want one", shots)
		}
		if tick < 19 && m.BreezePose() != protocol.PoseShooting {
			t.Fatalf("pose reset early at tick %d", tick)
		}
		if tick == 19 && (m.BreezePose() != protocol.PoseStanding || m.breeze.cooldown != 10 || m.breeze.shoot != 0) {
			t.Fatal("recovery did not set the ten-tick cooldown and clear shoot")
		}
		if tick > 19 && tick < 29 && m.breeze.cooldown != 29-tick {
			t.Fatalf("cooldown tick %d: %d", tick, m.breeze.cooldown)
		}
	}
}

func TestBreezeSlidesAwayFromAnInnerCirclePlayerThenArmsAShot(t *testing.T) {
	m, view := breezeScene(t)
	view.players[0].X = 3.5
	m.breezeStep(view)
	if m.BreezePose() != protocol.PoseSliding {
		t.Fatal("did not slide")
	}
	end := m.breeze.destination
	if d := Dist2(3.5, 64, 0.5, float64(end.X)+0.5, float64(end.Y), float64(end.Z)+0.5); d <= 9 {
		t.Fatalf("destination %+v is not farther from the player", end)
	}
	if m.nav.speedFactor != float64(float32(0.6)) {
		t.Fatalf("slide pace=%v", m.nav.speedFactor)
	}
	m.X, m.Y, m.Z = float64(end.X)+0.5, float64(end.Y), float64(end.Z)+0.5
	m.breezeStep(view)
	if m.BreezePose() != protocol.PoseStanding || m.breeze.shoot != 60 {
		t.Fatal("arrival did not arm shoot for sixty ticks")
	}
}

func TestBreezeLongJumpsBehindADistantPlayer(t *testing.T) {
	m, view := breezeScene(t)
	view.players[0].X = 12.5
	m.breezeStep(view)
	if m.BreezePose() != protocol.PoseInhaling || m.breeze.jumpInhaling != 10 || !m.breeze.hasJumpTarget {
		t.Fatalf("pose=%d inhaling=%d target=%v", m.BreezePose(), m.breeze.jumpInhaling, m.breeze.jumpTarget)
	}
	target := m.breeze.jumpTarget
	start := Dist2(m.X, m.Y, m.Z, float64(target.X)+0.5, float64(target.Y), float64(target.Z)+0.5)
	landed := false
	for tick := 1; tick <= 60; tick++ {
		m.Tick(view.w, view.players)
		if m.BreezePose() == protocol.PoseLongJumping {
			if tick < 10 {
				t.Fatalf("jumped before ten inhale ticks, at %d", tick)
			}
		}
		if m.BreezePose() == protocol.PoseStanding && tick > 10 {
			landed = true
			if !m.OnGround || m.breeze.noFriction || m.breeze.hasJumpTarget {
				t.Fatalf("landing at tick %d: ground=%v noFriction=%v target=%v", tick, m.OnGround, m.breeze.noFriction, m.breeze.hasJumpTarget)
			}
			if m.breeze.jumpCooldown < 2 || m.breeze.jumpCooldown > 10 || m.breeze.shoot != 100 {
				t.Fatalf("landing memories: cooldown=%d shoot=%d", m.breeze.jumpCooldown, m.breeze.shoot)
			}
			break
		}
	}
	if !landed {
		t.Fatalf("did not land in 60 ticks: pose=%d x=%v y=%v z=%v", m.BreezePose(), m.X, m.Y, m.Z)
	}
	end := Dist2(m.X, m.Y, m.Z, float64(target.X)+0.5, float64(target.Y), float64(target.Z)+0.5)
	if end >= start {
		t.Fatalf("landing distance %v is not closer than %v to %+v", end, start, target)
	}
}

func TestBreezeDropsUnseenTargetsAndRejectsCreativeSpectatorAndAllies(t *testing.T) {
	m, view := breezeScene(t)
	if p := m.breezeTarget(view); p == nil || p.EntityID != 9 {
		t.Fatal("no target")
	}
	view.players[0].X = 40
	for range 99 {
		if m.breezeTarget(view) == nil {
			t.Fatal("lost unseen target early")
		}
	}
	if m.breezeTarget(view) != nil || m.TargetID != 0 {
		t.Fatal("kept unseen target after 100 ticks")
	}
	for _, mode := range []string{"creative", "spectator", "ally", "dead"} {
		p := survivor(9, 10.5)
		switch mode {
		case "creative":
			p.Creative = true
		case "spectator":
			p.Spectator = true
		case "dead":
			p.Dead = true
		case "ally":
			m.Team = "team"
			p.Team = "team"
		}
		view.players = []Player{p}
		if m.breezeTarget(view) != nil {
			t.Fatalf("targeted %s player", mode)
		}
		m.Team = ""
	}
	m.retaliate = 9
	view.players = []Player{survivor(9, 40)}
	if m.breezeTarget(view) == nil {
		t.Fatal("did not retaliate against the last player")
	}
}

func TestBreezeAttributesSoundsPathMalusesAndTrialSpawning(t *testing.T) {
	m, view := breezeScene(t)
	if m.Health != 30 || m.pace() != float64(float32(0.63)) || m.eyeHeight() != 1.3452 || m.RollExperience(rules.LootKiller{}) != 10 {
		t.Fatal("incorrect breeze attributes")
	}
	if s := m.Sounds().Ambient.Name(); s != "minecraft:entity.breeze.idle_ground" {
		t.Fatalf("ground ambient=%q", s)
	}
	m.TargetID = 9
	if m.Sounds().Ambient.Name() != "" {
		t.Fatal("targeted grounded breeze has ambient")
	}
	m.OnGround = false
	if s := m.Sounds().Ambient.Name(); s != "minecraft:entity.breeze.idle_air" {
		t.Fatalf("air ambient=%q", s)
	}
	w := view.w.(testWorld)
	w.set(0, 63, 0, defaultState(t, "minecraft:oak_trapdoor"))
	foot := m.pathFootprint(w)
	if foot.malusOfType(positionPathType(w, 0, 64, 0)) != -1 || foot.fireMalus != -1 {
		t.Fatal("trapdoor tops or fire are traversable")
	}
	spawnWorld := newSpawnTestWorld()
	spawnWorld.testWorld = w
	if !TrialSpawnerAllows(spawnWorld, rand.New(rand.NewSource(1)), TypeBreeze, 3.5, 64, 3.5) {
		t.Fatal("trial spawner refused breeze")
	}
}
