package sounds

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const packLoudInstrument = `{
  "description": {"text": "Loud"},
  "range": 64.0,
  "sound_event": "minecraft:entity.ender_dragon.growl",
  "use_duration": 1.0
}`

// The classic wolf with its ambient sound replaced by the cat's.
const packCatlikeWolf = `{
  "adult_sounds": {
    "ambient_sound": "minecraft:entity.cat.ambient",
    "death_sound": "minecraft:entity.wolf.death",
    "growl_sound": "minecraft:entity.wolf.growl",
    "hurt_sound": "minecraft:entity.wolf.hurt",
    "pant_sound": "minecraft:entity.cat.ambient",
    "step_sound": "minecraft:entity.wolf.step",
    "whine_sound": "minecraft:entity.cat.ambient"
  },
  "baby_sounds": {
    "ambient_sound": "minecraft:entity.baby_wolf.ambient",
    "death_sound": "minecraft:entity.baby_wolf.death",
    "growl_sound": "minecraft:entity.baby_wolf.growl",
    "hurt_sound": "minecraft:entity.baby_wolf.hurt",
    "pant_sound": "minecraft:entity.baby_wolf.pant",
    "step_sound": "minecraft:entity.baby_wolf.step",
    "whine_sound": "minecraft:entity.baby_wolf.whine"
  }
}`

// TestDataPackInstrumentAndSoundVariant: a pack adding test:loud to
// minecraft:instrument and overriding wolf_sound_variant/classic makes a goat
// horn with that instrument play the pack's sound, and a classic wolf speak
// the pack's ambient sound.
// Process-wide registry state, so the test is not parallel.
// vanilla: InstrumentItem.play, Wolf.getSoundSet
func TestDataPackInstrumentAndSoundVariant(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRegistries(nil) })
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "sounds")
	for file, body := range map[string]string{
		"pack.mcmeta":                                    `{"pack":{"description":"sounds"}}`,
		"data/test/instrument/loud.json":                 packLoudInstrument,
		"data/minecraft/wolf_sound_variant/classic.json": packCatlikeWolf,
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, connect := servertest.Start(t, cfg)
	srv.StopTick()
	srv.SetDayTime(18000)
	blower := connect().Join("Blower")
	near := connect().Join("Near")
	blower.Settle()
	near.Settle()

	horn := protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:goat_horn"), Count: 1}
	horn.SetInstrument(registry.MustID("minecraft:instrument", "test:loud"))
	srv.GiveSlot("Blower", 36, horn)
	servertest.UseHeld(blower, 0, 0)
	var heard hornNews
	blower.SettleAndCollect(func(int32, []byte) {})
	near.SettleAndCollect(heard.record)
	want, _ := registry.SoundEventID("minecraft:entity.ender_dragon.growl")
	if heard.sounds != 1 || heard.soundID != want {
		t.Errorf("a listener heard %d horn sounds, the last %d, want one of the dragon growl %d", heard.sounds, heard.soundID, want)
	}

	id := srv.SpawnMob("minecraft:wolf", 2.5, 64, 1.5)
	near.ExpectMobSpawn(protocol.EntityTypeWolf)
	srv.SetMobSoundVariant(id, "minecraft:classic")
	var cat, wolf bool
	for tick := 0; tick < servertest.AmbientTicks && !cat; tick += 10 {
		srv.TickMobs(10)
		near.ReadTicks(srv, 0, func(pid int32, data []byte) bool {
			if pid != protocol.IDPlaySoundEffect {
				return false
			}
			switch name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data); name {
			case "minecraft:entity.cat.ambient":
				cat = true
			case "minecraft:entity.wolf.ambient", "minecraft:entity.wolf.pant":
				wolf = true
			}
			return false
		})
	}
	if !cat || wolf {
		t.Errorf("classic wolf spoke the cat's ambient sound: %v, the vanilla wolf's: %v; want only the cat's", cat, wolf)
	}
}
