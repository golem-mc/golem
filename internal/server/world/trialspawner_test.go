package world

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestTrialSpawnersFollowsPlaceAndBreak: the index of chunks carrying a trial
// spawner gains one on place and loses it on break, and a chunk without any
// is not walked.
func TestTrialSpawnersFollowsPlaceAndBreak(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{0, 0}
	_, _ = w.Acquire(pos)
	defer w.Release(pos)
	if got := w.TrialSpawners(); len(got) != 0 {
		t.Fatalf("%d spawners in an empty world", len(got))
	}
	spawner, ok := registry.FindState("minecraft:trial_spawner", nil)
	if !ok {
		t.Fatal("the registry has no trial spawner")
	}
	w.SetBlock(0, GroundLevel, 0, spawner)
	w.SetBlockEntityTag(0, GroundLevel, 0, "minecraft:trial_spawner", nbt.Compound{})
	if got := w.TrialSpawners(); len(got) != 1 {
		t.Fatalf("%d spawners after placing one, want 1", len(got))
	}
	w.SetBlockEntityTag(0, GroundLevel, 0, "minecraft:trial_spawner", nbt.Compound{"cooldown_ends_at": nbt.Long(3)})
	if got := w.TrialSpawners(); len(got) != 1 {
		t.Fatalf("%d spawners after replacing its data, want 1", len(got))
	}
	w.RemoveBlockEntity(0, GroundLevel, 0)
	if got := w.TrialSpawners(); len(got) != 0 {
		t.Fatalf("%d spawners after breaking it, want 0", len(got))
	}
	if len(w.tickingChunks) != 0 {
		t.Errorf("the index still holds %d chunks", len(w.tickingChunks))
	}
}
