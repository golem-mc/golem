package commands

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

func executePasses(out []protocol.Text) bool {
	return strings.Contains(fmt.Sprint(out), "conditional.pass")
}

// vanilla: EntityType.loadEntityRecursive, EntityType.by
func TestSummonedPassengersTakeIDsWithoutNamespace(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
	servertest.HoldTicks(t, srv)
	c.Settle()
	c.ChatCommand(`summon pig 0.5 64 4.5 {NoAI:1b,Passengers:[{id:"zombie",NoAI:1b},{id:"skeleton",NoAI:1b}]}`)
	c.ChatCommand(`summon minecart -5.5 64 4.5 {Passengers:[{id:"villager",NoAI:1b}]}`)
	c.Settle()
	for _, kind := range []string{"zombie", "skeleton", "villager"} {
		if !executePasses(srv.RunLine("execute if entity @e[type=" + kind + "]")) {
			t.Errorf("no %s rider spawned", kind)
		}
	}
	c.ChatCommand("ride @e[type=zombie,limit=1] dismount")
	servertest.AwaitSystemChatSaying(t, c, "commands.ride.dismount.success")
}

// vanilla: EntityType.loadEntityRecursive
func TestSavedPassengerWithoutNamespaceLoads(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	id := servertest.FixtureUUID(6, 3, 6, 3)
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id":   nbt.String("minecraft:pig"),
		"UUID": servertest.UuidWords(id),
		"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(4.5), nbt.Double(64), nbt.Double(0.5),
		}},
		"NoAI": nbt.Byte(1),
		"Passengers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"id": nbt.String("zombie"), "NoAI": nbt.Byte(1)},
		}},
	})
	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Loader")
	c.Configure()
	c.JoinSeeingMobs("Loader", id)
	if !executePasses(srv.RunLine("execute if entity @e[type=zombie]")) {
		t.Error("the saved rider with a short id did not load")
	}
}
