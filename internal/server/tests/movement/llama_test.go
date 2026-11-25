package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestAChestOnATameLlamaIsFittedAndSpent: a tame llama takes a chest like a
// donkey does.
func TestAChestOnATameLlamaIsFittedAndSpent(t *testing.T) {
	t.Parallel()
	rider, id, srv := equineSummon(t, "minecraft:llama", "{Tame:1b,Strength:3}")
	giveEquineItem(srv, rider, "minecraft:chest")
	rider.UseOn(id)
	chest := -1
	record := func(pid int32, data []byte) {
		if pid == protocol.IDPlayEntityMetadata {
			if c := chestFlag(data); c >= 0 {
				chest = c
			}
		}
	}
	rider.SettleAndCollect(record)
	srv.TickMobs(1)
	rider.SettleAndCollect(record)
	if chest != 1 {
		t.Errorf("chest flag after the click %d, want 1", chest)
	}
}
