package raids

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// TestARaiderSavesItsRaidId: an enrolled raider writes RaidId, and a raider
// loaded from that compound after raids.dat reloads rejoins the same raid in
// its wave.
// vanilla: Raider.addAdditionalSaveData, Raider.readAdditionalSaveData
func TestARaiderSavesItsRaidId(t *testing.T) {
	t.Parallel()
	srv, c, _ := village(t, rules.DifficultyNormal)
	startRaid(t, c, srv, 0)
	id := srv.SpawnMob("minecraft:pillager", 6.5, 64, 6.5)
	run(c, srv, 25)
	tag, ok := srv.MobSaveTag(id)
	if !ok {
		t.Fatal("no saved tag for the pillager")
	}
	raidID, ok := nbt.IntField(tag, "RaidId")
	if !ok || raidID < 1 {
		t.Fatalf("RaidId = %v, %v; want the raid's id", raidID, ok)
	}
	srv.SaveRaids()
	srv.ReloadRaids()
	loaded := srv.SpawnMobFromTag(tag, 6.5, 64, 6.5)
	run(c, srv, 21)
	if in, _ := srv.MobRaid(loaded); !in {
		t.Fatal("the reloaded raider is not in its raid")
	}
	if got := srv.Raids()[0].Alive; got != 1 {
		t.Errorf("raiders counted = %d, want 1", got)
	}
	if got := srv.Raids()[0].Progress; got != 1 {
		t.Errorf("restored health progress = %v, want 1", got)
	}
	back, _ := srv.MobSaveTag(loaded)
	if again, _ := nbt.IntField(back, "RaidId"); again != raidID {
		t.Errorf("RaidId after reload = %d, want %d", again, raidID)
	}
}

// vanilla: Raider.readAdditionalSaveData, Raid.addWaveMob
func TestReloadingARaiderPreservesWaveCountAndHealth(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Hero", servertest.OpsListing(t, "Hero"))
	servertest.HoldTicks(t, srv)
	srv.SetDifficulty(rules.DifficultyNormal)
	srv.SetPlayerGameMode("Hero", protocol.GameModeCreative)
	if !srv.SetBlock(3, 64, 0, registry.MustState("minecraft:bell", nil)) {
		t.Fatal("the bell was not placed")
	}
	spawnVillager(t, srv, [3]int32{3, 64, 0})
	startRaid(t, c, srv, 0)
	survivor := srv.SpawnMob("minecraft:pillager", 6.5, 64, 6.5)
	bar := srv.Raids()[0].BarWire
	var title protocol.Text
	var progress float32
	readBar := func(id int32, data []byte) bool {
		if id != protocol.IDPlayBossEvent {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadUUID() != bar {
			return false
		}
		switch r.ReadVarInt() {
		case protocol.BossEventName:
			title, _ = protocol.ReadText(r)
		case protocol.BossEventProgress:
			progress = r.ReadF32()
		}
		return false
	}
	c.ReadTicks(srv, 40, readBar)
	tag, ok := srv.MobSaveTag(survivor)
	if !ok {
		t.Fatal("no saved tag for the pillager")
	}
	totalHealth := float32(nbt.DoubleFieldOr(tag, "Health", 0))
	if totalHealth == 0 {
		t.Fatal("no enrolled raider health")
	}
	wantTitle := protocol.Text{Translate: "event.minecraft.raid", Extra: []protocol.Text{
		{Text: " - "},
		{Translate: "event.minecraft.raid.raiders_remaining", With: []protocol.Text{{Text: "1"}}},
	}}
	identity, _ := srv.MobUUID(survivor)
	for load := 1; load <= 3; load++ {
		c.ChatCommand(fmt.Sprintf("data merge entity %s {NoAI:1b,Health:12f,PortalCooldown:%d}", identity, load))
		servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.modified",
			servertest.EntityDisplayComponent("minecraft:pillager", "entity.minecraft.pillager", uuid.MustParse(identity)))
		c.ReadTicks(srv, 40, readBar)
		if got := srv.Raids()[0].Alive; got != 1 {
			t.Errorf("load %d: raiders counted = %d, want 1", load, got)
		}
		if !reflect.DeepEqual(title, wantTitle) {
			t.Errorf("load %d: boss bar title = %s, want %s", load, protocol.JSONText(title), protocol.JSONText(wantTitle))
		}
		want := 12 / totalHealth
		if got := srv.Raids()[0].Progress; got != want {
			t.Errorf("load %d: raid health progress = %v, want %v", load, got, want)
		}
		// Raid bars pass through a 24-bit fixed-point value before the wire.
		if math.Abs(float64(progress-want)) > 1.0/(1<<24) {
			t.Errorf("load %d: boss bar progress = %v, want %v", load, progress, want)
		}
	}
	srv.KillMob(survivor)
	c.ReadTicks(srv, 40, readBar)
	if got := srv.Raids()[0].Alive; got != 0 {
		t.Errorf("after death: raiders counted = %d, want 0", got)
	}
}
