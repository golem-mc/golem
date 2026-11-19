package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
)

// BenchmarkPlayerBlowOnTeammate measures a player's hurt with another
// player's blow as its cause, through the pvp and team friendly-fire gate, the
// two players on one team with friendly fire on so the hit lands.
func BenchmarkPlayerBlowOnTeammate(b *testing.B) {
	s, clients := newIdlePlayersBenchServer(b, 2)
	attacker, victim := clients[0], clients[1]
	for _, c := range clients {
		c.player.UpdateVitals(func(v *player.Vitals) { v.GameMode = protocol.GameModeSurvival })
	}
	s.scoreboard.AddTeam("red", nil)
	s.scoreboard.JoinTeam("red", []string{attacker.player.Name, victim.player.Name})
	cause := &damageCause{entityID: attacker.player.EntityID, x: 0, y: 64, z: 0,
		killer: &rules.Killer{Name: attacker.DisplayName()}}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		victim.player.UpdateVitals(func(v *player.Vitals) {
			v.Health = 20
			v.InvulnerableTicks = 0
		})
		victim.hurt(1, rules.DamageTypePlayerAttack, cause)
	}
	b.StopTimer()
	if health := victim.player.Vitals().Health; health >= 20 {
		b.Fatalf("the blow never reached the teammate: health %v", health)
	}
}
