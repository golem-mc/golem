package entities

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// farWatchers joins a near player at the origin and a far one east of them,
// both on a short view distance so the far one's tracking range and ear
// shot stop before the near body. The loop is held.
func farWatchers(t *testing.T, farX float64) (srv *server.Server, near, far *servertest.Client) {
	t.Helper()
	srv, near, far, _ = farWatchersWith(t, farX)
	return srv, near, far
}

// farWatchersWith is farWatchers that also hands back the connector, for the
// cases that need a further player.
func farWatchersWith(t *testing.T, farX float64) (srv *server.Server, near, far *servertest.Client, connect func() *servertest.Client) {
	t.Helper()
	cfg := servertest.BowTestConfig()
	cfg.ViewDistance = 4
	srv, connect = servertest.Start(t, cfg)
	near = connect().Join("Near")
	far = connect().Join("Far")
	far.MoveToGround(farX, 64, 5.5, true)
	far.Settle()
	near.Settle()
	servertest.HoldTicks(t, srv)
	return srv, near, far, connect
}

// entityNews is the entity a Damage Event, Hurt Animation, Entity Event,
// Set Entity Motion or Set Entity Metadata packet is about; ok is false for
// any other packet.
func entityNews(id int32, data []byte) (entityID int32, ok bool) {
	r := protocol.NewReader(data)
	switch id {
	case protocol.IDPlayDamageEvent, protocol.IDPlayHurtAnimation,
		protocol.IDPlaySetEntityMotion, protocol.IDPlayEntityMetadata:
		return r.ReadVarInt(), r.Err() == nil
	case protocol.IDPlayEntityEvent:
		return r.ReadI32(), r.Err() == nil
	}
	return 0, false
}

// farNews counts the news packets a client was sent about some entities, by
// packet ID, and the sound effects it was sent.
type farNews struct {
	byPacket map[int32]int
	sounds   int
}

// readNews runs ticks on c and counts what it is sent about the entities.
func readNews(c *servertest.Client, srv *server.Server, ticks int, about ...int32) farNews {
	c.T.Helper()
	n := farNews{byPacket: map[int32]int{}}
	c.ReadTicks(srv, ticks, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySoundEffect {
			n.sounds++
		}
		if entity, ok := entityNews(id, data); ok && slices.Contains(about, entity) {
			n.byPacket[id]++
		}
		return false
	})
	return n
}

// expectNoNews fails if the far player was sent any packet about the
// entities, or any sound, since it last read.
func expectNoNews(t *testing.T, srv *server.Server, far *servertest.Client, about ...int32) {
	t.Helper()
	got := readNews(far, srv, 0, about...)
	if len(got.byPacket) != 0 || got.sounds != 0 {
		t.Errorf("the far player was sent packets %v and %d sounds about bodies it never saw, want none", got.byPacket, got.sounds)
	}
}

// TestAFireworkBurstTellsItsHitToTheBodysWatchersAlone bursts a rocket beside
// a cow: the near player is sent the burst's entity event and the cow's
// damage event, knockback and health, and the far player, who never saw
// either body, none of them.
// vanilla: ServerLevel.broadcastEntityEvent, ChunkMap.TrackedEntity.sendToTrackingPlayers
func TestAFireworkBurstTellsItsHitToTheBodysWatchersAlone(t *testing.T) {
	t.Parallel()
	srv, near, far := farWatchers(t, 120.5)
	stone, ok := registry.BlockStates["minecraft:stone"]
	if !ok {
		t.Fatal("the block registry has no minecraft:stone")
	}
	// The ceiling stalls the rise within reach of the cow.
	srv.SetBlock(5, 65, 5, stone)
	cow := srv.SpawnMob("minecraft:cow", 7.5, 64, 5.5)
	srv.SetMobNoAI(cow)
	rocket := srv.SpawnFireworkStackEntity(servertest.FireworkStack(0, 1), 5.5, 64.2, 5.5)

	got := readNews(near, srv, 60, cow, rocket.ID)
	for _, want := range []int32{protocol.IDPlayDamageEvent, protocol.IDPlaySetEntityMotion,
		protocol.IDPlayEntityMetadata, protocol.IDPlayEntityEvent} {
		if got.byPacket[want] == 0 {
			t.Errorf("the near player was sent no packet 0x%02X about the burst or the cow", want)
		}
	}
	expectNoNews(t, srv, far, cow, rocket.ID)
}

// TestAnArrowHitTellsTheMobsWatchersAlone shoots a cow in sight of the near
// player: its damage event, flinch, knockback and health reach the near
// player and not the far one.
// vanilla: AbstractArrow.onHitEntity, ChunkMap.TrackedEntity.sendToTrackingPlayers
func TestAnArrowHitTellsTheMobsWatchersAlone(t *testing.T) {
	t.Parallel()
	srv, near, far := farWatchers(t, 120.5)
	cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	srv.SetMobNoAI(cow)
	srv.ShootArrow(10.5, 65, 0.5, -2, 0, 0)

	got := readNews(near, srv, 10, cow)
	for _, want := range []int32{protocol.IDPlayDamageEvent,
		protocol.IDPlayHurtAnimation, protocol.IDPlaySetEntityMotion, protocol.IDPlayEntityMetadata} {
		if got.byPacket[want] == 0 {
			t.Errorf("the near player was sent no packet 0x%02X about the cow", want)
		}
	}
	expectNoNews(t, srv, far, cow)
}

// TestAFireworkLaunchSoundStopsAtItsRange launches a rocket with a player
// beside it and one past the sound's forty-eight blocks that still sees the
// rocket itself: only the first hears the launch.
// vanilla: FireworkRocketEntity.tick, ServerLevel.playSeededSound
func TestAFireworkLaunchSoundStopsAtItsRange(t *testing.T) {
	t.Parallel()
	srv, near, far := farWatchers(t, 60.5)
	srv.SpawnFireworkEntity(5.5, 64.2, 5.5)

	if got := readNews(near, srv, 2); got.sounds == 0 {
		t.Error("the near player heard no launch")
	}
	expectNoNews(t, srv, far)
}

// burstBesideACow stalls a rocket under a ceiling at the near player's side
// and lets it burst beside a cow, returning the two bodies' IDs.
func burstBesideACow(t *testing.T, srv *server.Server) (cow, rocket int32) {
	t.Helper()
	stone, ok := registry.BlockStates["minecraft:stone"]
	if !ok {
		t.Fatal("the block registry has no minecraft:stone")
	}
	srv.SetBlock(5, 65, 5, stone)
	cow = srv.SpawnMob("minecraft:cow", 7.5, 64, 5.5)
	srv.SetMobNoAI(cow)
	return cow, srv.SpawnFireworkStackEntity(servertest.FireworkStack(0, 1), 5.5, 64.2, 5.5).ID
}
