package movement

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// llamaMeta reads a llama's strength and colour out of a Set Entity Metadata
// packet, -1 for a field the packet does not carry.
func llamaMeta(data []byte) (strength, variant int) {
	strength, variant = -1, -1
	r := protocol.NewReader(data)
	r.ReadVarInt()
	for {
		index := r.ReadU8()
		if index == protocol.MetadataEnd || r.Err() != nil {
			return
		}
		switch r.ReadVarInt() {
		case protocol.MetadataByte:
			r.ReadU8()
		case protocol.MetadataVarInt:
			v := int(r.ReadVarInt())
			switch index {
			case protocol.MetadataIndexLlamaStrength:
				strength = v
			case protocol.MetadataIndexLlamaVariant:
				variant = v
			}
		case protocol.MetadataFloat:
			r.ReadF32()
		case protocol.MetadataBoolean:
			r.ReadBool()
		default:
			return
		}
	}
}

// TestLlamasSpawnTellingTheirStrengthAndColour: a summoned llama or trader
// llama tells the strength and colour it was saved with.
// vanilla: Llama.defineSynchedData
func TestLlamasSpawnTellingTheirStrengthAndColour(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:llama", "minecraft:trader_llama"} {
		rider, _, _ := equineSummon(t, kind, "{Strength:4,Variant:2}")
		strength, variant := -1, -1
		for id, data := range rider.Packets(servertest.ExpectTimeout) {
			if id != protocol.IDPlayEntityMetadata {
				continue
			}
			if strength, variant = llamaMeta(data); strength >= 0 {
				break
			}
		}
		if strength != 4 || variant != 2 {
			t.Errorf("%s spawned with strength %d colour %d, want 4 and 2", kind, strength, variant)
		}
	}
}

// TestAChestOnATameLlamaIsFittedAndSpent: a tame llama takes a chest like a
// donkey does.
func TestAChestOnATameLlamaIsFittedAndSpent(t *testing.T) {
	t.Parallel()
	rider, id, srv := equineSummon(t, "minecraft:llama", "{Tame:1b,Strength:3}")
	giveEquineItem(srv, rider, "minecraft:chest")
	rider.UseOn(id)
	chest := -1
	record := func(pid int32, data []byte) {
		if pid == protocol.IDPlayEntityMetadata {
			if c := chestFlag(data); c >= 0 {
				chest = c
			}
		}
	}
	rider.SettleAndCollect(record)
	srv.TickMobs(1)
	rider.SettleAndCollect(record)
	if chest != 1 {
		t.Errorf("chest flag after the click %d, want 1", chest)
	}
}
