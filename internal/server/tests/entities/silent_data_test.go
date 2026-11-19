package entities

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Entity.load, Entity.saveWithoutId, Entity.setSilent
func TestSilentEntityDataAndMetadata(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Listener", servertest.OpsListing(t, "Listener"))
	servertest.HoldTicks(t, srv)
	r := servertest.NewMetadataRecorder(t, srv, c)
	for _, kind := range silentEntityKinds {
		t.Run(kind, func(t *testing.T) {
			packets := r.Run(silentSummonCommand(kind))
			typ, _ := registry.EntityTypeID("minecraft:" + kind)
			id := servertest.SpawnedID(packets, typ)
			if id == 0 {
				t.Fatal("no spawn received")
			}
			checkSilentMetadata(t, packets, id, true)
			target := "@e[type=minecraft:" + kind + ",limit=1]"
			checkSilentData(t, r.Run("data get entity "+target+" Silent"), true)
			for _, value := range []string{"0b", "1b"} {
				checkSilentMetadata(t, r.Run("data merge entity "+target+" {Silent:"+value+"}"), id, value == "1b")
				// Vanilla omits a false Silent from saved data.
				checkSilentData(t, r.Run("data get entity "+target+" Silent"), value == "1b")
			}
			r.Run("kill " + target)
		})
	}
}

func checkSilentMetadata(t *testing.T, packets []servertest.PushedPacket, id int32, want bool) {
	t.Helper()
	found := false
	for _, p := range packets {
		if p.Id != protocol.IDPlayEntityMetadata {
			continue
		}
		rd := protocol.NewReader(p.Data)
		if rd.ReadVarInt() != id {
			continue
		}
		entries, _ := servertest.ReadMetadata(rd)
		if e := servertest.MetaOf(entries, protocol.MetadataIndexSilent); e != nil {
			found = true
			if e.Typ != protocol.MetadataBoolean || e.FlagVal != want {
				t.Errorf("Silent metadata = %+v, want %v (boolean)", e, want)
			}
		}
	}
	if !found {
		t.Errorf("no Silent=%v metadata for entity %d", want, id)
	}
}

func checkSilentData(t *testing.T, packets []servertest.PushedPacket, want bool) {
	t.Helper()
	for _, p := range packets {
		if p.Id != protocol.IDPlaySystemChat {
			continue
		}
		text, ok := protocol.ReadText(protocol.NewReader(p.Data))
		if !ok {
			continue
		}
		if want && text.Translate == "commands.data.entity.query" && len(text.With) == 2 && text.With[1].Plain() == "1b" {
			return
		}
		if !want && strings.Contains(text.Plain(), "Found no elements matching Silent") {
			return
		}
	}
	t.Errorf("/data get Silent did not return expected value (set=%v)", want)
}

var silentEntityKinds = []string{
	"item_frame", "glow_item_frame", "painting", "oak_boat", "oak_chest_boat", "bamboo_raft", "bamboo_chest_raft",
	"minecart", "chest_minecart", "hopper_minecart", "furnace_minecart", "tnt_minecart", "command_block_minecart", "spawner_minecart",
	"armor_stand", "experience_orb", "arrow", "spectral_arrow", "trident", "falling_block", "mannequin", "area_effect_cloud", "item",
}

func silentSummonCommand(kind string) string {
	extra := ""
	switch kind {
	case "item_frame", "glow_item_frame":
		extra = ",Fixed:1b"
	case "painting":
		extra = ",variant:\"minecraft:kebab\""
	case "area_effect_cloud":
		extra = ",Duration:10000"
	case "item":
		extra = ",Item:{id:\"minecraft:stone\",count:1},PickupDelay:32767s"
	}
	return "summon minecraft:" + kind + " 4.5 70 4.5 {Silent:1b,NoGravity:1b" + extra + "}"
}

func TestSilentEntitiesSurviveRestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	srv.SetBlock(4, 70, 3, registry.BlockStone.DefaultState())
	r := servertest.NewMetadataRecorder(t, srv, c)
	r.Ticks = 0
	for _, kind := range silentEntityKinds {
		r.Run(silentSummonCommand(kind))
	}
	srv.FlushMobs()
	for _, kind := range silentEntityKinds {
		servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{}, "minecraft:"+kind, func(tag nbt.Compound) {
			if tag["Silent"] != nbt.Byte(1) {
				t.Errorf("saved %s Silent = %v, want 1b", kind, tag["Silent"])
			}
		})
	}
	c.Disconnect()
	srv.Shutdown()
	restarted := *cfg
	againSrv, connect := servertest.Start(t, &restarted, servertest.OpsListing(t, "Keeper"))
	servertest.FreezeUntilHeld(t, againSrv)
	again := connect().Join("Keeper")
	servertest.HoldTicks(t, againSrv)
	servertest.UntilTicks(t, againSrv, 20, "saved silent entities wake", func() bool {
		for _, kind := range silentEntityKinds {
			if len(againSrv.SelectedEntities("minecraft:"+kind)) != 1 {
				return false
			}
		}
		return true
	})
	r = servertest.NewMetadataRecorder(t, againSrv, again)
	r.Ticks = 0
	for _, kind := range silentEntityKinds {
		checkSilentData(t, r.Run("data get entity @e[type=minecraft:"+kind+",limit=1] Silent"), true)
	}
}
