package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

const (
	traderKind        = "minecraft:wandering_trader"
	traderDisappeared = "minecraft:entity.wandering_trader.disappeared"
	traderReappeared  = "minecraft:entity.wandering_trader.reappeared"
)

// traderWire is what the client has been told about one trader.
type traderWire struct {
	hand    protocol.ItemStack
	flags   uint8
	hands   uint8
	sounds  []string
	handHad []protocol.ItemStack
}

// read folds one packet into the wire state if it concerns the trader.
func (w *traderWire) read(c *servertest.Client, id int32, packetID int32, data []byte) {
	r := protocol.NewReader(data)
	switch packetID {
	case protocol.IDPlayEntityMetadata:
		if r.ReadVarInt() != id {
			return
		}
		for r.Err() == nil {
			index := r.ReadU8()
			if index == protocol.MetadataEnd {
				break
			}
			fieldType := r.ReadVarInt()
			switch {
			case index == protocol.MetadataIndexEntityFlags && fieldType == protocol.MetadataByte:
				w.flags = r.ReadU8()
			case index == protocol.MetadataIndexHandState && fieldType == protocol.MetadataByte:
				w.hands = r.ReadU8()
			case fieldType == protocol.MetadataParticles:
				for range r.ReadVarInt() {
					if r.ReadVarInt() == protocol.ParticleEntityEffect {
						r.ReadI32()
					}
				}
			default:
				servertest.SkipBodyEntry(c, r, fieldType)
			}
		}
	case protocol.IDPlayEntityEquipment:
		if r.ReadVarInt() != id {
			return
		}
		for {
			raw := r.ReadU8()
			stack, _ := r.ReadSlot()
			if raw&0x7F == protocol.EquipMainHand {
				w.hand = stack
				w.handHad = append(w.handHad, stack)
			}
			if raw&0x80 == 0 {
				break
			}
		}
	case protocol.IDPlaySoundEffect:
		name, _, _, _, _, _, _ := servertest.ReadAmbient(c.T, data)
		w.sounds = append(w.sounds, name)
	}
}

func (w *traderWire) heard(name string) bool {
	return slices.Contains(w.sounds, name)
}

func (w *traderWire) held(item int32) bool {
	for _, s := range w.handHad {
		if s.ItemID == item {
			return true
		}
	}
	return false
}

// TestAWanderingTraderDrinksInvisibilityAtNightAndMilkByDay: at night a
// trader holds the invisibility potion with the hand-active bit set, then
// goes invisible with an empty hand and says disappeared; by day it drinks
// milk, says reappeared and is shown again.
// vanilla: WanderingTrader.registerGoals, UseItemGoal
func TestAWanderingTraderDrinksInvisibilityAtNightAndMilkByDay(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Trader")
	srv.SetDayTime(18000)
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(traderKind, 4.5, 64, 4.5)
	potion := servertest.MustItemID(t, "minecraft:potion")
	milk := servertest.MustItemID(t, "minecraft:milk_bucket")

	var w traderWire
	var activeWithPotion bool
	read := func(pid int32, data []byte) bool {
		w.read(c, id, pid, data)
		if w.hand.ItemID == potion && w.hands&protocol.EntityFlagHandActive != 0 {
			activeWithPotion = true
		}
		return false
	}
	c.ReadTicks(srv, 150, read)
	if !w.held(potion) || !activeWithPotion {
		t.Errorf("no potion in the main hand with the hand-active bit (held %v, active %v)", w.held(potion), activeWithPotion)
	}
	if !w.heard(traderDisappeared) {
		t.Errorf("no %s sound at night; heard %v", traderDisappeared, w.sounds)
	}
	if w.flags&protocol.EntityFlagInvisible == 0 {
		t.Errorf("shared flags %#x at night after the drink, want the invisible bit", w.flags)
	}
	if w.hand.ItemID != 0 || w.hands&protocol.EntityFlagHandActive != 0 {
		t.Errorf("after the drink the hand holds item %d with hand state %#x, want it empty and idle", w.hand.ItemID, w.hands)
	}

	srv.SetDayTime(1000)
	c.ReadTicks(srv, 150, read)
	if !w.held(milk) {
		t.Error("no milk in the main hand by day")
	}
	if !w.heard(traderReappeared) {
		t.Errorf("no %s sound by day; heard %v", traderReappeared, w.sounds)
	}
	if w.flags&protocol.EntityFlagInvisible != 0 {
		t.Errorf("shared flags %#x by day after the milk, want the invisible bit cleared", w.flags)
	}
}
