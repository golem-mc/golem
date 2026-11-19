package redstone

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: NoteBlock.triggerEvent, SoundEvent.DIRECT_STREAM_CODEC, ByteBufCodecs.holder
func TestCustomHeadNotesUseInlineSoundHolders(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Noter", servertest.OpsListing(t, "Noter"))
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 4, 64, 2, "dirt")

	for _, tt := range []struct {
		name   string
		head   string
		holder int32
		sound  string
		pitch  float32
	}{
		{"registered custom sound", `player_head{note_block_sound:"minecraft:entity.pig.ambient"}`, 0, "minecraft:entity.pig.ambient", 1},
		{"custom namespace sound", `player_head{note_block_sound:"example:custom_note"}`, 0, "example:custom_note", 1},
		{"ordinary instrument", "air", int32(registry.SoundBlockNoteBlockHarp), "", float32(math.Pow(2, 1.0/12))},
		{"mob head instrument", "skeleton_skull", int32(registry.SoundBlockNoteBlockImitateSkeleton), "", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if registry.BlockOf(srv.BlockAt(4, 66, 2)) != registry.BlockAir {
				servertest.Setblock(t, c, 4, 66, 2, "air")
			}
			servertest.Setblock(t, c, 4, 65, 2, "note_block[note=12]")
			if tt.head != "air" {
				servertest.Setblock(t, c, 4, 66, 2, tt.head)
			}
			c.Settle()

			tones := 0
			c.UseBlock(4, 65, 2)
			c.SettleAndCollect(func(id int32, data []byte) {
				if id != protocol.IDPlaySoundEffect {
					return
				}
				r := protocol.NewReader(data)
				holder := r.ReadVarInt()
				var name string
				var fixedRange bool
				if holder == 0 {
					name = r.ReadString(32767)
					fixedRange = r.ReadBool()
					if fixedRange {
						r.ReadF32()
					}
				}
				category := r.ReadVarInt()
				x, y, z := r.ReadI32(), r.ReadI32(), r.ReadI32()
				if x != 4*8+4 || y != 65*8+4 || z != 2*8+4 {
					return
				}
				tones++
				if holder != tt.holder || name != tt.sound || fixedRange {
					t.Errorf("sound holder = %d, name %q, fixed range %v; want %d, %q, false", holder, name, fixedRange, tt.holder, tt.sound)
				}
				if category != protocol.SoundCategoryRecord {
					t.Errorf("category = %d, want records (%d)", category, protocol.SoundCategoryRecord)
				}
				volume, pitch := r.ReadF32(), r.ReadF32()
				if volume != 3 || pitch != tt.pitch {
					t.Errorf("volume, pitch = %g, %g; want 3, %g", volume, pitch, tt.pitch)
				}
				r.ReadI64() // The random seed is present, but its value is not fixed.
				if r.Err() != nil || r.Remaining() != 0 {
					t.Errorf("sound packet: %v, %d bytes left", r.Err(), r.Remaining())
				}
			})
			if tones != 1 {
				t.Errorf("heard %d note sounds, want 1", tones)
			}
		})
	}
}
