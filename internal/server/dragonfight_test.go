package server

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

func TestDragonFightSavedStateAndPartIDs(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	// HOLDING_PATTERN must be sent explicitly: the client's untold phase is HOVERING (10).
	dragon := entity.NewMob(entity.TypeEnderDragon, 1, 0, 128, 0, 1)
	dragon.SetDragonPhase(entity.DragonHolding, 0)
	r := protocol.NewReader(mobMetadataPayload(&mobEntity{mob: dragon}))
	r.ReadVarInt()
	if r.ReadU8() != protocol.MetadataIndexDragonPhase || r.ReadVarInt() != protocol.MetadataVarInt || r.ReadVarInt() != 0 {
		t.Fatal("spawn omitted holding phase")
	}
	id := s.nextMobEntity(entity.TypeEnderDragon)
	if next := s.nextEntity(); next != id+9 {
		t.Fatalf("next entity=%d, want %d after eight parts", next, id+9)
	}
	f := s.dragonFight
	if !f.scan || f.killed {
		t.Fatalf("fresh fight scan=%v killed=%v", f.scan, f.killed)
	}
	f.dragon = uuid.New()
	f.scan = false
	f.killed = true
	f.data["previously_killed"] = nbt.Byte(1)
	f.data["exit_portal_location"] = nbt.IntArray{0, 64, 0}
	f.ritual.Stage, f.ritual.Time = "preparing_to_summon_pillars", 17
	f.ritual.Crystals = []uuid.UUID{uuid.New()}
	s.saveDragonFight()
	saved := s.dimensionByName(world.EndName).w.ReadDragonFight()
	if !reflect.DeepEqual(saved, f.data) {
		t.Fatalf("saved data differs: %v", saved)
	}
	s.loadDragonFight()
	if s.dragonFight.ritual.Stage != f.ritual.Stage || s.dragonFight.ritual.Time != f.ritual.Time || !reflect.DeepEqual(s.dragonFight.ritual.Crystals, f.ritual.Crystals) {
		t.Fatal("respawn state changed on reload")
	}
	if s.dragonFight.scan || !s.dragonFight.killed || s.dragonFight.dragon != f.dragon || !reflect.DeepEqual(s.dragonFight.data, saved) {
		t.Fatal("fight state changed on reload")
	}
	gateways, _ := nbt.Get[nbt.List](saved, "gateways")
	seen := map[nbt.Tag]bool{}
	for _, g := range gateways.Items {
		seen[g] = true
	}
	if len(seen) != 20 {
		t.Fatalf("gateways=%v, want a permutation of 0..19", gateways)
	}
	// A dragon outside the linked fight pays 500; mob_drops suppresses it.
	total := func() int {
		xp := 0
		for _, orb := range s.OrbsInWorld() {
			xp += orb.XP
		}
		return xp
	}
	for tick := 155; tick <= 200; tick += 5 {
		s.applyDragonDeath(dragonDeathEffect{w: s.world, id: uuid.New(), time: tick, silent: true})
	}
	if xp := total(); xp != 500 {
		t.Fatalf("repeat dragon experience=%d", xp)
	}
	s.SetGameRule(world.RuleMobDrops.String(), false)
	for tick := 155; tick <= 200; tick += 5 {
		s.applyDragonDeath(dragonDeathEffect{w: s.world, id: uuid.New(), time: tick, silent: true})
	}
	if total() != 500 {
		t.Fatal("mob_drops=false awarded experience")
	}

}
