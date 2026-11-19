package world

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

type joinBorder struct {
	cx, cz, oldSize, newSize float64
	lerp                     int64
	portal, blocks, seconds  int32
}

func joinedBorder(t *testing.T, cfg *config.Config) joinBorder {
	t.Helper()
	// Held from the first tick: the border lerps on every tick, and a free
	// loop would take some off the saved remainder before the join reads it.
	_, connect := servertest.Start(t, cfg, func(srv *server.Server) { servertest.HoldTicks(t, srv) })
	c := connect()
	c.Login("Testificate")
	c.Configure()
	var got []byte
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayInitializeBorder {
			got = data
		}
	})
	if got == nil {
		t.Fatal("join sent no Initialize World Border")
	}
	r := protocol.NewReader(got)
	return joinBorder{r.ReadF64(), r.ReadF64(), r.ReadF64(), r.ReadF64(), r.ReadVarLong(),
		r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()}
}

func borderCompound(cx, cz, size float64, blocks, seconds int32) nbt.Compound {
	return nbt.Compound{
		"center_x": nbt.Double(cx), "center_z": nbt.Double(cz), "size": nbt.Double(size),
		"lerp_time": nbt.Long(0), "lerp_target": nbt.Double(0), "safe_zone": nbt.Double(5),
		"damage_per_block": nbt.Double(0.2),
		"warning_blocks":   nbt.Int(blocks), "warning_time": nbt.Int(seconds),
	}
}

// TestJoinSendsTheSavedWorldBorder checks that the overworld's
// world_border.dat sets the centre, size and warnings of the join's border.
func TestJoinSendsTheSavedWorldBorder(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	err := world.WriteSavedData(filepath.Join(cfg.LevelName, "dimensions", "minecraft", "overworld"),
		"data/minecraft/world_border.dat", borderCompound(100, -50, 1000, 7, 20))
	if err != nil {
		t.Fatal(err)
	}
	want := joinBorder{100, -50, 1000, 1000, 0, 29999984, 7, 20}
	if got := joinedBorder(t, cfg); got != want {
		t.Errorf("join border = %+v, want %+v", got, want)
	}
}

// TestJoinSendsALegacyLevelDatBorder checks that a level.dat from before 26.1
// still carrying Border* fields sets the overworld's border, with the
// warning time 15 the upgrade gives a field that is absent.
// vanilla: LegacyWorldBorderFix
func TestJoinSendsALegacyLevelDatBorder(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	encoded, err := nbt.Encode("", nbt.Compound{"Data": nbt.Compound{
		"BorderCenterX": nbt.Double(10), "BorderCenterZ": nbt.Double(20), "BorderSize": nbt.Double(500),
	}})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(encoded); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.LevelName, "level.dat"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	want := joinBorder{10, 20, 500, 500, 0, 29999984, 5, 15}
	if got := joinedBorder(t, cfg); got != want {
		t.Errorf("join border = %+v, want %+v", got, want)
	}
}

// TestWorldBorderSurvivesARestart checks that the commands' changes to the
// border, a lerp in progress included, are in world_border.dat after a
// shutdown and come back at the next servertest.Start.
func TestWorldBorderSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Warden", servertest.OpsListing(t, "Warden"))
	srv.StopTick()
	for _, cmd := range []string{
		"worldborder center 40 -30", "worldborder damage amount 0.5", "worldborder damage buffer 9",
		"worldborder warning distance 11", "worldborder warning time 13", "worldborder set 100 10s",
	} {
		c.ChatCommand(cmd)
	}
	// Commands run in order, so the last one's feedback means all took effect.
	servertest.AwaitCommandFeedback(t, c, "commands.worldborder.set.shrink", "100.0", "10.00")
	for range 50 {
		srv.RunTick()
	}
	srv.Shutdown()

	got := joinedBorder(t, cfg)
	if got.cx != 40.5 || got.cz != -29.5 || got.newSize != 100 || got.lerp != 150 ||
		got.portal != 29999984 || got.blocks != 11 || got.seconds != 13 {
		t.Errorf("restarted border = %+v, want centre 40.5,-29.5 (block centre) lerping to 100 with 150 ticks left, warnings 11 and 13", got)
	}
	if got.oldSize <= 100 || got.oldSize >= 59999968 {
		t.Errorf("restarted border size = %v, want a size part-way through the lerp", got.oldSize)
	}
	data, ok, err := world.ReadSavedData(filepath.Join(cfg.LevelName, "dimensions", "minecraft", "overworld"),
		"data/minecraft/world_border.dat", 1<<20)
	if err != nil || !ok {
		t.Fatalf("world_border.dat: ok %v, err %v", ok, err)
	}
	if d, _ := nbt.Get[nbt.Double](data, "damage_per_block"); d != 0.5 {
		t.Errorf("damage_per_block = %v, want 0.5", d)
	}
	if d, _ := nbt.Get[nbt.Double](data, "safe_zone"); d != 9 {
		t.Errorf("safe_zone = %v, want 9", d)
	}
}
