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

// TestAReeledInMobsKnockbackGoesToItsWatchersAlone pulls a hooked cow in
// with the rod: the motion that moves it reaches the near player and not the
// far one.
// vanilla: FishingHook.pullEntity, ChunkMap.TrackedEntity.sendToTrackingPlayers
func TestAReeledInMobsKnockbackGoesToItsWatchersAlone(t *testing.T) {
	t.Parallel()
	srv, near, far := farWatchers(t, 120.5)
	srv.GiveSlot("Near", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:fishing_rod"), Count: 1})
	cow := srv.SpawnMob("minecraft:cow", 3.5, 64, 0.5)
	srv.SetMobNoAI(cow)
	near.MoveTo(0.5, 64, 0.5, -90, 10)
	r := servertest.NewMetadataRecorder(t, srv, near)
	r.Read(1)
	hook := castHook(t, r, -90, 10)
	awaitBobberMetadata(t, r, "take", hook, 40)

	servertest.UseHeld(near, -90, 10)
	got := readNews(near, srv, 2, cow)
	if got.byPacket[protocol.IDPlaySetEntityMotion] == 0 {
		t.Error("the near player was sent no motion for the reeled-in cow")
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

// TestAFireworkBurstSkipsAPlayerInAnotherDimension bursts a rocket beside a
// cow while a third player stands at the same spot of the nether: that
// player is sent none of the overworld bodies' packets, the near one is.
// vanilla: ServerLevel.broadcastEntityEvent, ChunkMap.TrackedEntity.sendToTrackingPlayers
func TestAFireworkBurstSkipsAPlayerInAnotherDimension(t *testing.T) {
	t.Parallel()
	srv, near, far, connect := farWatchersWith(t, 120.5)
	nether := connect().Join("Nether")
	servertest.CarryIntoNether(t, srv, nether, "Nether", 5.5, 5.5)
	nether.Settle()
	cow, rocket := burstBesideACow(t, srv)

	got := readNews(near, srv, 60, cow, rocket)
	if got.byPacket[protocol.IDPlayEntityEvent] == 0 {
		t.Error("the near player was sent no entity event for the burst")
	}
	expectNoNews(t, srv, nether, cow, rocket)
	expectNoNews(t, srv, far, cow, rocket)
}

// TestASpectatorWatchingABurstIsTold bursts a rocket beside a cow with a
// spectator at their side: a spectator is shown the bodies like anyone
// else.
// vanilla: ChunkMap.TrackedEntity.updatePlayer
func TestASpectatorWatchingABurstIsTold(t *testing.T) {
	t.Parallel()
	srv, near, _, connect := farWatchersWith(t, 120.5)
	spectator := connect().Join("Ghost")
	spectator.MoveToGround(6.5, 64, 5.5, true)
	spectator.Settle()
	srv.RunLine("gamemode spectator Ghost")
	spectator.Settle()
	if srv.PlayerGameMode("Ghost") != protocol.GameModeSpectator {
		t.Fatal("the player never became a spectator")
	}
	cow, rocket := burstBesideACow(t, srv)

	readNews(near, srv, 60, cow, rocket)
	got := readNews(spectator, srv, 0, cow, rocket)
	for _, want := range []int32{protocol.IDPlayDamageEvent, protocol.IDPlayEntityEvent} {
		if got.byPacket[want] == 0 {
			t.Errorf("the spectator was sent no packet 0x%02X about the burst or the cow", want)
		}
	}
}

// TestAShootersOwnRocketBurstReachesThem launches the near player's own
// rocket at their feet: the burst's entity event reaches them, though the
// body is theirs.
// vanilla: ServerLevel.broadcastEntityEvent
func TestAShootersOwnRocketBurstReachesThem(t *testing.T) {
	t.Parallel()
	srv, near, far := farWatchers(t, 120.5)
	rocket := srv.SpawnPlayerFirework("Near", 0.5, 64, 0.5)

	got := readNews(near, srv, 60, rocket)
	if got.byPacket[protocol.IDPlayEntityEvent] == 0 {
		t.Error("the shooter was sent no entity event for their own rocket's burst")
	}
	expectNoNews(t, srv, far, rocket)
}

// TestAPlayerWhoWalksOutOfRangeBeforeTheBurstIsNotTold shows a rocket to a
// player, who then moves far from it in the tick before it bursts: the
// burst's entity event goes to the near player and not to the one who left.
// vanilla: ServerLevel.broadcastEntityEvent, ChunkMap.TrackedEntity.updatePlayer
func TestAPlayerWhoWalksOutOfRangeBeforeTheBurstIsNotTold(t *testing.T) {
	t.Parallel()
	srv, near, _, connect := farWatchersWith(t, 120.5)
	walker := connect().Join("Walker")
	walker.MoveToGround(6.5, 64, 5.5, true)
	walker.Settle()
	rocket := srv.SpawnFireworkAtAngle(5.5, 64.2, 5.5, 0, 0, 0, 3).ID
	readNews(near, srv, 1, rocket)
	walker.MoveToGround(120.5, 64, 5.5, true)
	walker.Settle()

	got := readNews(near, srv, 4, rocket)
	if got.byPacket[protocol.IDPlayEntityEvent] == 0 {
		t.Error("the near player was sent no entity event for the burst")
	}
	told := readNews(walker, srv, 0, rocket)
	if told.byPacket[protocol.IDPlayEntityEvent] != 0 {
		t.Errorf("the player who walked away was sent the burst's entity event %d times, want none", told.byPacket[protocol.IDPlayEntityEvent])
	}
}
