package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

// A merchant or mount window leaves its block coordinates at the origin and
// must not count as a viewer of a container there.
func TestWindowCoversIgnoresTraderAndMount(t *testing.T) {
	w := &world.World{}
	chest := &openWindow{kind: windowChest, w: w}
	if !chest.covers(w, 0, 0, 0) {
		t.Fatal("a chest window at the origin should cover it")
	}
	for name, o := range map[string]*openWindow{
		"merchant": {kind: windowMerchant, w: w, trader: &merchant{}},
		"horse":    {kind: windowHorse, w: w, mount: &mobEntity{}},
	} {
		if o.covers(w, 0, 0, 0) {
			t.Errorf("%s window covers the container at the origin", name)
		}
	}
}
