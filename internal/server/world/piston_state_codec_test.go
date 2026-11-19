package world

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: PistonMovingBlockEntity.saveAdditional, BlockState.CODEC
func TestMovingPistonStateCodec(t *testing.T) {
	t.Parallel()
	for _, state := range []uint16{
		registry.BlockOakStairs.DefaultState(),
		registry.MustState("minecraft:oak_stairs", map[string]string{"facing": "east"}),
	} {
		be := newBlockEntity(KindPiston, 4, 64, 0)
		be.MovedState = state
		saved := be.EncodeNBT()
		if state == registry.BlockOakStairs.DefaultState() {
			if saved["blockState"] != nbt.String("minecraft:oak_stairs") {
				t.Fatalf("default state = %v, want a bare name", saved["blockState"])
			}
		} else if _, ok := saved["blockState"].(nbt.Compound); !ok {
			t.Fatalf("non-default state = %v, want a compound", saved["blockState"])
		}
		back, ok := decodeBlockEntity(saved)
		if !ok || back.MovedState != state {
			t.Fatalf("piston state did not survive a reload: %+v", back)
		}
		if again := back.EncodeNBT(); !reflect.DeepEqual(again, saved) {
			t.Fatalf("piston saved differently after reload: %v", again)
		}
	}
}
