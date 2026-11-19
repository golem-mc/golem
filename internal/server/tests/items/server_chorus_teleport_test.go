package items

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAChorusFruitTeleportSendsItsParticleEvent: eating the fruit sends
// level event 2017 at the cell the eater left, whose data packs the clamped
// offset to the cell the eater reached.
// vanilla: TeleportRandomlyConsumeEffect.apply
func TestAChorusFruitTeleportSendsItsParticleEvent(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	mealedPlayerFile(t, cfg, "Eater")

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Eater")

	srv.GiveSlot("Eater", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:chorus_fruit"), Count: 1})
	c.Settle()
	cell := func() [3]int32 {
		p := srv.PlayerPosition("Eater")
		return [3]int32{int32(math.Floor(p.X)), int32(math.Floor(p.Y)), int32(math.Floor(p.Z))}
	}
	from := cell()
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	tick(33)

	var got [][4]int32
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayWorldEvent {
			return
		}
		if event, x, y, z, d := servertest.ReadWorldEvent(t, data); event == protocol.WorldEventConsumeTeleport {
			got = append(got, [4]int32{x, y, z, d})
		}
	})
	to := cell()
	if len(got) != 1 {
		t.Fatalf("got %d consume teleport events, want 1", len(got))
	}
	if [3]int32{got[0][0], got[0][1], got[0][2]} != from {
		t.Errorf("the event is at %v, want the cell left %v", got[0][:3], from)
	}
	if dest := servertest.UnpackPosDiff(from[0], from[1], from[2], got[0][3], 127, 127, 127); dest != to {
		t.Errorf("the event points at %v, want the cell reached %v", dest, to)
	}
}

// TestAChorusFruitTeleportStaysInsideTheWorldBorder: the target is clamped
// into the border before the eater drops to the floor, so the eater lands
// within the 3 blocks around 0.5 0.5.
// vanilla: LivingEntity.randomTeleport, WorldBorder.clampVec3ToBound
func TestAChorusFruitTeleportStaysInsideTheWorldBorder(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	mealedPlayerFile(t, cfg, "Eater")

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Eater"))
	tick := servertest.StopTicking(t, srv)
	c := connect()
	c.JoinAs("Eater")

	c.ChatCommand("worldborder center 0.5 0.5")
	c.ChatCommand("worldborder set 3")
	srv.GiveSlot("Eater", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:chorus_fruit"), Count: 1})
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	tick(33)
	c.Settle()

	if p := srv.PlayerPosition("Eater"); p.X < -1 || p.X >= 2 || p.Z < -1 || p.Z >= 2 {
		t.Errorf("the eater landed at %.5f %.5f %.5f, outside the border's 3x3 columns", p.X, p.Y, p.Z)
	}
}
