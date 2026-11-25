package server

import (
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkReadMobTickInputsShortLived measures the UUID scan with live fangs
// and bolts, including map reuse across ticks. Only input gathering runs, so
// neither kind expires during the measurement.
func BenchmarkReadMobTickInputsShortLived(b *testing.B) {
	for _, count := range []int{1, 200, 2000} {
		b.Run(fmt.Sprintf("%d_each", count), func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			s.mu.Lock()
			clear(s.clients)
			s.mu.Unlock()
			for i := range count {
				x, z := trackerBenchSpot(i % trackerBenchBodies)
				s.spawnFangs(fangsCast{w: w, x: x, y: -60, z: z})
				s.spawnBolt(w, x, -60, z, true, plugin.LightningWeather)
			}
			s.readMobTickInputs(s.resetMobTickInputs())
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.readMobTickInputs(s.resetMobTickInputs())
			}
			b.StopTimer()
			if len(s.fangs.list) != count || len(s.bolts) != count {
				b.Fatalf("live fangs/bolts = %d/%d, want %d each", len(s.fangs.list), len(s.bolts), count)
			}
		})
	}
}

// BenchmarkWholeTickShortLivedFrozen measures command-movable effects in the
// whole frozen tick, with eight real connections watching them. Freezing keeps
// both populations alive; setup and the initial pairing are outside timing.
func BenchmarkWholeTickShortLivedFrozen(b *testing.B) {
	for _, count := range []int{0, 200} {
		b.Run(fmt.Sprintf("%d_each", count), func(b *testing.B) {
			s, _ := newIdlePlayersBenchServer(b, trackerBenchWatchers)
			s.tickRate.SetFrozen(true)
			for i := range count {
				x, z := trackerBenchSpot(i)
				s.spawnFangs(fangsCast{w: s.world, x: x, y: -60, z: z})
				s.spawnBolt(s.world, x, -60, z, true, plugin.LightningWeather)
			}
			for range 20 {
				s.tick()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tick()
			}
			b.StopTimer()
			if len(s.fangs.list) != count || len(s.bolts) != count {
				b.Fatalf("live fangs/bolts = %d/%d, want %d each", len(s.fangs.list), len(s.bolts), count)
			}
		})
	}
}

// BenchmarkTickBoltsVisualOnly keeps 200 live bolts in the running bolt pass,
// replacing those that expire before each step. Their normal lifetime and
// tracking registration run, but visual-only bolts do not damage the watchers.
func BenchmarkTickBoltsVisualOnly(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for len(s.bolts) < trackerBenchBodies {
			x, z := trackerBenchSpot(len(s.bolts))
			s.spawnBolt(w, x, -60, z, true, plugin.LightningWeather)
		}
		s.tickBolts()
	}
}

// BenchmarkTickFangsBite repeats 200 ownerless bites with eight watchers.
// Resetting the attack models is timed identically on both sides, so every
// pass reaches the bite without expiring or advancing a world clock. The
// scaled case stands the watchers at scale 16 inside the bite grid, so each
// overlapping bite checks the section its box reaches.
func BenchmarkTickFangsBite(b *testing.B) {
	for _, scaled := range []bool{false, true} {
		name := "watchers_away"
		if scaled {
			name = "scaled_players_in_grid"
		}
		b.Run(name, func(b *testing.B) {
			s, w := newTrackerBenchServer(b)
			if scaled {
				i := 0
				s.mu.RLock()
				for _, c := range s.clients {
					x, z := trackerBenchSpot(i * 27)
					i++
					c.player.SetPosition(player.Position{X: x, Y: -60, Z: z})
					c.player.UpdateVitals(func(v *player.Vitals) { v.GameMode = protocol.GameModeSurvival })
					c.player.Attributes(func(a *player.Attributes) { a.SetBase(registry.AttributeScale, 16) })
				}
				s.mu.RUnlock()
			}
			for i := range trackerBenchBodies {
				x, z := trackerBenchSpot(i)
				s.spawnFangs(fangsCast{w: w, x: x, y: -60, z: z})
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				for _, it := range s.fangs.list {
					f := it.fangs
					it.fangs = entity.NewEvokerFangs(f.ID, f.X, f.Y, f.Z, 0, -7, 0)
				}
				// Timed identically on both sides: keep every bite landing.
				s.mu.RLock()
				for _, c := range s.clients {
					c.player.UpdateVitals(func(v *player.Vitals) {
						v.Health, v.Dead, v.InvulnerableTicks = 20, false, 0
					})
				}
				s.mu.RUnlock()
				s.tickFangs()
			}
			if scaled {
				hurt := false
				s.mu.RLock()
				for _, c := range s.clients {
					hurt = hurt || c.player.Vitals().Health < 20
				}
				s.mu.RUnlock()
				if !hurt {
					b.Fatal("no scaled player was bitten")
				}
			}
		})
	}
}

// BenchmarkTickOwnedFangsBite repeats an evoker's bites among allied illagers
// and its vexes so each bite checks alliances without changing the herd.
func BenchmarkTickOwnedFangsBite(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	owner := s.SpawnMob("minecraft:evoker", 17.5, -60, 0.5)
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		if i%2 == 0 {
			s.SpawnMob("minecraft:vindicator", x, -60, z)
		} else {
			vex := s.SpawnMob("minecraft:vex", x, -60, z)
			s.SetMobVexOwner(vex, owner)
		}
		s.spawnFangs(fangsCast{w: w, owner: owner, x: x, y: -60, z: z})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, it := range s.fangs.list {
			f := it.fangs
			it.fangs = entity.NewEvokerFangs(f.ID, f.X, f.Y, f.Z, 0, -7, owner)
		}
		s.tickFangs()
	}
}

// BenchmarkTickPlayerOwnedFangsBite repeats a player's bites among mobs on
// their team so each bite checks alliances without changing the herd.
func BenchmarkTickPlayerOwnedFangsBite(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	var owner *client
	s.mu.RLock()
	for _, c := range s.clients {
		owner = c
		break
	}
	s.mu.RUnlock()
	s.mobsMu.Lock()
	s.mobInputs.clientByEntityID = map[int32]*client{owner.player.EntityID: owner}
	s.mobInputs.playerTeams = map[uuid.UUID]string{owner.player.UUID: "t"}
	s.mobsMu.Unlock()
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		id := s.SpawnMob("minecraft:vindicator", x, -60, z)
		s.mobsMu.Lock()
		s.herdOf(w).byID[id].mob.Team = "t"
		s.mobsMu.Unlock()
		s.spawnFangs(fangsCast{w: w, owner: owner.player.EntityID, x: x, y: -60, z: z})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, it := range s.fangs.list {
			f := it.fangs
			it.fangs = entity.NewEvokerFangs(f.ID, f.X, f.Y, f.Z, 0, -7, owner.player.EntityID)
		}
		s.tickFangs()
	}
}

// BenchmarkTickFangsBiteOwned repeats 100 bites by an evoker's fangs on a
// pig each. Resetting the attack models and the victims is timed identically
// on both sides, so every pass reaches the bite and every bite lands.
func BenchmarkTickFangsBiteOwned(b *testing.B) {
	const count = 100
	s, w := newTrackerBenchServer(b)
	owner := s.SpawnMob("minecraft:evoker", 14.5, -60, 14.5)
	var pigs []*entity.Mob
	for i := range count {
		x, z := trackerBenchSpot(i)
		id := s.SpawnMob("minecraft:pig", x, -60, z)
		for _, m := range s.herdOf(w).herd {
			if m.ID == id {
				pigs = append(pigs, m)
			}
		}
		s.spawnFangs(fangsCast{w: w, owner: owner, x: x, y: -60, z: z})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, it := range s.fangs.list {
			f := it.fangs
			it.fangs = entity.NewEvokerFangs(f.ID, f.X, f.Y, f.Z, 0, -7, owner)
		}
		for _, m := range pigs {
			m.Health, m.InvulnTicks, m.Dead = m.MaxHealth, 0, false
		}
		s.tickFangs()
	}
}

// BenchmarkTickPlayerOwnedFangsBiteLanded repeats 100 bites by a player's
// fangs on a pig each, none on the owner's team, so every bite lands. Resetting
// the victims is timed identically on both sides.
func BenchmarkTickPlayerOwnedFangsBiteLanded(b *testing.B) {
	const count = 100
	s, w := newTrackerBenchServer(b)
	var owner *client
	s.mu.RLock()
	for _, c := range s.clients {
		owner = c
		break
	}
	s.mu.RUnlock()
	s.mobsMu.Lock()
	s.mobInputs.clientByEntityID = map[int32]*client{owner.player.EntityID: owner}
	s.mobsMu.Unlock()
	var pigs []*entity.Mob
	for i := range count {
		x, z := trackerBenchSpot(i)
		id := s.SpawnMob("minecraft:pig", x, -60, z)
		for _, m := range s.herdOf(w).herd {
			if m.ID == id {
				pigs = append(pigs, m)
			}
		}
		s.spawnFangs(fangsCast{w: w, owner: owner.player.EntityID, x: x, y: -60, z: z})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, it := range s.fangs.list {
			f := it.fangs
			it.fangs = entity.NewEvokerFangs(f.ID, f.X, f.Y, f.Z, 0, -7, owner.player.EntityID)
		}
		for _, m := range pigs {
			m.Health, m.InvulnTicks, m.Dead = m.MaxHealth, 0, false
		}
		s.tickFangs()
	}
}
