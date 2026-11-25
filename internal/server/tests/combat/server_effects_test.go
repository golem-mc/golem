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

func TestPlayerEffectParticleRefresh(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Affected"))
	watcher := connect().Join("Watcher")
	affected := connect().Join("Affected")
	entityID := watcher.ExpectPlayerAdded("Affected")
	servertest.HoldTicks(t, srv)

	readParticles := func(packetID int32, data []byte) ([][2]int32, bool) {
		if packetID != protocol.IDPlayEntityMetadata {
			return nil, false
		}
		return effectParticlesOf(watcher, protocol.NewReader(data), entityID)
	}
	awaitParticles := func(name string, want [][2]int32) {
		t.Helper()
		if !watcher.ReadTicks(srv, 2, func(packetID int32, data []byte) bool {
			got, ok := readParticles(packetID, data)
			if ok && !slices.Equal(got, want) {
				t.Errorf("%s particles = %v, want %v", name, got, want)
			}
			return ok
		}) {
			t.Fatalf("%s sent no particle metadata to the watcher", name)
		}
	}

	for _, tc := range []struct {
		name     string
		effectID int32
		particle [2]int32
		resends  bool
	}{
		{"trial_omen", registry.MobEffectTrialOmen, [2]int32{protocol.ParticleTrialOmen, 0}, false},
		{"raid_omen", registry.MobEffectRaidOmen, [2]int32{protocol.ParticleRaidOmen, 0}, false},
		{"speed", registry.MobEffectSpeed, [2]int32{protocol.ParticleEntityEffect, -0x1000000 | 3402751}, true},
	} {
		affected.ChatCommand("effect give @s " + tc.name + " 30")
		affected.ExpectWithinTicks(srv, 2, protocol.IDPlayUpdateMobEffect)
		awaitParticles(tc.name+" addition", [][2]int32{tc.particle})

		affected.ChatCommand("effect give @s " + tc.name + " 60")
		got := servertest.MobEffectFor(affected.ExpectWithinTicks(srv, 2, protocol.IDPlayUpdateMobEffect))
		if got.Effect != tc.effectID || got.Duration != 1200 {
			t.Fatalf("%s refresh = %+v, want effect %d for 1200 ticks", tc.name, got, tc.effectID)
		}
		if tc.resends {
			awaitParticles(tc.name+" refresh", [][2]int32{tc.particle})
		} else {
			watcher.ReadTicks(srv, 2, func(packetID int32, data []byte) bool {
				if got, ok := readParticles(packetID, data); ok {
					t.Errorf("%s refresh sent particle metadata %v to the watcher", tc.name, got)
				}
				return false
			})
		}

		affected.ChatCommand("effect clear @s")
		affected.ExpectWithinTicks(srv, 2, protocol.IDPlayRemoveMobEffect)
		awaitParticles(tc.name+" removal", nil)
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

func removeMobEffectBytes(entityID, effectID int32) []byte {
	var b protocol.Buffer
	b.PutVarInt(entityID)
	b.PutVarInt(effectID)
	return b.Bytes()
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
