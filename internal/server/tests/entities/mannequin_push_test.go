package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func TestAMannequinPushScanSurvivesABeeEnteringItsNest(t *testing.T) {
	t.Parallel()
	for _, mannequinFirst := range []bool{true, false} {
		name := "mannequin after bee enters"
		if mannequinFirst {
			name = "mannequin before bee enters"
		}
		t.Run(name, func(t *testing.T) {
			srv, c := joinMannequinOp(t, "NestWatcher")
			c.ChatCommand("setblock 5 64 2 bee_nest")
			if mannequinFirst {
				c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b}")
				awaitMannequin(t, c, srv)
			}
			// The bee's AI removes its tracking record when it enters the nest.
			c.ChatCommand("summon bee 5.5 64.5 1.5 {HasNectar:1b,hive_pos:[I;5,64,2]}")
			c.Settle()
			servertest.UntilTicks(t, srv, 20, "bee entering its nest", func() bool {
				tag, _ := srv.BlockEntityTagAt(5, 64, 2)
				bees, _ := nbt.Get[nbt.List](tag, "bees")
				return len(bees.Items) == 1
			})
			if !mannequinFirst {
				c.ChatCommand("summon mannequin 3.5 64 3.5 {immovable:1b}")
				awaitMannequin(t, c, srv)
			}
			servertest.RunTicks(t, srv, 1)
			if pos := mannequinTravelPosition(t, mannequinTravelData(t, c, srv)); pos != [3]float64{3.5, 64, 3.5} {
				t.Fatalf("immovable mannequin position = %v", pos)
			}
		})
	}
}
