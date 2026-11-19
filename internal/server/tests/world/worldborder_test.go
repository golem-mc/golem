package world

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// borderPlayer logs an operator in, with the loop stopped so that the test
// owns every tick.
func borderPlayer(t *testing.T, cfg *config.Config) (*servertest.Client, func(ticks int)) {
	t.Helper()
	srv, c := servertest.Join(t, cfg, "Warden", servertest.OpsListing(t, "Warden"))
	srv.StopTick()
	return c, func(ticks int) {
		for range ticks {
			srv.RunTick()
		}
	}
}

// TestWorldborderSetLerpsToTheSize checks the lerp: the command is answered
// with Set Border Lerp Size carrying the ticks the move takes, and ten seconds
// of ticks later the border is the size asked for.
func TestWorldborderSetLerpsToTheSize(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	c, tick := borderPlayer(t, cfg)

	c.ChatCommand("worldborder set 100 10s")
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlaySetBorderLerpSize))
	if got := r.ReadF64(); got != 59999968 {
		t.Errorf("lerp old size = %v, want 59999968", got)
	}
	if got := r.ReadF64(); got != 100 {
		t.Errorf("lerp new size = %v, want 100", got)
	}
	if got := r.ReadVarLong(); got != 200 {
		t.Errorf("lerp ticks = %d, want 200", got)
	}
	servertest.AwaitCommandFeedback(t, c, "commands.worldborder.set.shrink", "100.0", "10.00")

	tick(100)
	c.ChatCommand("worldborder get")
	servertest.AwaitCommandFeedback(t, c, "commands.worldborder.get", "30000034")

	tick(100)
	c.ChatCommand("worldborder get")
	servertest.AwaitCommandFeedback(t, c, "commands.worldborder.get", "100")
}

// TestWorldborderChangesAreTold checks each change's own packet.
func TestWorldborderChangesAreTold(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	c, _ := borderPlayer(t, cfg)

	c.ChatCommand("worldborder set 50")
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlaySetBorderSize))
	if got := r.ReadF64(); got != 50 {
		t.Errorf("set size = %v, want 50", got)
	}
	servertest.AwaitCommandFeedback(t, c, "commands.worldborder.set.immediate", "50.0")

	c.ChatCommand("worldborder center 10 -20")
	r = protocol.NewReader(c.ExpectPlay(protocol.IDPlaySetBorderCenter))
	if x, z := r.ReadF64(), r.ReadF64(); x != 10.5 || z != -19.5 {
		t.Errorf("centre = (%v, %v), want (10.5, -19.5)", x, z)
	}
	servertest.AwaitCommandFeedback(t, c, "commands.worldborder.center.success", "10.50", "-19.50")

	c.ChatCommand("worldborder warning distance 9")
	r = protocol.NewReader(c.ExpectPlay(protocol.IDPlaySetBorderWarningDistance))
	if got := r.ReadVarInt(); got != 9 {
		t.Errorf("warning distance = %d, want 9", got)
	}
	servertest.AwaitCommandFeedback(t, c, "commands.worldborder.warning.distance.success", "9")

	c.ChatCommand("worldborder warning time 10s")
	r = protocol.NewReader(c.ExpectPlay(protocol.IDPlaySetBorderWarningDelay))
	if got := r.ReadVarInt(); got != 200 {
		t.Errorf("warning delay = %d ticks, want 200", got)
	}

	// The same size again changes nothing.
	c.ChatCommand("worldborder set 50")
	servertest.AwaitCommandError(t, c, "commands.worldborder.set.failed.nochange")
}

// TestAJoiningPlayerIsToldTheBorder checks Initialize World Border carrying
// the border as the commands left it.
func TestAJoiningPlayerIsToldTheBorder(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Warden"))
	c := connect().Join("Warden")
	srv.StopTick()

	c.ChatCommand("worldborder center 100 200")
	c.ChatCommand("worldborder set 80 4s")
	c.ChatCommand("worldborder warning distance 7")
	c.Settle()

	late := connect()
	late.Login("Late")
	late.Configure()
	var packet []byte
	late.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayInitializeBorder {
			packet = data
		}
	})
	if packet == nil {
		t.Fatal("no Initialize World Border on join")
	}
	r := protocol.NewReader(packet)
	if x, z := r.ReadF64(), r.ReadF64(); x != 100.5 || z != 200.5 {
		t.Errorf("centre = (%v, %v), want (100.5, 200.5)", x, z)
	}
	if got := r.ReadF64(); got != 59999968 {
		t.Errorf("old size = %v, want 59999968", got)
	}
	if got := r.ReadF64(); got != 80 {
		t.Errorf("new size = %v, want 80", got)
	}
	if got := r.ReadVarLong(); got != 80 {
		t.Errorf("lerp ticks = %d, want 80", got)
	}
	r.ReadVarInt() // absolute maximum size
	if got := r.ReadVarInt(); got != 7 {
		t.Errorf("warning distance = %d, want 7", got)
	}
}

// TestAPlayerOutsideTheBorderIsHurt checks the border's damage: a survival
// body past the border and its buffer takes a hurt of the outside_border
// type, and a creative one does not.
func TestAPlayerOutsideTheBorderIsHurt(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, c, entityID := servertest.JoinAs(t, cfg, "Warden", servertest.OpsListing(t, "Warden"))
	srv.StopTick()

	c.ChatCommand("worldborder set 10")
	c.Settle()
	// Two and a half blocks past the edge at 5 is inside the buffer of 5.
	c.MoveToGround(7.5, 64, 0.5, true)
	c.Settle()
	for range 40 {
		srv.RunTick()
	}
	if got := srv.PlayerHealth("Warden"); got != 20 {
		t.Fatalf("health inside the buffer = %v, want 20", got)
	}

	c.MoveToGround(12.5, 64, 0.5, true)
	c.Settle()
	srv.RunTick()
	if got := srv.PlayerHealth("Warden"); got != 19 {
		t.Errorf("health = %v, want 19 after the border's hurt", got)
	}
	if got := c.ExpectHealth(); got.Health != 19 {
		t.Errorf("health packet = %v, want 19", got.Health)
	}
	c.ExpectDamageEvent(entityID, "minecraft:outside_border", 0)
}

// TestBlocksOutsideTheBorderAreRefused checks that a block neither placed nor
// broken beyond the border: each is answered with the cell as it stands.
func TestBlocksOutsideTheBorderAreRefused(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, c := servertest.Join(t, cfg, "Warden", servertest.OpsListing(t, "Warden"))

	// The player stands at the origin, outside a border of ten blocks
	// centred far away.
	c.ChatCommand("worldborder set 10")
	c.ChatCommand("worldborder center 100 100")
	c.Settle()

	ground := srv.BlockAt(2, 63, 0)
	if ground == 0 {
		t.Fatal("no ground at (2, 63, 0)")
	}

	c.StartDigging(2, 63, 0)
	if got := c.ExpectBlockUpdate(); got.X != 2 || got.Y != 63 || got.Z != 0 || got.State != ground {
		t.Errorf("break answered with %+v, want the ground %d put back", got, ground)
	}
	c.Settle()
	if got := srv.BlockAt(2, 63, 0); got != ground {
		t.Errorf("block after the refused break = %d, want %d", got, ground)
	}

	c.PlaceBlock(2, 63, 0, protocol.FaceTop)
	var overlay protocol.Buffer
	overlay.PutText(protocol.Text{
		Translate: "build.tooHigh",
		With:      []protocol.Text{{Text: "319"}},
		Color:     protocol.ColorRed,
	})
	overlay.PutBool(true) // over the hotbar
	if data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlaySystemChat); !ok || !bytes.Equal(data, overlay.Bytes()) {
		t.Errorf("refused place's overlay = % X, want % X", data, overlay.Bytes())
	}
	if got := c.ExpectBlockUpdate(); got.X != 2 || got.Y != 63 || got.Z != 0 || got.State != ground {
		t.Errorf("place answered the clicked cell with %+v, want the ground %d", got, ground)
	}
	if got := c.ExpectBlockUpdate(); got.X != 2 || got.Y != 64 || got.Z != 0 || got.State != 0 {
		t.Errorf("place answered the cell beside with %+v, want air at (2, 64, 0)", got)
	}
	c.Settle()
	if got := srv.BlockAt(2, 64, 0); got != 0 {
		t.Errorf("block after the refused place = %d, want air", got)
	}
}

// TestEachDimensionHasItsOwnBorder checks that a border set in the overworld
// is not the nether's: a player crossing is sent the nether's.
func TestEachDimensionHasItsOwnBorder(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Warden", servertest.OpsListing(t, "Warden"))

	c.ChatCommand("worldborder set 80")
	c.ChatCommand("execute in minecraft:the_nether run tp @s 8 40 8")
	servertest.PollUntil(t, 20*time.Second, func() bool {
		return srv.PlayerDimension("Warden") == world.NetherName
	})
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayInitializeBorder)
	if !ok {
		t.Fatal("no Initialize World Border for the crossing")
	}
	r := protocol.NewReader(data)
	r.ReadF64()
	r.ReadF64()
	if got := r.ReadF64(); got != 59999968 {
		t.Errorf("the nether's border size = %v, want the default 59999968", got)
	}
}
