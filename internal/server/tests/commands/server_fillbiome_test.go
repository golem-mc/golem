package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
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
	srv.StopTick()

	before := srv.OverworldNoiseBiomeAt(8, 64, 0)
	c.ChatCommand("fillbiome 1 65 2 7 70 6 minecraft:desert")
	// The corners snap down to cell origins: x 0..4, y 64..68, z 0..4.
	servertest.AwaitCommandFeedbackTextWithin(t, c, fillFeedbackWait, "commands.fillbiome.success.count",
		fillBiomeText(8, 0, 64, 0, 4, 68, 4)...)

	data := c.AwaitPlay(protocol.IDPlayChunksBiomes)
	r := protocol.NewReader(data)
	if n := r.ReadVarInt(); n != 1 {
		t.Fatalf("chunks in packet = %d, want 1", n)
	}
	if pos := r.ReadI64(); pos != 0 {
		t.Fatalf("chunk pos = %#x, want chunk 0,0", pos)
	}
	if body := r.ReadBytes(1 << 21); len(body) == 0 {
		t.Fatal("empty biome data")
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

func TestFillBiomeReplaceFilter(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Painter", servertest.OpsListing(t, "Painter"))
	srv.StopTick()

	srv.SetOverworldBiome(0, 0, "minecraft:plains")
	c.ChatCommand("fillbiome 0 64 0 3 64 3 minecraft:desert")
	servertest.AwaitCommandFeedbackTextWithin(t, c, fillFeedbackWait, "commands.fillbiome.success.count",
		fillBiomeText(1, 0, 64, 0, 0, 64, 0)...)
	// Only the desert cell matches the filter; the plains cells next to it stay.
	c.ChatCommand("fillbiome 0 64 0 7 64 0 minecraft:badlands replace minecraft:desert")
	servertest.AwaitCommandFeedbackTextWithin(t, c, fillFeedbackWait, "commands.fillbiome.success.count",
		fillBiomeText(1, 0, 64, 0, 4, 64, 0)...)
	if got := srv.OverworldNoiseBiomeAt(0, 64, 0); got != "minecraft:badlands" {
		t.Errorf("filtered cell = %q, want badlands", got)
	}
	if got := srv.OverworldNoiseBiomeAt(4, 64, 0); got != "minecraft:plains" {
		t.Errorf("unmatched cell = %q, want plains", got)
	}
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

func TestFillBiomeTooBig(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Painter", servertest.OpsListing(t, "Painter"))
	srv.StopTick()

	srv.SetGameRuleValue(world.RuleMaxBlockMods.String(), 10)
	limit, volume := int64(10), int64(5*5*5)
	c.ChatCommand("fillbiome 0 64 0 4 68 4 minecraft:desert")
	servertest.AwaitCommandErrorText(t, c, "commands.fillbiome.toobig",
		protocol.Text{Number: &limit}, protocol.Text{Long: &volume})
}

// fillFeedbackWait is how long a setup fill waits for its success feedback:
// one fill places up to a thousand blocks, and a runner sharing its cores
// with the rest of the packages can spend longer on a fill than the default
// command wait allows.
const fillFeedbackWait = time.Minute
