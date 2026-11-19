package registry

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestStoredAirStackIsEmpty checks a stored air stack reads as empty with no
// count, as ItemStack.isEmpty does.
func TestStoredAirStackIsEmpty(t *testing.T) {
	stack, ok := StoredStack(nbt.Compound{"id": nbt.String("air")})
	if !ok || !stack.Empty() {
		t.Fatalf("StoredStack(air) = %+v, %v; want empty", stack, ok)
	}
}
