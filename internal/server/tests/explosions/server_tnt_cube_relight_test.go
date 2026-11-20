package explosions

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestATntCubeRelightsDrain checks that the relights a cube of TNT books
// through its craters all finish and the loop keeps its pace while they run.
func TestATntCubeRelightsDrain(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	// The cube sits across the ground so its craters change lit columns.
	for x := range 6 {
		for y := range 6 {
			for z := range 6 {
				srv.SpawnTNT(10.5+float64(x), 58+float64(y), 10.5+float64(z), 1)
			}
		}
	}
	start := time.Now()
	servertest.RunTicks(t, srv, 5)
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("5 ticks of the cube took %v", d)
	}
	done := make(chan struct{})
	go func() { srv.WaitRelights(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the cube's relights never finished")
	}
	if srv.RelightsRun() == 0 {
		t.Fatal("the cube's craters booked no relight")
	}
}
