package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

func TestGeneratedVillageMobsWakeAndSendVillagerData(t *testing.T) {
	// Save one real village-centre chunk before a server can wake its entities;
	// the surrounding view uses the harness's flat terrain to keep generation cheap.
	dir := t.TempDir()
	w, err := world.NewWorld(dir, world.NewOverworldGenerator(0))
	if err != nil {
		t.Fatal(err)
	}
	pos := world.ChunkPos{X: 16, Z: 58}
	w.Acquire(pos)
	w.Release(pos)
	w.Close()
	cfg := servertest.MobTestConfig()
	cfg.LevelName, cfg.LevelSeed = dir, "0"
	cfg.SimulationDistance, cfg.ViewDistance = 2, 2
	cfg.GameMode = "spectator"
	srv, c := servertest.Join(t, cfg, "Visitor", servertest.OpsListing(t, "Visitor"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("tp @s 270 70 938")
	villager := villagerType(t)
	kinds := map[int32]int{}
	villagers := map[int32]*servertest.VariantSeen{}
	uuids := map[string]bool{}
	if !c.ReadTicks(srv, 100, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySpawnEntity:
			eid := r.ReadVarInt()
			uid := r.ReadUUID().String()
			kind := r.ReadVarInt()
			if kind != villager && kind != protocol.EntityTypeIronGolem && kind != protocol.EntityTypeCat {
				return false
			}
			if uuids[uid] {
				t.Fatal("a generated mob was spawned twice")
			}
			uuids[uid] = true
			kinds[kind]++
			if kind == villager {
				villagers[eid] = &servertest.VariantSeen{EntityID: eid, Meta: map[uint8][]int32{}}
			}
		case protocol.IDPlayEntityMetadata:
			if seen := villagers[r.ReadVarInt()]; seen != nil {
				c.ReadVariantMetadata(r, seen)
			}
		}
		if kinds[villager] != 2 || kinds[protocol.EntityTypeIronGolem] != 1 || kinds[protocol.EntityTypeCat] != 1 {
			return false
		}
		for _, seen := range villagers {
			if data := seen.Meta[protocol.MetadataIndexVillagerVillagerData]; len(data) != 3 || data[0] != 2 || data[2] != 1 {
				return false
			}
		}
		return true
	}) {
		t.Fatalf("village mobs or villager metadata missing: kinds %v, villagers %v", kinds, villagers)
	}
}
