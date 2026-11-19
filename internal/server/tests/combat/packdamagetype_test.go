package combat

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestAPackDamageTypeScalesAndNamesItsDeath checks a pack's damage type read
// through its fields: scaling always multiplies /damage by the difficulty,
// and a death by it sends death.attack.<message_id>; a generic type stays
// unscaled. The tables are process-wide, so the test is not parallel.
// vanilla: DamageType.CODEC, DamageSource.scalesWithDifficulty
func TestAPackDamageTypeScalesAndNamesItsDeath(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackDamageTypes(nil); registry.ApplyPackRegistries(nil) })
	const name = "Zapped"
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	for file, body := range map[string]string{
		"pack.mcmeta":                  `{"pack":{"description":"p"}}`,
		"data/pk/damage_type/zap.json": `{"message_id":"zap","scaling":"always","exhaustion":0.5}`,
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c, entityID := servertest.JoinAs(t, cfg, name, servertest.OpsListing(t, name))
	srv.SetDifficulty(rules.DifficultyHard)
	srv.SetHealth(name, 20)
	servertest.HoldTicks(t, srv)

	c.ChatCommand("damage @s 4 pk:zap")
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the zap lands", func() bool { return srv.PlayerHealth(name) < 20 })
	if got := srv.PlayerHealth(name); got != 14 {
		t.Errorf("/damage 4 of an always-scaling type on hard left %v, want 14", got)
	}

	servertest.RunTicks(t, srv, 20)
	srv.SetHealth(name, 20)
	c.ChatCommand("damage @s 4 minecraft:generic")
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the generic hurt lands", func() bool { return srv.PlayerHealth(name) < 20 })
	if got := srv.PlayerHealth(name); got != 16 {
		t.Errorf("/damage 4 generic on hard left %v, want 16", got)
	}

	servertest.RunTicks(t, srv, 20)
	c.ChatCommand("damage @s 1000 pk:zap")
	var want protocol.Buffer
	want.PutVarInt(entityID)
	want.PutText(protocol.Text{
		Translate: "death.attack.zap",
		With:      []protocol.Text{servertest.PlayerDisplayComponent(name)},
	})
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		if id == protocol.IDPlayCombatDeath {
			if !bytes.Equal(data, want.Bytes()) {
				t.Errorf("combat death = %x, want %x (death.attack.zap)", data, want.Bytes())
			}
			return
		}
	}
	t.Error("no Combat Death after a lethal zap")
}
