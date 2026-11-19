package commands

import (
	"bytes"
	"math"
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

// TestDamageCommandKillsWithAnyTypesMessage checks a type outside the
// explicit death messages words the death by its message_id.
func TestDamageCommandKillsWithAnyTypesMessage(t *testing.T) {
	t.Parallel()
	id, c := damageClient(t)

	c.ChatCommand("damage Testificate 100 minecraft:cramming")
	c.ExpectHealth()
	c.ExpectDamageEvent(id, "minecraft:cramming", 0)
	c.ExpectCombatDeath(id, protocol.Text{
		Translate: "death.attack.cramming",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Testificate")},
	})
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

// TestDamageCommandCreativeBypassType checks a type that bypasses
// invulnerability hurts a creative player.
func TestDamageCommandCreativeBypassType(t *testing.T) {
	t.Parallel()
	_, c, srv := damageServer(t)

	c.ChatCommand("gamemode creative")
	c.Settle()
	c.ChatCommand("damage Testificate 5 minecraft:out_of_world")
	c.Settle()
	if got := srv.PlayerHealth("Testificate"); got != 15 {
		t.Errorf("creative health = %v after bypassing type, want 15", got)
	}
	c.Disconnect()
}

// TestDamageCommandPlayerWindow checks a hit inside the hurt window that
// does not beat the last one is refused, and a larger one lands its excess.
func TestDamageCommandPlayerWindow(t *testing.T) {
	t.Parallel()
	_, c, srv := damageServer(t)
	// Held, so the window does not count down between the two commands.
	servertest.HoldTicks(t, srv)

	srv.OpenHurtWindow("Testificate", 5)
	c.ChatCommand("damage Testificate 3 minecraft:magic")
	servertest.ExpectCommandError(t, c, "commands.damage.invulnerable")
	c.ChatCommand("damage Testificate 8 minecraft:magic")
	c.Settle()
	if got := srv.PlayerHealth("Testificate"); got != 17 {
		t.Errorf("health = %v after 8 over a window of 5, want 17", got)
	}
	c.Disconnect()
}

// TestDamageCommandZeroOnPlayer checks an amount of 0 refuses a player.
func TestDamageCommandZeroOnPlayer(t *testing.T) {
	t.Parallel()
	_, c := damageClient(t)

	c.ChatCommand("damage Testificate 0")
	servertest.ExpectCommandError(t, c, "commands.damage.invulnerable")
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

// TestDamageCommandZeroOnMob checks an amount of 0 on a mob succeeds with
// its health untouched and opens the hurt window.
func TestDamageCommandZeroOnMob(t *testing.T) {
	t.Parallel()
	c, srv, cow, target := damageCow(t)

	c.ChatCommand("damage " + target + " 0")
	if msg := c.ExpectPlay(protocol.IDPlaySystemChat); bytes.Contains(msg, []byte("commands.damage.invulnerable")) {
		t.Error("amount 0 on a mob was refused as invulnerable")
	}
	c.Settle()
	mobHealthIs(t, srv, cow, 10)
	c.ChatCommand("damage " + target + " 0")
	servertest.ExpectCommandError(t, c, "commands.damage.invulnerable")
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

// TestDamageCommandMobBypassesArmour checks a type tagged bypasses_armor
// lands whole on an armoured mob.
func TestDamageCommandMobBypassesArmour(t *testing.T) {
	t.Parallel()
	_, c, srv := damageServer(t)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.StopTick()
	zombie := srv.SpawnMob("minecraft:zombie", 2.5, 64, 0.5)
	target := mobTarget(t, srv, zombie)
	c.Settle()

	c.ChatCommand("damage " + target + " 10 minecraft:magic")
	c.Settle()
	mobHealthIs(t, srv, zombie, 10)
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

// TestDamageCommandByFromSplitsCauseAndDirect checks the from clause names
// the responsible entity apart from the one that carried the blow.
func TestDamageCommandByFromSplitsCauseAndDirect(t *testing.T) {
	t.Parallel()
	c, srv, cow, target := damageCow(t)
	other := srv.SpawnMob("minecraft:pig", 4.5, 64, 0.5)
	c.Settle()
	otherTarget := mobTarget(t, srv, other)

	c.ChatCommand("damage Testificate 5 minecraft:magic by " + target + " from " + otherTarget)
	c.ExpectHealth()
	expectDamageSource(t, c, other+1, cow+1, nil)
	c.Disconnect()
}

// TestDamageCommandOnMobNamesTheSource checks a mob's Damage Event carries
// the by clause's entity as cause and direct source, and the at clause's
// position.
func TestDamageCommandOnMobNamesTheSource(t *testing.T) {
	t.Parallel()
	c, srv, _, target := damageCow(t)
	zombie := srv.SpawnMob("minecraft:zombie", 4.5, 64, 0.5)
	c.Settle()
	zombieTarget := mobTarget(t, srv, zombie)

	c.ChatCommand("damage " + target + " 2 minecraft:mob_attack by Testificate")
	damageSourceIs(t, c.AwaitPlay(protocol.IDPlayDamageEvent), c.PlayerEntityID+1, c.PlayerEntityID+1, nil)

	c.ChatCommand("damage " + zombieTarget + " 2 minecraft:magic at 1.5 64 -2.5")
	damageSourceIs(t, c.AwaitPlay(protocol.IDPlayDamageEvent), 0, 0, &[3]float64{1.5, 64, -2.5})
	c.Disconnect()
}

// TestDamageCommandByWithoutTargetFails checks a by clause naming nobody is
// refused and hurts nothing.
func TestDamageCommandByWithoutTargetFails(t *testing.T) {
	t.Parallel()
	_, c, srv := damageServer(t)

	c.ChatCommand("damage Testificate 5 minecraft:magic by @e[type=minecraft:cow]")
	c.Settle()
	if got := srv.PlayerHealth("Testificate"); got != 20 {
		t.Errorf("health = %v after a by clause naming nobody, want 20", got)
	}
	c.Disconnect()
}

// damageShove runs a /damage command and returns the horizontal velocity of
// the first Set Entity Motion the player is sent, or ok false when none arrives.
func damageShove(t *testing.T, c *servertest.Client, srv *server.Server, id int32, command string) (vx, vz float64, ok bool) {
	t.Helper()
	c.ChatCommand(command)
	// The shove comes in the tick that runs the command; a second of ticks
	// is room for the slowest to arrive.
	c.ReadTicks(srv, 20, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlaySetEntityMotion {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		vx, _, vz = r.ReadLpVec3()
		ok = true
		return true
	})
	return vx, vz, ok
}

// TestDamageCommandAtShovesAwayFromThePosition checks a landed hit from an
// at position pushes the player away from it.
func TestDamageCommandAtShovesAwayFromThePosition(t *testing.T) {
	t.Parallel()
	id, c, srv := damageServer(t)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	vx, _, ok := damageShove(t, c, srv, id, "damage Testificate 1 minecraft:mob_attack at 5.5 64 0.5")
	if !ok || vx >= 0 {
		t.Errorf("shove = %v (sent %v), want a push toward -x", vx, ok)
	}
	c.Disconnect()
}

// TestDamageCommandNoKnockbackTypeDoesNotShove checks a type in no_knockback
// lands without a push.
func TestDamageCommandNoKnockbackTypeDoesNotShove(t *testing.T) {
	t.Parallel()
	id, c, srv := damageServer(t)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	if vx, _, ok := damageShove(t, c, srv, id, "damage Testificate 1 minecraft:generic at 5.5 64 0.5"); ok {
		t.Errorf("no_knockback type shoved by %v", vx)
	}
	c.Disconnect()
}

// TestDamageCommandTypeOnlyShovesPlayer checks a hit with no source pushes
// the player the full 0.4 in some horizontal direction, and a no_knockback
// type still leaves them be.
func TestDamageCommandTypeOnlyShovesPlayer(t *testing.T) {
	t.Parallel()
	id, c, srv := damageServer(t)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	if vx, vz, ok := damageShove(t, c, srv, id, "damage Testificate 1 minecraft:generic"); ok {
		t.Errorf("no_knockback type with no source shoved by (%v, %v)", vx, vz)
	}
	vx, vz, ok := damageShove(t, c, srv, id, "damage Testificate 1 minecraft:mob_attack")
	if speed := math.Hypot(vx, vz); !ok || math.Abs(speed-0.4) > 0.01 {
		t.Errorf("shove = (%v, %v) (sent %v), want horizontal speed 0.4", vx, vz, ok)
	}
	c.Disconnect()
}

// TestDamageCommandTypeOnlyShovesMob checks a hit with no source pushes a
// mob the full 0.4 in some horizontal direction, and a no_knockback type
// leaves it still. The loop is stopped, so the velocity read is the shove.
func TestDamageCommandTypeOnlyShovesMob(t *testing.T) {
	t.Parallel()
	c, srv, cow, target := damageCow(t)

	c.ChatCommand("damage " + target + " 1 minecraft:generic")
	c.Settle()
	mobHealthIs(t, srv, cow, 9)
	if vx, _, vz, _ := srv.MobVelocity(cow); vx != 0 || vz != 0 {
		t.Errorf("no_knockback type with no source shoved by (%v, %v)", vx, vz)
	}

	cow2 := srv.SpawnMob("minecraft:cow", -2.5, 64, 0.5)
	c.Settle()
	c.ChatCommand("damage " + mobTarget(t, srv, cow2) + " 1 minecraft:mob_attack")
	c.Settle()
	mobHealthIs(t, srv, cow2, 9)
	vx, _, vz, ok := srv.MobVelocity(cow2)
	if speed := math.Hypot(vx, vz); !ok || math.Abs(speed-0.4) > 0.01 {
		t.Errorf("mob velocity = (%v, %v) (found %v), want horizontal speed 0.4", vx, vz, ok)
	}
	c.Disconnect()
}

// TestDamageCommandScalesOnlyWhenCausedByALivingNonPlayer checks a
// when_caused_by_living_non_player type grows by half on hard with a mob as
// the cause and stays unscaled with a player as the cause.
// vanilla: DamageSource.scalesWithDifficulty
func TestDamageCommandScalesOnlyWhenCausedByALivingNonPlayer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		mob  bool
		want float64
	}{
		{"mob", true, 14},
		{"player", false, 16},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, c, srv := damageServer(t)
			srv.SetDifficulty(rules.DifficultyHard)
			servertest.HoldTicks(t, srv)
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			by := "by Testificate"
			if tc.mob {
				by = "by " + mobTarget(t, srv, srv.SpawnMob("minecraft:cow", 2.5, 64, 0.5))
				c.Settle()
			}
			c.ChatCommand("damage Testificate 4 minecraft:generic " + by)
			c.Settle()
			if got := srv.PlayerHealth("Testificate"); got != tc.want {
				t.Errorf("health = %v after 4 generic %s on hard, want %v", got, by, tc.want)
			}
			c.Disconnect()
		})
	}
}
