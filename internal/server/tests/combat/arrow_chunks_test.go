package combat

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: AbstractArrow.findHitEntities, EntitySectionStorage.forEachAccessibleNonEmptySection
func TestAnArrowHitsAMobOverhangingTheNextChunk(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon cow 8.5 64 4.5 {NoAI:1b,Tags:["arrow_target"]}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.ChatCommand(`attribute @e[tag=arrow_target,limit=1] minecraft:scale base set 5`)
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
	c.ChatCommand(`tp @e[tag=arrow_target,limit=1] 16.5 64 4.5`)
	servertest.AwaitSystemChatSaying(t, c, "commands.teleport.success.location.single")

	// The query ends at x=15.75. The padding reaches the chunk holding
	// the cow's feet, and its expanded hitbox reaches the arrow at x=14.5.
	srv.ShootArrow(14.5, 65, 0.5, 0, 0, 3)
	ids := srv.MobIDsOfKind("minecraft:cow")
	if len(ids) != 1 {
		t.Fatalf("cows = %v, want the summoned cow", ids)
	}
	if !c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		return id == protocol.IDPlayDamageEvent && protocol.NewReader(data).ReadVarInt() == ids[0]
	}) {
		t.Fatal("the arrow flew through the cow's overhanging hitbox")
	}
}

// vanilla: AbstractArrow.findHitEntities, Level.getEntities
func TestAnArrowHitsADragonPartOutsideItsParentsChunkSearch(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	c.MoveTo(0.5, 128, 0.5, 0, 0)
	c.ChatCommand(`summon ender_dragon 16.25 128 8.5 {DragonPhase:10,Rotation:[0f,0f]}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	servertest.RunTicks(t, srv, 1) // Position the wings.
	ids := srv.MobIDsOfKind("minecraft:ender_dragon")
	if len(ids) != 1 {
		t.Fatalf("dragons = %v, want the summoned dragon", ids)
	}

	// The left wing crosses x=12.25. Even with the padding, the query
	// ends at x=15.5 and cannot see the chunk holding the parent.
	owner := servertest.UuidWords(protocol.OfflineUUID("Watcher"))
	c.ChatCommand(fmt.Sprintf(`summon arrow 12.25 131 3.5 {Motion:[0d,0d,4d],Owner:[I;%d,%d,%d,%d]}`, owner[0], owner[1], owner[2], owner[3]))
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	if !c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		return id == protocol.IDPlayDamageEvent && protocol.NewReader(data).ReadVarInt() == ids[0]
	}) {
		t.Fatal("the arrow flew through the dragon's wing")
	}
}

// vanilla: ThrownTrident.tick
func TestLoyalTridentsFindAMobOwnerInADistantChunk(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon cow 28.5 64 4.5 {NoAI:1b,UUID:[I;1,2,3,4]}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.ChatCommand(`summon trident 8.5 63.9 4.5 {inGround:1b,DealtDamage:1b,Owner:[I;1,2,3,4],item:{id:"minecraft:trident",count:1,components:{"minecraft:enchantments":{"minecraft:loyalty":3}}}}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.ChatCommand(`summon trident 8.5 65 5.5 {DealtDamage:1b,Owner:[I;1,2,3,4],item:{id:"minecraft:trident",count:1,components:{"minecraft:enchantments":{"minecraft:loyalty":3}}}}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")

	servertest.RunTicks(t, srv, 1)
	tridents := srv.ArrowsInDimension(world.DimensionName)
	if len(tridents) != 2 {
		t.Fatalf("tridents = %v, want both thrown tridents", tridents)
	}
	for _, trident := range tridents {
		if trident.X <= 8.5 || trident.VX <= 0 {
			t.Errorf("trident at (%g, %g, %g) has x velocity %g, want it moving toward the owner", trident.X, trident.Y, trident.Z, trident.VX)
		}
	}
}
