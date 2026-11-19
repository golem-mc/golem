package storage

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestABatKeepsItsRestingFlagOverARestart saves a hanging and a flying NoAI
// bat and finds each resting as its BatFlags said after a restart.
// vanilla: Bat.addAdditionalSaveData, Bat.readAdditionalSaveData
func TestABatKeepsItsRestingFlagOverARestart(t *testing.T) {
	t.Parallel()
	again, _ := restartAround(t, func(srv *server.Server) {
		srv.RunLine(`summon minecraft:bat 2.5 100 3.5 {NoAI:1b,BatFlags:1b}`)
		srv.RunLine(`summon minecraft:bat 4.5 100 3.5 {NoAI:1b,BatFlags:0b}`)
	}, nil)
	var bats []server.TestMob
	servertest.UntilTicks(t, again, 40, "the bats woke", func() bool {
		bats = bats[:0]
		for _, m := range again.Mobs() {
			if m.Kind == "minecraft:bat" {
				bats = append(bats, m)
			}
		}
		return len(bats) == 2
	})
	for _, b := range bats {
		if want := b.X < 3.5; b.Resting != want {
			t.Errorf("the bat at x=%v is resting %v after a restart, want %v", b.X, b.Resting, want)
		}
	}
}
