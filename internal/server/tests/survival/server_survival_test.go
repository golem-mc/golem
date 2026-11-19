package survival

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestPlayerJoinsInConfiguredMode checks that the gamemode of
// server.properties decides the mode a player joins in: the config says
// creative, and the abilities packet of the join is flying's.
func TestPlayerJoinsInConfiguredMode(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	_, c := servertest.Dial(t, cfg)
	c.Login("Testificate")
	c.Configure()

	var sawAbilities bool
	for packetID, data := range c.Packets(servertest.JoinTimeout) {
		switch packetID {
		case protocol.IDPlayAbilities:
			if got := protocol.NewReader(data).ReadI8(); got != servertest.AbilityFlagsFor(protocol.GameModeCreative) {
				t.Errorf("abilities flags = %#04x, want the creative set", got)
			}
			sawAbilities = true
		case protocol.IDPlayChunkBatchFinished:
			if !sawAbilities {
				t.Fatal("the join sequence ended before abilities arrived")
			}
			return
		}
	}
	t.Fatalf("the join did not finish within %s", servertest.JoinTimeout)
}

// TestGameModeCommand checks /gamemode against the runner themselves: every
// mode it can name, the packets each change sends, the chat feedback, and
// the mode a numeric argument does not change (vanilla takes names
// only).
func TestGameModeCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// /gamemode is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	playerUUID := c.Login("Testificate")
	c.Configure()
	c.EnterWorldAs("Testificate")

	for _, tc := range []struct {
		word string
		mode uint8
	}{
		{"creative", protocol.GameModeCreative},
		{"spectator", protocol.GameModeSpectator},
		{"adventure", protocol.GameModeAdventure},
		{"survival", protocol.GameModeSurvival},
	} {
		c.ChatCommand("gamemode " + tc.word)
		c.ExpectModePackets(playerUUID, tc.mode, true)
	}

	// A number names no mode, so it changes nothing: the proof lands on the
	// file the player is saved to, which would hold the number's mode had
	// the server believed it.
	c.ChatCommand("gamemode 1")
	c.Settle()
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Testificate"))
	if got, ok := nbt.Get[nbt.Int](root, "playerGameType"); !ok || got != nbt.Int(protocol.GameModeSurvival) {
		t.Errorf("playerGameType = %v, %v; want %d (survival, the last named mode)", got, ok, protocol.GameModeSurvival)
	}
}

// TestFallDamage checks the whole story a six-block fall tells: the health it
// costs the faller, the damage event and hurt animation the others see, and
// the numbers their player file is saved with.
func TestFallDamage(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	watcher := connect()
	watcher.JoinAs("Watcher")

	faller := connect()
	fallerID := faller.JoinAs("Faller")

	watcher.ExpectPlayerAdded("Faller")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Faller")

	// The landing hurts in the move's handling, and with the loop stopped
	// no regeneration tick can heal the body before the file is written: a
	// fed body heals every ten ticks.
	srv.StopTick()

	// A six-block fall: taken a step at a time, like a falling body is, and
	// ended on ground.
	faller.MoveToGround(0.5, 70, 0.5, false)
	faller.MoveToGround(0.5, 67, 0.5, false)
	faller.MoveToGround(0.5, 64, 0.5, true)

	// The first three blocks are free; the rest are a half-heart apiece.
	faller.AwaitPlay(protocol.IDPlayLevelParticles)
	got := faller.ExpectHealth()
	if wantHealth, wantFood, wantSat := float32(17.0), int32(20), float32(5.0); got.Health != wantHealth || got.Food != wantFood || got.Saturation != wantSat {
		t.Errorf("after the fall: health=%v food=%v saturation=%v, want %v/%v/%v",
			got.Health, got.Food, got.Saturation, wantHealth, wantFood, wantSat)
	}
	faller.ExpectDamageEvent(fallerID, "minecraft:fall", 0)

	// The onlookers see the landing, in the two packets it comes in.
	watcher.AwaitPlay(protocol.IDPlayLevelParticles)
	watcher.ExpectDamageEvent(fallerID, "minecraft:fall", 0)

	// The fall is written to the player file: a body that fell six blocks
	// and lost three half-hearts, still fed and still in survival.
	faller.Disconnect()
	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Faller"))
	if health, ok := nbt.Get[nbt.Float](root, "Health"); !ok || health != 17 {
		t.Errorf("player file Health = %v, %v; want 17, true", health, ok)
	}
	if food, ok := nbt.Get[nbt.Int](root, "foodLevel"); !ok || food != 20 {
		t.Errorf("player file foodLevel = %v, %v; want 20, true", food, ok)
	}
	if sat, ok := nbt.Get[nbt.Float](root, "foodSaturationLevel"); !ok || sat != 5 {
		t.Errorf("player file foodSaturationLevel = %v, %v; want 5, true", sat, ok)
	}
	if mode, ok := nbt.Get[nbt.Int](root, "playerGameType"); !ok || mode != 0 {
		t.Errorf("player file playerGameType = %v, %v; want 0 (survival)", mode, ok)
	}
}

// TestHoneyLandingPlaysTheSlideAndTheFall checks the honey block's landing:
// the fall softens to a fifth (health 19 from an eleven-block drop), the
// slide event reaches the lander and everyone, and the slide and the block's
// own fall sound reach everyone but the lander, whose client plays them
// itself. The fall sound rides the computed damage, so an eleven-block drop
// that damages sends it.
func TestHoneyLandingPlaysTheSlideAndTheFall(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, func(s *server.Server) {
		s.SetLoadedBlock(0, 63, 0, servertest.BlockState(t, "minecraft:honey_block"))
	})
	watcher := connect()
	watcher.JoinAs("Watcher")

	c := connect()
	playerID := c.JoinAs("Faller")
	watcher.ExpectPlayerAdded("Faller")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Faller")
	srv.StopTick()

	c.MoveToGround(0.5, 75, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)

	slide, _ := registry.SoundEventID("minecraft:block.honey_block.slide")
	fall, _ := registry.SoundEventID("minecraft:block.honey_block.fall")

	// The lander's client plays the sounds itself, so the server sends it the
	// slide event and neither sound.
	landedEvent := false
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySoundEffect:
			switch protocol.NewReader(data).ReadVarInt() - 1 {
			case slide:
				t.Error("the honey landing sent the lander its own slide sound")
			case fall:
				t.Error("the honey landing sent the lander its own fall sound")
			}
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() == playerID && r.ReadU8() == protocol.EntityEventHoneySlide {
				landedEvent = true
			}
		}
	})

	var sawSlide, sawFall, sawEvent bool
	watcher.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySoundEffect:
			switch protocol.NewReader(data).ReadVarInt() - 1 {
			case slide:
				sawSlide = true
			case fall:
				sawFall = true
			}
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() == playerID && r.ReadU8() == protocol.EntityEventHoneySlide {
				sawEvent = true
			}
		}
	})

	if got := srv.PlayerHealth("Faller"); math.Abs(got-19) > 1e-3 {
		t.Errorf("health after the eleven-block fall onto honey = %v, want 19", got)
	}
	if !landedEvent {
		t.Error("the honey landing did not tell the lander of the slide event")
	}
	if !sawEvent {
		t.Error("the honey landing broadcast no slide event to a watcher")
	}
	if !sawSlide {
		t.Error("the honey landing sent no slide sound to a watcher")
	}
	if !sawFall {
		t.Error("the honey landing that damaged sent no fall sound to a watcher")
	}
}

// testFallResettingEffect gives the second of two fallers the named effect and
// has both take the same six-block fall: the body under the effect lands whole
// while the plain one takes the three half-hearts past the safe distance,
// which is what proves the steps were seen at all.
func testFallResettingEffect(t *testing.T, effectName string) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)
	plain := connect()
	plain.JoinAs("Plain")

	under := connect()
	under.JoinAs("Under")

	srv.GiveEffect("Under", effectName, 600, 0, false)
	under.AwaitMobEffect()

	srv.StopTick()
	plain.FallSix(0.5)
	under.FallSix(20.5)

	if got := srv.PlayerHealth("Plain"); got != 17 {
		t.Errorf("health of the plain faller = %v, want 17, the fall having cost the three past the safe distance", got)
	}
	if got := srv.PlayerHealth("Under"); got != 20 {
		t.Errorf("health of the faller under %s = %v, want 20, the effect having reset the fall", effectName, got)
	}
}

// jumpSteps is the y-motion of a jump, one entry a tick, the way the game's
// own physics take a jump up and hand it to gravity: a lift of twenty-two
// fiftieths of a block on the first tick, then each tick's step the drag of
// one past gravity's eighth, falling for good once the step points down.
func jumpSteps() []float64 {
	step := func(v float64) float64 { return (v - 0.08) * 0.98 }
	steps := []float64{}
	for v := 0.42; len(steps) < 25; v = step(v) {
		steps = append(steps, v)
	}
	return steps
}

// TestDeathEmptiesTheSlotsOnTheDeathScreen checks the inventory a dying
// client is left looking at: the vanilla server's container sync reaches the
// dying player the way it reaches any other, so the slots the death screen
// is drawn over are the emptied ones - one Set Container Slot per slot that
// had something in it, each of them empty - and not the items the body
// carried into its death, which the respawn would only clear later.
func TestDeathEmptiesTheSlotsOnTheDeathScreen(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c, entityID := servertest.JoinAs(t, cfg, "Dier")

	diamond, _ := registry.ItemID("minecraft:diamond")
	stone, _ := registry.ItemID("minecraft:stone")
	srv.GiveSlot("Dier", player.HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 7})
	// Slot 9 is the first of the main inventory rows, out of the hand.
	srv.GiveSlot("Dier", 9, protocol.ItemStack{ItemID: stone, Count: 3})

	// A fall of thirty blocks: twenty health against twenty-seven damage.
	c.MoveStep(0.5, 94, 0.5, false)
	c.MoveStep(0.5, 64, 0.5, true)

	// The stacks came off the body before the respawn screen did: two item
	// entities, holding exactly what was carried.
	gotDiamond, gotStone := false, false
	for !gotDiamond || !gotStone {
		item := c.ExpectDroppedItem()
		switch {
		case item.Stack.ItemID == diamond && item.Stack.Count == 7:
			gotDiamond = true
		case item.Stack.ItemID == stone && item.Stack.Count == 3:
			gotStone = true
		default:
			c.T.Errorf("death dropped an unexpected %+v", item.Stack)
		}
	}

	// The death screen's own packet comes next, and behind it the slots the
	// death emptied: the main inventory's stone and the hotbar's diamond,
	// each told empty in its own packet, in the order the slots are
	// numbered.
	fellMessage := protocol.Text{
		Translate: "death.fell.accident.generic",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Dier")},
	}
	c.ExpectCombatDeath(entityID, fellMessage)
	if got := servertest.ExpectEmptiedSlot(c); got != 9 {
		t.Errorf("the first emptied slot = %d, want 9, the main inventory's stone", got)
	}
	if got := servertest.ExpectEmptiedSlot(c); got != 36 {
		t.Errorf("the second emptied slot = %d, want 36, the hotbar's diamond", got)
	}
	// And no more of them: two slots carried something, and two slots were
	// told of.
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayContainerSlot))
}

// ladderColumn builds the ladder of the fall tests: a column of east-facing
// ladders up the cell at x, z, standing on the flat world's ground, and
// waits for the chunk that has to be there to place them into.
func ladderColumn(t *testing.T, srv *server.Server, top int32) {
	t.Helper()
	ladder := servertest.BlockStateWith(t, "minecraft:ladder", "facing=east,waterlogged=false")
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.BlockAt(10, 63, 10) != 0 })
	for y := int32(64); y <= top; y++ {
		if !srv.SetBlock(10, y, 10, ladder) {
			t.Fatal("the ladder column's chunk was not loaded")
		}
	}
}

// climbFall walks a fall the way a caught body tells it: one step through
// the air that lands the body at y, then the fifteenths of a block the
// catch settles into, with the horizontal drift of a step off when the test
// asks for one. A step is one movement packet, the whole of how the server
// sees a fall; the landing on the ground the column stands on is the
// test's own.
func climbFall(c *servertest.Client, x, y, z float64, slide int, drift float64) {
	c.MoveToGround(x, y, z, false)
	for range slide {
		y -= 0.15
		x += drift
		c.MoveToGround(x, y, z, false)
	}
}

// TestALadderCatchesAFallUnhurt checks the climb's mercy on a falling
// player: however far the fall into the ladder went, the catch itself and
// the landing it slides down to cost nothing, the climb writing the fall it
// caught off as the steps pass through it.
func TestALadderCatchesAFallUnhurt(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c, _ := servertest.JoinAs(t, cfg, "Catcher")

	ladderColumn(t, srv, 71)

	// Up above the column first, so the fall into the climb is a fall:
	// five blocks down through the air and into it, then the slide the
	// catch settles into, down to the ground the column stands on.
	c.MoveToGround(10.5, 74, 10.5, false)
	climbFall(c, 10.5, 68.8, 10.5, 32, 0)
	c.MoveToGround(10.5, 64, 10.5, true)

	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDamageEvent, protocol.IDPlaySetHealth))
}

// TestVoidDamage checks the world below the world: a player taken below the
// height where there is anything to stand on is hurt by the void itself,
// four half-hearts at a time.
func TestVoidDamage(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c, entityID := servertest.JoinAs(t, cfg, "Testificate")
	servertest.HoldTicks(t, srv)

	// Out of the world entirely: below the height 128 blocks under the
	// bottom of it, there is nothing to stand on and nothing to be hurt by
	// but the void. Settled, so the 25 held ticks are all below it.
	c.MoveToGround(0.5, -129, 0.5, false)
	c.Settle()

	// The first hit lands on the first tick below the world and the hurt
	// window spaces the rest ten ticks apart: 25 ticks hold three hits,
	// and two would be a twenty-tick spacing.
	var hits int
	first := float32(-1)
	c.PumpTicks(srv, 25, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayDamageEvent:
			hits++
			if got := r.ReadVarInt(); got != entityID {
				t.Errorf("damage landed on entity %d, want %d", got, entityID)
			}
			if got, want := r.ReadVarInt(), registry.MustID("minecraft:damage_type", "minecraft:out_of_world"); got != want {
				t.Errorf("damage type ID = %d, want %d (out_of_world)", got, want)
			}
		case protocol.IDPlaySetHealth:
			if first < 0 {
				first = r.ReadF32()
			}
		}
		return false
	})
	if hits != 3 {
		t.Errorf("%d void hits in 25 ticks, want 3", hits)
	}
	if want := float32(16.0); first != want {
		t.Errorf("health = %v after the first hit, want %v", first, want)
	}
}

// TestStarvation checks the bill an empty food bar sends: with no food left,
// the body loses a half-heart every four seconds. The player starts hungry
// out of a player file, which is the only way to arrive empty.
func TestStarvation(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	root := nbt.Compound{
		"playerGameType":      nbt.Int(0),
		"Health":              nbt.Float(20),
		"foodLevel":           nbt.Int(0),
		"foodSaturationLevel": nbt.Float(0),
		"foodExhaustionLevel": nbt.Float(0),
	}
	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatalf("encode player file: %v", err)
	}
	path := servertest.PlayerFile(cfg.LevelName, "Hungry")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create player directory: %v", err)
	}
	if err := servertest.WriteGzip(path, data); err != nil {
		t.Fatalf("write player file: %v", err)
	}

	_, c, entityID := servertest.JoinAs(t, cfg, "Hungry")

	// The starve tick is a four-second clock, and its bites take the health
	// down a half-heart each. Under a loaded test run the join itself can
	// outlast a bite, so whatever bites this reads are the starvation's
	// work; what the numbers say is that an empty food bar is eating the
	// health, bite by bite.
	got := c.ExpectHealth()
	if got.Health >= 20 {
		t.Fatalf("health = %v, want a bite of starvation taken out of it", got.Health)
	}
	if got.Health < 1 {
		t.Errorf("health = %v, starvation stops at half a heart on normal difficulty", got.Health)
	}
	if got.Food != 0 {
		t.Errorf("food = %d, want 0", got.Food)
	}
	c.ExpectDamageEvent(entityID, "minecraft:starve", 0)
}

// TestPvPDamage checks what a punch does between two players who can be hurt:
// a half-heart, the event naming the attacker, a push away from them — and
// then the half-second of peace a hit buys, inside which a second hit does
// nothing at all.
func TestPvPDamage(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	victim := connect()
	victimID := victim.JoinAs("Victim")

	attacker := connect()
	attackerID := attacker.JoinAs("Attacker")

	victim.ExpectPlayerAdded("Attacker")
	victim.ExpectAnnouncement("multiplayer.player.joined", "Attacker")

	attacker.MoveToGround(0.5, 64, 0.5, true)
	victim.MoveToGround(3.5, 64, 0.5, true)
	// Both moves taken in before the punch, or the knockback is worked out
	// from where the victim joined, on the attacker's own spot.
	attacker.Settle()
	victim.Settle()
	// Below 18 food natural regeneration stops, which would otherwise heal
	// the victim whenever this test runs slowly enough to reach its turn.
	srv.SetFood("Victim", 17)

	// The punch: a fist with nothing in it, one half-heart. The wait gives
	// the swing its recovery, the attack-strength cooldown a fresh join
	// starts at the bottom of.
	servertest.AwaitFullSwing(t, srv, "Attacker")
	attacker.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(victimID) })

	got := victim.ExpectHealth()
	if got.Health == 20 && got.Food == 17 {
		// The tick's own telling of the lowered food bar, which can land
		// either side of the punch; the punch's comes next.
		got = victim.ExpectHealth()
	}
	if want := float32(19.0); got.Health != want {
		t.Errorf("victim health = %v, want %v", got.Health, want)
	}
	victim.ExpectDamageEvent(victimID, "minecraft:player_attack", attackerID+1)
	victim.ExpectHurtAnimation(victimID)

	// Knockback: away from the attacker, and up.
	motion := protocol.NewReader(victim.ExpectSurvival(protocol.IDPlaySetEntityMotion))
	if got := motion.ReadVarInt(); got != victimID {
		t.Errorf("velocity for entity %d, want %d", got, victimID)
	}
	vx, vy, vz := motion.ReadLpVec3()
	if err := motion.Err(); err != nil {
		victim.T.Fatalf("set entity velocity: %v", err)
	}
	if vx <= 0 || vz != 0 {
		t.Errorf("knockback = (%v, %v, %v), wanted east and no south (the victim stands east of the attacker)", vx, vy, vz)
	}
	if math.Abs(vy-0.4) > 0.1 {
		t.Errorf("knockback lift = %v, want 0.4", vy)
	}

	// The attacker sees the flinch they caused.
	attacker.ExpectDamageEvent(victimID, "minecraft:player_attack", attackerID+1)

	// A second hit inside the half-second window changes nothing, which is
	// what the window is for. The third, outside it, takes another
	// half-heart: 18, and not the 17 two landed hits would have left.
	attacker.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(victimID) })
	attacker.Settle()
	servertest.AwaitTicks(t, srv, 12) // the window is 10 game ticks
	attacker.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(victimID) })

	got = victim.ExpectHealth()
	if want := float32(18.0); got.Health != want {
		t.Errorf("victim health after the third hit = %v, want %v (the second, inside the window, must not have landed)", got.Health, want)
	}
	victim.ExpectDamageEvent(victimID, "minecraft:player_attack", attackerID+1)
	victim.ExpectHurtAnimation(victimID)
	victim.ExpectSurvival(protocol.IDPlaySetEntityMotion) // the push, again

	attacker.ExpectDamageEvent(victimID, "minecraft:player_attack", attackerID+1)
}

// TestDeathAndRespawn checks the last page of the survival state: a fall far
// enough to empty the health bar plays out as a death — message in chat and
// on the respawn screen, the body falling over for the others — and the
// respawn the client asks for puts a whole player back at the world spawn,
// for the others as much as for themselves.
func TestDeathAndRespawn(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	// The watcher joins first, so that the dier's arrival is what the
	// watcher is told about - a player who joins second is shown the
	// first inside its own join sequence, and cannot ask for it after.
	watcher := connect()
	watcher.JoinAs("Watcher")

	dier := connect()
	dierID := dier.JoinAs("Dier")

	if got := watcher.ExpectPlayerAdded("Dier"); got != dierID {
		t.Fatalf("entity ID disagreement: watcher saw %d, dier is %d", got, dierID)
	}
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Dier")

	// A fall of thirty blocks: twenty health against twenty-seven damage.
	dier.MoveToGround(0.5, 94, 0.5, false)
	dier.MoveToGround(0.5, 64, 0.5, true)

	// Death, as the dying client sees it: the empty bar, the event, the
	// respawn screen with its message, and the same message in chat.
	dier.AwaitPlay(protocol.IDPlayLevelParticles)
	if got := dier.ExpectHealth(); got.Health != 0 {
		t.Fatalf("health after the fall = %v, want 0", got.Health)
	}
	dier.ExpectDamageEvent(dierID, "minecraft:fall", 0)
	fellMessage := protocol.Text{
		Translate: "death.fell.accident.generic",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Dier")},
	}
	dier.ExpectCombatDeath(dierID, fellMessage)
	dier.ExpectDeathMessage(fellMessage)

	// Death, as the others see it: the fall, the flinch, the chat message,
	// the body going down, and the health their copy of the body carries
	// reaching zero.
	watcher.AwaitPlay(protocol.IDPlayLevelParticles)
	watcher.ExpectDamageEvent(dierID, "minecraft:fall", 0)
	watcher.ExpectDeathMessage(fellMessage)
	watcher.ExpectEntityEvent(dierID, protocol.EntityStatusDeath)
	watcher.ExpectDeathMetadata(dierID)

	// The respawn the death screen asks for. The dying player asks; the
	// server answers with the full standing-in-the-world sequence again:
	// the Remove Entities taking the bodies the client held back out, the
	// packet, the bars, the abilities and the place.
	dier.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) { b.PutVarInt(protocol.ClientCommandPerformRespawn) })

	respawnData, ok := servertest.ReadUntil(t, dier, servertest.ExpectTimeout, protocol.IDPlayRespawn)
	if !ok {
		t.Fatal("no respawn packet arrived")
	}
	r := protocol.NewReader(respawnData)
	if got := r.ReadVarInt(); got != registry.MustID("minecraft:dimension_type", "minecraft:overworld") {
		t.Errorf("respawn dimension type = %d, want the overworld", got)
	}
	if got := r.ReadString(32767); got != "minecraft:overworld" {
		t.Errorf("respawn dimension = %q, want minecraft:overworld", got)
	}
	r.ReadI64() // hashed seed
	if got := uint8(r.ReadVarInt()); got != protocol.GameModeSurvival {
		t.Errorf("respawn game mode = %d, want %d (the mode survived the death)", got, protocol.GameModeSurvival)
	}
	if got := r.ReadVarInt(); got != 0 {
		t.Errorf("respawn previous game mode = %d, want 0 (absent)", got)
	}
	if r.ReadBool() {
		t.Error("respawn flags a debug world")
	}
	if !r.ReadBool() {
		t.Error("respawn does not flag its superflat world")
	}
	if !r.ReadBool() {
		t.Error("respawn carries no death location")
	} else {
		r.ReadString(32767) // death dimension
		r.ReadPosition()
	}
	if got := r.ReadVarInt(); got != 0 {
		t.Errorf("respawn portal cooldown = %d, want 0", got)
	}
	// The world the harness builds is a flat one, so its overworld reports
	// the flat sea level, one above the world's own floor.
	if got := r.ReadVarInt(); got != -63 {
		t.Errorf("respawn sea level = %d, want -63", got)
	}
	if got := r.ReadU8(); got != 0 {
		t.Errorf("respawn data kept = %#02x, want 0 (a normal respawn keeps nothing)", got)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("respawn: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("respawn packet has %d bytes left over", r.Remaining())
	}

	alive := dier.ExpectHealth()
	if alive.Health != 20 || alive.Food != 20 || alive.Saturation != 5 {
		t.Errorf("after the respawn: health=%v food=%v saturation=%v, want 20/20/5", alive.Health, alive.Food, alive.Saturation)
	}
	// The experience the death dropped comes back as none, the bar telling
	// so right behind the health it reset with.
	if got := dier.Expect(protocol.IDPlaySetExperience); len(got) == 0 {
		t.Error("no experience packet after the respawn")
	}
	if got := protocol.NewReader(dier.Expect(protocol.IDPlayAbilities)).ReadI8(); got != 0 {
		t.Errorf("abilities flags after respawn = %#04x, want 0 (survival)", got)
	}

	sync := protocol.NewReader(dier.Expect(protocol.IDPlaySyncPosition))
	sync.ReadVarInt() // teleport ID
	if x, y, z := sync.ReadF64(), sync.ReadF64(), sync.ReadF64(); x != 0.5 || y != 64 || z != 0.5 {
		t.Errorf("respawn position = (%v, %v, %v), want the world spawn (0.5, 64, 0.5)", x, y, z)
	}
	// And the game event that lets the client's loading screen close once
	// the chunks arrive, which vanilla sends after every respawn.
	if got := protocol.NewReader(dier.ExpectWithinTicks(srv, 100, protocol.IDPlayGameEvent)).ReadU8(); got != protocol.GameEventStartWaitingForChunks {
		t.Errorf("game event after the respawn = %d, want %d (start waiting for level chunks)", got, protocol.GameEventStartWaitingForChunks)
	}

	// The body the others were left looking at comes out, and a live one
	// takes its place at the spawn point.
	remove := protocol.NewReader(watcher.ExpectSurvival(protocol.IDPlayRemoveEntities))
	if got := remove.ReadVarInt(); got != 1 {
		t.Fatalf("removed entities = %d, want 1", got)
	}
	if got := remove.ReadVarInt(); got != dierID {
		t.Errorf("removed entity = %d, want %d", got, dierID)
	}
	spawn := protocol.NewReader(watcher.ExpectSurvival(protocol.IDPlaySpawnEntity))
	if got := spawn.ReadVarInt(); got != dierID {
		t.Errorf("respawned entity = %d, want %d", got, dierID)
	}
	if got := spawn.ReadUUID(); got != protocol.OfflineUUID("Dier") {
		t.Errorf("respawned entity UUID = %s, want Dier's", got)
	}
	if got := spawn.ReadVarInt(); got != protocol.EntityTypePlayer {
		t.Errorf("respawned entity type = %d, want a player", got)
	}
	if err := spawn.Err(); err != nil {
		t.Fatalf("spawn entity: %v", err)
	}
	watcher.ExpectMetadata()
	watcher.ExpectEquipment(dierID)
}

// TestADeathFallsOverForTheWatchers checks the watchers' half of a death:
// the entity-data health reaching zero — what makes a client tip the body
// over and turn it red; the death event alone leaves a player standing and
// stuck — and the body leaving their view after twenty ticks, with a fresh
// one shown on the respawn.
func TestADeathFallsOverForTheWatchers(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg)

	// The watcher joins first, so the dier's arrival is what the watcher is
	// told about - a player who joins second is shown the first inside its
	// own join sequence, and cannot ask for it after.
	watcher := connect()
	watcher.JoinAs("Watcher")

	dier := connect()
	dierID := dier.JoinAs("Dier")

	if got := watcher.ExpectPlayerAdded("Dier"); got != dierID {
		t.Fatalf("entity ID disagreement: watcher saw %d, dier is %d", got, dierID)
	}
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Dier")

	// A fall of thirty blocks: twenty health against twenty-seven damage.
	dier.MoveToGround(0.5, 94, 0.5, false)
	dier.MoveToGround(0.5, 64, 0.5, true)

	fellMessage := protocol.Text{
		Translate: "death.fell.accident.generic",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Dier")},
	}

	// The watcher's side of the death: the blow, the flinch, the message,
	// the event, and the health their copy of the body holds reaching zero.
	watcher.AwaitPlay(protocol.IDPlayLevelParticles)
	watcher.ExpectDamageEvent(dierID, "minecraft:fall", 0)
	watcher.ExpectDeathMessage(fellMessage)
	watcher.ExpectEntityEvent(dierID, protocol.EntityStatusDeath)
	watcher.ExpectDeathMetadata(dierID)

	// The body lies for twenty ticks, then the poof and the removal.
	watcher.ExpectEntityEvent(dierID, protocol.EntityStatusDeathPoof)
	remove := protocol.NewReader(watcher.ExpectSurvival(protocol.IDPlayRemoveEntities))
	if got := remove.ReadVarInt(); got != 1 {
		t.Fatalf("removed entities = %d, want just the dead one", got)
	}
	if got := remove.ReadVarInt(); got != dierID {
		t.Errorf("removed entity = %d, want the corpse's %d", got, dierID)
	}
	if err := remove.Err(); err != nil {
		t.Fatalf("remove entities: %v", err)
	}

	// The respawn swaps the entity again (removal, then a fresh spawn).
	dier.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) { b.PutVarInt(protocol.ClientCommandPerformRespawn) })
	watcher.ExpectSurvival(protocol.IDPlayRemoveEntities)
	spawn := protocol.NewReader(watcher.ExpectSurvival(protocol.IDPlaySpawnEntity))
	if got := spawn.ReadVarInt(); got != dierID {
		t.Errorf("respawned entity = %d, want %d", got, dierID)
	}
}

// TestSurvivalDiggingChecksTheMode checks the difference between the two
// kinds of digging in the protocol: survival holds a block until the client
// reports the dig finished, creative removes it on the click that starts one.
func TestSurvivalDiggingChecksTheMode(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// The dig ends in creative, switched into with /gamemode, whose
	// runner is listed as an operator.
	srv, c, _ := servertest.JoinAs(t, cfg, "Digger", servertest.OpsListing(t, "Digger"))
	// Where the dug blocks lie within the block interaction's reach of the
	// digger: vanilla refuses a dig past the reach.
	c.MoveToGround(0.5, 64, 10.5, true)
	c.Settle()
	// The block to dig is ice, whose loot needs a silk touch, so the break
	// says nothing about drops; the pickaxe digs it at better than seven
	// tenths a tick, which is what makes the finished dig the one that
	// breaks it: a bare hand, or the plain pickaxe's eight thirtieths,
	// would leave the dig waiting on the tick to finish it.
	ice, ok := registry.FindState("minecraft:ice", nil)
	if !ok {
		t.Fatal("the registry holds no ice")
	}
	if !srv.SetBlock(0, 63, 12, ice) {
		t.Fatal("could not stand the ice")
	}
	srv.GiveSlot("Digger", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:efficiency", 2))
	ground := srv.BlockAt(0, 63, 12)
	if ground == 0 {
		t.Fatal("the block to dig is already air")
	}

	// Survival: a start does not break the block, and the finished dig
	// that follows it does. If the start had removed the block, the
	// finished dig would find air and change nothing, and the block
	// update this reads for would never come.
	c.StartDigging(0, 63, 12)
	c.StopDigging(0, 63, 12)
	change := c.ExpectBlockChange()
	// Ice mined without Silk Touch leaves water over the solid ground.
	water := registry.BlockWater.DefaultState()
	if change.State != water {
		t.Errorf("block after the finished dig = %d, want water (%d)", change.State, water)
	}
	if got := srv.BlockAt(0, 63, 12); got != water {
		t.Errorf("block after the finished dig in the world = %d, want water (%d)", got, water)
	}

	// Creative, once switched into: the click alone is enough.
	c.ChatCommand("gamemode creative")
	c.ExpectModePackets(protocol.OfflineUUID("Digger"), protocol.GameModeCreative, true)

	ground = srv.BlockAt(0, 63, 13)
	if ground == 0 {
		t.Fatal("the block to click away is already air")
	}
	c.StartDigging(0, 63, 13)
	// The melted ice spreads five ticks after the break, so its flow can
	// reach the client ahead of the clicked cell's update.
	change = c.ExpectBlockUpdate()
	for change.X != 0 || change.Y != 63 || change.Z != 13 {
		change = c.ExpectBlockUpdate()
	}
	if change.State != 0 {
		t.Errorf("block after a creative click = %d, want air", change.State)
	}
}

// TestAdventureAndSpectatorCannotBuild checks the modes that cannot: in
// adventure nothing a player asks the world for — a break, a placement — is
// granted, and in spectator the same. Each refusal still plays fair with
// the client's prediction, which is what the acknowledgement is for.
func TestAdventureAndSpectatorCannotBuild(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// The modes are switched into with /gamemode, whose runner is listed
	// as an operator.
	srv, c, _ := servertest.JoinAs(t, cfg, "Wanderer", servertest.OpsListing(t, "Wanderer"))

	c.ChatCommand("gamemode adventure")
	c.ExpectModePackets(protocol.OfflineUUID("Wanderer"), protocol.GameModeAdventure, true)

	stone, _ := registry.ItemID("minecraft:stone")
	ground := srv.BlockAt(0, 63, 12)
	if ground == 0 {
		t.Fatal("the block to break at is already air")
	}

	// Digging is refused: the dig is acknowledged, and the block stands.
	c.StartDigging(0, 63, 12)
	c.StopDigging(0, 63, 12)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("acknowledged sequence = %d, want 1", seq)
	}
	if got := srv.BlockAt(0, 63, 12); got != ground {
		t.Errorf("block after an adventure dig = %d, want it still standing (%d)", got, ground)
	}

	// Placing is refused the same way: with a block in hand, the placement
	// comes back acknowledged and the spot stays empty.
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})
	c.PlaceBlock(0, 63, 12, protocol.FaceTop)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("acknowledged sequence = %d, want 1", seq)
	}
	if got := srv.BlockAt(0, 64, 12); got != 0 {
		t.Errorf("block after an adventure placement = %d, want the spot still empty", got)
	}

	// Spectators dig no better than adventurers do.
	c.ChatCommand("gamemode spectator")
	c.ExpectModePackets(protocol.OfflineUUID("Wanderer"), protocol.GameModeSpectator, true)

	c.BreakBlock(0, 63, 12)
	if seq := c.ExpectBlockAck(); seq != 1 {
		t.Errorf("acknowledged sequence = %d, want 1", seq)
	}
	if got := srv.BlockAt(0, 63, 12); got != ground {
		t.Errorf("block after a spectator dig = %d, want it still standing", got)
	}
}

// TestALoadingPlayerCannotBeHurt checks vanilla's loading grace
// (ServerPlayer.isInvulnerableTo, !hasClientLoaded): a client still behind
// its loading screen takes nothing from a fall that would kill it; once it
// sends Player Loaded, the same fall lands. The main loop is stopped so the
// grace's own sixty ticks cannot run out under a slow join; falls are
// judged as the moves arrive, not on the tick.
func TestALoadingPlayerCannotBeHurt(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg)
	srv.StopTick()

	c := connect()
	c.Login("Loader")
	c.Configure()
	c.EnterWorldLoading("Loader")

	c.MoveToGround(0.5, 94, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySetHealth))

	c.Send(protocol.IDPlayLoaded, func(*protocol.Buffer) {})
	c.MoveToGround(0.5, 94, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.AwaitPlay(protocol.IDPlayLevelParticles)
	if got := c.ExpectHealth(); got.Health != 0 {
		t.Errorf("a loaded player's thirty-block fall left %v health, want the fall to kill", got.Health)
	}
}

// TestBreakingABlockInSurvivalCostsExhaustion checks a survival break is
// charged Block.playerDestroy's 0.005 exhaustion, and a creative one nothing.
func TestBreakingABlockInSurvivalCostsExhaustion(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// The dig ends in creative, switched into with /gamemode, whose
	// runner is listed as an operator.
	srv, c, _ := servertest.JoinAs(t, cfg, "Digger", servertest.OpsListing(t, "Digger"))
	// Where the dug blocks lie within the block interaction's reach of the
	// digger: vanilla refuses a dig past the reach.
	c.MoveToGround(0.5, 64, 10.5, true)
	c.Settle()
	// The block to dig is ice, whose loot needs a silk touch, so the break
	// leaves no item on the wire; the pickaxe puts the dig's progress past
	// the seven tenths one finished dig breaks at, the way the plain
	// pickaxe's eight thirtieths of a tick would not.
	ice, ok := registry.FindState("minecraft:ice", nil)
	if !ok {
		t.Fatal("the registry holds no ice")
	}
	if !srv.SetBlock(0, 63, 12, ice) {
		t.Fatal("could not stand the ice")
	}
	srv.GiveSlot("Digger", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:diamond_pickaxe", "minecraft:efficiency", 2))

	c.StartDigging(0, 63, 12)
	c.BreakBlock(0, 63, 12)
	c.ExpectBlockChange()
	if got := srv.PlayerExhaustion("Digger"); math.Abs(got-0.005) > 1e-9 {
		t.Errorf("exhaustion after a survival break = %v, want 0.005", got)
	}
	// The dig's acks may still be on their way; the mode change is read
	// strictly.
	c.Settle()

	c.ChatCommand("gamemode creative")
	c.ExpectModePackets(protocol.OfflineUUID("Digger"), protocol.GameModeCreative, true)
	c.StartDigging(0, 63, 13)
	c.ExpectBlockChange()
	if got := srv.PlayerExhaustion("Digger"); math.Abs(got-0.005) > 1e-9 {
		t.Errorf("exhaustion after a creative break = %v, want the survival break's 0.005 alone", got)
	}
}

// TestDeathDestroysItemsWithTheVanishingCurse checks that a stack carrying
// the vanishing curse is deleted at death while a plain one of the same
// kind drops.
// vanilla: Player.destroyVanishingCursedItems
func TestDeathDestroysItemsWithTheVanishingCurse(t *testing.T) {
	t.Parallel()
	srv, c, entityID := servertest.JoinAs(t, servertest.SurvivalConfig(), "Dier")

	cursed := servertest.EnchantedStack(t, "minecraft:diamond_sword", "minecraft:vanishing_curse", 1)
	plain := protocol.ItemStack{ItemID: cursed.ItemID, Count: 1}
	srv.GiveSlot("Dier", 9, cursed)
	srv.GiveSlot("Dier", 10, plain)

	c.MoveStep(0.5, 94, 0.5, false)
	c.MoveStep(0.5, 64, 0.5, true)

	item := c.ExpectDroppedItem()
	if item.Stack.ItemID != plain.ItemID || len(item.Stack.Enchantments) != 0 {
		t.Errorf("death dropped %+v, want the plain sword", item.Stack)
	}
	// Item drops precede the death screen, so a second drop would have
	// arrived before this packet.
	fellMessage := protocol.Text{
		Translate: "death.fell.accident.generic",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Dier")},
	}
	c.ExpectCombatDeath(entityID, fellMessage)
}
