package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestLightningTurnsAVillagerIntoAWitch: a bolt on a villager outside
// peaceful replaces it with a persistent witch that keeps the villager's
// name; on peaceful the villager stays and is hurt instead.
// vanilla: Villager.thunderHit
func TestLightningTurnsAVillagerIntoAWitch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		difficulty uint8
		converts   bool
	}{{"normal", 2, true}, {"peaceful", 0, false}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), "Struck")
			srv.StopTick()
			srv.SetDifficulty(tc.difficulty)
			pos := srv.PlayerPosition("Struck")
			villager := srv.SpawnMob("minecraft:villager", pos.X+1, pos.Y, pos.Z)
			srv.SetMobNamed(villager, "Hex", false)

			srv.StrikeLightning(pos.X+1, pos.Y, pos.Z)
			srv.TickBolts(1)

			counts := kindsNear(srv)
			if !tc.converts {
				if counts["minecraft:villager"] != 1 || counts["minecraft:witch"] != 0 {
					t.Fatalf("mobs after a peaceful bolt = %v, want the villager alone", counts)
				}
				if hp, _ := srv.MobHealth(villager); hp >= 20 {
					t.Errorf("the peaceful villager has %v health, want it hurt by the bolt", hp)
				}
				return
			}
			if counts["minecraft:villager"] != 0 || counts["minecraft:witch"] != 1 {
				t.Fatalf("mobs after the bolt = %v, want one witch and no villager", counts)
			}
			for _, m := range srv.Mobs() {
				if m.Kind != "minecraft:witch" {
					continue
				}
				if m.Name != "Hex" {
					t.Errorf("the witch is named %q, want the villager's Hex", m.Name)
				}
				if !srv.MobPersistent(m.ID) {
					t.Error("the witch is not persistent")
				}
				// The conversion takes the bolt's first harm tick; the witch is
				// hurt only by a later one.
				if hp, _ := srv.MobHealth(m.ID); hp != 26 {
					t.Errorf("the witch has %v health, want a fresh witch's 26", hp)
				}
			}
		})
	}
}

// TestLightningFlipsAMooshroomOncePerBolt: a bolt turns a red mooshroom
// brown with entity.mooshroom.convert at volume 2, once however many ticks
// it lingers, without hurting or burning it; a second bolt turns it red.
// vanilla: MushroomCow.thunderHit
func TestLightningFlipsAMooshroomOncePerBolt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Struck")
	srv.StopTick()
	srv.SetDifficulty(2)
	pos := srv.PlayerPosition("Struck")
	cow := srv.SpawnMob("minecraft:mooshroom", pos.X+1, pos.Y, pos.Z)
	mushroom := func() string {
		for _, m := range srv.Mobs() {
			if m.ID == cow {
				return m.Mushroom
			}
		}
		t.Fatal("the mooshroom is gone")
		return ""
	}
	// A bolt lingers while it flashes; the next strikes once it is gone.
	strike := func() {
		srv.StrikeLightning(pos.X+1, pos.Y, pos.Z)
		for range 200 {
			if srv.BoltCount() == 0 {
				return
			}
			srv.TickBolts(1)
		}
		t.Fatal("the bolt never ended")
	}
	if got := mushroom(); got != "red" {
		t.Fatalf("a spawned mooshroom is %q, want red", got)
	}

	strike()
	if got := mushroom(); got != "brown" {
		t.Fatalf("a struck mooshroom is %q, want brown", got)
	}
	if hp, _ := srv.MobHealth(cow); hp != 10 {
		t.Errorf("the struck mooshroom has %v health, want its full 10", hp)
	}
	if fire := srv.MobFireTicks(cow); fire > 0 {
		t.Errorf("the struck mooshroom burns for %d ticks, want none", fire)
	}
	converts := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySoundEffect {
			return
		}
		name, category, _, _, _, volume, _ := servertest.ReadAmbient(t, data)
		if name != "minecraft:entity.mooshroom.convert" {
			return
		}
		converts++
		if category != protocol.SoundCategoryNeutral || volume != 2 {
			t.Errorf("the conversion played in category %d at volume %v, want neutral at 2", category, volume)
		}
	})
	if converts != 1 {
		t.Errorf("the bolt played %d mooshroom conversions, want one", converts)
	}

	strike()
	if got := mushroom(); got != "red" {
		t.Errorf("a mooshroom struck twice is %q, want red again", got)
	}
}
