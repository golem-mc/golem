package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The goatFlags bits (screaming, each horn gone) are named so that zero is
// every field's default.
const (
	goatFlagScreaming = 1 << iota
	goatFlagNoLeftHorn
	goatFlagNoRightHorn
)

func goatMetadataFlags(m *entity.Mob) uint8 {
	var flags uint8
	if m.Type != entity.TypeGoat {
		return 0
	}
	if m.Goat.Screaming {
		flags |= goatFlagScreaming
	}
	if m.Goat.NoLeftHorn {
		flags |= goatFlagNoLeftHorn
	}
	if m.Goat.NoRightHorn {
		flags |= goatFlagNoRightHorn
	}
	return flags
}

// putGoatFlags writes the goat's booleans that differ between was and now.
// vanilla: Goat.DATA_IS_SCREAMING_GOAT, DATA_HAS_LEFT_HORN, DATA_HAS_RIGHT_HORN
func putGoatFlags(b *protocol.Buffer, now, was uint8) {
	put := func(bit uint8, index uint8, value bool) {
		if now&bit != was&bit {
			b.PutMetadata(index, protocol.MetadataBoolean)
			b.PutBool(value)
		}
	}
	put(goatFlagScreaming, protocol.MetadataIndexGoatScreaming, now&goatFlagScreaming != 0)
	put(goatFlagNoLeftHorn, protocol.MetadataIndexGoatLeftHorn, now&goatFlagNoLeftHorn == 0)
	put(goatFlagNoRightHorn, protocol.MetadataIndexGoatRightHorn, now&goatFlagNoRightHorn == 0)
}

// goatHorn is a horn a goat snapped off during the tick.
type goatHorn struct {
	w          *world.World
	event      entity.MobEvent
	instrument string
}

// dropGoatHorn drops the goat horn item of the goat's tag instrument.
// vanilla: Goat.dropHorn, Goat.createHorn
func (s *Server) dropGoatHorn(h goatHorn) {
	stack := protocol.ItemStack{ItemID: int32(registry.ItemGoatHorn), Count: 1}
	stack.SetInstrument(registry.MustID("minecraft:instrument", h.instrument))
	ev := h.event
	s.spawnItem(h.w, stack, ev.X, ev.Y, ev.Z, ev.VX, ev.VY, ev.VZ, 0)
}

// milkGoat fills the held bucket from a grown goat.
// vanilla: Goat.mobInteract
func (c *client) milkGoat(ent *mobEntity, hand int32) error {
	c.srv.mobsMu.Lock()
	screaming := ent.mob.Goat.Screaming
	c.srv.mobsMu.Unlock()
	sound := registry.SoundEntityGoatMilk
	if screaming {
		sound = registry.SoundEntityGoatScreamingMilk
	}
	return c.milkAnimal(ent, hand, sound)
}
