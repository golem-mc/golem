package survival

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"os"
	"path/filepath"
	"slices"
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

// TestGameModeReaderSkipsEntityTraffic checks a mode change read through the
// entity traffic a ticking world drops between the command and its packets.
// The metadata is real: turning a skin layer broadcasts metadata for this
// player's own body, the same packet a mob's flags or a body catching fire
// sends. The other kinds are queued ahead of the command, where a body in
// view's spawn, removal, equipment and event can land.
func TestGameModeReaderSkipsEntityTraffic(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	playerUUID := c.Login("Testificate")
	c.Configure()
	c.EnterWorldAs("Testificate")

	c.Unread(
		servertest.PushedPacket{Id: protocol.IDPlaySpawnEntity},
		servertest.PushedPacket{Id: protocol.IDPlayEntityMetadata},
		servertest.PushedPacket{Id: protocol.IDPlayRemoveEntities},
		servertest.PushedPacket{Id: protocol.IDPlayEntityEquipment},
		servertest.PushedPacket{Id: protocol.IDPlayEntityEvent},
	)
	c.SetSkinParts(0x00)
	c.AwaitPlay(protocol.IDPlayPlayerInfoUpdate) // the hat going off
	c.ChatCommand("gamemode creative")
	c.ExpectModePackets(playerUUID, protocol.GameModeCreative, true)
}

// TestGameModeSelectorWithSpacesInItsBrackets checks the target selector read
// whole: the runner switches its own mode through a selector whose options
// are parted by spaces, the same packets a bare self-change makes.
func TestGameModeSelectorWithSpacesInItsBrackets(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	playerUUID := c.Login("Testificate")
	c.Configure()
	c.EnterWorldAs("Testificate")

	c.ChatCommand("gamemode creative @s[ name=Testificate ]")
	c.ExpectModePackets(playerUUID, protocol.GameModeCreative, true)

	c.Disconnect()
}

// TestGameModeCommandSuggests checks the tab completions of /gamemode: the
// four modes by name, trimmed by whatever has been typed of them, and then
// the selectors and names of the players a target would name.
func TestGameModeCommandSuggests(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// The completions of /gamemode need the command in the tree, which
	// needs the runner to be an operator.
	_, c, _ := servertest.JoinAs(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.CommandSuggestion(1, "/gamemode ")
	id, _, _, matches := c.ExpectCommandSuggestions()
	if id != 1 {
		t.Errorf("suggestion id = %d, want 1", id)
	}
	if want := []string{"adventure", "creative", "spectator", "survival"}; !servertest.EqualStrings(matches, want) {
		t.Errorf("mode suggestions = %v, want %v", matches, want)
	}

	c.CommandSuggestion(2, "/gamemode cr")
	_, _, _, matches = c.ExpectCommandSuggestions()
	if want := []string{"creative"}; !servertest.EqualStrings(matches, want) {
		t.Errorf("mode suggestions = %v, want %v", matches, want)
	}

	c.CommandSuggestion(3, "/gamemode creative ")
	_, _, _, matches = c.ExpectCommandSuggestions()
	want := append([]string{"@a", "@e", "@n", "@p", "@r", "@s"}, "Testificate")
	if !servertest.EqualStrings(matches, want) {
		t.Errorf("target suggestions = %v, want %v", matches, want)
	}

	c.CommandSuggestion(4, "/gamemode creative Te")
	_, _, _, matches = c.ExpectCommandSuggestions()
	if want := []string{"Testificate"}; !servertest.EqualStrings(matches, want) {
		t.Errorf("target suggestions = %v, want %v", matches, want)
	}
}

// TestGameModeCommandChangesAnotherPlayer checks /gamemode with a target:
// the runner's command switches someone else's mode, that player sees the
// switch through the same three packets a self-change makes, and the runner
// gets the tab-list update and a message about someone else rather than the
// game event and abilities, which are not theirs to feel.
func TestGameModeCommandChangesAnotherPlayer(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// /gamemode is a gamemaster's command, so Runner, who runs it, is
	// listed as an operator; Target is only ever switched.
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Runner"))

	runner := connect()
	runner.JoinAs("Runner")

	target := connect()
	targetUUID := target.Login("Target")
	target.Configure()
	target.EnterWorldAs("Target")

	runner.ExpectPlayerAdded("Target")
	runner.ExpectAnnouncement("multiplayer.player.joined", "Target")

	runner.ChatCommand("gamemode creative Target")

	// The target gets the switch itself.
	target.ExpectModePackets(targetUUID, protocol.GameModeCreative, false)

	// The runner gets the broadcast and the message about someone else;
	// whichever of the two crosses the wire first is the mailbox's
	// choice, so both are read in whichever order they arrive.
	var gotInfo, gotChat bool
	for packetID, data := range runner.Packets(servertest.ExpectTimeout) {
		switch packetID {
		case protocol.IDPlayUpdateTime,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity,
			protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
			protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk:
			// Background traffic, not the runner's share.
		case protocol.IDPlayPlayerInfoUpdate:
			servertest.ExpectGameModeUpdateIn(t, data, targetUUID, protocol.GameModeCreative)
			gotInfo = true
		case protocol.IDPlaySystemChat:
			var want protocol.Buffer
			want.PutText(protocol.Text{
				Translate: "commands.gamemode.success.other",
				With: []protocol.Text{
					servertest.PlayerDisplayComponent("Target"),
					{Translate: "gameMode." + servertest.GameModeName(protocol.GameModeCreative)},
				},
			})
			want.PutBool(false)
			if !bytes.Equal(data, want.Bytes()) {
				t.Errorf("gamemode feedback = % X, want % X", data, want.Bytes())
			}
			gotChat = true
		default:
			t.Fatalf("unexpected packet 0x%02X while reading the runner's share", packetID)
		}
		if gotInfo && gotChat {
			break
		}
	}
	if !gotInfo || !gotChat {
		t.Fatalf("read the runner's share of the mode change: nothing came within %s", servertest.ExpectTimeout)
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

// TestFallOntoASoftBlockScalesTheDamage checks the landing block's word on a
// fall (Block.fallOn): a hay bale and a honey block soften it to a fifth, a
// bed halves the distance before the safe three while a straw bed does not, a
// slime block spares the
// body whole, and a cobweb writes it off outright. The drop is from y=75 onto
// the flat world's stone at y=63, eleven blocks, eight of them past the safe
// three.
func TestFallOntoASoftBlockScalesTheDamage(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		block  string
		cellY  int32
		health float64
	}{
		{"hay", "minecraft:hay_block", 63, 20 - 1},
		{"honey", "minecraft:honey_block", 63, 20 - 1},
		{"bed", "minecraft:red_bed", 63, 20 - 2},
		{"straw bed", "minecraft:straw_bed", 63, 20 - 8},
		{"slime", "minecraft:slime_block", 63, 20},
		{"cobweb", "minecraft:cobweb", 64, 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false

			srv, c, _ := servertest.JoinAs(t, cfg, "Faller", func(s *server.Server) {
				s.SetLoadedBlock(0, tc.cellY, 0, servertest.BlockState(t, tc.block))
			})

			// The loop is stopped so no regeneration tick can heal the body
			// between the landing and the read.
			srv.StopTick()

			// Up to the drop height first, so the fall is measured from
			// there alone, then down onto the block with the ground flag set.
			c.MoveToGround(0.5, 75, 0.5, false)
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()

			if got := srv.PlayerHealth("Faller"); math.Abs(got-tc.health) > 1e-3 {
				t.Errorf("health after the eleven-block fall onto %s = %v, want %v", tc.block, got, tc.health)
			}
			// A landing the block spares whole must not hurt at all.
			if tc.health == 20 {
				c.SettleAndCollect(func(id int32, _ []byte) {
					if id == protocol.IDPlayDamageEvent || id == protocol.IDPlaySetHealth {
						t.Errorf("a fall onto %s that costs nothing sent packet 0x%02X", tc.block, id)
					}
				})
			}
		})
	}
}

// TestAShortDropOntoASoftBlockDoesNotHurt pins the floor after the landing
// block's scaling in the wire's own terms: a six-block fall has three past
// the safe three, so a hay bale's fifth and a bed's own halving both cost
// nothing — no hurt, no damage event, no health packet.
func TestAShortDropOntoASoftBlockDoesNotHurt(t *testing.T) {
	t.Parallel()
	for _, block := range []string{"minecraft:hay_block", "minecraft:red_bed"} {
		t.Run(block, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			srv, c, _ := servertest.JoinAs(t, cfg, "Faller", func(s *server.Server) {
				s.SetLoadedBlock(0, 63, 0, servertest.BlockState(t, block))
			})
			srv.StopTick()

			c.MoveToGround(0.5, 70, 0.5, false)
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()

			if got := srv.PlayerHealth("Faller"); got != 20 {
				t.Errorf("health after a six-block fall onto %s = %v, want 20", block, got)
			}
			c.SettleAndCollect(func(id int32, _ []byte) {
				if id == protocol.IDPlayDamageEvent || id == protocol.IDPlaySetHealth {
					t.Errorf("a six-block fall onto %s sent packet 0x%02X, want no hurt", block, id)
				}
			})
		})
	}
}

// TestASneakingPlayerOnSlimeTakesNoDamage pins vanilla's SlimeBlock.fallOn: the
// block spares a fall whole whether or not the body is suppressing its
// bounce, so the sneak changes nothing.
func TestASneakingPlayerOnSlimeTakesNoDamage(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c, _ := servertest.JoinAs(t, cfg, "Faller", func(s *server.Server) {
		s.SetLoadedBlock(0, 63, 0, servertest.BlockState(t, "minecraft:slime_block"))
	})
	srv.StopTick()

	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	c.MoveToGround(0.5, 75, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	if got := srv.PlayerHealth("Faller"); got != 20 {
		t.Errorf("health after an eleven-block fall onto slime while sneaking = %v, want 20", got)
	}
}

// TestAWebAboveTheFeetStopsTheFall pins the body-wide scan: the web's
// entityInside runs for every cell the body's box fills, so one at the head
// writes the fall off just as one at the feet does.
func TestAWebAboveTheFeetStopsTheFall(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c, _ := servertest.JoinAs(t, cfg, "Faller", func(s *server.Server) {
		s.SetLoadedBlock(0, 65, 0, servertest.BlockState(t, "minecraft:cobweb"))
	})
	srv.StopTick()

	c.MoveToGround(0.5, 75, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	if got := srv.PlayerHealth("Faller"); got != 20 {
		t.Errorf("health after an eleven-block fall into a web at the head = %v, want 20", got)
	}
}

// TestAFastFallThroughAResettingBlockIsForgiven pins Entity.move's fall
// sweep: a fall carried across a one-thick layer in a single step a block or
// longer is written off when the layer is crossed — a web, a berry bush, a
// climbable or water — so only the three-block step down onto the floor below
// is left, which costs nothing, though the layer never stands in the landing
// box.
// vanilla: Entity.move, ClipContext.Block.FALLDAMAGE_RESETTING
func TestAFastFallThroughAResettingBlockIsForgiven(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		block string
	}{
		{"cobweb", "minecraft:cobweb"},
		{"sweet berry bush", "minecraft:sweet_berry_bush"},
		{"ladder", "minecraft:ladder"},
		{"water", "minecraft:water"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			srv, c, _ := servertest.JoinAs(t, cfg, "Faller", func(s *server.Server) {
				s.SetLoadedBlock(0, 66, 0, servertest.BlockState(t, tc.block))
			})
			srv.StopTick()

			// Up to the drop height first, then falls held above the layer's
			// cell at 66 to carry a fall of thirteen into the last step.
			c.MoveToGround(0.5, 80, 0.5, false)
			c.MoveToGround(0.5, 72, 0.5, false)
			c.MoveToGround(0.5, 67, 0.5, false)
			c.Settle()
			if got := srv.PlayerFallDistance("Faller"); got <= 3 {
				t.Fatalf("the fall carried into %s's layer = %v, want a fall past the safe three", tc.name, got)
			}

			// The last step drops three blocks, through the layer and onto
			// the floor, with no landing box ever in the layer's cell.
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			if got := srv.PlayerHealth("Faller"); got != 20 {
				t.Errorf("health after a fast fall through %s = %v, want 20", tc.name, got)
			}
			if got := srv.PlayerFallDistance("Faller"); got != 0 {
				t.Errorf("the fall distance after landing through %s = %v, want none", tc.name, got)
			}
		})
	}
}

// TestAFastFallThroughAPortalIsForgivenForAPlayer pins the player-only arm of
// Entity.move's fall sweep: ClipContext.Block.FALLDAMAGE_RESETTING treats the
// end portal and gateway, and a nether portal while
// players_nether_portal_default_delay is zero, as full cubes, so a player's
// fast drop across one writes the whole fall off.
// vanilla: Entity.move, ClipContext.Block.FALLDAMAGE_RESETTING
func TestAFastFallThroughAPortalIsForgivenForAPlayer(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		block         string
		instantPortal bool
	}{
		{"end portal", "minecraft:end_portal", false},
		{"end gateway", "minecraft:end_gateway", false},
		{"instant nether portal", "minecraft:nether_portal", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			srv, c, _ := servertest.JoinAs(t, cfg, "Faller", func(s *server.Server) {
				s.SetLoadedBlock(0, 66, 0, servertest.BlockState(t, tc.block))
				if tc.instantPortal {
					s.SetGameRuleValue("players_nether_portal_default_delay", 0)
				}
			})
			srv.StopTick()

			// Up to the drop height first, then falls held above the layer's
			// cell at 66 to carry a fall of thirteen into the last step.
			c.MoveToGround(0.5, 80, 0.5, false)
			c.MoveToGround(0.5, 72, 0.5, false)
			c.MoveToGround(0.5, 67, 0.5, false)
			c.Settle()
			if got := srv.PlayerFallDistance("Faller"); got <= 3 {
				t.Fatalf("the fall carried into %s's layer = %v, want a fall past the safe three", tc.name, got)
			}

			// The last step drops three blocks, through the layer and onto
			// the floor, with no landing box ever in the layer's cell.
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			if got := srv.PlayerHealth("Faller"); got != 20 {
				t.Errorf("health after a fast fall through %s = %v, want 20", tc.name, got)
			}
			if got := srv.PlayerFallDistance("Faller"); got != 0 {
				t.Errorf("the fall distance after landing through %s = %v, want none", tc.name, got)
			}
		})
	}
}

// TestAFallCarriedInLavaIsHalved checks Entity.baseTick's lava halving on the
// player path: while the body's box touches lava the fall it carries counts
// half each tick, so a drop held in the pool lands on the safe three blocks
// and costs nothing where the whole fall would have taken eight points. Fire
// resistance keeps the lava's own bites out of the health read.
// vanilla: Entity.baseTick
func TestAFallCarriedInLavaIsHalved(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c, _ := servertest.JoinAs(t, cfg, "Diver", func(s *server.Server) {
		s.SetLoadedBlock(0, 64, 10, servertest.BlockState(t, "minecraft:lava"))
		s.SetLoadedBlock(0, 65, 10, servertest.BlockState(t, "minecraft:lava"))
	})
	srv.GiveEffect("Diver", "minecraft:fire_resistance", 6000, 0, false)
	srv.StopTick()

	// The fall is carried to 65.5, where the box already reaches the lava,
	// held there three ticks while the halving runs, then landed one more
	// block down onto the pool's floor.
	c.MoveToGround(0.5, 75, 10.5, false)
	c.MoveToGround(0.5, 66, 10.5, false)
	c.MoveToGround(0.5, 65.5, 10.5, false)
	c.Settle()
	srv.TickSurvival()
	srv.TickSurvival()
	srv.TickSurvival()
	c.MoveToGround(0.5, 64, 10.5, true)
	c.Settle()

	if got := srv.PlayerHealth("Diver"); got != 20 {
		t.Errorf("health after a fall carried through the lava = %v, want 20", got)
	}
}

// TestAFallWithLavaAboveTheFeetIsHalved pins that the halving reads the whole
// box and not the feet cell alone: the body falls with its feet clear of the
// lava and the pool only at chest height, still counting the fall half each
// tick it touches. Fire resistance keeps the lava's own bites out of the
// health read.
// vanilla: Entity.isInLava
func TestAFallWithLavaAboveTheFeetIsHalved(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c, _ := servertest.JoinAs(t, cfg, "Diver", func(s *server.Server) {
		s.SetLoadedBlock(0, 65, 10, servertest.BlockState(t, "minecraft:lava"))
	})
	srv.GiveEffect("Diver", "minecraft:fire_resistance", 6000, 0, false)
	srv.StopTick()

	// The feet sit at 64.5, the air cell below the lava: only the box's upper
	// half reaches the pool.
	c.MoveToGround(0.5, 75, 10.5, false)
	c.MoveToGround(0.5, 66, 10.5, false)
	c.MoveToGround(0.5, 64.5, 10.5, false)
	c.Settle()
	srv.TickSurvival()
	srv.TickSurvival()
	srv.TickSurvival()
	c.MoveToGround(0.5, 64, 10.5, true)
	c.Settle()

	if got := srv.PlayerHealth("Diver"); got != 20 {
		t.Errorf("health after a fall with the lava at the chest = %v, want 20", got)
	}
}

// TestABodyBesideWaterIsInItForTheFall pins the box read on the player's own
// water: a body whose 0.6-wide box overlaps a water cell is in the water even
// though the cell under its centre is dry, so the fall it carried is written
// off and the landing below costs nothing.
// vanilla: EntityFluidInteraction.update, Entity.isInWater
func TestABodyBesideWaterIsInItForTheFall(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c, _ := servertest.JoinAs(t, cfg, "Faller", func(s *server.Server) {
		s.SetLoadedBlock(0, 64, 0, servertest.BlockState(t, "minecraft:water"))
	})
	srv.StopTick()

	// The body falls at x=1.1, beside the water: the cell under its centre
	// (1,64,0) is dry while the box's 0.8 edge reaches into the source at
	// x=0, so the 64.8 step is in water and the fall it carried is forgiven.
	c.MoveToGround(1.1, 75, 0.5, false)
	c.MoveToGround(1.1, 66, 0.5, false)
	c.MoveToGround(1.1, 64.8, 0.5, false)
	c.Settle()
	if got := srv.PlayerFallDistance("Faller"); got != 0 {
		t.Errorf("fall carried beside the water = %v, want none", got)
	}

	c.MoveToGround(1.1, 64, 0.5, true)
	c.Settle()
	if got := srv.PlayerHealth("Faller"); got != 20 {
		t.Errorf("health after a fall beside the water = %v, want 20", got)
	}
}

// TestASwimmingBodyBesideWaterKeepsThePose pins swimmingNow's continue test
// on the body's box: once a sprinter is swimming, any of the box touching
// water keeps them swimming, so a body whose centre leaves the water but
// whose box still reaches it is not stood up.
// vanilla: Entity.updateSwimming, Entity.isInWater
func TestASwimmingBodyBesideWaterKeepsThePose(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg)
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Swimmer")

	// A pool at the spawn point, deep enough to put the eyes under.
	srv.SetLoadedBlock(0, 64, 0, servertest.BlockState(t, "minecraft:water"))
	srv.SetLoadedBlock(0, 65, 0, servertest.BlockState(t, "minecraft:water"))
	c.MoveToGround(0.5, 64, 0.5, false)
	c.Settle()

	// Sprinting with the eyes under water starts the swim.
	c.SendSprint(entityID, true)
	if got := c.ExpectMetadata(); got.Pose != protocol.PoseSwimming {
		t.Fatalf("pose after sprinting in the water = %d, want %d (swimming)", got.Pose, protocol.PoseSwimming)
	}

	// The body steps beside the water, its centre cell dry but its box still
	// reaching the pool: the swim has to carry on.
	c.MoveToGround(1.1, 64, 0.5, false)
	c.Settle()
	tick(1)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		if pose := servertest.BoatPlayerPose(t, entityID, data); pose != protocol.PoseSwimming {
			t.Errorf("pose beside the water = %d, want %d (swimming)", pose, protocol.PoseSwimming)
		}
	})
}

// TestASwimDoesNotStartWithoutWaterAtTheFeet pins swimmingNow's start test on
// the feet block: vanilla starts a swim only when the fluid at the feet block
// is water (Entity.updateSwimming), so eyes under a body of water whose feet
// block is dry do not strike the swim pose.
// vanilla: Entity.updateSwimming
func TestASwimDoesNotStartWithoutWaterAtTheFeet(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg)
	servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Stander")

	// Water only at the eye cell: the eyes are under its surface while the
	// feet block is air.
	srv.SetLoadedBlock(0, 65, 0, servertest.BlockState(t, "minecraft:water"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	c.SendSprint(entityID, true)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		if pose := servertest.BoatPlayerPose(t, entityID, data); pose == protocol.PoseSwimming {
			t.Errorf("pose with a dry feet block = %d, want not swimming", pose)
		}
	})
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

// TestHoneyLandingSoundFollowsTheDamageNotTheHurt pins that the block's fall
// sound is tied to the computed fall damage, not to a hurt that landed: with
// fall_damage off the eleven-block drop still computes a point of damage, so
// the sound reaches a watcher while the health bar stays whole.
// vanilla: LivingEntity.causeFallDamage, HoneyBlock.fallOn
func TestHoneyLandingSoundFollowsTheDamageNotTheHurt(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, func(s *server.Server) {
		s.SetLoadedBlock(0, 63, 0, servertest.BlockState(t, "minecraft:honey_block"))
		s.SetGameRule("fall_damage", false)
	})
	watcher := connect()
	watcher.JoinAs("Watcher")

	c := connect()
	c.JoinAs("Faller")
	watcher.ExpectPlayerAdded("Faller")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Faller")
	srv.StopTick()

	c.MoveToGround(0.5, 75, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)
	// Settle the faller first, so the landing has run before the watcher's
	// ping lets its read end.
	c.Settle()

	fall, _ := registry.SoundEventID("minecraft:block.honey_block.fall")
	sawFall := false
	watcher.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySoundEffect && protocol.NewReader(data).ReadVarInt()-1 == fall {
			sawFall = true
		}
	})

	if got := srv.PlayerHealth("Faller"); got != 20 {
		t.Errorf("health with fall_damage off = %v, want 20", got)
	}
	if !sawFall {
		t.Error("the honey landing with fall_damage off sent no block fall sound")
	}
}

// TestCreativeFallDoesNoDamage checks the other half of fall damage: the mode
// a player is in can make the same fall cost nothing. The untouched health
// bar is proven by the next one, which only shows what it should if the
// creative fall was free.
func TestCreativeFallDoesNoDamage(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	// The test switches back to survival with /gamemode, whose runner is
	// listed as an operator.
	_, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	playerUUID := c.Login("Testificate")
	c.Configure()
	c.EnterWorldAs("Testificate")

	// The same six-block fall, into the mode that cannot be hurt.
	c.MoveToGround(0.5, 70, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	// Back into survival, and a fall worth one half-heart past the free
	// three. The bar the client sees after it is 19 — the proof that the
	// fall six blocks up, in creative, cost nothing at all.
	c.ChatCommand("gamemode survival")
	c.ExpectModePackets(playerUUID, protocol.GameModeSurvival, true)

	c.MoveToGround(0.5, 68, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.AwaitPlay(protocol.IDPlayLevelParticles)
	if got := c.ExpectHealth(); got.Health != 19 {
		t.Errorf("health after the survival fall = %v, want 19 health, the creative fall having cost nothing", got.Health)
	}
}

// TestJumpBoostRaisesTheSafeFallDistance falls six blocks under Jump Boost II:
// the effect adds a block a level to safe_fall_distance, so the safe distance
// is five and only the block past it hurts, leaving 19 rather than a plain
// fall's 17.
func TestJumpBoostRaisesTheSafeFallDistance(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c, _ := servertest.JoinAs(t, cfg, "Hopper")

	srv.GiveEffect("Hopper", "minecraft:jump_boost", 600, 1, false)
	c.AwaitMobEffect()
	// The modifier rides the attribute map, refreshed on the tick, so two
	// ticks have to run before the loop is stopped for the fall. The settle
	// drops the effect particles those ticks sent, which the health read
	// would otherwise trip over.
	since := srv.TicksRun()
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.TicksRun() >= since+2 })
	c.Settle()
	srv.StopTick()

	c.MoveToGround(0.5, 70, 0.5, false)
	c.MoveToGround(0.5, 67, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)

	c.AwaitPlay(protocol.IDPlayLevelParticles)
	if got := c.ExpectHealth(); got.Health != 19 {
		t.Errorf("health after the six-block fall under Jump Boost II = %v, want 19 (a safe distance of five)", got.Health)
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

// TestSlowFallingTakesTheFallBack lands the six-block fall under Slow Falling:
// the effect resets the fall distance every tick, so the body keeps all
// twenty.
func TestSlowFallingTakesTheFallBack(t *testing.T) {
	t.Parallel()
	testFallResettingEffect(t, "minecraft:slow_falling")
}

// TestLevitationTakesTheFallBack lands the six-block fall under Levitation:
// the effect resets the fall distance every tick, so the body keeps all
// twenty.
func TestLevitationTakesTheFallBack(t *testing.T) {
	t.Parallel()
	testFallResettingEffect(t, "minecraft:levitation")
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

// TestSprintJumpOffAnEdgeDealsNoFallDamage replays the sprint jump the
// report came from: off the edge of a block two above the ground, whose arc
// adds a block and a quarter to the fall. The quarter past the safe three
// blocks is not a half-heart: vanilla counts a landing's damage in whole
// blocks fallen past the safe fall, and three and a quarter is three for
// that count, which is nothing.
func TestSprintJumpOffAnEdgeDealsNoFallDamage(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c, _ := servertest.JoinAs(t, cfg, "Jumper")

	// The edge: a pillar of two blocks, its top two above the flat world's
	// ground. Standing on it and jumping off it east is the reported fall.
	// The pillar's chunk is one the join fills in its own time, so the
	// ground is what the wait waits on before the pillar goes up.
	stone := servertest.BlockState(t, "minecraft:stone")
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.BlockAt(10, 63, 10) != 0 })
	if !srv.SetBlock(10, 64, 10, stone) || !srv.SetBlock(10, 65, 10, stone) {
		t.Fatal("the pillar's chunk was not loaded")
	}
	c.MoveToGround(10.5, 66, 10.5, true)
	c.Settle()

	// The jump: one movement packet a tick, the arc rising off the pillar
	// while it carries east, and the landing two blocks below where it
	// started. The whole fall from the arc's top is three and a quarter
	// blocks.
	y := 66.0
	x := 10.5
	for _, step := range jumpSteps() {
		y += step
		x += 0.3
		if y <= 64 {
			c.MoveToGround(x, 64, 10.5, true)
			break
		}
		c.MoveToGround(x, y, 10.5, false)
	}
	c.Settle()

	// Nothing hurt: no damage event went out and the bars the client was
	// left with are the bars the join gave, the quarter past the safe fall
	// having cost nothing.
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDamageEvent, protocol.IDPlaySetHealth))
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

// TestAFallPastALadderHurts checks the climb's edge: the same fall taken one
// cell over, where there is nothing to climb, lands the whole of its
// distance — the first three blocks free, the rest a half-heart apiece.
func TestAFallPastALadderHurts(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c, _ := servertest.JoinAs(t, cfg, "Skipper")

	ladderColumn(t, srv, 71)

	c.MoveToGround(11.5, 74, 10.5, false)
	c.MoveToGround(11.5, 68.8, 10.5, false)
	c.MoveToGround(11.5, 64, 10.5, true)
	c.AwaitPlay(protocol.IDPlayLevelParticles)
	if got := c.ExpectHealth(); got.Health != 13 {
		t.Errorf("health after the fall past the ladder = %v, want 13", got.Health)
	}
}

// TestAStepOffALadderCountsOnlyTheNewDrop checks the catch's other half: the
// climb forgives the fall it caught, and the drop that follows the step off
// is a fall of its own, banked from where the catch left the body.
func TestAStepOffALadderCountsOnlyTheNewDrop(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c, _ := servertest.JoinAs(t, cfg, "Stepper")

	ladderColumn(t, srv, 71)

	// Up above the column, the fall into the climb, two slide steps into
	// it; then the step off, east and out of the column, and the free fall
	// to the ground below it.
	c.MoveToGround(10.5, 74, 10.5, false)
	climbFall(c, 10.5, 68.8, 10.5, 2, 0.3)
	c.MoveToGround(11.4, 66.5, 10.5, false)
	c.MoveToGround(11.4, 64, 10.5, true)
	c.AwaitPlay(protocol.IDPlayLevelParticles)
	if got := c.ExpectHealth(); got.Health != 19 {
		t.Errorf("health after the step off = %v, want 19, the drop past the catch being the fall's own", got.Health)
	}
}

// TestAnOpenTrapdoorOverALadderCatches checks the trapdoor's rule: an open
// trapdoor stands in for the ladder under it when their facing matches, and
// a fall caught in the trapdoor's cell costs nothing, the same mercy a
// ladder's own cell shows.
func TestAnOpenTrapdoorOverALadderCatches(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c, _ := servertest.JoinAs(t, cfg, "Trapdoorer")

	ladder := servertest.BlockStateWith(t, "minecraft:ladder", "facing=east,waterlogged=false")
	trapdoor := servertest.BlockStateWith(t, "minecraft:oak_trapdoor",
		"facing=east,half=bottom,open=true,powered=false,waterlogged=false")
	servertest.PollUntil(t, 10*time.Second, func() bool { return srv.BlockAt(10, 63, 10) != 0 })
	if !srv.SetBlock(10, 64, 10, ladder) || !srv.SetBlock(10, 65, 10, trapdoor) {
		t.Fatal("the trapdoor rig's chunk was not loaded")
	}

	// Three blocks down through the air into the trapdoor's cell, one
	// slide step, and the landing on the ground below it.
	c.MoveToGround(10.5, 68.8, 10.5, false)
	c.MoveToGround(10.5, 65.5, 10.5, false)
	c.MoveToGround(10.5, 65.35, 10.5, false)
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

// TestTheVoidKillsTheInvulnerableModes: the void's out_of_world hurt is on
// vanilla's bypasses-invulnerability list, so it reaches a creative or
// spectator player the modes otherwise keep out of harm's way.
func TestTheVoidKillsTheInvulnerableModes(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"creative", "spectator"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.GameMode = mode

			_, c, entityID := servertest.JoinAs(t, cfg, "Testificate")

			c.MoveToGround(0.5, -129, 0.5, false)
			c.Settle()

			got := c.ExpectHealth()
			if want := float32(16.0); got.Health != want {
				t.Errorf("health = %v, want %v after the first hit of void damage", got.Health, want)
			}
			c.ExpectDamageEvent(entityID, "minecraft:out_of_world", 0)
		})
	}
}

// TestTheVoidSparesTheBlockAtItsFloor: vanilla's checkBelowWorld is a strict
// y < minY - 64, so a body exactly on the void's floor line is still in the
// world and takes no void damage.
func TestTheVoidSparesTheBlockAtItsFloor(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c, _ := servertest.JoinAs(t, cfg, "Testificate")

	// The overworld floor is -64, so the void begins at -128; exactly
	// there is not yet out of the world.
	c.MoveToGround(0.5, -128, 0.5, false)
	c.PumpTicks(srv, 15, func(id int32, data []byte) bool {
		if id == protocol.IDPlayDamageEvent || id == protocol.IDPlaySetHealth {
			t.Error("the void hurt a body exactly on its floor line")
			return true
		}
		return false
	})
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

// TestAStrongerHurtInsideTheWindowLandsTheDifference checks vanilla's rule
// for a hit landing inside the invulnerability window the previous one
// opened (LivingEntity.hurtServer): only a hit stronger than the last one
// lands, and it lands what it beats it by, while one no stronger is
// refused. A half-heart prick then a three-half-heart blow five ticks later
// leaves seventeen, where dropping the second hit would leave nineteen, and
// the hit that landed only its excess is told by the health alone — no
// damage event and no flinch ride it, for the victim or a watcher.
func TestAStrongerHurtInsideTheWindowLandsTheDifference(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, connect := servertest.Start(t, cfg)
	watcher := connect()
	watcher.JoinAs("Watcher")

	c := connect()
	entityID := c.JoinAs("Bruised")
	watcher.ExpectPlayerAdded("Bruised")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Bruised")
	// Below the natural-regeneration food floor, so no heal reaches the
	// body while the test waits out the window.
	srv.SetFood("Bruised", 17)
	// The window counts down on the tick, so the ticks between the hits are
	// driven by hand: a live loop catching up on a loaded machine can close
	// it before the second hit.
	srv.StopTick()

	// The whole hit: the health and the damage event reach the victim, and
	// the watcher sees the event and the flinch too.
	if !srv.Hurt("Bruised", 1) {
		t.Fatal("the first hurt did not land")
	}
	if got := srv.PlayerHealth("Bruised"); got != 19 {
		t.Fatalf("health after the first hurt = %v, want 19", got)
	}
	// A tick before StopTick may have told the lowered food first, at full
	// health; the hurt's own health follows it.
	for c.ExpectHealth().Health != 19 {
	}
	c.ExpectDamageEvent(entityID, "minecraft:mob_attack", 1)
	c.ExpectHurtAnimation(entityID)
	watcher.ExpectDamageEvent(entityID, "minecraft:mob_attack", 1)

	// Five of the window's ten ticks gone: the second hit still lands
	// inside it, and takes only what it beats the first by.
	for range 5 {
		srv.TickSurvival()
	}
	if !srv.Hurt("Bruised", 3) {
		t.Fatal("the stronger hurt inside the window was dropped")
	}
	if got := srv.PlayerHealth("Bruised"); got != 17 {
		t.Errorf("health after the stronger hurt = %v, want 17 (three less the one already taken)", got)
	}
	// A hit no stronger than the last is refused while the window is open,
	// checked here so the waits below cannot let it expire first.
	if srv.Hurt("Bruised", 3) {
		t.Error("a hurt no stronger than the last landed inside the window")
	}
	if got := srv.PlayerHealth("Bruised"); got != 17 {
		t.Errorf("health after the equal hurt = %v, want 17 (nothing landed)", got)
	}

	// A hit that landed only its excess is told by the health alone.
	c.ExpectHealth()
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayDamageEvent || id == protocol.IDPlayHurtAnimation {
			t.Errorf("a hit inside the window was told of like a whole one, packet 0x%02X", id)
		}
	})
	watcher.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayDamageEvent || id == protocol.IDPlayHurtAnimation {
			t.Errorf("a hit inside the window reached a watcher like a whole one, packet 0x%02X", id)
		}
	})
}

func TestDyingPlayerReceivesOwnDeathEntityEvent(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Dier", servertest.OpsListing(t, "Dier"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand("kill @s")
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 2, protocol.IDPlayEntityEvent))
	if got := r.ReadI32(); got != c.PlayerEntityID {
		t.Errorf("death entity ID = %d, want own ID %d", got, c.PlayerEntityID)
	}
	if got := r.ReadU8(); got != protocol.EntityStatusDeath {
		t.Errorf("entity status = %d, want %d (death)", got, protocol.EntityStatusDeath)
	}
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

// TestADeadBodyShownToALatecomerFallsOver checks that a player who joins
// while another is dead is shown that body fallen over: the join's spawn
// pairing carries the dead player's health at zero, which its default would
// otherwise leave out.
func TestADeadBodyShownToALatecomerFallsOver(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	dier := connect()
	dierID := dier.JoinAs("Dier")
	// The corpse lasts twenty ticks; login must not spend that lifetime.
	servertest.HoldTicks(t, srv)

	// A fall of thirty blocks: twenty health against twenty-seven damage.
	dier.MoveToGround(0.5, 94, 0.5, false)
	dier.MoveToGround(0.5, 64, 0.5, true)
	dier.SettleKeeping()
	fellMessage := protocol.Text{
		Translate: "death.fell.accident.generic",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Dier")},
	}
	dier.AwaitPlay(protocol.IDPlayLevelParticles)
	if got := dier.ExpectHealth(); got.Health != 0 {
		t.Fatalf("health after the fall = %v, want 0", got.Health)
	}
	dier.ExpectDamageEvent(dierID, "minecraft:fall", 0)
	dier.ExpectCombatDeath(dierID, fellMessage)
	dier.ExpectDeathMessage(fellMessage)

	// A latecomer joins while the body is still there. Its own join shows it
	// the players in the world, and Dier's pairing must carry the health
	// that draws the corpse rather than a standing, living player.
	late := connect()
	late.Login("Late")
	late.Configure()

	var sawSpawn, sawHealth bool
	// Pairing is sent by the join itself, so its ping barrier needs no tick.
	late.ReadTicks(srv, 0, func(packetID int32, data []byte) bool {
		switch packetID {
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			entity := r.ReadVarInt()
			r.ReadUUID()
			if entity == dierID && r.ReadVarInt() == protocol.EntityTypePlayer {
				sawSpawn = true
			}
		case protocol.IDPlayEntityMetadata:
			if entity, health, hasHealth := servertest.PlayerMetadataHealth(data); entity == dierID && hasHealth && health == 0 {
				sawHealth = true
			}
		}
		return sawSpawn && sawHealth
	})
	if !sawSpawn {
		t.Fatal("the latecomer was never shown the dead body")
	}
	if !sawHealth {
		t.Fatal("the latecomer's copy of the dead body has no health-0 entry, so it is drawn standing alive")
	}
	// The chunk stream waits for ticks; finish it after checking the pairing.
	servertest.ReleaseTicks(t, srv)
	late.EnterWorld("Late")
}

// TestARespawnWaitsForTheDeathQueuedAheadOfIt holds the mailbox's writer,
// kills the player and asks for the respawn while everything the death sent
// is still queued: the Respawn must wait for the queue rather than overtake
// it, so the death's empty bars go out before it and the fresh ones after.
// The bars are written through the mailbox while the respawn's sequence is
// written straight to the connection, and the two paths would otherwise
// interleave whole packets in either order.
func TestARespawnWaitsForTheDeathQueuedAheadOfIt(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Dier")

	release := srv.HoldMailbox("Dier")
	t.Cleanup(release)

	// A fall of thirty blocks: twenty health against twenty-seven damage.
	// The death's traffic - the empty bar among it - queues behind the held
	// writer.
	c.MoveStep(0.5, 94, 0.5, false)
	c.MoveStep(0.5, 64, 0.5, true)

	// The respawn asked for while the death is still queued.
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	// The held writer cannot answer a barrier ping, so the ticks run alone.
	servertest.RunTicks(t, srv, servertest.QuietTicks)
	release()

	// The held queue drains ahead of the Respawn now, the death's own bars
	// among it, and the respawn's own bars follow it.
	var sawDeathBars, sawRespawn, sawFreshBars bool
	for packetID, data := range c.Packets(servertest.ExpectTimeout) {
		switch packetID {
		case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayTeleportEntity, protocol.IDPlayChunkData,
			protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkBatchFinished,
			protocol.IDPlayCenterChunk, protocol.IDPlaySetEntityMotion,
			protocol.IDPlayEntityMetadata, protocol.IDPlaySpawnEntity,
			protocol.IDPlayDamageEvent, protocol.IDPlayLevelParticles, protocol.IDPlayHurtAnimation,
			protocol.IDPlayEntityEvent, protocol.IDPlaySoundEffect,
			protocol.IDPlayWorldEvent, protocol.IDPlayCombatDeath,
			protocol.IDPlaySystemChat, protocol.IDPlayContainerSlot,
			protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks, protocol.IDPlayAckBlockChange,
			protocol.IDPlaySetExperience, protocol.IDPlayAbilities,
			protocol.IDPlaySyncPosition, protocol.IDPlayGameEvent,
			protocol.IDPlayUpdateAttributes:
			// The death's own traffic and the arrival's, read past.
		case protocol.IDPlaySetHealth:
			health := protocol.NewReader(data).ReadF32()
			if !sawRespawn {
				if sawDeathBars || health != 0 {
					c.T.Fatalf("a bars packet of %v arrived before the Respawn, want only the death's empty one", health)
				}
				sawDeathBars = true
			} else {
				if health != 20 {
					c.T.Errorf("health after the respawn = %v, want 20 (the bars a death empties)", health)
				}
				sawFreshBars = true
			}
		case protocol.IDPlayRespawn:
			if !sawDeathBars {
				c.T.Fatal("the Respawn overtook the death's own bars")
			}
			sawRespawn = true
		default:
			c.T.Fatalf("unexpected packet 0x%02X in the respawn sequence", packetID)
		}
		if sawFreshBars {
			break
		}
	}
	if !sawDeathBars {
		c.T.Fatal("the death's empty bars never arrived")
	}
	if !sawRespawn {
		c.T.Fatal("the respawn never arrived")
	}
	if !sawFreshBars {
		c.T.Fatal("no fresh bars after the respawn")
	}
}

// TestARespawnWhoseDrainFailsGivesTheWriterBack checks that a respawn whose
// held queue fails to write does not keep the mailbox's writer held for
// good. The write cannot fail without the connection failing with it, so
// what the test watches is the hold itself, through a probe that outlives
// the player. The writer goroutine is stopped so nothing else can take the
// respawn's turn at the held writer.
func TestARespawnWhoseDrainFailsGivesTheWriterBack(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Dier")
	srv.StopTick()
	if !srv.StopMailbox("Dier") {
		t.Fatal("Dier is not online")
	}

	release := srv.HoldMailbox("Dier")
	t.Cleanup(release)

	// A fall of thirty blocks: twenty health against twenty-seven damage.
	// The death's traffic - the empty bar among it - queues behind the held
	// writer, and with the writer stopped nothing drains it.
	c.MoveStep(0.5, 94, 0.5, false)
	c.MoveStep(0.5, 64, 0.5, true)
	held, ok := srv.WriterHeld("Dier")
	if !ok {
		t.Fatal("Dier is not online")
	}

	// The respawn parks on the held writer - the Respawn it would write
	// never comes.
	holding := make(chan struct{})
	if !srv.BeforeWriterHold("Dier", func() { close(holding) }) {
		t.Fatal("Dier is not online")
	}
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	// The read loop must reach the hold before the closed socket can end it.
	select {
	case <-holding:
	case <-time.After(5 * time.Second):
		t.Fatal("the respawn did not reach the writer hold")
	}

	// Closed from the server's side, so the respawn's queue fails to write
	// the moment it gets its turn; a client-side close leaves the socket
	// writable and the drain would go through.
	if !srv.DropConnection("Dier") {
		t.Fatal("Dier is not online")
	}
	release()

	// The respawn runs on the read loop, which notices the closed
	// connection only once the respawn has returned - its hold given back
	// with it. Waiting for the player to be gone first keeps the check
	// below from passing on the moment the writer sits free between the
	// release and the parked respawn's taking of it.
	servertest.PollUntil(t, 5*time.Second, func() bool { return srv.PlayerCount() == 0 })
	if held() {
		t.Fatal("the respawn's failed drain kept the writer held")
	}
}

// TestAVitalsTickStraddlingTheDeathSendsNoBarsOfItsOwn kills the player
// inside the window a vitals tick leaves between reading their bars and
// comparing them - the straddle a blow from another connection wins in
// production. The death's own bars went out with the death; the tick must
// send none for the body it now sees dead, a stale empty bar being the one
// that would reach the client across the respawn's Respawn.
func TestAVitalsTickStraddlingTheDeathSendsNoBarsOfItsOwn(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()

	srv, c := servertest.Join(t, cfg, "Dier")
	servertest.HoldTicks(t, srv)

	// The tick below straddles the death: it reads the bars whole, the
	// death empties them inside its window, and the comparison it ends on
	// sees a dead body against the live one it read.
	var landed bool
	srv.BarsBetween("Dier", func() { landed = srv.Hurt("Dier", 27) })
	srv.TickSurvival()
	if !landed {
		t.Fatal("the blow did not land")
	}

	// The death's own empty bar, and nothing behind it.
	var empty, other int
	c.ReadTicks(srv, 10, func(packetID int32, data []byte) bool {
		if packetID == protocol.IDPlaySetHealth {
			if health := protocol.NewReader(data).ReadF32(); health == 0 {
				empty++
			} else {
				other++
			}
		}
		return false
	})
	if empty != 1 {
		t.Errorf("empty bars packets = %d, want 1 (the death's own)", empty)
	}
	if other != 0 {
		t.Errorf("the dead body's tick sent %d bars packets of its own", other)
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

// TestTheLoadingGraceRunsOut checks the other way the grace ends: a client
// that never sends Player Loaded is treated as loaded once sixty ticks have
// gone by (ServerGamePacketListenerImpl.tickClientLoadTimeout).
func TestTheLoadingGraceRunsOut(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg)

	c := connect()
	c.Login("Silent")
	c.Configure()
	c.EnterWorldLoading("Silent")
	// Well past the sixty ticks, keep-alives answered meanwhile.
	c.ExpectNoneWithinTicks(srv, 80, servertest.PacketIs(protocol.IDPlaySetHealth))

	c.MoveToGround(0.5, 94, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.AwaitPlay(protocol.IDPlayLevelParticles)
	if got := c.ExpectHealth(); got.Health != 0 {
		t.Errorf("the thirty-block fall after the grace left %v health, want it to kill", got.Health)
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

// TestBreakingWithoutTheCorrectToolCostsNoExhaustion checks a break that
// does not harvest skips Block.playerDestroy's exhaustion.
func TestBreakingWithoutTheCorrectToolCostsNoExhaustion(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c, _ := servertest.JoinAs(t, cfg, "Digger")
	c.MoveToGround(0.5, 64, 10.5, true)
	c.Settle()
	snow, ok := registry.FindState("minecraft:snow", nil)
	if !ok {
		t.Fatal("the registry holds no snow")
	}
	for i, z := range []int32{12, 13} {
		if !srv.SetBlock(0, 63, z, snow) {
			t.Fatal("could not stand the snow")
		}
		want := 0.0
		if i == 1 {
			srv.GiveSlot("Digger", player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemWoodenShovel), Count: 1})
			want = 0.005
		}
		c.StartDigging(0, 63, z)
		servertest.RunTicks(t, srv, 10)
		c.BreakBlock(0, 63, z)
		c.ExpectBlockChange()
		if got := srv.PlayerExhaustion("Digger"); math.Abs(got-want) > 1e-9 {
			t.Errorf("exhaustion after break %d = %v, want %v", i, got, want)
		}
	}
}

// TestGameModeCommandReadsLikeVanilla pins the parts of /gamemode the
// vanilla GameModeCommand and GameModeArgument decide: a word that names no
// mode is refused while the command parses, a selector switches every player
// it names rather than the first, and a player already in the mode is passed
// over without a word.
func TestGameModeCommandReadsLikeVanilla(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// /gamemode is a gamemaster's command, so Runner, who runs it, is
	// listed as an operator; Other is only ever switched.
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Runner"))

	runner := connect()
	runner.JoinAs("Runner")

	other := connect()
	other.JoinAs("Other")
	runner.ExpectPlayerAdded("Other")
	runner.ExpectAnnouncement("multiplayer.player.joined", "Other")

	// The refusal names the word and points past it, the vanilla server's
	// "...mode bogus<--[HERE]".
	runner.ChatCommand("gamemode bogus")
	runner.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.gamemode.invalid", "bogus"),
		servertest.CommandErrorContextForTest("gamemode bogus", len("gamemode bogus")))

	// @a reaches both players, not only the first one it found.
	runner.ChatCommand("gamemode creative @a")
	servertest.PollUntil(t, 5*time.Second, func() bool {
		return srv.PlayerGameMode("Runner") == protocol.GameModeCreative &&
			srv.PlayerGameMode("Other") == protocol.GameModeCreative
	})

	// Switching them again to the same mode changes nothing and says
	// nothing.
	runner.Drain()
	runner.ChatCommand("gamemode creative @a")
	runner.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySystemChat {
			t.Errorf("a gamemode that changed nobody answered % X", data)
		}
	})
}

// TestLandingOnAnUpwardDripstoneTipHurtsAsStalagmite pins
// PointedDripstoneBlock.fallOn on the wire: a one-block drop onto an upward
// tip costs a half-heart pair (floor((1 + 2.5 - 3) * 2) = 1) under the
// stalagmite damage type, while a downward tip lets the same drop pass.
func TestLandingOnAnUpwardDripstoneTipHurtsAsStalagmite(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		direction string
		health    float64
	}{{"up", 19}, {"down", 20}} {
		t.Run(tc.direction, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			srv, c, id := servertest.JoinAs(t, cfg, "Faller", func(s *server.Server) {
				s.SetLoadedBlock(0, 63, 0, servertest.BlockStateWith(t, "minecraft:pointed_dripstone",
					"thickness=tip,vertical_direction="+tc.direction+",waterlogged=false"))
			})
			srv.StopTick()

			c.MoveToGround(0.5, 65, 0.5, false)
			c.MoveToGround(0.5, 64, 0.5, true)
			var types []int32
			c.SettleAndCollect(func(pid int32, data []byte) {
				if pid == protocol.IDPlayDamageEvent {
					r := protocol.NewReader(data)
					if r.ReadVarInt() == id {
						types = append(types, r.ReadVarInt())
					}
				}
			})
			want := []int32(nil)
			if tc.health < 20 {
				want = []int32{registry.MustID("minecraft:damage_type", "minecraft:stalagmite")}
			}
			if !slices.Equal(types, want) {
				t.Errorf("damage event types after the drop onto a %s tip = %v, want %v", tc.direction, types, want)
			}

			if got := srv.PlayerHealth("Faller"); math.Abs(got-tc.health) > 1e-3 {
				t.Errorf("health after a one-block drop onto a %s tip = %v, want %v", tc.direction, got, tc.health)
			}
		})
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

// TestDeathDestroysVanishingCursedBodyAndSaddleItems checks that the
// vanishing curse also deletes the stacks in the body and saddle slots,
// which are equipment outside the main inventory.
// vanilla: Player.destroyVanishingCursedItems
func TestDeathDestroysVanishingCursedBodyAndSaddleItems(t *testing.T) {
	t.Parallel()
	srv, c, entityID := servertest.JoinAs(t, servertest.SurvivalConfig(), "Dier")

	sword := servertest.EnchantedStack(t, "minecraft:diamond_sword", "minecraft:vanishing_curse", 1)
	plain := protocol.ItemStack{ItemID: sword.ItemID, Count: 1}
	srv.GiveSlot("Dier", 9, plain)
	srv.GiveBody("Dier", servertest.EnchantedStack(t, "minecraft:diamond_horse_armor", "minecraft:vanishing_curse", 1))
	srv.GiveSaddle("Dier", servertest.EnchantedStack(t, "minecraft:saddle", "minecraft:vanishing_curse", 1))

	c.MoveStep(0.5, 94, 0.5, false)
	c.MoveStep(0.5, 64, 0.5, true)
	c.ExpectDroppedItem()
	// The death's drops are spawned before its Combat Death is sent.
	c.ExpectCombatDeath(entityID, protocol.Text{
		Translate: "death.fell.accident.generic",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Dier")},
	})

	var got []string
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		got = append(got, it.Item)
	}
	if !slices.Equal(got, []string{"minecraft:diamond_sword"}) {
		t.Errorf("death dropped %v, want only the plain sword", got)
	}
}

// TestFallDamageScalesWithTheMultiplierAttribute checks the player's
// fall_damage_multiplier attribute (LivingEntity.calculateFallDamage): a
// multiplier of 2 doubles the three points a six-block fall costs.
func TestFallDamageScalesWithTheMultiplierAttribute(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Faller"))

	faller := connect()
	faller.JoinAs("Faller")

	faller.ChatCommand("attribute Faller minecraft:fall_damage_multiplier base set 2")
	faller.ExpectPlay(protocol.IDPlaySystemChat)
	srv.StopTick()

	faller.MoveToGround(0.5, 70, 0.5, false)
	faller.MoveToGround(0.5, 67, 0.5, false)
	faller.MoveToGround(0.5, 64, 0.5, true)
	faller.AwaitPlay(protocol.IDPlayLevelParticles)

	if got := faller.ExpectHealth(); got.Health != 14 {
		t.Errorf("after the fall: health=%v, want 14", got.Health)
	}
}

// TestACreativePlayerStillRegeneratesFromFood checks that FoodData.tick runs
// in every mode (vanilla: ServerPlayer.doTick): a hurt creative player on a
// full bar with saturation heals on the saturated rhythm.
func TestACreativePlayerStillRegeneratesFromFood(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Builder")
	srv.SetPlayerGameMode("Builder", protocol.GameModeCreative)
	servertest.HoldTicks(t, srv)
	srv.SetHealth("Builder", 10)
	// The setter changes health outside the tick, so the next tick sends the
	// Set Health of 10; take it before the heal.
	srv.TickSurvival()
	c.Settle()
	protocol.NewReader(c.ExpectWithinTicks(srv, 40, protocol.IDPlaySetHealth))

	r := protocol.NewReader(c.ExpectWithinTicks(srv, 40, protocol.IDPlaySetHealth))
	if health := r.ReadF32(); health <= 10 {
		t.Errorf("a creative player's health went to %v, want it to rise from 10", health)
	}
}

// TestAPlayersFallIsHeardByOthers pins that a damaging landing sends the
// other players the player fall sound and the block's fall sound, and the
// faller neither.
// vanilla: LivingEntity.causeFallDamage, playBlockFallSound
func TestAPlayersFallIsHeardByOthers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		fromY float64
		sound string
	}{
		{"big", 75, "minecraft:entity.player.big_fall"},
		{"small", 69, "minecraft:entity.player.small_fall"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			srv, connect := servertest.Start(t, cfg, func(s *server.Server) {
				s.SetLoadedBlock(0, 63, 0, servertest.BlockState(t, "minecraft:stone"))
			})
			watcher := connect()
			watcher.JoinAs("Watcher")
			c := connect()
			c.JoinAs("Faller")
			watcher.ExpectPlayerAdded("Faller")
			watcher.ExpectAnnouncement("multiplayer.player.joined", "Faller")
			srv.StopTick()

			c.MoveToGround(0.5, tc.fromY, 0.5, false)
			c.MoveToGround(0.5, 64, 0.5, true)

			player, _ := registry.SoundEventID(tc.sound)
			block, _ := registry.SoundEventID("minecraft:block.stone.fall")
			count := func(cl *servertest.Client) (p, b int) {
				cl.SettleAndCollect(func(id int32, data []byte) {
					if id != protocol.IDPlaySoundEffect {
						return
					}
					switch protocol.NewReader(data).ReadVarInt() - 1 {
					case player:
						p++
					case block:
						b++
					}
				})
				return
			}
			if p, b := count(c); p != 0 || b != 0 {
				t.Errorf("the faller heard its own fall: %d player, %d block sounds", p, b)
			}
			if p, b := count(watcher); p != 1 || b != 1 {
				t.Errorf("the watcher heard %d %s and %d block.stone.fall, want 1 and 1", p, tc.sound, b)
			}
		})
	}
}

// A landing past the safe distance bursts into block particles of the ground
// under the body, whether or not the fall hurt; a fall within it sends none.
// vanilla: LivingEntity.checkFallDamage
func TestLandingFromASixBlockFallBurstsIntoBlockParticles(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg)
	watcher := connect()
	watcher.JoinAs("Watcher")
	faller := connect()
	faller.JoinAs("Faller")
	watcher.ExpectPlayerAdded("Faller")
	srv.StopTick()

	faller.MoveToGround(0.5, 70, 0.5, false)
	faller.MoveToGround(0.5, 67, 0.5, false)
	faller.MoveToGround(0.5, 64, 0.5, true)

	ground := srv.BlockAt(0, 63, 0)
	for name, c := range map[string]*servertest.Client{"watcher": watcher, "faller": faller} {
		r := protocol.NewReader(c.AwaitPlay(protocol.IDPlayLevelParticles))
		if kind := r.ReadVarInt(); kind != protocol.ParticleBlock {
			t.Fatalf("%s: particle type %d, want block", name, kind)
		}
		// The block state follows the type, ahead of the common tail.
		state := r.ReadVarInt()
		p := servertest.ReadParticleTail(t, r)
		want := int32(150 * math.Min(float64(float32(0.2))+3/15.0, 2.5))
		if state != int32(ground) || p.Y != 64 || p.DX != 0 || p.DY != 0 || p.DZ != 0 || p.Speed != 0.15 || p.Count != want || want != 60 {
			t.Errorf("%s: state=%d y=%v offsets=%v,%v,%v speed=%v count=%d; want state %d y 64 speed 0.15 count %d",
				name, state, p.Y, p.DX, p.DY, p.DZ, p.Speed, p.Count, ground, want)
		}
	}
}

func TestLandingFromAThreeBlockFallSendsNoBlockParticles(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Faller")
	servertest.HoldTicks(t, srv)

	c.MoveToGround(0.5, 67, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.ReadTicks(srv, 5, func(id int32, _ []byte) bool {
		if id == protocol.IDPlayLevelParticles {
			t.Error("a three-block fall sent particles")
		}
		return false
	})
}
