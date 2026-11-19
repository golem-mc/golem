package entities

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// glowFireKinds maps each kind to the shared flags byte its tick leaves set
// by Glowing:1b,HasVisualFire:1b: hanging entities never run Entity.baseTick,
// so they show only the glowing bit.
// vanilla: Entity.load, Entity.saveWithoutId, Entity.setSharedFlagOnFire
var glowFireKinds = map[string]uint8{
	"arrow":             protocol.EntityFlagGlowing | protocol.EntityFlagOnFire,
	"experience_orb":    protocol.EntityFlagGlowing | protocol.EntityFlagOnFire,
	"area_effect_cloud": protocol.EntityFlagGlowing | protocol.EntityFlagOnFire,
	"item_frame":        protocol.EntityFlagGlowing,
	"painting":          protocol.EntityFlagGlowing,
}

func glowFireSummon(kind string) string {
	return "summon minecraft:" + kind + " 4.5 70 4.5 {Glowing:1b,HasVisualFire:1b,NoGravity:1b}"
}

func checkFlagsMetadata(t *testing.T, packets []servertest.PushedPacket, id int32, want uint8) {
	t.Helper()
	var got *uint8
	for _, p := range packets {
		if p.Id != protocol.IDPlayEntityMetadata {
			continue
		}
		rd := protocol.NewReader(p.Data)
		if rd.ReadVarInt() != id {
			continue
		}
		entries, _ := servertest.ReadMetadata(rd)
		if e := servertest.MetaOf(entries, protocol.MetadataIndexEntityFlags); e != nil {
			v := e.ByteVal
			got = &v
		}
	}
	switch {
	case got == nil:
		t.Errorf("no shared flags metadata, want %#x", want)
	case *got != want:
		t.Errorf("shared flags = %#x, want %#x", *got, want)
	}
}

func checkTagData(t *testing.T, packets []servertest.PushedPacket, key string, want bool) {
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
		if !want && strings.Contains(text.Plain(), "Found no elements matching "+key) {
			return
		}
	}
	t.Errorf("/data get %s did not return expected value (set=%v)", key, want)
}

func TestGlowingVisualFireSurviveRestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	srv.SetBlock(4, 70, 3, registry.BlockStone.DefaultState())
	r := servertest.NewMetadataRecorder(t, srv, c)
	r.Ticks = 0
	for kind := range glowFireKinds {
		r.Run(glowFireSummon(kind))
	}
	srv.FlushMobs()
	for kind := range glowFireKinds {
		servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{}, "minecraft:"+kind, func(tag nbt.Compound) {
			for _, key := range []string{"Glowing", "HasVisualFire"} {
				if tag[key] != nbt.Byte(1) {
					t.Errorf("saved %s %s = %v, want 1b", kind, key, tag[key])
				}
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
	servertest.UntilTicks(t, againSrv, 20, "saved entities wake", func() bool {
		for kind := range glowFireKinds {
			if len(againSrv.SelectedEntities("minecraft:"+kind)) != 1 {
				return false
			}
		}
		return true
	})
	r = servertest.NewMetadataRecorder(t, againSrv, again)
	r.Ticks = 0
	for kind := range glowFireKinds {
		for _, key := range []string{"Glowing", "HasVisualFire"} {
			checkTagData(t, r.Run("data get entity @e[type=minecraft:"+kind+",limit=1] "+key), key, true)
		}
	}
}
