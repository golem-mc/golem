package entities

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

var invulnerableKinds = []string{"oak_boat", "oak_chest_boat", "minecart", "chest_minecart", "tnt_minecart", "item", "item_frame", "glow_item_frame"}

func summonProtected(t *testing.T, kind string) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = "survival"
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	c.MoveToGrounded(4.5, 64, 2.5, 0, 0, true)
	c.Settle()
	srv.SetBlock(4, 64, 5, registry.BlockObsidian.DefaultState())
	extra := ""
	if kind == "item" {
		extra = `,Item:{id:"minecraft:stone",count:1},PickupDelay:32767s`
	} else if kind == "item_frame" || kind == "glow_item_frame" {
		extra = `,Facing:2b,Item:{id:"minecraft:stone",count:1}`
	}
	c.ChatCommand(`summon minecraft:` + kind + ` 4.5 64 4.5 {Invulnerable:1b,NoGravity:1b,Tags:[protected]` + extra + `}`)
	typ, ok := registry.EntityTypeID("minecraft:" + kind)
	if !ok {
		t.Fatal("missing type", kind)
	}
	spawned := c.AwaitSpawn(typ)
	c.Settle()
	return srv, c, spawned.EntityID
}

func protectedTag(t *testing.T, srv *server.Server, kind string) nbt.Compound {
	t.Helper()
	selected := protectedEntities(srv, kind)
	if len(selected) != 1 {
		t.Fatalf("%s count = %d, want 1", kind, len(selected))
	}
	tag, ok := selected[0].(command.DataTarget).DataTag()
	if !ok {
		t.Fatal("entity has no saved tag")
	}
	return tag.Entries()
}

func expectProtected(t *testing.T, srv *server.Server, kind string) {
	t.Helper()
	tag := protectedTag(t, srv, kind)
	if got := tag["Invulnerable"]; got != nbt.Byte(1) {
		t.Errorf("Invulnerable = %v, want 1b", got)
	}
	if kind == "item" && tag["Health"] != nbt.Short(5) {
		t.Errorf("Health = %v, want 5s", tag["Health"])
	}
	if kind == "item_frame" || kind == "glow_item_frame" {
		if _, ok := tag["Item"]; !ok {
			t.Error("refused hurt popped the frame's item")
		}
	}
}

// vanilla: Entity.load, Entity.saveWithoutId, Entity.isInvulnerableToBase
func TestInvulnerableSavedKindsRefuseDamageCommands(t *testing.T) {
	for _, kind := range invulnerableKinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := summonProtected(t, kind)
			expectProtected(t, srv, kind)
			target := "@e[type=minecraft:" + kind + ",limit=1]"
			c.ChatCommand("damage " + target + " 100")
			servertest.AwaitCommandError(t, c, "commands.damage.invulnerable")
			expectProtected(t, srv, kind)
			c.ChatCommand("damage " + target + " 100 minecraft:out_of_world")
			c.Settle()
			// A non-explosion hurt pops a filled frame before breaking the frame.
			if kind == "item_frame" || kind == "glow_item_frame" {
				if _, ok := protectedTag(t, srv, kind)["Item"]; ok {
					t.Fatal("bypassing damage did not pop the item")
				}
				c.ChatCommand("damage " + target + " 100 minecraft:out_of_world")
				c.Settle()
			}
			servertest.RunTicks(t, srv, 2)
			if got := len(protectedEntities(srv, kind)); got != 0 {
				t.Errorf("%d entities after bypassing damage, want 0", got)
			}
		})
	}
}

// vanilla: VehicleEntity.hurtServer, ItemFrame.hurtServer
func TestInvulnerableSavedKindsRefuseSurvivalAttacksAndArrows(t *testing.T) {
	for _, kind := range invulnerableKinds {
		// ItemEntity is not pickable.
		if kind == "item" {
			continue
		}
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c, id := summonProtected(t, kind)
			for range 3 {
				c.Attack(id)
			}
			c.Settle()
			expectProtected(t, srv, kind)
			// The shaft crosses the box clear of the player and reaches the wall.
			srv.ShootArrow(4.5, 64.4, 3.5, 0, 0, 3)
			servertest.RunTicks(t, srv, 5)
			expectProtected(t, srv, kind)
			srv.SetPlayerGameMode("Keeper", protocol.GameModeCreative)
			for range 2 {
				c.Attack(id)
			}
			c.Settle()
			servertest.RunTicks(t, srv, 2)
			if got := len(protectedEntities(srv, kind)); got != 0 {
				t.Errorf("%d entities after creative attacks, want 0", got)
			}
		})
	}
}

func protectedEntities(srv *server.Server, kind string) []command.Entity {
	return slices.DeleteFunc(srv.SelectedEntities("minecraft:"+kind), func(e command.Entity) bool { return !slices.Contains(e.Tags(), "protected") })
}
