package entities

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"fmt"
	"math"
	"testing"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// The lightning tests: a thunderstorm's strike die, the bolt entity shown to
// a tracking client, and what a bolt does where it lands.

// boltSeen is one lightning bolt arrival read off the stream.
type boltSeen struct {
	id      int32
	x, y, z float64
}

// boltWatch reads the stream for lightning bolt arrivals and removals while
// the server ticks the bolts n times.
func boltWatch(c *servertest.Client, srv *server.Server, n int) (arrived []boltSeen, removed []int32) {
	c.T.Helper()
	boltType, _ := registry.EntityTypeID("minecraft:lightning_bolt")
	see := func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			eid := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == boltType {
				arrived = append(arrived, boltSeen{eid, r.ReadF64(), r.ReadF64(), r.ReadF64()})
			}
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for range r.ReadVarInt() {
				removed = append(removed, r.ReadVarInt())
			}
		}
		return false
	}
	srv.TickBolts(n)
	c.SettleAndCollect(func(id int32, data []byte) { see(id, data) })
	return arrived, removed
}

func thunderstorm(srv *server.Server) {
	srv.SetWeatherState(true, true, 100000, 100000, 0)
	for range 400 {
		srv.TickWeather()
	}
}

// TestAThunderstormStrikesAWatchedChunkWithABolt checks the strike die: with
// it forced, a thunderstorm puts a bolt entity in front of a tracking client,
// the bolt lights a fire where it lands, and the bolt is removed when spent.
func TestAThunderstormStrikesAWatchedChunkWithABolt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	srv.StopTick()
	srv.SetDifficulty(2)
	thunderstorm(srv)
	if !srv.Thundering() {
		t.Fatal("the world is not thundering")
	}
	srv.SetLightningOdds(1)

	srv.TickRandomTicks(1)
	arrived, _ := boltWatch(c, srv, 1)
	if len(arrived) == 0 {
		t.Fatal("a thunderstorm with the die forced showed no lightning bolt")
	}
	lit := 0
	for _, b := range arrived {
		name, _, _ := registry.BlockStateName(srv.BlockAt(int32(math.Floor(b.x)), int32(math.Floor(b.y)), int32(math.Floor(b.z))))
		if name == "minecraft:fire" {
			lit++
		}
	}
	if lit == 0 {
		t.Errorf("no fire stands at any of the %d strikes", len(arrived))
	}

	var removed []int32
	for range 40 {
		_, gone := boltWatch(c, srv, 1)
		removed = append(removed, gone...)
	}
	if len(removed) < len(arrived) {
		t.Errorf("%d bolts removed after 40 ticks, want all %d", len(removed), len(arrived))
	}
}

// TestRainAloneStrikesNoLightning is the control: the die is forced but only
// a thunderstorm rolls it.
func TestRainAloneStrikesNoLightning(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
	srv.StopTick()
	srv.SetWeatherState(true, false, 100000, 100000, 0)
	for range 400 {
		srv.TickWeather()
	}
	srv.SetLightningOdds(1)

	srv.TickRandomTicks(3)
	if arrived, _ := boltWatch(c, srv, 1); len(arrived) != 0 {
		t.Errorf("rain without thunder showed %d bolts", len(arrived))
	}
}

// TestALightningBoltChargesACreeperAndHurtsAPlayer checks the bolt's box: a
// survival player in it loses five health, a creeper in it is charged, and a
// creeper outside it is not.
func TestALightningBoltChargesACreeperAndHurtsAPlayer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Struck")
	srv.StopTick()
	pos := srv.PlayerPosition("Struck")
	near := srv.SpawnMob("minecraft:creeper", pos.X+1, pos.Y, pos.Z)
	far := srv.SpawnMob("minecraft:creeper", pos.X+10, pos.Y, pos.Z)
	before := srv.PlayerHealth("Struck")

	srv.StrikeLightning(pos.X, pos.Y, pos.Z)
	boltWatch(c, srv, 1)

	if got := before - srv.PlayerHealth("Struck"); got < 5 {
		t.Errorf("health lost to the bolt = %v, want at least 5", got)
	}
	for _, m := range srv.Mobs() {
		switch m.ID {
		case near:
			if !m.Powered {
				t.Error("the creeper in the bolt's box was not charged")
			}
		case far:
			if m.Powered {
				t.Error("the creeper outside the bolt's box was charged")
			}
		}
	}
}

// TestSummonLightningBoltStrikesWhereTheCommandPoints summons a bolt by
// command: the bolt arrives at the command's position, the success line
// names it, and on its first tick it lights the strike cell.
func TestSummonLightningBoltStrikesWhereTheCommandPoints(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
	srv.StopTick()
	srv.SetDifficulty(2)
	pos := srv.PlayerPosition("Summoner")
	x, y, z := int32(math.Floor(pos.X+8)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z))
	srv.SetBlock(x, y-1, z, registry.BlockStates["minecraft:stone"])
	srv.SetBlock(x, y, z, registry.BlockStates["minecraft:air"])
	bx, bz := float64(x)+0.5, float64(z)+0.5

	boltType, _ := registry.EntityTypeID("minecraft:lightning_bolt")
	var spawned bool
	var identity uuid.UUID
	var feedback []byte
	c.ChatCommand(fmt.Sprintf("summon minecraft:lightning_bolt %v %d %v", bx, y, bz))
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			r.ReadVarInt()
			u := r.ReadUUID()
			if r.ReadVarInt() != boltType {
				return
			}
			if gx, gy, gz := r.ReadF64(), r.ReadF64(), r.ReadF64(); gx != bx || gy != float64(y) || gz != bz {
				t.Errorf("the bolt arrived at (%v, %v, %v), want (%v, %d, %v)", gx, gy, gz, bx, y, bz)
			}
			spawned, identity = true, u
		case protocol.IDPlaySystemChat:
			feedback = data
		}
	})
	if !spawned {
		t.Fatal("/summon minecraft:lightning_bolt showed no bolt")
	}
	if feedback == nil {
		t.Fatal("/summon minecraft:lightning_bolt sent no feedback")
	}
	servertest.CheckCommandFeedback(t, feedback, "commands.summon.success",
		servertest.EntityDisplayComponent("minecraft:lightning_bolt", "entity.minecraft.lightning_bolt", identity))

	srv.TickBolts(1)
	if name := srv.BlockNameAt(x, y, z); name != "minecraft:fire" {
		t.Errorf("the summoned bolt's strike cell holds %q, want fire", name)
	}
}

// strikeFloatingStone summons a weightless stone item eight blocks from the
// player and three above the ground, where no fire survives, and a bolt
// onto it; it returns once the item is in the world and the bolt is summoned.
func strikeFloatingStone(t *testing.T, invulnerable int) *server.Server {
	t.Helper()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Striker", servertest.OpsListing(t, "Striker"))
	servertest.HoldTicks(t, srv)
	srv.SetDifficulty(2)
	pos := srv.PlayerPosition("Striker")
	bx, y, bz := math.Floor(pos.X+8)+0.5, math.Floor(pos.Y)+3, math.Floor(pos.Z)+0.5
	extra := ""
	if invulnerable > 0 {
		extra = fmt.Sprintf(",invulnerable_time:%d", invulnerable)
	}
	c.ChatCommand(fmt.Sprintf(`summon minecraft:item %v %v %v {NoGravity:1b,Item:{id:"minecraft:stone",count:1}%s}`, bx, y, bz, extra))
	c.Settle()
	if items := srv.ItemsInDimension("minecraft:overworld"); len(items) != 1 {
		t.Fatalf("the summon left %d items, want the stone", len(items))
	}
	c.ChatCommand(fmt.Sprintf("summon minecraft:lightning_bolt %v %v %v", bx, y, bz))
	c.Settle()
	if srv.BoltCount() != 1 {
		t.Fatal("the bolt was never summoned")
	}
	return srv
}

// TestALightningBoltBurnsUpAnItem summons a bolt onto a stone item in the
// air: the bolt's five damage takes the item's five health on its first tick.
// vanilla: LightningBolt.tick, Entity.thunderHit, ItemEntity.hurtServer
func TestALightningBoltBurnsUpAnItem(t *testing.T) {
	t.Parallel()
	srv := strikeFloatingStone(t, 0)
	servertest.UntilTicks(t, srv, 2, "the struck item's removal", func() bool {
		return len(srv.ItemsInDimension("minecraft:overworld")) == 0
	})
}
