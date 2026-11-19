package entities

import (
	"slices"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// awaitBoatCargoTitle waits for the chest boat's 9x3 window and returns its title.
func awaitBoatCargoTitle(t *testing.T, srv *server.Server, c *servertest.Client) (protocol.Text, bool) {
	t.Helper()
	var title protocol.Text
	opened := false
	c.ReadTicksSkipping(srv, 20, servertest.BoatSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayOpenScreen {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt() // the window ID
		if r.ReadVarInt() != protocol.MenuGeneric9x3 {
			return false
		}
		var ok bool
		if title, ok = protocol.ReadText(r); !ok {
			c.T.Fatal("open screen title did not read back")
		}
		opened = true
		return true
	})
	return title, opened
}

// vanilla: AbstractChestBoat.openCustomInventoryScreen, interact
func TestOpeningChestBoatCargoEmitsContainerOpen(t *testing.T) {
	t.Parallel()
	for _, riding := range []bool{false, true} {
		name := "sneak click"
		if riding {
			name = "inventory key"
		}
		t.Run(name, func(t *testing.T) {
			srv, c, _ := servertest.BoatPlayer(t, "creative", "Captain", 0)
			servertest.HoldTicks(t, srv)
			boatID := srv.SpawnBoat(2.5, 64, 0.5, "minecraft:oak_chest_boat")
			var mu sync.Mutex
			var events [][3]float64
			srv.ObserveGameEventContexts(func(event string, x, y, z float64, ctx rules.GameEventContext) {
				if event == "minecraft:container_open" && ctx.Player == protocol.OfflineUUID("Captain") {
					mu.Lock()
					events = append(events, [3]float64{x, y, z})
					mu.Unlock()
				}
			})
			if riding {
				c.UseOn(boatID)
				c.Settle()
				if !slices.Contains(srv.BoatPassengerIDs(boatID), c.PlayerEntityID) {
					t.Fatal("player did not board the chest boat")
				}
				c.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
					b.PutVarInt(c.PlayerEntityID)
					b.PutVarInt(protocol.PlayerCommandOpenInventory)
					b.PutVarInt(0)
				})
			} else {
				c.UseOnSneaking(boatID)
			}
			title, opened := awaitBoatCargoTitle(t, srv, c)
			if !opened || title.Translate != "entity.minecraft.oak_chest_boat" {
				t.Fatalf("cargo opened=%v, title=%+v, want the oak chest boat's 9x3 menu within 20 ticks", opened, title)
			}
			c.Settle()
			mu.Lock()
			defer mu.Unlock()
			if len(events) != 1 || events[0] != [3]float64{2.5, 64, 0.5} {
				t.Errorf("container open events = %v, want one at the boat with the player as source", events)
			}
		})
	}
}

// TestAChestBoatCargoIsTitledWithTheBoatsName: the window carries the
// entity's display name, not the chest's.
// vanilla: AbstractChestBoat.interact, Entity.getDisplayName
func TestAChestBoatCargoIsTitledWithTheBoatsName(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "creative", "Captain", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_chest_boat")
	c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(boatID)
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0.5, 0.5, 0.5)
		b.PutBool(true)
	})
	title, opened := awaitBoatCargoTitle(t, srv, c)
	if !opened {
		t.Fatal("the chest boat never opened its cargo")
	}
	if title.Translate != "entity.minecraft.oak_chest_boat" {
		t.Errorf("title translates %q, want entity.minecraft.oak_chest_boat", title.Translate)
	}
}

// TestASpectatorOpensAChestBoatsCargo: a spectator's click on a chest boat
// opens its menu, and boards nothing.
// vanilla: Player.interactOn
func TestASpectatorOpensAChestBoatsCargo(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "spectator", "Ghost", 0)
	boatID := srv.SpawnBoat(4.5, 64, 4.5, "minecraft:oak_chest_boat")
	c.UseOn(boatID)
	if _, opened := awaitBoatCargoTitle(t, srv, c); !opened {
		t.Fatal("a spectator's click opened nothing")
	}
}

// TestASpectatorOpensNothingOnAChestBoatWithALootTable: the table stays
// unrolled and no window opens.
// vanilla: AbstractChestBoat.createMenu
func TestASpectatorOpensNothingOnAChestBoatWithALootTable(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "spectator", "Ghost", 0)
	boatID := summonLootBoat(t, srv, `{LootTable:"`+lootBoatTable+`",LootTableSeed:7L}`)
	servertest.HoldTicks(t, srv)
	c.UseOn(boatID)
	if _, opened := awaitBoatCargoTitle(t, srv, c); opened {
		t.Error("a spectator opened a chest boat that still holds a loot table")
	}
	heart := servertest.MustItemID(t, "minecraft:heart_of_the_sea")
	if got := cargoCount(srv, boatID, heart); got != 0 {
		t.Errorf("the boat holds %d hearts after a spectator's click, want its table unrolled", got)
	}
	if tag, _ := srv.BoatSaveTag(boatID); tag["LootTable"] == nil {
		t.Error("the spectator's click rolled the loot table")
	}
}

// TestANamedChestBoatIsNamedInCommandsAndItsWindow: a summoned CustomName
// names the boat in /tag's feedback and titles its cargo window.
// vanilla: Entity.load, Entity.getDisplayName, AbstractChestBoat.interact
func TestANamedChestBoatIsNamedInCommandsAndItsWindow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Captain", servertest.OpsListing(t, "Captain"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.RunLine(`summon minecraft:oak_chest_boat 4.5 64 4.5 {CustomName:"Bob"}`)
	boats := srv.SelectedEntities("minecraft:oak_chest_boat")
	if len(boats) != 1 {
		t.Fatalf("%d chest boats answer the selector, want 1", len(boats))
	}
	want := servertest.EntityDisplayComponent("minecraft:oak_chest_boat", "", boats[0].UUID())
	want.Text, want.HoverEntity.Name = "Bob", protocol.Text{Text: "Bob"}
	c.ChatCommand("tag @e[type=oak_chest_boat,limit=1] add t")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single", protocol.Text{Text: "t"}, want)

	c.Send(protocol.IDPlayInteractIn, func(b *protocol.Buffer) {
		b.PutVarInt(srv.BoatIDs()[0])
		b.PutVarInt(protocol.HandMain)
		b.PutLpVec3(0.5, 0.5, 0.5)
		b.PutBool(true) // sneaking, so the click opens rather than boards
	})
	title, opened := awaitBoatCargoTitle(t, srv, c)
	if !opened {
		t.Fatal("the chest boat never opened its cargo")
	}
	if title.Text != "Bob" || title.Translate != "" {
		t.Errorf("title is %+v, want the text Bob", title)
	}
	if tag, _ := srv.BoatSaveTag(srv.BoatIDs()[0]); tag["CustomName"] != nbt.String("Bob") {
		t.Errorf("the boat saves CustomName %v, want Bob", tag["CustomName"])
	}
}

// TestANamedBoatTeleportedAcrossKeepsItsName: the twin /tp raises in
// another dimension carries the CustomName and CustomNameVisible.
// vanilla: Entity.restoreFrom
func TestANamedBoatTeleportedAcrossKeepsItsName(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Captain")
	// No player stands in the nether to keep the twin's chunk loaded.
	defer srv.HoldChunkInDimension(world.NetherName, 8, 8)()
	srv.RunLine(`summon minecraft:oak_boat 4.5 64 4.5 {CustomName:"Bob",CustomNameVisible:1b}`)
	srv.RunLine("execute in minecraft:the_nether run tp @e[type=minecraft:oak_boat,limit=1] 8.5 70 8.5")
	if n := srv.BoatCountInDimension(world.NetherName); n != 1 {
		t.Fatalf("%d boats in the nether, want 1", n)
	}
	tag, _ := srv.BoatSaveTag(srv.BoatIDs()[0])
	if tag["CustomName"] != nbt.String("Bob") || tag["CustomNameVisible"] != nbt.Byte(1) {
		t.Errorf("the twin saves CustomName %v, CustomNameVisible %v, want Bob and 1b", tag["CustomName"], tag["CustomNameVisible"])
	}
}

// TestAChestBoatsWindowClosesPastTheEntityRangeToItsBox: a survival player
// keeps the window within 3 + 4 of the hull's box and loses it past that,
// though still inside the block range plus 4.
// vanilla: ContainerEntity.isChestVehicleStillValid
func TestAChestBoatsWindowClosesPastTheEntityRangeToItsBox(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.BoatPlayer(t, "survival", "Sailor", 0)
	// The box spans x 1.8125..3.1875 and y 64..64.5625; the player stands
	// on z 0.5, inside its z span, so the eye (y 65.62) is dx and 1.0575
	// from it.
	boatID := srv.SpawnBoat(2.5, 64, 0.5, "minecraft:oak_chest_boat")
	c.UseOnSneaking(boatID) // a plain click boards it
	if _, opened := awaitBoatCargoTitle(t, srv, c); !opened {
		t.Fatal("the chest boat never opened its cargo")
	}
	isClose := func(id int32, _ []byte) bool { return id == protocol.IDPlayCloseContainer }
	c.MoveToGround(-4.6, 64, 0.5, true) // 6.5 from the box
	c.ExpectNoneWithinTicks(srv, 5, isClose)
	c.MoveToGround(-5.6125, 64, 0.5, true) // 7.5 from the box
	c.ExpectWithinTicks(srv, 5, protocol.IDPlayCloseContainer)
}
