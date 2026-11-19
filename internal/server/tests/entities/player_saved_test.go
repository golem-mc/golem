package entities

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: Player.addAdditionalSaveData, LivingEntity.addAdditionalSaveData, Entity.saveWithoutId
func TestPlayerSavedNBT(t *testing.T) {
	t.Parallel()
	cfg := livingSavedConfig()
	cfg.GameMode = "survival"
	srv, c := servertest.Join(t, cfg, "Scribe", servertest.OpsListing(t, "Scribe"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("item replace entity @s hotbar.0 with stick")
	c.Settle()
	for _, predicate := range []string{
		`{abilities:{mayBuild:1b,walkSpeed:0.1f,flySpeed:0.05f,invulnerable:0b,flying:0b,mayfly:0b,instabuild:0b}}`,
		`{equipment:{}}`,
		`{Invulnerable:0b,HurtTime:0s,DeathTime:0s,FallFlying:0b,Brain:{memories:{}}}`,
	} {
		c.ChatCommand("execute if entity @s[nbt=" + predicate + "]")
		line, _ := protocol.ReadText(protocol.NewReader(c.NextSystemChat()))
		if line.Translate != "commands.execute.conditional.pass_count" {
			t.Errorf("selector %s: %+v", predicate, line)
		}
	}
	attrs := livingData(t, c, "@s", "attributes")
	want := `[{id: "minecraft:block_interaction_range", base: 4.5d}, {id: "minecraft:entity_interaction_range", base: 3.0d}, {id: "minecraft:movement_speed", base: 0.10000000149011612d}, {id: "minecraft:waypoint_transmit_range", base: 6.0E7d}]`
	if attrs != want {
		t.Errorf("attributes = %s, want %s", attrs, want)
	}
	for _, mode := range []struct{ name, abilities string }{
		{"creative", `{invulnerable:1b,mayfly:1b,instabuild:1b,mayBuild:1b}`},
		{"spectator", `{invulnerable:1b,mayfly:1b,instabuild:0b,mayBuild:0b,flying:1b}`},
		{"adventure", `{invulnerable:0b,mayfly:0b,instabuild:0b,mayBuild:0b,flying:0b}`},
		{"survival", `{invulnerable:0b,mayfly:0b,instabuild:0b,mayBuild:1b,flying:0b}`},
	} {
		c.ChatCommand("gamemode " + mode.name)
		c.Settle()
		c.ChatCommand("execute if entity @s[nbt={Invulnerable:0b,abilities:" + mode.abilities + "}]")
		c.ExpectCommandFeedbackText("commands.execute.conditional.pass_count", protocol.Text{Number: new(int64(1))})
	}
	// Setting the default still creates an instance; a plain value read does not.
	c.ChatCommand("attribute @s minecraft:luck base set 0")
	c.Settle()
	if got := livingData(t, c, "@s", `attributes[{id:"minecraft:luck"}].base`); got != "0.0d" {
		t.Errorf("explicit default attribute = %s, want 0.0d", got)
	}
	c.ChatCommand("attribute @s minecraft:attack_knockback base set 2")
	c.Settle()
	if got := livingData(t, c, "@s", `attributes[{id:"minecraft:attack_knockback"}].base`); got != "2.0d" {
		t.Errorf("extra attribute = %s, want 2.0d", got)
	}
	c.ChatCommand("item replace entity @s hotbar.0 with air")
	c.Settle()
	if data := livingData(t, c, "@s", ""); strings.Contains(data, "equipment:") {
		t.Errorf("empty-handed player still has equipment: %s", data)
	}

	release := srv.HoldChunk(0, 0)
	t.Cleanup(release)
	if !srv.SetBlock(0, 64, 0, servertest.BlockState(t, "minecraft:powder_snow")) {
		t.Fatal("powder snow chunk not loaded")
	}
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 140)
	c.ChatCommand("execute if entity @s[nbt={TicksFrozen:140}]")
	c.ExpectCommandFeedbackText("commands.execute.conditional.pass_count", protocol.Text{Number: new(int64(1))})
	c.MoveToGround(2.5, 64, 0.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 70)
	if data := livingData(t, c, "@s", ""); strings.Contains(data, "TicksFrozen") {
		t.Errorf("thawed player still has TicksFrozen: %s", data)
	}
}

// vanilla: LivingEntity.hurtServer, baseTick, tickDeath, Player.tryToStartFallFlying
func TestPlayerSavedLivingState(t *testing.T) {
	t.Parallel()
	cfg := livingSavedConfig()
	cfg.GameMode = "survival"
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Scribe"))
	c := connect().Join("Scribe")
	servertest.HoldTicks(t, srv)
	c.ChatCommand("damage @s 1")
	c.Settle()
	if got := livingData(t, c, "@s", "HurtTime"); got != "10s" {
		t.Fatalf("HurtTime after damage = %s, want 10s", got)
	}
	servertest.RunTicks(t, srv, 1)
	c.ChatCommand("damage @s 2")
	c.Settle()
	if got := livingData(t, c, "@s", "HurtTime"); got != "9s" {
		t.Fatalf("HurtTime after excess damage = %s, want 9s", got)
	}
	servertest.RunTicks(t, srv, 9)
	if got := livingData(t, c, "@s", "HurtTime"); got != "0s" {
		t.Fatalf("HurtTime after countdown = %s, want 0s", got)
	}
	c.ChatCommand("item replace entity @s armor.chest with elytra")
	c.Settle()
	c.MoveToGround(0.5, 68, 0.5, false)
	servertest.StartFallFlying(c)
	c.Settle()
	if got := livingData(t, c, "@s", "FallFlying"); got != "1b" {
		t.Fatalf("FallFlying while gliding = %s, want 1b", got)
	}
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if got := livingData(t, c, "@s", "FallFlying"); got != "0b" {
		t.Errorf("FallFlying after landing = %s, want 0b", got)
	}
	c.ChatCommand("kill @s")
	c.Settle()
	servertest.RunTicks(t, srv, 3)
	if got := livingData(t, c, "@s", "DeathTime"); got != "3s" {
		t.Errorf("DeathTime after three ticks = %s, want 3s", got)
	}
	c.Disconnect()
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Scribe"))
	if root["DeathTime"] != nbt.Short(3) || root["HurtTime"] != nbt.Short(7) {
		t.Errorf("saved death/hurt timers = %v/%v, want 3s/7s", root["DeathTime"], root["HurtTime"])
	}
	srv.StopTick()
	again := connect().Join("Scribe")
	for path, want := range map[string]string{"DeathTime": "3s", "HurtTime": "7s"} {
		if got := livingData(t, again, "@s", path); got != want {
			t.Errorf("%s on rejoining dead = %s, want %s", path, got, want)
		}
	}
}

// vanilla: LivingEntity.readAdditionalSaveData, Entity.load, Abilities.apply
func TestPlayerSavedStateLoads(t *testing.T) {
	t.Parallel()
	cfg := livingSavedConfig()
	cfg.GameMode = "survival"
	cfg.LevelName = t.TempDir()
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Scribe"), nbt.Compound{
		"Health": nbt.Float(20), "HurtTime": nbt.Short(7), "DeathTime": nbt.Short(3),
		"TicksFrozen": nbt.Int(80), "FallFlying": nbt.Byte(1), "Invulnerable": nbt.Byte(1),
		"abilities": nbt.Compound{"flySpeed": nbt.Float(0.07), "walkSpeed": nbt.Float(0.12)},
		"equipment": nbt.Compound{"chest": nbt.Compound{"id": nbt.String("minecraft:elytra"), "count": nbt.Int(1)}},
	})
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Scribe"))
	// Keep the restored timers and flight flag still through both joins.
	servertest.StopTicking(t, srv)
	for range 2 {
		c := connect().Join("Scribe")
		for path, want := range map[string]string{
			"HurtTime": "7s", "DeathTime": "3s", "TicksFrozen": "80", "FallFlying": "1b",
			"Invulnerable": "1b", "abilities.flySpeed": "0.07f", "abilities.walkSpeed": "0.12f",
			"equipment.chest.id": `"minecraft:elytra"`,
		} {
			if got := livingData(t, c, "@s", path); got != want {
				t.Errorf("%s after loading = %s, want %s", path, got, want)
			}
		}
		c.Disconnect()
	}
}
