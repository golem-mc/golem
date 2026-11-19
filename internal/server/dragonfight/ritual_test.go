package dragonfight

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

func TestRitualStagesAndSavedClock(t *testing.T) {
	var r Ritual
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	r.Start(ids)
	spikes, events := 0, 0
	for tick := 1; tick <= 604; tick++ {
		data := nbt.Compound{}
		r.Save(data)
		var loaded Ritual
		loaded.Load(data)
		if !reflect.DeepEqual(r, loaded) {
			t.Fatalf("tick %d lost saved state", tick)
		}
		fx := loaded.Step(1)
		r = loaded
		if fx.Spike >= 0 {
			if fx.Spike != spikes {
				t.Fatalf("spike=%d want %d", fx.Spike, spikes)
			}
			spikes++
		}
		if fx.Event {
			events++
		}
		if fx.Finish != (tick == 604) {
			t.Fatalf("completion at tick %d: %v", tick, fx.Finish)
		}
	}
	if spikes != 10 || events != 33 || r.Stage != "" || len(r.Crystals) != 0 {
		t.Fatalf("spikes=%d events=%d state=%+v", spikes, events, r)
	}
	r.Start(ids)
	r.Abort()
	if !reflect.DeepEqual(r, Ritual{}) {
		t.Fatal("abort retained ritual state")
	}
}
