// Package dragonfight steps the End crystal respawn ritual without server locks or packets.
package dragonfight

import (
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// Ritual is the saved stage, clock and participating crystal identities.
type Ritual struct {
	Stage    string
	Time     int
	Crystals []uuid.UUID
}

// Effects describes the changes requested by one ritual tick.
type Effects struct {
	Beam          *[3]int32
	Event, Finish bool
	Spike         int // -1 when no spike is rebuilt
}

// Start begins the ritual with the crystals found on the podium rim.
func (r *Ritual) Start(ids []uuid.UUID) { r.Stage, r.Time, r.Crystals = "start", 0, ids }

// Abort clears the saved ritual.
func (r *Ritual) Abort() { *r = Ritual{} }

// Step advances one tick. vanilla: DragonRespawnStage
func (r *Ritual) Step(seed int64) Effects {
	fx := Effects{Spike: -1}
	t := r.Time
	r.Time++
	next := func(stage string) { r.Stage, r.Time = stage, 0 }
	beam := func(p [3]int32) { fx.Beam = &p }
	event := func() { fx.Event = true }
	switch r.Stage {
	case "start":
		beam([3]int32{0, 128, 0})
		next("preparing_to_summon_pillars")
	case "preparing_to_summon_pillars":
		if t >= 100 {
			next("summoning_pillars")
		} else if t == 0 || t == 50 || t == 51 || t == 52 || t >= 95 {
			event()
		}
	case "summoning_pillars":
		if i := t / 40; i >= 10 {
			if t%40 == 0 {
				next("summoning_dragon")
			}
		} else if t%40 == 0 {
			beam(world.EndSpikeTop(seed, i))
		} else if t%40 == 39 {
			fx.Spike = i
		}
	case "summoning_dragon":
		if t >= 100 {
			next("")
			r.Crystals = nil
			fx.Finish = true
		} else if t == 0 {
			beam([3]int32{0, 128, 0})
		} else if t < 5 || t >= 80 {
			event()
		}
	}
	return fx
}

// Load restores the ritual from the fight compound.
func (r *Ritual) Load(data nbt.Compound) {
	r.Abort()
	stage, _ := nbt.Get[nbt.String](data, "respawn_stage")
	r.Stage = string(stage)
	time, _ := nbt.IntField(data, "respawn_time")
	r.Time = int(time)
	if list, ok := nbt.Get[nbt.List](data, "respawn_crystals"); ok {
		for _, item := range list.Items {
			if id, ok := nbt.UUIDField(nbt.Compound{"id": item}, "id"); ok {
				r.Crystals = append(r.Crystals, id)
			}
		}
	}
}

// Save writes the ritual fields into the fight compound.
func (r *Ritual) Save(data nbt.Compound) {
	data["respawn_time"] = nbt.Int(r.Time)
	delete(data, "respawn_stage")
	if r.Stage != "" {
		data["respawn_stage"] = nbt.String(r.Stage)
	}
	list := nbt.List{Elem: nbt.TypeIntArray}
	for _, id := range r.Crystals {
		list.Items = append(list.Items, nbt.UUIDToIntArray(id))
	}
	data["respawn_crystals"] = list
}
