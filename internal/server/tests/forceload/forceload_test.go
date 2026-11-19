package forceload

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

const overworldTickets = "dimensions/minecraft/overworld/data/minecraft/chunk_tickets.dat"

func joined(lines []protocol.Text) string {
	var parts []string
	for _, l := range lines {
		parts = append(parts, l.Plain())
	}
	return strings.Join(parts, "\n")
}

func forceloadConfig(t *testing.T) *config.Config {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	return cfg
}

// TestForceloadKeepsAMobTickingWithNobodyOnline checks that a forced chunk
// stays loaded with no player in the world, so a mob standing in it keeps
// falling.
func TestForceloadKeepsAMobTickingWithNobodyOnline(t *testing.T) {
	t.Parallel()
	cfg := forceloadConfig(t)
	srv := servertest.NewTestServer(t, cfg)

	if out := joined(srv.RunLine("forceload add 0 0")); !strings.Contains(out, "Marked chunk [0, 0] in minecraft:overworld to be force loaded") {
		t.Fatalf("forceload add answered %q", out)
	}
	if !srv.ChunkLoadedInDimension("minecraft:overworld", 0, 0) {
		t.Fatal("the forced chunk is not loaded")
	}
	id := srv.SpawnMob("minecraft:cow", 8.5, 120, 8.5)
	_, y0, _, ok := srv.MobPos(id)
	if !ok {
		t.Fatal("the mob did not stand up")
	}
	// Held, the fall is counted in the loop's own ticks: a free fall of a
	// block and more takes under ten.
	servertest.HoldTicks(t, srv)
	servertest.UntilTicks(t, srv, 40, "the cow falling in the forced chunk", func() bool {
		_, y, _, ok := srv.MobPos(id)
		return ok && y < y0-1
	})
	if !srv.ChunkLoadedInDimension("minecraft:overworld", 0, 0) {
		t.Error("the forced chunk unloaded")
	}

	if out := joined(srv.RunLine("forceload add 0 0")); !strings.Contains(out, "No chunks were marked for force loading") {
		t.Errorf("a repeated add answered %q", out)
	}
	if out := joined(srv.RunLine("forceload query 3 4")); !strings.Contains(out, "Chunk at [0, 0] in minecraft:overworld is marked for force loading") {
		t.Errorf("query answered %q", out)
	}
	if out := joined(srv.RunLine("forceload query 16 0")); !strings.Contains(out, "Chunk at [1, 0] in minecraft:overworld is not marked for force loading") {
		t.Errorf("query of an unforced chunk answered %q", out)
	}
	if out := joined(srv.RunLine("forceload add 0 0 -1 -1")); !strings.Contains(out, "Marked 3 chunks in minecraft:overworld from [-1, -1] to [0, 0] to be force loaded") {
		t.Errorf("add of an area answered %q", out)
	}
	if out := joined(srv.RunLine("forceload query")); !strings.Contains(out, "4 force loaded chunks were found in minecraft:overworld at: [0, -1], [-1, -1], [0, 0], [-1, 0]") {
		t.Errorf("list answered %q", out)
	}
	if out := joined(srv.RunLine("forceload add 0 0 4000 4000")); !strings.Contains(out, "Too many chunks in the specified area (maximum 256, but specified") {
		t.Errorf("a huge area answered %q", out)
	}
	if out := joined(srv.RunLine("forceload remove all")); !strings.Contains(out, "Unmarked all force loaded chunks in minecraft:overworld") {
		t.Errorf("remove all answered %q", out)
	}
	if out := joined(srv.RunLine("forceload query")); !strings.Contains(out, "No force loaded chunks were found in minecraft:overworld") {
		t.Errorf("an empty list answered %q", out)
	}
	if out := joined(srv.RunLine("forceload remove 0 0")); !strings.Contains(out, "No chunks were removed from force loading") {
		t.Errorf("a remove of nothing answered %q", out)
	}
	srv.FlushSaves()
	if srv.ChunkLoadedInDimension("minecraft:overworld", 0, 0) {
		t.Error("the chunk stayed loaded after it was unforced")
	}
}

// TestForceloadSurvivesARestart checks the ticket file: the forced chunk is
// written in the shape a vanilla server reads, and a restarted server loads
// it again without anyone joining.
func TestForceloadSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := forceloadConfig(t)
	srv := servertest.NewTestServer(t, cfg)
	srv.RunLine("forceload add 32 -16")
	srv.Shutdown()

	data := readTickets(t, filepath.Join(cfg.LevelName, overworldTickets))
	tickets, _ := nbt.Get[nbt.List](data, "tickets")
	if len(tickets.Items) != 1 {
		t.Fatalf("tickets = %v, want the one forced chunk", tickets.Items)
	}
	entry := tickets.Items[0].(nbt.Compound)
	if typ, _ := nbt.Get[nbt.String](entry, "type"); typ != "minecraft:forced" {
		t.Errorf("type = %q, want minecraft:forced", typ)
	}
	if level, _ := nbt.Get[nbt.Int](entry, "level"); level != 31 {
		t.Errorf("level = %d, want 31", level)
	}
	if pos, _ := nbt.Get[nbt.IntArray](entry, "chunk_pos"); len(pos) != 2 || pos[0] != 2 || pos[1] != -1 {
		t.Errorf("chunk_pos = %v, want [2 -1]", pos)
	}

	restarted := *cfg
	again := servertest.NewTestServer(t, &restarted)
	if !again.ChunkLoadedInDimension("minecraft:overworld", 32, -16) {
		t.Error("the restarted server did not load the forced chunk")
	}
	if out := joined(again.RunLine("forceload query")); !strings.Contains(out, "A force loaded chunk was found in minecraft:overworld at: [2, -1]") {
		t.Errorf("list answered %q", out)
	}
}

// TestForceloadReadsAVanillaTicketFile checks that the forced chunks a
// vanilla server wrote are loaded, and that a ticket of another type is
// written back when the file is rewritten.
func TestForceloadReadsAVanillaTicketFile(t *testing.T) {
	t.Parallel()
	cfg := forceloadConfig(t)
	file := filepath.Join(cfg.LevelName, overworldTickets)
	servertest.WritePlayerFile(t, file, nbt.Compound{"DataVersion": nbt.Int(4903), "data": nbt.Compound{"tickets": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"chunk_pos": nbt.IntArray{5, 6}, "type": nbt.String("minecraft:forced"), "level": nbt.Int(31)},
		nbt.Compound{"chunk_pos": nbt.IntArray{7, 8}, "type": nbt.String("minecraft:portal"), "level": nbt.Int(31), "ticks_left": nbt.Long(200)},
	}}}})
	srv := servertest.NewTestServer(t, cfg)
	if !srv.ChunkLoadedInDimension("minecraft:overworld", 80, 96) {
		t.Fatal("the vanilla forced chunk is not loaded")
	}
	srv.RunLine("forceload add 0 0")
	srv.Shutdown()

	data := readTickets(t, file)
	tickets, _ := nbt.Get[nbt.List](data, "tickets")
	if len(tickets.Items) != 3 {
		t.Fatalf("tickets = %v, want the two forced chunks and the portal ticket", tickets.Items)
	}
	portal := false
	for _, item := range tickets.Items {
		if typ, _ := nbt.Get[nbt.String](item.(nbt.Compound), "type"); typ == "minecraft:portal" {
			portal = true
		}
	}
	if !portal {
		t.Error("the portal ticket was dropped")
	}
}

// readTickets reads a chunk_tickets.dat's data compound.
func readTickets(t *testing.T, path string) nbt.Compound {
	t.Helper()
	root, _, err := servertest.ReadPlayerFileData(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	data, _ := nbt.Get[nbt.Compound](root, "data")
	return data
}

// copperBlock is the state a bare copper block is placed as.
func copperBlock(t *testing.T) uint16 {
	t.Helper()
	id, ok := registry.FindState("minecraft:copper_block", nil)
	if !ok {
		t.Fatal("no copper block state")
	}
	return id
}

// TestForcedChunkRandomTicksWithNobodyOnline checks that a forced chunk takes
// random ticks with no player in the dimension: a volume of copper weathers.
func TestForcedChunkRandomTicksWithNobodyOnline(t *testing.T) {
	t.Parallel()
	srv := servertest.NewTestServer(t, forceloadConfig(t))
	srv.RunLine("forceload add 0 0")
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), 4096)
	copper := copperBlock(t)
	for x := range int32(16) {
		for y := int32(64); y < 80; y++ {
			for z := range int32(16) {
				srv.SetBlock(x, y, z, copper)
			}
		}
	}
	servertest.HoldTicks(t, srv)
	servertest.UntilTicks(t, srv, 200, "a copper block weathering in the forced chunk", func() bool {
		for x := range int32(16) {
			for y := int32(64); y < 80; y++ {
				for z := range int32(16) {
					if srv.BlockAt(x, y, z) != copper {
						return true
					}
				}
			}
		}
		return false
	})
}
