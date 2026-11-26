package combat

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: ChunkMap.tick, ServerEntity.sendChanges, FireworkRocketEntity.tick
func TestACrossbowRocketMovesOnceOnItsFirstFrozenStep(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Launcher", servertest.OpsListing(t, "Launcher"))
	servertest.HoldTicks(t, srv)
	c.MoveTo(0.5, 65, 3.5, -90, 0)
	c.ChatCommand("tick freeze")
	c.ExpectTickingState(20, true)

	crossbow := servertest.ItemStack(t, "minecraft:crossbow", 1)
	crossbow.SetChargedProjectiles([]protocol.ItemStack{servertest.FireworkStack(1, 0)})
	srv.GiveSlot("Launcher", 36, crossbow)
	servertest.DrawBow(c, -90, 0)
	frozen := readFireworkTraffic(c, srv, 1)
	rocket := frozen.fireworkOf()
	if rocket == nil || rocket.Vx < 1 {
		t.Fatalf("eastbound rocket spawn = %+v", rocket)
	}
	if life, _, ok := srv.FireworkState(rocket.Uuid); !ok || life != 0 {
		t.Fatalf("frozen rocket life = %d, present = %v, want 0", life, ok)
	}

	c.ChatCommand("tick step 1")
	c.ExpectTickingStep(1)
	stepped := readFireworkTraffic(c, srv, 1)
	if life, _, ok := srv.FireworkState(rocket.Uuid); !ok || life != 1 {
		t.Fatalf("stepped rocket life = %d, present = %v, want 1", life, ok)
	}

	// The server's packets can arrive before the client runs its one tick.
	// Relative movement is decoded from the packet baseline, then the
	// rocket simulates flight from that position using its known velocity.
	clientX, vx := rocket.X, rocket.Vx
	for _, move := range stepped.positions {
		if move.Id == rocket.EntityID && move.Dx != 0 {
			clientX = (math.Round(clientX*4096) + float64(move.Dx)) / 4096
		}
	}
	for _, move := range stepped.teleports {
		if move.Id == rocket.EntityID {
			clientX = move.X
		}
	}
	for _, motion := range stepped.motions {
		if motion.Id == rocket.EntityID {
			vx = motion.Vx
		}
	}
	clientX += vx

	c.ChatCommand("data get entity @e[type=minecraft:firework_rocket,limit=1] Pos[0]")
	reply := servertest.FeedbackComponent(t, c)
	if reply.Translate != "commands.data.entity.query" || len(reply.With) != 2 {
		t.Fatalf("rocket Pos[0] query returned %+v", reply)
	}
	serverX, err := strconv.ParseFloat(strings.TrimSuffix(reply.With[1].Plain(), "d"), 64)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(clientX-serverX) > 1.0/4096 {
		t.Errorf("rocket after one stepped tick: client x = %.6f, server x = %.6f", clientX, serverX)
	}
	if math.Abs(serverX-(rocket.X+rocket.Vx)) > 1.0/4096 {
		t.Errorf("server x = %.6f, want one velocity step from %.6f at %.6f", serverX, rocket.X, rocket.Vx)
	}

	// The first position packet was due while the rocket still stood at
	// its launch point, before the client's tick step was scheduled.
	for _, move := range frozen.positions {
		if move.Id == rocket.EntityID && move.Dx == 0 && move.Dy == 0 && move.Dz == 0 {
			return
		}
	}
	t.Error("the frozen tracking tick did not send the rocket's launch position")
}

// TestAFireworkRocketsVelocityGoesToItsWatchersAlone flies a rocket beside
// one player while another stands past its tracking range: the velocity
// its movement ticks carry is told to the player watching it and to nobody
// else.
// vanilla: ServerEntity.sendChanges, sendToTrackingPlayers
func TestAFireworkRocketsVelocityGoesToItsWatchersAlone(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = 4 // the rocket is then seen from 64 blocks
	srv, connect := servertest.Start(t, cfg)
	near := connect().Join("Near")
	far := connect().Join("Far")
	servertest.HoldTicks(t, srv)
	// 120 blocks east is out of the 64 blocks four chunks of sight reach.
	far.MoveToGround(120.5, 64, 0.5, true)
	far.ReadTicks(srv, 0, func(int32, []byte) bool { return false })

	// Flight 3: a life of 40 to 51 ticks, so the 25 cover three movement
	// ticks of a rise that speeds up.
	id := srv.SpawnFireworkStackEntity(servertest.FireworkStack(3, 0), 3.5, 65, 3.5).ID
	motionOf := servertest.PacketAbout(id, protocol.IDPlaySetEntityMotion)
	far.ExpectNoneWithinTicks(srv, 25, motionOf)

	var told int
	near.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if motionOf(id, data) {
			told++
		}
		return false
	})
	if told == 0 {
		t.Fatal("the watching player was told no velocity of the rising rocket")
	}
}

// TestAFireworkRocketsBurnEndsIsTold sets a rocket alight for four ticks and
// flies it on: the watcher is told nothing before the first tick; then is told the fire flag once, kept through tick
// three, then cleared on the fourth, off the rocket's ten-tick movement
// period, while the rocket is still flying.
// vanilla: Entity.baseTick, ServerEntity.sendChanges
func TestAFireworkRocketsBurnEndsIsTold(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Watcher")
	servertest.HoldTicks(t, srv)
	rocket := srv.SpawnFireworkAtAngle(3.5, 65, 3.5, 0, 0, 0, 200)
	srv.RunLine("data merge entity " + rocket.UUID.String() + " {Fire:4s}")

	fireFlag := func(id int32, data []byte) (on, told bool) {
		if id != protocol.IDPlayEntityMetadata {
			return false, false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != rocket.ID {
			return false, false
		}
		flags, ok := servertest.ReadEntityFlags(r)
		return flags&protocol.EntityFlagOnFire != 0, ok
	}
	set, cleared := 0, false
	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if _, told := fireFlag(id, data); told {
			t.Fatal("the fire flag was told before the rocket's first tick")
		}
		return false
	})
	count := func(id int32, data []byte) bool {
		if on, told := fireFlag(id, data); told {
			if on {
				set++
			} else {
				cleared = true
			}
		}
		return false
	}
	c.ReadTicks(srv, 1, count)
	if set != 1 || cleared {
		t.Fatalf("on tick one the fire flag was told set %d times, cleared = %v; want set once", set, cleared)
	}
	c.ReadTicks(srv, 2, count)
	if set != 1 || cleared {
		t.Fatalf("through tick three the fire flag was told set %d times, cleared = %v; want set once, never cleared", set, cleared)
	}
	if !c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		on, told := fireFlag(id, data)
		return told && !on
	}) {
		t.Fatal("the fire flag was not told cleared on tick four")
	}
	if _, _, ok := srv.FireworkState(rocket.UUID); !ok {
		t.Fatal("the rocket ended before its burn did")
	}
}

// TestACrossbowFlameRocketLightsOnItsFirstTick fires a crossbow loaded with a
// Flame rocket: the rocket pairs unlit and is told alight on its first tick.
// vanilla: Projectile.applyOnProjectileSpawned, Ignite, Entity.baseTick
func TestACrossbowFlameRocketLightsOnItsFirstTick(t *testing.T) {
	t.Parallel()
	cfg := servertest.BowTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Archer")
	servertest.HoldTicks(t, srv)
	crossbow := servertest.ItemStack(t, "minecraft:crossbow", 1)
	crossbow.SetChargedProjectiles([]protocol.ItemStack{
		servertest.EnchantedStack(t, "minecraft:firework_rocket", "minecraft:flame", 1)})
	srv.GiveSlot("Archer", 36, crossbow)
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)

	var id int32
	paired := false
	if !c.ReadTicks(srv, 0, func(pid int32, data []byte) bool {
		if pid == protocol.IDPlaySpawnEntity {
			if entityID, kind := servertest.SpawnEntityHead(data); kind == protocol.EntityTypeFireworkRocket {
				id = entityID
			}
			return false
		}
		if id == 0 || pid != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		// The pairing payload carries a slot, which ReadEntityFlags cannot
		// step, after the flags entry it leads with when it has one.
		lit := r.ReadU8() == protocol.MetadataIndexEntityFlags && r.ReadVarInt() == protocol.MetadataByte &&
			r.ReadU8()&protocol.EntityFlagOnFire != 0
		paired = !lit
		return true
	}) {
		t.Fatal("the rocket was not shown before a tick ran")
	}
	if !paired {
		t.Fatal("the rocket paired alight before its first tick")
	}
	if !c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		flags, ok := servertest.ReadEntityFlags(r)
		return ok && flags&protocol.EntityFlagOnFire != 0
	}) {
		t.Fatal("the rocket was not told alight on its first tick")
	}
}

// TestAFireworkRocketsBurnIsToldOnceToAWatcherPairedOnItsLightingTick moves a
// player into range of a rocket on the tick the rocket lights: the pairing
// carries the flag and the tick's delta is not sent again.
// vanilla: ChunkMap.TrackedEntity.updatePlayer, ServerEntity.sendChanges
func TestAFireworkRocketsBurnIsToldOnceToAWatcherPairedOnItsLightingTick(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Watcher")
	servertest.HoldTicks(t, srv)
	// Past the two chunks of sight, inside the chunks the world keeps loaded.
	rocket := srv.SpawnFireworkAtAngle(40.5, 65, 0.5, 0, 0, 0, 200)
	srv.RunLine("data merge entity " + rocket.UUID.String() + " {Fire:4s}")
	c.MoveToGround(36.5, 64, 0.5, true)

	set := 0
	c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != rocket.ID {
			return false
		}
		// The pairing's slot entry is past what ReadEntityFlags reads, but the
		// flags entry comes first.
		if r.ReadU8() == protocol.MetadataIndexEntityFlags && r.ReadVarInt() == protocol.MetadataByte &&
			r.ReadU8()&protocol.EntityFlagOnFire != 0 {
			set++
		}
		return false
	})
	if set != 1 {
		t.Fatalf("the fire flag was told %d times to a watcher paired on the lighting tick; want once", set)
	}
}

// TestAFireworkRocketKeepsANegativeFireValue merges a negative Fire into a
// rocket: it reads back as written.
// vanilla: Entity.readAdditionalSaveData, Entity.saveWithoutId
func TestAFireworkRocketKeepsANegativeFireValue(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, _ := servertest.Join(t, cfg, "Watcher")
	servertest.HoldTicks(t, srv)
	rocket := srv.SpawnFireworkAtAngle(0.5, 65, 0.5, 0, 0, 0, 200)
	srv.RunLine("data merge entity " + rocket.UUID.String() + " {Fire:-20s}")
	for _, text := range srv.RunLine("data get entity " + rocket.UUID.String() + " Fire") {
		if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
			if got := text.With[1].Plain(); got != "-20s" {
				t.Fatalf("Fire = %s, want -20s", got)
			}
			return
		}
	}
	t.Fatal("no Fire answer")
}
