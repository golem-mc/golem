package commands

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func damageClient(t *testing.T) (id int32, c *servertest.Client) {
	t.Helper()
	id, c, _ = damageServer(t)
	return id, c
}

func damageServer(t *testing.T) (id int32, c *servertest.Client, srv *server.Server) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c = servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	c.Login("Testificate")
	c.Configure()
	return c.EnterWorldAs("Testificate"), c, srv
}

// damageCow is a cow with the loop stopped, so the hurt window never counts
// down between commands.
func damageCow(t *testing.T) (c *servertest.Client, srv *server.Server, cow int32, target string) {
	t.Helper()
	_, c, srv = damageServer(t)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.StopTick()
	cow = srv.SpawnMob("minecraft:cow", 2.5, 64, 0.5)
	c.Settle()
	return c, srv, cow, mobTarget(t, srv, cow)
}

// mobTarget names a mob by its UUID, which no other entity shares.
func mobTarget(t *testing.T, srv *server.Server, id int32) string {
	t.Helper()
	u, ok := srv.MobUUID(id)
	if !ok {
		t.Fatalf("mob %d has no UUID", id)
	}
	return u
}

func mobHealthIs(t *testing.T, srv *server.Server, id int32, want float64) {
	t.Helper()
	if got, ok := srv.MobHealth(id); !ok || got < want-0.001 || got > want+0.001 {
		t.Errorf("mob health = %v (found %v), want %v", got, ok, want)
	}
}

// TestDamageCommandHurtsByType checks /damage lowers health by the amount,
// names the type in the Damage Event and kills with that type's message.
func TestDamageCommandHurtsByType(t *testing.T) {
	t.Parallel()
	id, c := damageClient(t)

	c.ChatCommand("damage Testificate 5 minecraft:magic")
	if h := c.ExpectHealth(); h.Health != 15 {
		t.Errorf("health = %v after 5 magic, want 15", h.Health)
	}
	c.ExpectDamageEvent(id, "minecraft:magic", 0)

	c.Disconnect()
}

// TestDamageCommandKillsWithTheTypesMessage checks a death from /damage
// uses the damage type's message.
func TestDamageCommandKillsWithTheTypesMessage(t *testing.T) {
	t.Parallel()
	id, c := damageClient(t)

	c.ChatCommand("damage Testificate 100 minecraft:magic")
	if h := c.ExpectHealth(); h.Health != 0 {
		t.Errorf("health = %v after 100 magic, want 0", h.Health)
	}
	c.ExpectDamageEvent(id, "minecraft:magic", 0)
	c.ExpectCombatDeath(id, protocol.Text{
		Translate: "death.attack.magic",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Testificate")},
	})
	c.Disconnect()
}

// TestDamageCommandRefusesInvulnerable checks a creative player is not
// hurt by a type that does not bypass invulnerability.
func TestDamageCommandRefusesInvulnerable(t *testing.T) {
	t.Parallel()
	_, c := damageClient(t)

	c.ChatCommand("gamemode creative")
	c.Settle()
	c.ChatCommand("damage Testificate 5 minecraft:magic")
	servertest.ExpectCommandError(t, c, "commands.damage.invulnerable")
	c.Disconnect()
}

// TestDamageCommandScalesWithDifficulty checks an always-scaling type grows
// by half on hard and is scaled away on peaceful, and a type that never
// scales is left alone.
func TestDamageCommandScalesWithDifficulty(t *testing.T) {
	t.Parallel()
	_, c, srv := damageServer(t)
	// Held, so no regeneration lands between a blow and its reading.
	servertest.HoldTicks(t, srv)

	srv.SetDifficulty(rules.DifficultyHard)
	c.ChatCommand("damage Testificate 10 minecraft:explosion")
	c.Settle()
	if got := srv.PlayerHealth("Testificate"); got != 5 {
		t.Errorf("health = %v after 10 explosion on hard, want 5", got)
	}

	srv.SetHealth("Testificate", 20)
	c.ChatCommand("damage Testificate 10 minecraft:magic")
	c.Settle()
	if got := srv.PlayerHealth("Testificate"); got != 10 {
		t.Errorf("health = %v after 10 magic on hard, want 10", got)
	}

	srv.SetHealth("Testificate", 20)
	srv.SetDifficulty(rules.DifficultyPeaceful)
	c.ChatCommand("damage Testificate 10 minecraft:explosion")
	servertest.ExpectCommandError(t, c, "commands.damage.invulnerable")
	if got := srv.PlayerHealth("Testificate"); got != 20 {
		t.Errorf("health = %v after explosion on peaceful, want 20", got)
	}
	c.Disconnect()
}

// TestDamageCommandHurtsMob checks the mob path: health lowers by the
// amount, a hit inside the window that does not beat it is refused and a
// larger one lands only its excess.
func TestDamageCommandHurtsMob(t *testing.T) {
	t.Parallel()
	c, srv, cow, target := damageCow(t)

	c.ChatCommand("damage " + target + " 4")
	c.Settle()
	mobHealthIs(t, srv, cow, 6)

	c.ChatCommand("damage " + target + " 4")
	servertest.ExpectCommandError(t, c, "commands.damage.invulnerable")
	mobHealthIs(t, srv, cow, 6)

	c.ChatCommand("damage " + target + " 5 minecraft:magic")
	c.Settle()
	mobHealthIs(t, srv, cow, 5)
	c.Disconnect()
}

// TestDamageCommandMobArmour checks a mob's natural armour softens a type
// that does not bypass it and a bypassing type lands whole.
func TestDamageCommandMobArmour(t *testing.T) {
	t.Parallel()
	_, c, srv := damageServer(t)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.StopTick()
	zombie := srv.SpawnMob("minecraft:zombie", 2.5, 64, 0.5)
	target := mobTarget(t, srv, zombie)
	c.Settle()

	c.ChatCommand("damage " + target + " 10 minecraft:mob_attack")
	c.Settle()
	mobHealthIs(t, srv, zombie, 20-9.84)
	c.Disconnect()
}

// expectDamageSource reads the next Damage Event and checks its cause and
// direct entity IDs (entity ID + 1, 0 for none) and its source position.
func expectDamageSource(t *testing.T, c *servertest.Client, cause, direct int32, at *[3]float64) {
	t.Helper()
	damageSourceIs(t, c.ExpectSurvival(protocol.IDPlayDamageEvent), cause, direct, at)
}

// damageSourceIs checks a Damage Event's cause and direct entity IDs and its
// source position.
func damageSourceIs(t *testing.T, data []byte, cause, direct int32, at *[3]float64) {
	t.Helper()
	r := protocol.NewReader(data)
	r.ReadVarInt() // victim
	r.ReadVarInt() // type
	if got := r.ReadVarInt(); got != cause {
		t.Errorf("damage cause ID = %d, want %d", got, cause)
	}
	if got := r.ReadVarInt(); got != direct {
		t.Errorf("damage direct ID = %d, want %d", got, direct)
	}
	has := r.ReadBool()
	if has != (at != nil) {
		t.Fatalf("damage event has position = %v, want %v", has, at != nil)
	}
	if has {
		got := [3]float64{r.ReadF64(), r.ReadF64(), r.ReadF64()}
		if got != *at {
			t.Errorf("damage event position = %v, want %v", got, *at)
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("damage event: %v", err)
	}
}

// TestDamageCommandAtGivesThePosition checks the at clause puts its position
// into the Damage Event and no entity behind the source.
func TestDamageCommandAtGivesThePosition(t *testing.T) {
	t.Parallel()
	_, c := damageClient(t)

	c.ChatCommand("damage Testificate 5 minecraft:magic at 1.5 64 -2.5")
	if h := c.ExpectHealth(); h.Health != 15 {
		t.Errorf("health = %v, want 15", h.Health)
	}
	expectDamageSource(t, c, 0, 0, &[3]float64{1.5, 64, -2.5})
	c.Disconnect()
}

// TestDamageCommandByNamesTheEntity checks the by clause makes the entity
// both cause and direct source of the Damage Event, without a position, and
// names it in the death message.
func TestDamageCommandByNamesTheEntity(t *testing.T) {
	t.Parallel()
	c, _, cow, target := damageCow(t)

	c.ChatCommand("damage Testificate 100 minecraft:magic by " + target)
	c.ExpectHealth()
	expectDamageSource(t, c, cow+1, cow+1, nil)
	death := c.ExpectSurvival(protocol.IDPlayCombatDeath)
	for _, want := range []string{"death.attack.magic", "entity.minecraft.cow"} {
		if !bytes.Contains(death, []byte(want)) {
			t.Errorf("combat death lacks %q: % X", want, death)
		}
	}
	c.Disconnect()
}
