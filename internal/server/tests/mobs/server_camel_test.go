package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The camel: its body, the two seats, the sitting pose, the dash and cactus
// breeding.
// vanilla: Camel

// camelSaddled is a camel that has long stood, wearing a saddle.
const camelSaddled = `{LastPoseTick:0L,equipment:{saddle:{id:"minecraft:saddle",count:1}}}`

func camelType(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID("minecraft:camel")
	if !ok {
		t.Fatal("no camel entity type")
	}
	return id
}

// camelRiders joins the named players beside a saddled camel and returns the
// camel's entity ID; the loop is stopped and the mobs have ticked once.
func camelRiders(t *testing.T, names ...string) (*server.Server, []*servertest.Client, int32) {
	t.Helper()
	s, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, names[0]))
	var clients []*servertest.Client
	for _, name := range names {
		c := connect()
		c.Join(name)
		c.MoveToGround(2.5, 64, 5.5, true)
		c.Settle()
		clients = append(clients, c)
	}
	s.StopTick()
	s.SetWorldAge(1000)
	clients[0].ChatCommand("summon minecraft:camel 5.5 64 5.5 " + camelSaddled)
	camel := clients[0].AwaitSpawn(camelType(t))
	for _, c := range clients[1:] {
		c.AwaitSpawn(camelType(t))
	}
	s.TickMobs(1)
	return s, clients, camel.EntityID
}

// TestTwoPlayersRideOneCamelInOneSetPassengersPacket: the second player's
// click seats them behind the first, and the packet names both.
// vanilla: Camel.mobInteract, canAddPassenger
func TestTwoPlayersRideOneCamelInOneSetPassengersPacket(t *testing.T) {
	t.Parallel()
	_, clients, id := camelRiders(t, "First", "Second")
	first, second := clients[0], clients[1]
	first.UseOn(id)
	first.Settle()
	second.UseOn(id)
	var last []int32
	second.SettleAndCollect(func(packet int32, data []byte) {
		if packet == protocol.IDPlaySetPassengers {
			if vehicle, riders := servertest.ReadPassengers(second, data); vehicle == id {
				last = riders
			}
		}
	})
	want := []int32{first.PlayerEntityID, second.PlayerEntityID}
	if !slices.Equal(last, want) {
		t.Errorf("passengers %v, want %v", last, want)
	}
}

// TestARidersJumpMakesASaddledCamelDash: the jump input sets DASH (metadata
// 19) and plays the dash sound.
// vanilla: Camel.handleStartJump
func TestARidersJumpMakesASaddledCamelDash(t *testing.T) {
	t.Parallel()
	srv, clients, id := camelRiders(t, "Rider")
	rider := clients[0]
	rider.UseOn(id)
	rider.Settle()
	rider.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
		b.PutVarInt(rider.PlayerEntityID)
		b.PutVarInt(protocol.PlayerCommandStartRidingJump)
		b.PutVarInt(80)
	})
	dashed, sounded := false, false
	record := func(packet int32, data []byte) {
		switch packet {
		case protocol.IDPlaySoundEffect:
			sounded = sounded || servertest.SoundOf(data) == "minecraft:entity.camel.dash"
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != id {
				return
			}
			entries, _ := servertest.ReadMetadata(r)
			if dash := servertest.MetaOf(entries, protocol.MetadataIndexCamelDash); dash != nil && dash.FlagVal {
				dashed = true
			}
		}
	}
	rider.SettleAndCollect(record)
	srv.TickMobs(1)
	rider.SettleAndCollect(record)
	if !sounded {
		t.Error("the dash made no dash sound")
	}
	if !dashed {
		t.Error("DASH (metadata 19) was not told")
	}
}

// TestTwoCamelsFedCactusBreedABaby: cactus sends each camel into love and the
// pair bears a baby camel.
// vanilla: Camel.handleEating, canMate
func TestTwoCamelsFedCactusBreedABaby(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:cactus", 8, func(s *server.Server) {
		s.SetWorldAge(1000)
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	a := srv.SpawnMob("minecraft:camel", 1.5, 64, 2.5)
	b := srv.SpawnMob("minecraft:camel", 3.5, 64, 2.5)
	for range 2 {
		if !servertest.ASpawnOfWithinTicks(c, srv, 3, camelType(t)) {
			t.Fatal("a camel was not told within 3 ticks")
		}
	}
	c.UseOn(a)
	c.AwaitEntityEvent(a, loveHearts)
	c.UseOn(b)
	c.AwaitEntityEvent(b, loveHearts)
	servertest.UntilTicks(t, srv, 400, "the baby camel", func() bool { return srv.MobCount() == 3 })

	baby := c.AwaitSpawn(camelType(t))
	if flag := servertest.MetaOf(c.AwaitMetadataFor(baby.EntityID), protocol.MetadataIndexBaby); flag == nil || !flag.FlagVal {
		t.Error("the bred camel is not a baby")
	}
}

// TestForwardInputStandsARiddenSittingCamelUp: a rider holding forward gets a
// settled sitting camel to its feet, which the last pose change tick tells.
// vanilla: Camel.tickRidden
func TestForwardInputStandsARiddenSittingCamelUp(t *testing.T) {
	t.Parallel()
	s, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Rider"))
	rider := connect()
	rider.Join("Rider")
	rider.MoveToGround(2.5, 64, 5.5, true)
	rider.Settle()
	s.StopTick()
	s.SetWorldAge(1000)
	// The sit animation has finished, but RandomSitting cannot stand the
	// camel up before the rider sends forward input.
	rider.ChatCommand("summon minecraft:camel 5.5 64 5.5 {LastPoseTick:-900L,equipment:{saddle:{id:\"minecraft:saddle\",count:1}}}")
	id := rider.AwaitSpawn(camelType(t)).EntityID
	s.TickMobs(1)
	rider.UseOn(id)
	rider.Settle()
	rider.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputForward) })
	rider.Settle()
	s.TickMobs(1)
	stood := false
	rider.SettleAndCollect(func(packet int32, data []byte) {
		if packet != protocol.IDPlayEntityMetadata {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return
		}
		entries, _ := servertest.ReadMetadata(r)
		if tick := servertest.MetaOf(entries, protocol.MetadataIndexCamelLastPoseChangeTick); tick != nil && tick.LongVal > 0 {
			stood = true
		}
	})
	if !stood {
		t.Error("the camel did not stand up on forward input")
	}
}
