package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestTwoOpeningShellsShoveAMobBetweenThemInOrder: the second shell finds the
// mob where the first one left it.
// vanilla: Shulker.onPeekAmountChange
func TestTwoOpeningShellsShoveAMobBetweenThemInOrder(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	servertest.HoldTicks(t, srv)
	srv.RunLine(`fill -8 99 -8 8 99 8 stone`)
	srv.RunLine(`setblock -1 100 0 stone`)
	srv.RunLine(`setblock 3 100 0 stone`)
	srv.RunLine(`summon shulker 0.5 100 0.5 {AttachFace:4b,Peek:100b,NoAI:1b}`)
	srv.RunLine(`summon shulker 2.5 100 0.5 {AttachFace:5b,Peek:100b,NoAI:1b}`)
	srv.RunLine(`summon pig 1.46 100 0.5 {NoAI:1b}`)
	pig := srv.MobIDsOfKind("minecraft:pig")[0]
	servertest.RunTicks(t, srv, 3)
	x, _, _, _ := srv.MobPos(pig)
	// The first shell pushes the pig into the second one's slab; refiling the
	// pig at its new box gives 1.5044924, the stale entry 1.5083395.
	if math.Abs(x-1.5044924) > 1e-6 {
		t.Errorf("pig x = %.7f after three ticks, want 1.5044924", x)
	}
}
