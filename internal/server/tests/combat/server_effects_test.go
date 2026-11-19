package combat

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// effectParticlesOf reads one Set Entity Metadata packet and reports the
// effect particles it carries for the entity, if it names it and has them.
func effectParticlesOf(c *servertest.Client, r *protocol.Reader, entityID int32) ([][2]int32, bool) {
	c.T.Helper()
	if r.ReadVarInt() != entityID {
		return nil, false
	}
	for {
		index := r.ReadU8()
		if index == protocol.MetadataEnd || r.Err() != nil {
			return nil, false
		}
		switch r.ReadVarInt() {
		case protocol.MetadataByte, protocol.MetadataBoolean:
			r.ReadU8()
		case protocol.MetadataVarInt, protocol.MetadataPose, protocol.MetadataHumanoidArm:
			r.ReadVarInt()
		case protocol.MetadataFloat:
			r.ReadF32()
		case protocol.MetadataOptionalPosition:
			if r.ReadBool() {
				r.ReadI64()
			}
		case protocol.MetadataParticles:
			var particles [][2]int32
			for range r.ReadVarInt() {
				p := [2]int32{r.ReadVarInt(), 0}
				if p[0] == protocol.ParticleEntityEffect {
					p[1] = r.ReadI32()
				}
				particles = append(particles, p)
			}
			return particles, true
		default:
			c.T.Fatalf("metadata entry %d of a type this reader skips", index)
		}
	}
}

// writeEffectsPlayerFile writes a player file holding the given vitals.
func writeEffectsPlayerFile(t *testing.T, level, name string, root nbt.Compound) {
	t.Helper()
	data, err := nbt.Encode("", root)
	if err != nil {
		t.Fatalf("encode player file: %v", err)
	}
	path := servertest.PlayerFile(level, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := servertest.WriteGzip(path, data); err != nil {
		t.Fatal(err)
	}
}

// TestRottenFleshGivesHunger eats rotten flesh with the roll under its 0.8
// chance: the eater is sent the hunger effect (thirty seconds, level one,
// particles and icon, blended in as a new effect) and everyone sees its
// particles in the eater's colour.
func TestRottenFleshGivesHunger(t *testing.T) {
	t.Parallel()
	srv, c, id := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Eater")
	c.Settle()

	srv.SetFood("Eater", 10)
	srv.GiveSlot("Eater", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:rotten_flesh"), Count: 2})
	srv.SetWearSeed(1) // first Float32 0.6046603: under the 0.8
	c.UseItemRightClicks(protocol.HandMain)

	got := c.AwaitMobEffect()
	want := servertest.MobEffect{Entity: id, Effect: registry.MustMobEffect("minecraft:hunger"), Amplifier: 0, Duration: 600, Flags: 0x02 | 0x04 | 0x08}
	if got != want {
		t.Errorf("update mob effect = %+v, want %+v", got, want)
	}
	particles := c.AwaitEffectParticles(id)
	wantParticles := [][2]int32{{protocol.ParticleEntityEffect, -0x1000000 | 0x587653}}
	if len(particles) != 1 || particles[0] != wantParticles[0] {
		t.Errorf("effect particles = %v, want %v", particles, wantParticles)
	}
}

// TestPoisonHurtsOnItsRhythmDownToOneHealth runs poison tick by tick: a hurt
// of magic on the first tick (a duration of 200 is a multiple of 25), none
// for the next 24, the next on the 26th, and none once the health is down
// to 1 (vanilla: PoisonMobEffect).
func TestPoisonHurtsOnItsRhythmDownToOneHealth(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	writeEffectsPlayerFile(t, cfg.LevelName, "Sick", nbt.Compound{
		"playerGameType": nbt.Int(0), "Health": nbt.Float(3),
		"foodLevel": nbt.Int(10), "foodSaturationLevel": nbt.Float(0),
	})
	srv, c, _ := servertest.JoinAs(t, cfg, "Sick")
	c.Settle()
	srv.StopTick()
	c.Settle()

	srv.GiveEffect("Sick", "minecraft:poison", 200, 0, true)
	if got := c.AwaitMobEffect(); got.Effect != registry.MustMobEffect("minecraft:poison") || got.Duration != 200 {
		t.Fatalf("update mob effect = %+v, want poison for 200 ticks", got)
	}
	c.Settle()

	healths := func(ticks int) []float32 {
		for range ticks {
			srv.TickSurvival()
		}
		var got []float32
		c.SettleAndCollect(func(packetID int32, data []byte) {
			if packetID == protocol.IDPlaySetHealth {
				got = append(got, protocol.NewReader(data).ReadF32())
			}
		})
		return got
	}
	if got := healths(1); len(got) != 1 || got[0] != 2 {
		t.Fatalf("health after the first tick = %v, want [2]", got)
	}
	if got := healths(24); len(got) != 0 {
		t.Fatalf("health over the next 24 ticks = %v, want no change", got)
	}
	if got := healths(1); len(got) != 1 || got[0] != 1 {
		t.Fatalf("health on the 26th tick = %v, want [1]", got)
	}
	if got := healths(100); len(got) != 0 {
		t.Errorf("health at 1 over 100 more ticks = %v, want no change", got)
	}
}

// TestMilkClearsEffects drinks a milk bucket with two effects on: both are
// removed, and the bucket is left behind.
func TestMilkClearsEffects(t *testing.T) {
	t.Parallel()
	srv, c, id := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Drinker")
	c.Settle()

	srv.GiveEffect("Drinker", "minecraft:hunger", 600, 0, true)
	c.AwaitMobEffect()
	srv.GiveEffect("Drinker", "minecraft:speed", 600, 0, true)
	c.AwaitMobEffect()

	srv.GiveSlot("Drinker", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:milk_bucket"), Count: 1})
	c.UseItemRightClicks(protocol.HandMain)
	for _, want := range []string{"minecraft:speed", "minecraft:hunger"} {
		if entity, effect := c.AwaitRemoveMobEffect(); entity != id || effect != registry.MustMobEffect(want) {
			t.Errorf("remove mob effect = (%d, %d), want (%d, %s)", entity, effect, id, want)
		}
	}
	servertest.WaitFor(t, srv, 100, func() bool {
		return srv.InventorySlot("Drinker", player.HotbarSlot).ItemID == servertest.MustItemID(t, "minecraft:bucket")
	})
}

// TestEffectsSurviveRejoining saves an effect into the player file in
// vanilla's shape and sends it back with the next join.
func TestEffectsSurviveRejoining(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.JoinAs("Keeper")
	c.Settle()
	srv.GiveEffect("Keeper", "minecraft:poison", 1200, 1, false)
	c.AwaitMobEffect()
	c.Disconnect()

	root := servertest.ReadPlayerFile(t, servertest.PlayerFile(cfg.LevelName, "Keeper"))
	list, ok := nbt.Get[nbt.List](root, "active_effects")
	if !ok || len(list.Items) != 1 {
		t.Fatalf("active_effects = %v, %v; want one effect", list, ok)
	}
	saved := list.Items[0].(nbt.Compound)
	duration, _ := nbt.Get[nbt.Int](saved, "duration")
	if saved["id"] != nbt.String("minecraft:poison") || saved["amplifier"] != nbt.Byte(1) ||
		saved["show_particles"] != nbt.Byte(0) || saved["show_icon"] != nbt.Byte(0) ||
		duration <= 1100 || duration > 1200 || len(saved) != 5 {
		t.Errorf("saved effect = %v, want poison II for under 1200 ticks, particles and icon hidden", saved)
	}

	// The join sends it among the world's own packets, before the chunks.
	c = connect()
	c.Login("Keeper")
	c.Configure()
	var id int32
	var got servertest.MobEffect
	c.ReadJoin(func(packetID int32, data []byte) {
		r := protocol.NewReader(data)
		switch packetID {
		case protocol.IDPlayLogin:
			id = r.ReadI32()
		case protocol.IDPlayUpdateMobEffect:
			got = servertest.MobEffectFor(data)
		}
	})
	c.MarkLoaded()
	if got.Entity != id || got.Effect != registry.MustMobEffect("minecraft:poison") || got.Amplifier != 1 ||
		got.Flags != 0 || got.Duration > int32(duration) || got.Duration < int32(duration)-100 {
		t.Errorf("rejoined effect = %+v, want poison II for about %d ticks, no flags", got, duration)
	}
}

// TestHealthBoostSurvivesRejoining saves a health above the base bar, held
// there by health boost, and reloads it whole rather than capping it to
// twenty before the effect that raises the bar is read.
func TestHealthBoostSurvivesRejoining(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.JoinAs("Keeper")
	c.Settle()
	srv.GiveEffect("Keeper", "minecraft:health_boost", 1200, 0, false)
	c.AwaitMobEffect()
	srv.SetHealth("Keeper", 24)
	c.Disconnect()

	c = connect()
	c.JoinAs("Keeper")
	c.Settle()
	if got := srv.PlayerHealth("Keeper"); got != 24 {
		t.Errorf("rejoined health = %v, want the saved 24", got)
	}
}

// TestClearingABoostSendsTheHealth sends Set Health when a command clears the
// boost that held the health above the base bar: the drop happens between
// ticks, where the tick's own comparison never sees it.
func TestClearingABoostSendsTheHealth(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Boosted", servertest.OpsListing(t, "Boosted"))
	c.Settle()

	srv.GiveEffect("Boosted", "minecraft:health_boost", 6000, 0, true)
	if got := c.AwaitMobEffect(); got.Effect != registry.MustMobEffect("minecraft:health_boost") {
		t.Fatalf("update mob effect = %+v, want health boost", got)
	}
	srv.SetHealth("Boosted", 24)
	c.Settle()

	c.ChatCommand("effect clear @s minecraft:health_boost")
	c.AwaitRemoveMobEffect()
	data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlaySetHealth)
	if !ok {
		t.Fatal("no set health after the boost was cleared")
	}
	if got := protocol.NewReader(data).ReadF32(); got != 20 {
		t.Errorf("set health after clearing the boost = %v, want 20", got)
	}
}

// TestRegenerationOnARefusedStarveTickSendsTheHealth heals on a tick whose
// starvation bite falls inside the invulnerability window: the heal still
// reaches the client (vanilla: ServerPlayer.doTick sends any health change).
func TestRegenerationOnARefusedStarveTickSendsTheHealth(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Starving")
	c.Settle()
	srv.StopTick()
	c.Settle()

	srv.SetFood("Starving", 0)
	ticks := func(n int) {
		for range n {
			srv.TickSurvival()
		}
	}
	ticks(75)
	if !srv.Hurt("Starving", 1) {
		t.Fatal("the blow did not land")
	}
	ticks(4)
	srv.GiveEffect("Starving", "minecraft:regeneration", 600, 0, true)
	c.Settle()
	ticks(1) // regeneration due, and the starve bite inside the window
	var healths []float32
	c.SettleAndCollect(func(packetID int32, data []byte) {
		if packetID == protocol.IDPlaySetHealth {
			healths = append(healths, protocol.NewReader(data).ReadF32())
		}
	})
	if len(healths) != 1 || healths[0] != 20 {
		t.Errorf("health sent on the healing starve tick = %v, want [20]", healths)
	}
}

// TestEffectCommand gives and clears an effect with /effect and checks the
// feedback vanilla's EffectCommands sends.
func TestEffectCommand(t *testing.T) {
	t.Parallel()
	_, c, id := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Potion", servertest.OpsListing(t, "Potion"))
	c.Settle()

	// The effect packet goes through the mailbox and the feedback straight
	// out, so the two are read in whichever order they arrive.
	run := func(command string) (effect, feedback []byte) {
		t.Helper()
		c.ChatCommand(command)
		c.SettleAndCollect(func(packetID int32, data []byte) {
			switch packetID {
			case protocol.IDPlayUpdateMobEffect, protocol.IDPlayRemoveMobEffect:
				effect = data
			case protocol.IDPlaySystemChat:
				feedback = data
			}
		})
		return effect, feedback
	}
	feedback := func(key string, with ...protocol.Text) []byte {
		var b protocol.Buffer
		b.PutText(protocol.Text{Translate: key, With: with})
		b.PutBool(false)
		return b.Bytes()
	}
	speed := registry.MustMobEffect("minecraft:speed")
	speedName := protocol.Text{Translate: "effect.minecraft.speed"}

	effect, said := run("effect give Potion speed 20 2")
	got := servertest.MobEffectFor(effect)
	if want := (servertest.MobEffect{Entity: id, Effect: speed, Amplifier: 2, Duration: 400, Flags: 0x02 | 0x04 | 0x08}); got != want {
		t.Errorf("update mob effect = %+v, want %+v", got, want)
	}
	twenty := int64(20)
	if want := feedback("commands.effect.give.success.single", speedName, servertest.PlayerDisplayComponent("Potion"),
		protocol.Text{Number: &twenty}); !bytes.Equal(said, want) {
		t.Errorf("give feedback = % X, want % X", said, want)
	}

	effect, said = run("effect clear @s minecraft:speed")
	if want := removeMobEffectBytes(id, speed); !bytes.Equal(effect, want) {
		t.Errorf("remove mob effect = % X, want % X", effect, want)
	}
	if want := feedback("commands.effect.clear.specific.success.single", speedName,
		servertest.PlayerDisplayComponent("Potion")); !bytes.Equal(said, want) {
		t.Errorf("clear feedback = % X, want % X", said, want)
	}

	if _, said = run("effect clear"); !bytes.Contains(said, []byte("commands.effect.clear.everything.failed")) {
		t.Errorf("clearing nothing answered % X, want commands.effect.clear.everything.failed", said)
	}

	// The hideParticles value is read with the walk's own reader, so the
	// quoted spelling vanilla's boolean reader takes hides the particles
	// rather than leaving the command unanswered.
	effect, said = run(`effect give Potion speed 5 1 "true"`)
	got = servertest.MobEffectFor(effect)
	if want := (servertest.MobEffect{Entity: id, Effect: speed, Amplifier: 1, Duration: 100, Flags: 0x08}); got != want {
		t.Errorf("hidden-particles update mob effect = %+v, want %+v", got, want)
	}
	five := int64(5)
	if want := feedback("commands.effect.give.success.single", speedName, servertest.PlayerDisplayComponent("Potion"),
		protocol.Text{Number: &five}); !bytes.Equal(said, want) {
		t.Errorf("hidden-particles give feedback = % X, want % X", said, want)
	}
}

// TestInvisibilityAndGlowingFlagsReachOtherPlayers checks that the invisibility
// and glowing effects set the shared flags another player draws the body with,
// and that clearing each effect clears its bit.
func TestInvisibilityAndGlowingFlagsReachOtherPlayers(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Ghost"))

	watcher := connect().Join("Watcher")

	ghost := connect().Join("Ghost")

	entityID := watcher.ExpectPlayerAdded("Ghost")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Ghost")

	srv.GiveEffect("Ghost", "minecraft:invisibility", 1200, 0, true)
	ghost.AwaitMobEffect()
	if got := watcher.AwaitEntityFlags(entityID); got&protocol.EntityFlagInvisible == 0 {
		t.Errorf("entity flags = %#02x after invisibility, want the invisible bit", got)
	}

	srv.GiveEffect("Ghost", "minecraft:glowing", 1200, 0, true)
	ghost.AwaitMobEffect()
	if got := watcher.AwaitEntityFlags(entityID); got&protocol.EntityFlagGlowing == 0 {
		t.Errorf("entity flags = %#02x after glowing, want the glowing bit", got)
	}

	// The bits share the byte with the state flags, so a state change carries
	// them along instead of clearing them.
	ghost.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	if got := watcher.AwaitEntityFlags(entityID); got&protocol.EntityFlagSneaking == 0 ||
		got&protocol.EntityFlagInvisible == 0 || got&protocol.EntityFlagGlowing == 0 {
		t.Errorf("entity flags = %#02x while sneaking invisible, want the sneak, invisible and glowing bits", got)
	}

	ghost.ChatCommand("effect clear @s minecraft:invisibility")
	if got := watcher.AwaitEntityFlags(entityID); got&protocol.EntityFlagInvisible != 0 {
		t.Errorf("entity flags = %#02x after clearing invisibility, want the invisible bit clear", got)
	}

	ghost.ChatCommand("effect clear @s minecraft:glowing")
	if got := watcher.AwaitEntityFlags(entityID); got&protocol.EntityFlagGlowing != 0 {
		t.Errorf("entity flags = %#02x after clearing glowing, want the glowing bit clear", got)
	}
}

// TestInfestedShowsItsOwnParticle gives a player infested: the particle in
// its metadata is the infested type, not a coloured swirl.
func TestInfestedShowsItsOwnParticle(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Ghost"))

	watcher := connect().Join("Watcher")

	ghost := connect().Join("Ghost")
	entityID := watcher.ExpectPlayerAdded("Ghost")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Ghost")

	srv.GiveEffect("Ghost", "minecraft:infested", 1200, 0, true)
	ghost.AwaitMobEffect()
	got := watcher.AwaitEffectParticles(entityID)
	if len(got) != 1 || got[0] != [2]int32{protocol.ParticleInfested, 0} {
		t.Errorf("effect particles = %v, want one infested particle", got)
	}
}

// TestInvisibilityAndGlowingFlagsOnALateJoin checks that a watcher who joins
// after the effects were applied is drawn with them: the spawn metadata their
// join carries for the other player already has the invisible and glowing
// bits set.
func TestInvisibilityAndGlowingFlagsOnALateJoin(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())

	ghost := connect().Join("Ghost")
	ghost.Settle()

	srv.GiveEffect("Ghost", "minecraft:invisibility", 1200, 0, false)
	ghost.AwaitMobEffect()
	srv.GiveEffect("Ghost", "minecraft:glowing", 1200, 0, false)
	ghost.AwaitMobEffect()

	watcher := connect()
	watcher.Login("Watcher")
	watcher.Configure()
	if got := watcher.EnterWorldSeeingPlayerFlags(); got&protocol.EntityFlagInvisible == 0 ||
		got&protocol.EntityFlagGlowing == 0 {
		t.Errorf("spawn flags = %#02x for the late watcher, want the invisible and glowing bits", got)
	}
}

// TestEffectExpiryClearsTheInvisibleFlag checks that the bit clears when the
// effect runs out on its own, not only when it is taken away: the tick that
// ends the effect resends the flags without the bit.
func TestEffectExpiryClearsTheInvisibleFlag(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())

	watcher := connect().Join("Watcher")

	ghost := connect().Join("Ghost")
	entityID := watcher.ExpectPlayerAdded("Ghost")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Ghost")

	srv.StopTick()
	watcher.Settle()

	// The effect lasts 10 ticks: the first tick sets the bit, and the tenth,
	// whose countdown reaches zero, removes it and clears the bit again.
	srv.GiveEffect("Ghost", "minecraft:invisibility", 10, 0, false)
	ghost.AwaitMobEffect()
	srv.TickSurvival()
	if got := watcher.AwaitEntityFlags(entityID); got&protocol.EntityFlagInvisible == 0 {
		t.Fatalf("entity flags = %#02x after invisibility, want the invisible bit", got)
	}
	for range 9 {
		srv.TickSurvival()
	}
	if got := watcher.AwaitEntityFlags(entityID); got&protocol.EntityFlagInvisible != 0 {
		t.Errorf("entity flags = %#02x after invisibility ran out, want the invisible bit clear", got)
	}
}

func removeMobEffectBytes(entityID, effectID int32) []byte {
	var b protocol.Buffer
	b.PutVarInt(entityID)
	b.PutVarInt(effectID)
	return b.Bytes()
}

// TestEffectCommandSuggestions checks the effect argument's suggestions
// against vanilla's SharedSuggestionProvider.filterResources: the partial may
// sit at the start of the name or path, or after a `.`, `_` or `/`, case
// folded, and the matches are sorted.
func TestEffectCommandSuggestions(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Potion", servertest.OpsListing(t, "Potion"))

	all := slices.Clone(registry.MobEffects)
	slices.Sort(all)
	for _, tc := range []struct {
		name    string
		text    string
		matches []string
	}{
		{"path without the namespace", "/effect give @s sp", []string{"minecraft:speed"}},
		{"after an underscore", "/effect give @s fall", []string{"minecraft:slow_falling"}},
		{"case folded", "/effect give @s Sp", []string{"minecraft:speed"}},
		{"whole name", "/effect give @s minecraft:sl", []string{"minecraft:slow_falling", "minecraft:slowness"}},
		{"sorted, not registry order", "/effect clear @s i", []string{
			"minecraft:infested", "minecraft:instant_damage",
			"minecraft:instant_health", "minecraft:invisibility",
		}},
		{"everything, sorted", "/effect clear @s ", all},
		{"nothing matches", "/effect give @s zz", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.CommandSuggestion(1, tc.text)
			_, _, _, matches := c.ExpectCommandSuggestions()
			if !slices.Equal(matches, tc.matches) {
				t.Errorf("matches = %v, want %v", matches, tc.matches)
			}
		})
	}

	c.Disconnect()
}

// TestHasteShortensAStoneDig: a bare hand takes stone at 1/150 a tick, so it
// breaks on the hundred and forty-ninth tick of a dig; Haste II multiplies the
// speed by 1.4, bringing the break forward to the hundred and seventh.
// vanilla: Player.getDestroySpeed, MobEffectUtil.getDigSpeedAmplification
func TestHasteShortensAStoneDig(t *testing.T) {
	t.Parallel()
	bare := stoneDigTicks(t, "", "", 0)
	haste := stoneDigTicks(t, "", "minecraft:haste", 1)
	if bare != 149 {
		t.Errorf("the bare hand broke the stone on tick %d, want 149", bare)
	}
	if haste != 107 {
		t.Errorf("Haste II broke the stone on tick %d, want 107", haste)
	}
	if haste >= bare {
		t.Errorf("Haste II took %d ticks, want fewer than the bare hand's %d", haste, bare)
	}
}

// TestMiningFatigueIIIDigsStoneAtVanillaSpeed: an iron pickaxe takes stone at
// 6 / 1.5 / 30 a tick, and Mining Fatigue III scales the speed by 0.3^3 =
// 0.027, so the stone breaks on the two hundred and seventy-seventh tick of the
// dig, as on the client; a scale of 0.0027 would take ten times that.
// vanilla: Player.getDestroySpeed
func TestMiningFatigueIIIDigsStoneAtVanillaSpeed(t *testing.T) {
	t.Parallel()
	if got := stoneDigTicks(t, "minecraft:iron_pickaxe", "minecraft:mining_fatigue", 2); got != 277 {
		t.Errorf("Mining Fatigue III with an iron pickaxe broke the stone on tick %d, want 277", got)
	}
}

// stoneDigTicks joins a survival digger on the ground holding the named item
// (a bare hand for "") against stone, gives it the named effect when there is
// one, and steps ticks until the stone is air, returning the tick it broke on.
// The loop is stepped one tick at a time, so the count is the server's own.
func stoneDigTicks(t *testing.T, heldName, effectName string, amplifier int32) int {
	t.Helper()
	clock := servertest.NewStepClock()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Digger", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	c.MoveToGround(0.5, 64, 0.5, true)
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("no stone state")
	}
	if !srv.SetBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], stone) {
		t.Fatal("could not stand the stone")
	}
	c.Settle()
	if heldName != "" {
		srv.GiveSlot("Digger", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, heldName), Count: 1})
		c.Settle()
	}
	if effectName != "" {
		srv.GiveEffect("Digger", effectName, 1000000, amplifier, false)
		c.Settle()
	}

	c.StartDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.StopDigging(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.Settle()

	for ticks := 1; ; ticks++ {
		servertest.StepTicks(t, srv, clock, 1)
		if srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]) == 0 {
			return ticks
		}
		if ticks > 400 {
			t.Fatal("the stone did not break within 400 ticks")
		}
	}
}

// TestTurtleHelmetBreathesWaterOutOfIt checks the water breathing a turtle
// helmet grants while the eyes are out of the water (vanilla:
// Player.turtleHelmetTick): the Update Mob Effect of water_breathing for 200
// ticks with no particles and the icon shown, blended in as a new effect,
// none while the eyes are under water, and the breath the effect spares for
// the ten seconds it lingers.
func TestTurtleHelmetBreathesWaterOutOfIt(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()

	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	id := c.JoinAs("Shelly")
	srv.SetFood("Shelly", 17)
	c.MoveToGround(0.5, 64, 50.5, true)
	c.Settle()
	srv.GiveSlot("Shelly", player.ArmorSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:turtle_helmet"), Count: 1})

	// Eyes out of water: the helmet grants the effect, no particles, the
	// icon shown, blended in as a new one.
	tick(1)
	got := c.AwaitMobEffect()
	want := servertest.MobEffect{Entity: id, Effect: registry.MustMobEffect("minecraft:water_breathing"), Amplifier: 0, Duration: 200, Flags: 0x04 | 0x08}
	if got != want {
		t.Fatalf("turtle helmet update mob effect = %+v, want %+v", got, want)
	}

	// Eyes under water: the helmet grants nothing, so the effect it left
	// behind is not refreshed.
	water := servertest.BlockState(t, "minecraft:water")
	if !srv.SetBlock(0, 64, 50, water) || !srv.SetBlock(0, 65, 50, water) {
		t.Fatal("the water's chunk was not loaded")
	}
	tick(20)
	refreshes := 0
	c.SettleAndCollect(func(packetID int32, data []byte) {
		if packetID != protocol.IDPlayUpdateMobEffect {
			return
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		if r.ReadVarInt() == registry.MustMobEffect("minecraft:water_breathing") {
			refreshes++
		}
	})
	if refreshes != 0 {
		t.Errorf("%d water breathing updates arrived with the eyes under water, want none", refreshes)
	}

	// The breath stays full for the effect's ten seconds and drains once it
	// has run out.
	tick(180)
	if air := srv.PlayerAir("Shelly"); air != rules.MaxAirSupply {
		t.Errorf("the breath over the effect's ten seconds = %d, want the full %d", air, rules.MaxAirSupply)
	}
	tick(25)
	if air := srv.PlayerAir("Shelly"); air >= rules.MaxAirSupply {
		t.Errorf("the breath after the effect ran out = %d, want below the full %d", air, rules.MaxAirSupply)
	}
}

// TestInstantHealthUnderHealthBoostHealsToTheRaisedBar checks the heal cap
// under Health Boost I: +4 on max_health raises the bar to twenty-four, and
// an Instant Health III (+16) on a body at ten heals to that raised bar
// rather than to the bare twenty.
// vanilla: MobEffects.HEALTH_BOOST, LivingEntity.heal
func TestInstantHealthUnderHealthBoostHealsToTheRaisedBar(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Healed")
	c.Settle()
	srv.StopTick()
	c.Settle()

	srv.GiveEffect("Healed", "minecraft:health_boost", 6000, 0, true)
	srv.SetHealth("Healed", 10)
	srv.GiveEffect("Healed", "minecraft:instant_health", 1, 2, true)
	srv.TickSurvival()

	if got := c.HealthReports(); len(got) != 1 || got[0] != 24 {
		t.Errorf("health sent after the Instant Health III = %v, want [24]", got)
	}
	if got := srv.PlayerHealth("Healed"); got != 24 {
		t.Errorf("health after the Instant Health III under Health Boost I = %v, want 24", got)
	}
}

// TestNaturalRegenerationUnderHealthBoostReachesTheRaisedBar checks the
// food-led heal's gate: a fed body under Health Boost I mends from
// twenty-two to the full twenty-four of the boost, a half-heart every
// eighty ticks. vanilla: FoodData.tick, Player.isHurt
func TestNaturalRegenerationUnderHealthBoostReachesTheRaisedBar(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Regrown")
	c.Settle()
	srv.StopTick()
	c.Settle()

	srv.GiveEffect("Regrown", "minecraft:health_boost", 6000, 0, true)
	// Eighteen: the least food the food-led heal runs from.
	srv.SetFood("Regrown", 18)
	srv.SetHealth("Regrown", 22)
	for range 160 {
		srv.TickSurvival()
	}

	// The first packet carries the 22 the test set, which the client was
	// not yet sent; the exhaustion's saturation drain after a heal sends
	// nothing. vanilla: ServerPlayer.doTick
	if got := c.HealthReports(); !slices.Equal(got, []float32{22, 23, 24}) {
		t.Errorf("health sent over the natural regeneration = %v, want [22 23 24]", got)
	}
	if got := srv.PlayerHealth("Regrown"); got != 24 {
		t.Errorf("health after the natural regeneration under Health Boost I = %v, want 24", got)
	}
}

// TestRegenerationUnderHealthBoostReachesTheRaisedBar checks the
// regeneration effect's own heal, which gates and caps at the attribute:
// Regeneration I mends a body at twenty to the boost's twenty-four, one
// half-heart every fifty ticks.
// vanilla: RegenerationMobEffect.applyEffectTick
func TestRegenerationUnderHealthBoostReachesTheRaisedBar(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Mended")
	c.Settle()
	srv.StopTick()
	c.Settle()

	srv.GiveEffect("Mended", "minecraft:health_boost", 6000, 0, true)
	// Seventeen: below the eighteen the food-led heal needs, so the
	// regeneration effect's own ticks are the only mends.
	srv.SetFood("Mended", 17)
	srv.SetHealth("Mended", 20)
	srv.GiveEffect("Mended", "minecraft:regeneration", 6000, 0, true)
	for range 155 {
		srv.TickSurvival()
	}

	if got := c.HealthReports(); len(got) != 4 || got[0] != 21 || got[3] != 24 {
		t.Errorf("health sent over the regeneration = %v, want [21 22 23 24]", got)
	}
	if got := srv.PlayerHealth("Mended"); got != 24 {
		t.Errorf("health after the regeneration under Health Boost I = %v, want 24", got)
	}
}

// TestGoldenAppleHealingUnderHealthBoostReachesTheRaisedBar eats a golden
// apple under Health Boost I: its regeneration II and the meal's own
// saturation both mend a body at twenty up to the boost's twenty-four, the
// apple eaten the way a client eats it.
func TestGoldenAppleHealingUnderHealthBoostReachesTheRaisedBar(t *testing.T) {
	t.Parallel()
	clock := servertest.NewStepClock()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Eater", func(s *server.Server) {
		s.SetTickClock(clock, nil)
		s.SetKeepAlive(time.Hour, time.Hour)
	})
	c.Settle()

	srv.GiveEffect("Eater", "minecraft:health_boost", 1_000_000, 0, true)
	// Hungry enough for the meal.
	srv.SetFood("Eater", 16)
	srv.SetHealth("Eater", 20)
	srv.GiveSlot("Eater", player.HotbarSlot,
		protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:golden_apple"), Count: 1})
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	for ticks := 600; srv.PlayerHealth("Eater") != 24; ticks-- {
		if ticks == 0 {
			t.Fatal("the golden apple's healing never reached the boost's bar")
		}
		servertest.StepTicks(t, srv, clock, 1)
	}

	if got := c.HealthReports(); len(got) == 0 || got[len(got)-1] != 24 {
		t.Errorf("the last health the meal's healing told = %v, want it to end at 24", got)
	}
}

// TestHealthBoostRunningOutClampsTheHealthToTwenty checks the other side of
// the attribute: once the boost runs out, the health above the bare twenty
// it had raised the bar above is clamped back to it.
// vanilla: LivingEntity.onAttributeUpdated
func TestHealthBoostRunningOutClampsTheHealthToTwenty(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Faint")
	c.Settle()
	srv.StopTick()
	c.Settle()

	srv.GiveEffect("Faint", "minecraft:health_boost", 10, 0, true)
	srv.SetHealth("Faint", 10)
	srv.GiveEffect("Faint", "minecraft:instant_health", 1, 2, true)
	for range 10 {
		srv.TickSurvival()
	}

	if got := c.HealthReports(); len(got) != 2 || got[0] != 24 || got[1] != 20 {
		t.Errorf("health sent over the boost's ten ticks = %v, want [24 20]", got)
	}
	if got := srv.PlayerHealth("Faint"); got != 20 {
		t.Errorf("health after the boost ran out = %v, want 20", got)
	}
}

// TestSaturationDropAfterHealSendsNoSetHealth checks that the exhaustion steps
// after the last heal, which lower only the saturation, send nothing
// (vanilla: ServerPlayer.doTick compares health, food and saturation-zero with
// the last values sent).
func TestSaturationDropAfterHealSendsNoSetHealth(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.CombatTestConfig(), servertest.OpsListing(t, "Healer"))
	c := connect()
	c.JoinAs("Healer")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	c.ChatCommand("effect give Healer minecraft:saturation 1 20 true")
	c.ExpectWithinTicks(srv, 20, protocol.IDPlayUpdateMobEffect)
	servertest.RunTicks(t, srv, 40)
	c.ChatCommand("damage Healer 9 minecraft:generic")
	for {
		if protocol.NewReader(c.ExpectWithinTicks(srv, 20, protocol.IDPlaySetHealth)).ReadF32() == 11 {
			break
		}
	}
	servertest.RunTicks(t, srv, 300)

	var healths []float32
	for id, data := range c.Packets(300 * time.Millisecond) {
		if id == protocol.IDPlaySetHealth {
			healths = append(healths, protocol.NewReader(data).ReadF32())
		}
	}
	if len(healths) != 9 {
		t.Fatalf("set health packets after the hurt = %v, want 9 heals", healths)
	}
	if last := healths[len(healths)-1]; last != 20 {
		t.Errorf("last health = %v, want 20", last)
	}
	servertest.RunTicks(t, srv, 60)
	for id := range c.Packets(300 * time.Millisecond) {
		if id == protocol.IDPlaySetHealth {
			t.Error("set health sent after the last heal")
		}
	}
}
