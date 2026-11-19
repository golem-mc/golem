package redstone

// Sculk-sensor vibration sources other than the player: priming, teleports,
// lightning and bursts. vanilla: GameEvent, VibrationSystem

import (
	"slices"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// eventLog records the game events posted from now on.
type eventLog struct {
	mu   sync.Mutex
	seen map[string][][3]float64
}

func observeEvents(srv *server.Server) *eventLog {
	l := &eventLog{seen: map[string][][3]float64{}}
	srv.ObserveGameEvents(func(event string, x, y, z float64) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.seen[event] = append(l.seen[event], [3]float64{x, y, z})
	})
	return l
}

func (l *eventLog) of(event string) [][3]float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.seen[event])
}

func giveFlint(t *testing.T, srv *server.Server) {
	t.Helper()
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:flint_and_steel"), Count: 1})
}

// TestFlintAndSteelOnTNTPostsPrimeFuse: lighting a TNT block posts prime_fuse
// at its centre.
// vanilla: TntBlock.prime
func TestFlintAndSteelOnTNTPostsPrimeFuse(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	srv.SetBlock(1, 64, 4, servertest.MustState(t, "minecraft:tnt", nil))
	giveFlint(t, srv)
	c.Settle()
	events := observeEvents(srv)
	c.UseBlock(1, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	if got := events.of("minecraft:prime_fuse"); !slices.Equal(got, [][3]float64{{1.5, 64.5, 4.5}}) {
		t.Errorf("prime_fuse events = %v, want one at the block centre", got)
	}
}

// TestAPrimedTNTWakesASculkSensorEightBlocksAway: the priming is heard.
// vanilla: TntBlock.prime
func TestAPrimedTNTWakesASculkSensorEightBlocksAway(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	srv.SetBlock(1, 64, 4, servertest.MustState(t, "minecraft:tnt", nil))
	srv.SetBlock(9, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	giveFlint(t, srv)
	c.Settle()
	c.UseBlock(1, 64, 4)
	c.Settle()
	for range 30 {
		servertest.RunTicks(t, srv, 1)
		if _, props := servertest.StatePropsOf(t, srv, 9, 64, 4); props["sculk_sensor_phase"] != "inactive" {
			return
		}
	}
	t.Error("the sensor never left the inactive phase")
}

// TestALightningBoltPostsLightningStrike: a summoned bolt posts the event at
// itself.
// vanilla: LightningBolt.tick
func TestALightningBoltPostsLightningStrike(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	srv.SetPlayerGameMode("Actor", protocol.GameModeCreative)
	c.Settle()
	events := observeEvents(srv)
	c.ChatCommand("summon minecraft:lightning_bolt 3.5 64 3.5")
	c.Settle()
	servertest.RunTicks(t, srv, 10)
	got := events.of("minecraft:lightning_strike")
	if len(got) != 1 || got[0][0] != 3.5 || got[0][2] != 3.5 {
		t.Errorf("lightning_strike events = %v, want one at the bolt", got)
	}
}

// TestAChorusFruitPostsTeleportAtTheStart: eating the fruit posts teleport at
// the place left.
// vanilla: TeleportRandomlyConsumeEffect.apply
func TestAChorusFruitPostsTeleportAtTheStart(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:chorus_fruit"), Count: 1})
	c.Settle()
	from := srv.PlayerPosition("Actor")
	events := observeEvents(srv)
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	servertest.RunTicks(t, srv, 34)
	got := events.of("minecraft:teleport")
	if len(got) != 1 || got[0] != [3]float64{from.X, from.Y, from.Z} {
		t.Errorf("teleport events = %v, want one at %v", got, from)
	}
}

// TestAFireworkBurstPostsExplode: a rocket that bursts posts explode.
// vanilla: FireworkRocketEntity.explode
func TestAFireworkBurstPostsExplode(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	c.Settle()
	events := observeEvents(srv)
	srv.SpawnFireworkAtAngle(1.5, 66, 4.5, 0, 0, 0, 2)
	servertest.RunTicks(t, srv, 6)
	if got := events.of("minecraft:explode"); len(got) != 1 {
		t.Errorf("explode events = %v, want one", got)
	}
}

// TestAnIgnitedCreeperPostsPrimeFuse: a creeper lit with flint and steel
// posts prime_fuse when its swell starts.
// vanilla: Creeper.tick
func TestAnIgnitedCreeperPostsPrimeFuse(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	srv.SetPlayerGameMode("Actor", protocol.GameModeCreative)
	giveFlint(t, srv)
	p := srv.PlayerPosition("Actor")
	id := srv.SpawnMob("minecraft:creeper", p.X+2, p.Y, p.Z)
	c.Settle()
	events := observeEvents(srv)
	c.UseOn(id)
	c.Settle()
	servertest.RunTicks(t, srv, 3)
	if got := events.of("minecraft:prime_fuse"); len(got) != 1 {
		t.Errorf("prime_fuse events = %v, want one", got)
	}
}

// TestADispenserLightingTNTPostsPrimeFuse: flint and steel dispensed into a
// TNT block posts prime_fuse at its centre.
// vanilla: FlintAndSteelDispenseItemBehavior, TntBlock.prime
func TestADispenserLightingTNTPostsPrimeFuse(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Dispensing(t)
	srv.SetBlock(4, 64, 1, servertest.MustState(t, "minecraft:tnt", nil))
	srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:flint_and_steel", 1))
	events := observeEvents(srv)
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	if got := events.of("minecraft:prime_fuse"); !slices.Equal(got, [][3]float64{{4.5, 64.5, 1.5}}) {
		t.Errorf("prime_fuse events = %v, want one at the block centre", got)
	}
}

// vanilla: Entity.startRiding, Entity.removeVehicle
func TestBoatBoardingAndSneakingOutPostVibrations(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	boat := srv.SpawnBoat(2.5, 64, 4.5, "minecraft:oak_boat")
	c.Settle()
	events := observeEvents(srv)
	c.UseOn(boat)
	c.Settle()
	if got := events.of("minecraft:entity_mount"); !slices.Equal(got, [][3]float64{{2.5, 64, 4.5}}) {
		t.Errorf("mount events = %v, want one at the hull", got)
	}
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	c.Settle()
	if got := events.of("minecraft:entity_dismount"); !slices.Equal(got, [][3]float64{{2.5, 64, 4.5}}) {
		t.Errorf("dismount events = %v, want one at the hull despite sneaking", got)
	}
}

func TestRideCommandsPostMountAndDismountVibrations(t *testing.T) {
	for _, rider := range []string{"@s", "@e[type=zombie,limit=1]"} {
		vehicles := []string{"oak_boat", "minecart", "pig"}
		if rider == "@s" {
			vehicles = append(vehicles, "cushion")
		}
		for _, vehicle := range vehicles {
			t.Run(rider+"/"+vehicle, func(t *testing.T) {
				t.Parallel()
				srv, c := survivalScene(t)
				c.ChatCommand("summon minecraft:" + vehicle + " 2.5 64 4.5")
				c.ChatCommand("summon minecraft:zombie 1.5 64 4.5 {NoAI:1b}")
				c.Settle()
				events := observeEvents(srv)
				c.ChatCommand("ride " + rider + " mount @e[type=" + vehicle + ",limit=1]")
				servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
				c.Settle()
				if got := events.of("minecraft:entity_mount"); !slices.Equal(got, [][3]float64{{2.5, 64, 4.5}}) {
					t.Errorf("mount events = %v, want one at the vehicle", got)
				}
				c.ChatCommand("ride " + rider + " dismount")
				servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
				c.Settle()
				if got := events.of("minecraft:entity_dismount"); !slices.Equal(got, [][3]float64{{2.5, 64, 4.5}}) {
					t.Errorf("dismount events = %v, want one at the vehicle", got)
				}
				c.ChatCommand("ride " + rider + " dismount")
				servertest.AwaitSystemChatSaying(t, c, "commands.ride.not_riding")
				c.Settle()
				if got := events.of("minecraft:entity_dismount"); len(got) != 1 {
					t.Errorf("failed dismount posted another event: %v", got)
				}
			})
		}
	}
}

func TestSneakingOutOfABoatActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	boat := srv.SpawnBoat(2.5, 64, 4.5, "minecraft:oak_boat")
	c.UseOn(boat)
	c.Settle()
	placeSensor(t, srv)
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	c.Settle()
	servertest.RunTicks(t, srv, 8)
	_, props := servertest.StatePropsOf(t, srv, 4, 64, 4)
	if props["sculk_sensor_phase"] != "active" {
		t.Errorf("sensor phase = %s, want active after the sneak dismount", props["sculk_sensor_phase"])
	}
	expectComparator(t, srv, "5")
}

func TestVehicleRemovalPostsOneDismountPerPassenger(t *testing.T) {
	for _, vehicle := range []string{"oak_boat", "minecart", "pig", "cushion"} {
		t.Run(vehicle, func(t *testing.T) {
			t.Parallel()
			srv, c := survivalScene(t)
			c.ChatCommand("summon minecraft:" + vehicle + " 2.5 64 4.5 {NoAI:1b}")
			c.ChatCommand("ride @s mount @e[type=" + vehicle + ",limit=1]")
			servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
			if vehicle != "cushion" {
				c.ChatCommand("summon minecraft:zombie 1.5 64 4.5 {NoAI:1b}")
				c.ChatCommand("ride @e[type=zombie,limit=1] mount @e[type=" + vehicle + ",limit=1]")
				servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
			}
			c.Settle()
			events := observeEvents(srv)
			c.ChatCommand("kill @e[type=" + vehicle + ",limit=1]")
			c.Settle()
			servertest.RunTicks(t, srv, 22)
			want := [][3]float64{{2.5, 64, 4.5}}
			if vehicle != "cushion" {
				want = append(want, want[0])
			}
			if got := events.of("minecraft:entity_dismount"); !slices.Equal(got, want) {
				t.Errorf("dismount events = %v, want %v", got, want)
			}
		})
	}
}

func TestMountVibrationsIdentifyTheirPassenger(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	boat := srv.SpawnBoat(2.5, 64, 4.5, "minecraft:oak_boat")
	zombie := srv.SpawnMob("minecraft:zombie", 1.5, 64, 4.5)
	mobUUID, _ := srv.MobUUID(zombie)
	var mu sync.Mutex
	var got []rules.GameEventContext
	srv.ObserveGameEventContexts(func(event string, _, _, _ float64, ctx rules.GameEventContext) {
		if event == "minecraft:entity_mount" || event == "minecraft:entity_dismount" {
			mu.Lock()
			got = append(got, ctx)
			mu.Unlock()
		}
	})
	c.UseOn(boat)
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	c.ChatCommand("ride @e[type=zombie,limit=1] mount @e[type=oak_boat,limit=1]")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.mount.success")
	c.ChatCommand("ride @e[type=zombie,limit=1] dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
	c.Settle()
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 4 {
		t.Fatalf("mount/dismount contexts = %v, want four", got)
	}
	for _, ctx := range got[:2] {
		if ctx.Player != protocol.OfflineUUID("Actor") || ctx.Mob != uuid.Nil {
			t.Errorf("player rider context = %+v", ctx)
		}
	}
	if !got[1].Sneaking {
		t.Error("sneak dismount lost the rider's sneaking context")
	}
	for _, ctx := range got[2:] {
		if ctx.Mob.String() != mobUUID || ctx.Player != uuid.Nil {
			t.Errorf("mob rider context = %+v, want %s", ctx, mobUUID)
		}
	}
}

func TestBoardingABoatActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, c := survivalScene(t)
	boat := srv.SpawnBoat(2.5, 64, 4.5, "minecraft:oak_boat")
	placeSensor(t, srv)
	c.UseOn(boat)
	c.Settle()
	servertest.RunTicks(t, srv, 8)
	expectComparator(t, srv, "6")
}

const saddledCamel = `{LastPoseTick:0L,equipment:{saddle:{id:"minecraft:saddle",count:1}}}`

// camelScene joins a rider beside a camel summoned with nbt; the loop is
// stopped and the mobs have ticked once.
func camelScene(t *testing.T, nbt string) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	s, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Rider"))
	rider := connect()
	rider.Join("Rider")
	rider.MoveToGround(2.5, 64, 5.5, true)
	rider.Settle()
	s.StopTick()
	s.SetWorldAge(1000)
	rider.ChatCommand("summon minecraft:camel 5.5 64 5.5 " + nbt)
	id, ok := registry.EntityTypeID("minecraft:camel")
	if !ok {
		t.Fatal("no camel entity type")
	}
	camel := rider.AwaitSpawn(id)
	s.TickMobs(1)
	return s, rider, camel.EntityID
}

// TestACamelDashPostsEntityAction: a rider's jump command makes the camel
// dash, which is heard.
// vanilla: Camel.handleStartJump
func TestACamelDashPostsEntityAction(t *testing.T) {
	t.Parallel()
	srv, rider, id := camelScene(t, saddledCamel)
	rider.UseOn(id)
	rider.Settle()
	events := observeEvents(srv)
	rider.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
		b.PutVarInt(rider.PlayerEntityID)
		b.PutVarInt(protocol.PlayerCommandStartRidingJump)
		b.PutVarInt(80)
	})
	rider.Settle()
	srv.TickMobs(1)
	if got := events.of("minecraft:entity_action"); len(got) != 1 {
		t.Errorf("entity_action events = %v, want one at the camel", got)
	}
}

// TestACamelStandingUpPostsEntityAction: forward input stands a ridden sitting
// camel up, which is heard.
// vanilla: Camel.standUp
func TestACamelStandingUpPostsEntityAction(t *testing.T) {
	t.Parallel()
	srv, rider, id := camelScene(t, `{LastPoseTick:-900L,equipment:{saddle:{id:"minecraft:saddle",count:1}}}`)
	rider.UseOn(id)
	rider.Settle()
	events := observeEvents(srv)
	rider.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputForward) })
	rider.Settle()
	srv.TickMobs(1)
	if got := events.of("minecraft:entity_action"); len(got) != 1 {
		t.Errorf("entity_action events = %v, want one on the stand-up tick", got)
	}
}

// TestAWolfShakingDryPostsEntityAction: the tick a wet wolf starts its shake.
// vanilla: Wolf.aiStep
func TestAWolfShakingDryPostsEntityAction(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Builder")
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetOverworldBiome(8, 8, "minecraft:plains")
	id := srv.SpawnMob("minecraft:wolf", 8.5, 64, 8.5)
	srv.SetMobNoAI(id)
	srv.SetBlock(8, 64, 8, servertest.MustState(t, "minecraft:water", map[string]string{"level": "0"}))
	servertest.AwaitTicks(t, srv, 3)
	srv.SetBlock(8, 64, 8, 0)
	events := observeEvents(srv)
	servertest.AwaitTicks(t, srv, 10)
	if got := events.of("minecraft:entity_action"); len(got) != 1 {
		t.Errorf("entity_action events = %v, want one for the shake", got)
	}
}

// TestAnActivatedCreakingPostsEntityAction: a player's look wakes a creaking, which is heard.
// vanilla: Creaking.activate
func TestAnActivatedCreakingPostsEntityAction(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), "Warden")
	c.Settle()
	srv.StopTick()
	srv.SetSpawning(false)
	srv.SetDifficulty(2)
	pos := srv.PlayerPosition("Warden")
	srv.SpawnMob("minecraft:creaking", pos.X+8, pos.Y, pos.Z)
	c.MoveTo(pos.X, pos.Y, pos.Z, -90, 0) // east: a look wakes it
	c.Settle()
	events := observeEvents(srv)
	srv.TickMobs(3)
	if got := events.of("minecraft:entity_action"); len(got) == 0 {
		t.Error("the activated creaking posted no entity_action")
	}
}

const entityPlace = "minecraft:entity_place"

// hostileScene joins a player at the origin on Hard difficulty at night.
func hostileScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(rules.DifficultyHard)
	srv.SetDayTime(18000)
	c := connect().Join("Target")
	servertest.HoldTicks(t, srv)
	return srv, c
}

// TestAHensEggPostsEntityPlace: a laid egg is a placement at the hen.
// vanilla: Chicken.aiStep
func TestAHensEggPostsEntityPlace(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	srv.StopTick()
	srv.SetDayTime(18000)
	c := connect().Join("Farmer")
	id := srv.SpawnMob("minecraft:chicken", 2.5, 64, 1.5)
	c.ExpectMobSpawn(protocol.EntityTypeChicken)
	events := observeEvents(srv)
	srv.SetEggTimer(id, 1)
	srv.TickMobs(2)
	if got := events.of(entityPlace); len(got) != 1 || got[0][0] != 2.5 || got[0][2] != 1.5 {
		t.Errorf("entity_place events = %v, want one at the hen", got)
	}
}

// TestEvokerFangsPostEntityPlace: each fang is a placement.
// vanilla: Evoker.EvokerAttackSpellGoal.createSpellEntity
func TestEvokerFangsPostEntityPlace(t *testing.T) {
	t.Parallel()
	srv, c := hostileScene(t)
	c.MoveToGround(20.5, 64, 0.5, true)
	for i := range 8 {
		srv.SpawnMob("minecraft:vex", -4.5, 64, float64(i)-3.5)
	}
	srv.SpawnMob("minecraft:evoker", 10.5, 64, 0.5)
	events := observeEvents(srv)
	fangsType, _ := registry.EntityTypeID("minecraft:evoker_fangs")
	fangs := 0
	count := func(pid int32, data []byte) bool {
		if pid == protocol.IDPlaySpawnEntity {
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == fangsType {
				fangs++
			}
		}
		return false
	}
	c.ReadTicksSkipping(srv, 600, servertest.HostileSkip, func(pid int32, data []byte) bool {
		count(pid, data)
		return fangs > 0
	})
	c.SettleAndCollect(func(pid int32, data []byte) {
		if !servertest.HostileSkip(pid) {
			count(pid, data)
		}
	})
	if fangs == 0 {
		t.Fatal("no fangs were spawned")
	}
	if got := len(events.of(entityPlace)); got != fangs {
		t.Errorf("entity_place events = %d, want %d (one per fang)", got, fangs)
	}
}

// TestAnEvokersVexesPostEntityPlace: each summoned vex is a placement.
// vanilla: Evoker.EvokerSummonSpellGoal.performSpellCasting
func TestAnEvokersVexesPostEntityPlace(t *testing.T) {
	t.Parallel()
	srv, c := hostileScene(t)
	srv.SpawnMob("minecraft:evoker", 10.5, 64, 0.5)
	events := observeEvents(srv)
	vexes := func() int {
		n := 0
		for _, m := range srv.Mobs() {
			if m.Kind == "minecraft:vex" {
				n++
			}
		}
		return n
	}
	c.ReadTicksSkipping(srv, 240, servertest.HostileSkip, func(int32, []byte) bool { return vexes() >= 3 })
	if got := len(events.of(entityPlace)); vexes() != 3 || got != 3 {
		t.Errorf("vexes = %d, entity_place events = %d, want 3 and 3", vexes(), got)
	}
}

// TestAnOminousItemSpawnerPostsEntityPlace: the drop is a placement at the spawner.
// vanilla: OminousItemSpawner.tick
func TestAnOminousItemSpawnerPostsEntityPlace(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.MobTestConfig())
	connect().Join("Trialist")
	servertest.HoldTicks(t, srv)
	srv.RunLine("summon minecraft:ominous_item_spawner 4.5 66 4.5 {item:{id:\"minecraft:trial_key\",count:1},spawn_item_after_ticks:1L}")
	events := observeEvents(srv)
	srv.TickOminousItemSpawners(1)
	if got := events.of(entityPlace); len(got) != 1 || got[0] != [3]float64{4.5, 66, 4.5} {
		t.Errorf("entity_place events = %v, want one at the spawner", got)
	}
}
