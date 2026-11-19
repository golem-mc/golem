package world

import (
	"github.com/golem-mc/golem/registry"
	"testing"
)

func TestFrogspawnBooksOneHatchAndChecksSupport(t *testing.T) {
	for _, supported := range []bool{true, false} {
		t.Run(map[bool]string{true: "supported", false: "unsupported"}[supported], func(t *testing.T) {
			w := newTestWorld(t, NewFlatGenerator())
			t.Cleanup(func() { _ = w.Close() })
			w.Acquire(ChunkPos{})
			p := NewPhysics(w)
			if supported {
				setBlock(t, w, p, 2, 63, 2, waterByLevel[0])
			}
			spawn := registry.BlockFrogspawn.DefaultState()
			setBlock(t, w, p, 2, 64, 2, spawn)
			key := blockKey{2, 64, 2}
			due := int64(0)
			for _, v := range p.queue {
				if v.event == frogspawnHatchEvent {
					due = v.due
				}
			}
			if due < 3600 || due >= 12000 {
				t.Fatalf("hatch due=%d", due)
			}
			p.NotifyChanged(2, 64, 2, spawn)
			if !p.frogspawnBooked[key] {
				t.Fatal("missing booked hatch")
			}
			p.Tick(due - 1)
			if w.BlockAt(2, 64, 2) != spawn {
				t.Fatal("hatched early")
			}
			p.Tick(due)
			h := p.TakeTadpoleHatches()
			if supported && (len(h) < 2 || len(h) > 5) {
				t.Fatalf("hatches=%v", h)
			}
			if !supported && len(h) != 0 {
				t.Fatal("hatched unsupported spawn")
			}
			if w.BlockAt(2, 64, 2) != airState || p.frogspawnBooked[key] {
				t.Fatal("hatch left block or booking")
			}
			if len(p.TakeTadpoleHatches()) != 0 {
				t.Fatal("hatch queue did not drain")
			}
		})
	}
}
