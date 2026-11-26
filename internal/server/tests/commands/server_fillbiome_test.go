package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func fillBiomeText(vals ...int64) []protocol.Text {
	out := make([]protocol.Text, len(vals))
	for i := range vals {
		out[i] = protocol.Text{Number: &vals[i]}
	}
	return out
}

func TestFillBiomeRewritesCellsAndResendsBiomes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Painter", servertest.OpsListing(t, "Painter"))
	servertest.HoldTicks(t, srv)

	before := srv.OverworldNoiseBiomeAt(8, 64, 0)
	// Cells of the section at y 64..79 (the ninth from the world's bottom at
	// -64), in wire order: x fastest, then z, then y.
	var original [64]string
	for i := range original {
		original[i] = srv.OverworldNoiseBiomeAt(int32(i&3)*4, 64+int32(i>>4)*4, int32(i>>2&3)*4)
	}
	c.ChatCommand("fillbiome 1 65 2 7 70 6 minecraft:desert")
	// The corners snap down to cell origins: x 0..4, y 64..68, z 0..4.
	servertest.AwaitCommandFeedbackTextWithin(t, c, fillFeedbackWait, "commands.fillbiome.success.count",
		fillBiomeText(8, 0, 64, 0, 4, 68, 4)...)

	data := c.ExpectWithinTicks(srv, 1, protocol.IDPlayChunksBiomes)
	r := protocol.NewReader(data)
	if n := r.ReadVarInt(); n != 1 {
		t.Fatalf("chunks in packet = %d, want 1", n)
	}
	if pos := r.ReadI64(); pos != 0 {
		t.Fatalf("chunk pos = %#x, want chunk 0,0", pos)
	}
	// The sections' containers come as one length-prefixed byte array.
	r = protocol.NewReader(r.ReadBytes(1 << 21))
	var cells [64]int32
	for sec := 0; r.Remaining() > 0 && r.Err() == nil; sec++ {
		got := readBiomeContainer(r)
		if sec == 8 {
			cells = got
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("decoding biome sections: %v", err)
	}
	desert, _ := registry.BiomeByName("minecraft:desert")
	for i, id := range cells {
		x, y, z := i&3, i>>4, i>>2&3
		want := original[i]
		if x < 2 && z < 2 && y < 2 {
			want = "minecraft:desert"
		}
		if got := registry.Biome(id).Name(); got != want {
			t.Errorf("wire cell %d (x %d y %d z %d) = %q (id %d, desert %d), want %q", i, x, y, z, got, id, desert, want)
		}
	}
	for _, p := range [][3]int32{{0, 64, 0}, {4, 68, 4}, {3, 67, 3}} {
		if got := srv.OverworldNoiseBiomeAt(p[0], p[1], p[2]); got != "minecraft:desert" {
			t.Errorf("biome at %v = %q, want desert", p, got)
		}
	}
	for _, p := range [][3]int32{{8, 64, 0}, {0, 72, 0}, {0, 60, 0}, {0, 64, 8}} {
		if got := srv.OverworldNoiseBiomeAt(p[0], p[1], p[2]); got == "minecraft:desert" && before != "minecraft:desert" {
			t.Errorf("biome at %v changed to desert", p)
		}
	}
}

// readBiomeContainer decodes one section's 64-cell biome container: a single
// value, a palette with packed indices, or packed raw IDs.
// vanilla: PalettedContainer.write
func readBiomeContainer(r *protocol.Reader) [64]int32 {
	var cells [64]int32
	bitsPer := int(r.ReadU8())
	if bitsPer == 0 {
		v := r.ReadVarInt()
		for i := range cells {
			cells[i] = v
		}
		return cells
	}
	var palette []int32
	if bitsPer <= 3 {
		palette = make([]int32, r.ReadVarInt())
		for i := range palette {
			palette[i] = r.ReadVarInt()
		}
	}
	perLong := 64 / bitsPer
	longs := make([]uint64, (64+perLong-1)/perLong)
	for i := range longs {
		longs[i] = uint64(r.ReadI64())
	}
	for i := range cells {
		v := longs[i/perLong] >> (uint(i%perLong) * uint(bitsPer)) & (1<<uint(bitsPer) - 1)
		if palette != nil {
			cells[i] = palette[v]
		} else {
			cells[i] = int32(v)
		}
	}
	return cells
}

// TestFillbiomeSameBiomeIsRefused checks that only cells holding another
// biome count: a box already of the biome is refused and resends nothing,
// and the result is the changed count. vanilla: FillBiomeCommand.fill
func TestFillbiomeSameBiomeIsRefused(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Painter", servertest.OpsListing(t, "Painter"))
	srv.StopTick()

	srv.SetOverworldBiome(0, 0, "minecraft:plains")
	c.ChatCommand("fillbiome 0 64 0 7 64 0 minecraft:plains")
	servertest.AwaitCommandError(t, c, "commands.fillbiome.no_changes")
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayChunksBiomes))

	c.ChatCommand("fillbiome 0 64 0 0 64 0 minecraft:desert")
	servertest.AwaitCommandFeedbackTextWithin(t, c, fillFeedbackWait, "commands.fillbiome.success.count",
		fillBiomeText(1, 0, 64, 0, 0, 64, 0)...)
	// Of the two cells x 0 and x 4, only x 4 still needs the desert.
	c.ChatCommand("execute store result storage t:s r int 1 run fillbiome 0 64 0 7 64 0 minecraft:desert")
	servertest.AwaitCommandFeedbackTextWithin(t, c, fillFeedbackWait, "commands.fillbiome.success.count",
		fillBiomeText(1, 0, 64, 0, 4, 64, 0)...)
	c.ChatCommand("data get storage t:s r")
	servertest.AwaitCommandFeedback(t, c, "commands.data.storage.query", "t:s", "1")
}

// fillFeedbackWait is how long a setup fill waits for its success feedback:
// one fill places up to a thousand blocks, and a runner sharing its cores
// with the rest of the packages can spend longer on a fill than the default
// command wait allows.
const fillFeedbackWait = time.Minute
