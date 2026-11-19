package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The allay accelerates by the control's current speed, not its base speed.
func TestAllayFlyInputAcceleratesByTheControlSpeed(t *testing.T) {
	speed := kinds[TypeAllay].speed
	for _, factor := range []float64{1, 2.25} {
		m := NewMob(TypeAllay, 1, 0, 64, 0, 1)
		m.Yaw = 0
		m.flyInput(factor, 0)
		want := factor * speed * factor * speed
		if got := math.Abs(m.VZ); math.Abs(got-want) > 1e-12 {
			t.Fatalf("factor %v: acceleration %v, want %v", factor, got, want)
		}
	}
}

func TestAllayNoteMemoryUsesBlockChessboardDistance(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mobX    float64
		note    GlobalPos
		inRange bool
	}{
		{"same block", 0.9, GlobalPos{"minecraft:overworld", 0, 64, 0}, true},
		{"diagonal boundary", 0.9, GlobalPos{"minecraft:overworld", 1024, 1088, 1024}, true},
		{"negative boundary", 0.9, GlobalPos{"minecraft:overworld", -1024, -960, -1024}, true},
		{"positive X beyond", 0.9, GlobalPos{"minecraft:overworld", 1025, 64, 0}, false},
		{"negative X beyond", 0.9, GlobalPos{"minecraft:overworld", -1025, 64, 0}, false},
		{"positive Y beyond", 0.9, GlobalPos{"minecraft:overworld", 0, 1089, 0}, false},
		{"negative Y beyond", 0.9, GlobalPos{"minecraft:overworld", 0, -961, 0}, false},
		{"positive Z beyond", 0.9, GlobalPos{"minecraft:overworld", 0, 64, 1025}, false},
		{"negative Z beyond", 0.9, GlobalPos{"minecraft:overworld", 0, 64, -1025}, false},
		{"fraction within boundary block", 1024.99, GlobalPos{"minecraft:overworld", 0, 64, 0}, true},
		{"negative fraction floors down", -0.01, GlobalPos{"minecraft:overworld", 1024, 64, 0}, false},
		{"other dimension", 0.9, GlobalPos{"minecraft:the_nether", 0, 64, 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			n := tc.note
			w.set(n.X, n.Y, n.Z, registry.BlockStates["minecraft:note_block"])
			m := NewMob(TypeAllay, 1, tc.mobX, 64.9, 0.9, 1)
			m.Village = fakeVillage{w: w}
			m.allay.note, m.allay.noteTicks = &n, allayNoteMemory
			x, y, z, ok := m.allayDeposit(&tickView{w: w})
			if ok != tc.inRange || (m.allay.note != nil) != tc.inRange {
				t.Fatalf("deposit found = %v, note = %v; want in range = %v", ok, m.allay.note, tc.inRange)
			}
			if ok && (x != float64(n.X)+0.5 || y != float64(n.Y+1) || z != float64(n.Z)+0.5) {
				t.Fatalf("deposit = (%v, %v, %v), want the note block's top", x, y, z)
			}
		})
	}
}
