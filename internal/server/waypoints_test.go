package server

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
)

func TestForgetWaypointsPublishesAfterDeletion(t *testing.T) {
	t.Parallel()
	w := &world.World{}
	c := &client{player: &player.Player{}}
	viewer := &client{player: &player.Player{}}
	viewer.dim.Store(&dimension{w: w})
	c.waypoints.set(viewer, &waypointLink{viewer: viewer})
	s := &Server{}
	// Stop forgetting before either deletion loop, while the tick can still
	// consume the flag. The list lock is not needed for the tick's Swap.
	s.mu.Lock()
	done := make(chan struct{})
	go func() {
		s.forgetWaypoints(c, w)
		close(done)
	}()
	// Wait for the goroutine to reach the held lock, rather than relying on a
	// scheduling delay to put the tick's consumption after forgetting starts.
	deadline := time.Now().Add(5 * time.Second)
	blocked := false
	for time.Now().Before(deadline) {
		buf := make([]byte, 1<<20)
		stacks := string(buf[:runtime.Stack(buf, true)])
		for stack := range strings.SplitSeq(stacks, "\n\n") {
			if strings.Contains(stack, "(*Server).forgetWaypoints") && strings.Contains(stack, ".RLock(") {
				blocked = true
				break
			}
		}
		if blocked {
			break
		}
		runtime.Gosched()
	}
	c.waypointsForgot.Swap(false)
	s.mu.Unlock()
	<-done
	if !blocked {
		t.Fatal("forgetting did not reach the held player-list lock")
	}
	if len(trackedWatchers(c.waypoints)) != 0 {
		t.Fatal("forgetting left the waypoint link")
	}
	if !c.waypointsForgot.Swap(false) {
		t.Fatal("deletion left no pending repair after a tick consumed the flag")
	}
}
