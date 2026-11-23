package server

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

func TestPluginMobGettersMatchSavedData(t *testing.T) {
	s, pw := newPluginHandleTestWorld(t)
	const id = "00000000-0000-0000-0000-000000000001"
	h := pluginEntity{host: pw.host, id: uuid.MustParse(id)}
	mob := mobOps{e: h}
	run := func(line, key string) {
		t.Helper()
		if out := s.RunLine(line); len(out) != 1 || out[0].Translate != key {
			t.Fatalf("%s: feedback %+v, want %s", line, out, key)
		}
	}
	checkSaved := func(t *testing.T) {
		t.Helper()
		saved := h.saved()
		if saved == nil {
			t.Fatal("zombie did not save")
		}
		health, _ := nbt.FloatField(saved, "Health")
		absorption, _ := nbt.FloatField(saved, "AbsorptionAmount")
		air, _ := nbt.ShortField(saved, "Air")
		fire, _ := nbt.ShortField(saved, "Fire")
		frozen, _ := nbt.IntField(saved, "TicksFrozen")
		motion, ok := nbt.Get[nbt.List](saved, "Motion")
		if !ok || len(motion.Items) != 3 || motion.Elem != nbt.TypeDouble {
			t.Fatalf("saved Motion = %+v, want three doubles", motion)
		}
		velocity := plugin.Vec3{
			X: float64(motion.Items[0].(nbt.Double)),
			Y: float64(motion.Items[1].(nbt.Double)),
			Z: float64(motion.Items[2].(nbt.Double)),
		}
		for _, read := range []struct {
			name      string
			got, want any
		}{
			{"Health", h.Health(), health},
			{"Absorption", h.Absorption(), absorption},
			{"Air", h.Air(), int(air)},
			{"FireTicks", h.FireTicks(), int(fire)},
			{"FrozenTicks", h.FrozenTicks(), int(frozen)},
			{"NoGravity", h.NoGravity(), nbt.BoolField(saved, "NoGravity")},
			{"Invulnerable", h.Invulnerable(), nbt.BoolField(saved, "Invulnerable")},
			{"AI", mob.AI(), !nbt.BoolField(saved, "NoAI")},
			{"Persistent", mob.Persistent(), nbt.BoolField(saved, "PersistenceRequired")},
			{"Velocity", h.Velocity(), velocity},
		} {
			if read.got != read.want {
				t.Errorf("%s = %v, saved value %v", read.name, read.got, read.want)
			}
		}
	}

	run("summon zombie 0 -60 0 {UUID:[I;0,0,0,1]}", "commands.summon.success")
	run("data merge entity "+id+" {Health:12.5f,AbsorptionAmount:3.25f,Air:-17s,Fire:137s,TicksFrozen:84,NoGravity:1b,Invulnerable:1b,NoAI:1b,PersistenceRequired:1b,Motion:[0.5d,-0.25d,0.125d]}", "commands.data.entity.modified")
	t.Run("merged", checkSaved)
	t.Run("allocations", func(t *testing.T) {
		if allocs := testing.AllocsPerRun(10, func() { _ = h.Health() }); allocs != 0 {
			t.Errorf("Health allocated %g times per call, want 0", allocs)
		}
	})

	run("data merge entity "+id+" {TicksFrozen:-9,NoGravity:0b,Invulnerable:0b,NoAI:0b,PersistenceRequired:0b}", "commands.data.entity.modified")
	t.Run("cleared", checkSaved)

	// A live counter can exceed the short range without going through a load.
	s.mobsMu.Lock()
	live := pw.dim.mobs.byUUID[h.id]
	live.AirSupply, live.FireTicks = 40_000, 50_000
	s.mobsMu.Unlock()
	t.Run("short limits", checkSaved)

	run("kill "+id, "commands.kill.success.single")
	if h.IsValid() {
		t.Fatal("zombie is still in the live lookup after kill")
	}
	t.Run("dying", checkSaved)
}
