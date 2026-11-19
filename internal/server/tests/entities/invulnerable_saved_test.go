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

// vanilla: Entity.isInvulnerableToBase, BlockAttachedEntity.push
func TestInvulnerableSavedKindsBlastDamageAndPush(t *testing.T) {
	for _, kind := range invulnerableKinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, _, _ := summonProtected(t, kind)
			srv.BlastAt(4.5, 64, 3.5, 2)
			if kind == "item_frame" || kind == "glow_item_frame" {
				// Invulnerable refuses the hurt; the exposed frame still breaks on push.
				if len(protectedEntities(srv, kind)) != 0 {
					t.Error("explosion push left the frame hanging")
				}
			} else {
				expectProtected(t, srv, kind)
			}
		})
	}
}

// vanilla: Entity.load, Entity.saveWithoutId
func TestInvulnerableSavedKindsKeepFlagOnDisk(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	for _, kind := range invulnerableKinds {
		extra := ""
		if kind == "item" {
			extra = `,Item:{id:"minecraft:stone",count:1},PickupDelay:32767s`
		}
		c.ChatCommand(`summon minecraft:` + kind + ` 4.5 70 4.5 {Invulnerable:1b,NoGravity:1b,Tags:[protected]` + extra + `}`)
		typ, _ := registry.EntityTypeID("minecraft:" + kind)
		c.AwaitSpawn(typ)
	}
	c.Settle()
	srv.FlushMobs()
	saved := servertest.SavedMobsByUUID(t, cfg.LevelName)
	if len(saved) != len(invulnerableKinds) {
		t.Fatalf("saved %d entities, want %d", len(saved), len(invulnerableKinds))
	}
	for _, tag := range saved {
		if tag["Invulnerable"] != nbt.Byte(1) {
			t.Errorf("%s saved Invulnerable = %v, want 1b", tag["id"], tag["Invulnerable"])
		}
	}

	c.Disconnect()
	srv.Shutdown()
	restarted := *cfg
	againSrv, connect := servertest.Start(t, &restarted, servertest.OpsListing(t, "Keeper"))
	servertest.FreezeUntilHeld(t, againSrv)
	again := connect().Join("Keeper")
	servertest.HoldTicks(t, againSrv)
	servertest.UntilTicks(t, againSrv, 20, "saved bodies wake", func() bool {
		for _, kind := range invulnerableKinds {
			if len(protectedEntities(againSrv, kind)) != 1 {
				return false
			}
		}
		return true
	})
	for _, kind := range invulnerableKinds {
		if protectedTag(t, againSrv, kind)["Invulnerable"] != nbt.Byte(1) {
			t.Errorf("%s lost Invulnerable on reload", kind)
		}
		again.ChatCommand("damage @e[type=minecraft:" + kind + ",limit=1] 100")
		servertest.AwaitCommandError(t, again, "commands.damage.invulnerable")
	}
}

// vanilla: DamageSource.isCreativePlayer, Entity.isInvulnerableToBase
func TestInvulnerableSavedKindsAllowCreativeCommandSources(t *testing.T) {
	for _, kind := range invulnerableKinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := summonProtected(t, kind)
			srv.SetPlayerGameMode("Keeper", protocol.GameModeCreative)
			for range 2 {
				c.ChatCommand("damage @e[type=minecraft:" + kind + ",limit=1] 100 minecraft:generic by Keeper")
				c.Settle()
			}
			servertest.RunTicks(t, srv, 2)
			if got := len(protectedEntities(srv, kind)); got != 0 {
				t.Errorf("%d entities after creative damage source, want 0", got)
			}
		})
	}
}

// vanilla: EntityDataAccessor.setData, Entity.load
func TestInvulnerableSavedKindsCanChangeFlagWithData(t *testing.T) {
	for _, kind := range invulnerableKinds {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := summonProtected(t, kind)
			target := "@e[type=minecraft:" + kind + ",limit=1]"
			c.ChatCommand("data merge entity " + target + " {Invulnerable:0b}")
			c.Settle()
			if protectedTag(t, srv, kind)["Invulnerable"] != nbt.Byte(0) {
				t.Fatal("data merge did not clear Invulnerable")
			}
			c.ChatCommand("data merge entity " + target + " {Invulnerable:1b}")
			c.Settle()
			expectProtected(t, srv, kind)
			c.ChatCommand("damage " + target + " 100")
			servertest.AwaitCommandError(t, c, "commands.damage.invulnerable")
			expectProtected(t, srv, kind)
		})
	}
}

func protectedEntities(srv *server.Server, kind string) []command.Entity {
	return slices.DeleteFunc(srv.SelectedEntities("minecraft:"+kind), func(e command.Entity) bool { return !slices.Contains(e.Tags(), "protected") })
}

// A shielded explosion has one damage even with no exposure, but no push.
// vanilla: ServerExplosion.hurtEntities, BlockAttachedEntity.push
func TestInvulnerableFrameOutlastsAShieldedBlast(t *testing.T) {
	t.Parallel()
	srv, _, _ := summonProtected(t, "item_frame")
	for x := int32(2); x <= 6; x++ {
		for y := int32(62); y <= 67; y++ {
			srv.SetBlock(x, y, 5, registry.BlockObsidian.DefaultState())
		}
	}
	srv.BlastAt(4.5, 64.5, 6.5, 2)
	expectProtected(t, srv, "item_frame")
}
