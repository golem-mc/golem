package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// Piglins anger at a player who opens a guarded container in their sight or
// breaks a guarded block.
// vanilla: PiglinAi.angerNearbyPiglins

func openChestBeside(t *testing.T, c *servertest.Client, srv *server.Server) {
	t.Helper()
	c.ChatCommand("setblock 3 64 0 minecraft:chest")
	c.Settle()
	c.UseBlockWith(0, 3, 64, 0)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
}

func breakGoldBeside(t *testing.T, c *servertest.Client, srv *server.Server) {
	t.Helper()
	c.ChatCommand("setblock 3 64 0 minecraft:gold_block")
	c.ChatCommand("effect give Slayer minecraft:haste 100 255 true")
	c.Settle()
	c.BreakBlock(3, 64, 0)
	c.Settle()
	servertest.RunTicks(t, srv, 10)
}

func TestAPiglinAngersAtAPlayerOpeningAChestInItsSight(t *testing.T) {
	t.Parallel()
	c, id, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 6.5, 64, 0.5)
	servertest.RunTicks(t, srv, 2)
	openChestBeside(t, c, srv)
	if srv.MobTargetID(p) != id {
		t.Fatal("the piglin did not turn on the player who opened a chest in front of it")
	}
}

func TestAPiglinBehindAWallIgnoresAChestOpened(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	c.ChatCommand("fill 5 60 -3 5 70 3 minecraft:stone")
	c.Settle()
	p := srv.SpawnMob(barterPiglin, 7.5, 64, 0.5)
	servertest.RunTicks(t, srv, 2)
	openChestBeside(t, c, srv)
	if srv.MobTargetID(p) != 0 {
		t.Fatal("a piglin that cannot see the player took offence at the chest")
	}
}

func TestAPiglinBehindAWallStillAngersAtBrokenGold(t *testing.T) {
	t.Parallel()
	c, id, srv := barterWorld(t)
	c.ChatCommand("fill 5 60 -3 5 70 3 minecraft:stone")
	c.Settle()
	p := srv.SpawnMob(barterPiglin, 7.5, 64, 0.5)
	servertest.RunTicks(t, srv, 2)
	breakGoldBeside(t, c, srv)
	if srv.MobTargetID(p) != id {
		t.Fatal("the piglin did not hear the gold block break")
	}
}

// gearedPlayers puts golden boots on each named player so that no piglin
// hunts them unprovoked.
func gearedPlayers(c *servertest.Client, names ...string) {
	for _, n := range names {
		c.ChatCommand("item replace entity " + n + " armor.feet with minecraft:golden_boots")
	}
	c.Settle()
}

func TestUniversalAngerTurnsAPiglinOnTheNearestPlayerNotTheOpener(t *testing.T) {
	t.Parallel()
	for _, universal := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[universal], func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Slayer"))
			srv.SetDayTime(18000)
			srv.SetGameRule("universal_anger", universal)
			c := connect()
			opener := c.JoinAs("Slayer")
			c.MoveToGround(2.5, 64, 0.5, true)
			other := connect()
			otherID := other.JoinAs("Other")
			other.MoveToGround(5.5, 64, 0.5, true)
			gearedPlayers(c, "Slayer", "Other")
			other.Settle()
			servertest.HoldTicks(t, srv)
			p := srv.SpawnMob(barterPiglin, 7.5, 64, 0.5)
			servertest.RunTicks(t, srv, 2)
			openChestBeside(t, c, srv)
			want := opener
			if universal {
				want = otherID
			}
			if got := srv.MobTargetID(p); got != want {
				t.Fatalf("universal_anger %v: target %d, want %d", universal, got, want)
			}
		})
	}
}

func TestAPiglinBruteIgnoresAChestOpenedButAPiglinBesideItDoesNot(t *testing.T) {
	t.Parallel()
	c, id, srv := barterWorld(t)
	// Beyond the brute's own sight of 12 blocks but inside the 16 of the alert.
	piglin := srv.SpawnMob(barterPiglin, 17.5, 64, 0.5)
	brute := srv.SpawnMob(bruteKind, 17.5, 64, 2.5)
	servertest.RunTicks(t, srv, 2)
	openChestBeside(t, c, srv)
	if srv.MobTargetID(piglin) != id {
		t.Fatal("the piglin 15 blocks away did not hear the chest")
	}
	if srv.MobTargetID(brute) != 0 {
		t.Fatal("the piglin brute took offence at the chest")
	}
}

func TestAPiglinIgnoresACreativePlayerOpeningAChest(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	c.ChatCommand("gamemode creative")
	c.Settle()
	p := srv.SpawnMob(barterPiglin, 6.5, 64, 0.5)
	servertest.RunTicks(t, srv, 2)
	openChestBeside(t, c, srv)
	if srv.MobTargetID(p) != 0 {
		t.Fatal("the piglin turned on a creative player")
	}
}

func TestAPiglinAngersAtAPlayerOpeningAChestMinecartInItsSight(t *testing.T) {
	t.Parallel()
	c, id, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 6.5, 64, 0.5)
	c.ChatCommand("summon minecraft:chest_minecart 3.5 64.0625 0.5")
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	carts := srv.MinecartsInDimension(world.DimensionName)
	if len(carts) != 1 {
		t.Fatalf("%d carts stand, want 1", len(carts))
	}
	c.UseOn(carts[0].ID)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if srv.MobTargetID(p) != id {
		t.Fatal("the piglin did not turn on the player who opened a chest minecart in front of it")
	}
}

func TestAPiglinIgnoresAPlayerOpeningAHopperMinecart(t *testing.T) {
	t.Parallel()
	c, _, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 6.5, 64, 0.5)
	c.ChatCommand("summon minecraft:hopper_minecart 3.5 64.0625 0.5")
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	carts := srv.MinecartsInDimension(world.DimensionName)
	if len(carts) != 1 {
		t.Fatalf("%d carts stand, want 1", len(carts))
	}
	c.UseOn(carts[0].ID)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if srv.MobTargetID(p) != 0 {
		t.Fatal("the piglin turned on a player who opened a hopper minecart")
	}
}

func TestAPiglinAngersAtAPlayerOpeningAChestBoatInItsSight(t *testing.T) {
	t.Parallel()
	c, id, srv := barterWorld(t)
	p := srv.SpawnMob(barterPiglin, 6.5, 64, 0.5)
	boat := srv.SpawnBoat(3.5, 64, 0.5, "minecraft:oak_chest_boat")
	if boat == 0 {
		t.Fatal("SpawnBoat came to nothing")
	}
	c.Settle()
	// A plain click boards a free seat; a sneaking one opens the cargo.
	c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(boat)
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0.5, 0.5, 0.5)
		b.PutBool(true)
	})
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if srv.MobTargetID(p) != id {
		t.Fatal("the piglin did not turn on the player who opened a chest boat in front of it")
	}
}

// TestAPiglinAngersAtAPlayerBreakingAChestMinecartByHand: a survival blow
// that destroys a chest minecart, hopper minecart or chest boat angers the
// piglin in sight, unless entity_drops is off.
// vanilla: ContainerEntity.chestVehicleDestroyed
func TestAPiglinAngersAtAPlayerBreakingAChestMinecartByHand(t *testing.T) {
	t.Parallel()
	vehicles := []struct {
		name  string
		spawn func(c *servertest.Client, srv *server.Server) int32
	}{
		{"chest minecart", func(c *servertest.Client, srv *server.Server) int32 {
			c.ChatCommand("summon minecraft:chest_minecart 3.5 64.0625 0.5")
			c.Settle()
			servertest.RunTicks(t, srv, 2)
			return minecartID(t, srv)
		}},
		{"hopper minecart", func(c *servertest.Client, srv *server.Server) int32 {
			c.ChatCommand("summon minecraft:hopper_minecart 3.5 64.0625 0.5")
			c.Settle()
			servertest.RunTicks(t, srv, 2)
			return minecartID(t, srv)
		}},
		{"chest boat", func(c *servertest.Client, srv *server.Server) int32 {
			id := srv.SpawnBoat(3.5, 64, 0.5, "minecraft:oak_chest_boat")
			if id == 0 {
				t.Fatal("SpawnBoat came to nothing")
			}
			servertest.RunTicks(t, srv, 2)
			return id
		}},
	}
	for _, v := range vehicles {
		for _, drops := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s drops %v", v.name, drops), func(t *testing.T) {
				t.Parallel()
				c, id, srv := barterWorld(t)
				srv.SetGameRule("entity_drops", drops)
				c.ChatCommand("gamemode survival")
				c.ChatCommand("item replace entity Slayer weapon.mainhand with minecraft:diamond_sword")
				c.Settle()
				servertest.AwaitFullSwing(t, srv, "Slayer")
				p := srv.SpawnMob(barterPiglin, 6.5, 64, 0.5)
				target := v.spawn(c, srv)
				if srv.MobTargetID(p) != 0 {
					t.Fatal("the piglin was angry before the blow")
				}
				c.Attack(target)
				c.Settle()
				servertest.RunTicks(t, srv, 1)
				if got := srv.MobTargetID(p) == id; got != drops {
					t.Fatalf("piglin targets the player = %v with entity_drops %v", got, drops)
				}
			})
		}
	}
}

// minecartID is the entity ID of the one minecart standing.
func minecartID(t *testing.T, srv *server.Server) int32 {
	t.Helper()
	carts := srv.MinecartsInDimension(world.DimensionName)
	if len(carts) != 1 {
		t.Fatalf("%d carts stand, want 1", len(carts))
	}
	return carts[0].ID
}
