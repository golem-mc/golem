package commands

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSummonTeamTagJoinsTeam checks that a summoned body's Team string puts
// its UUID on that team, which /team list shows and the summon line formats,
// that a Team naming no team leaves it off, and that /data merge's Team
// moves it.
// vanilla: Entity.load, SummonCommand
func TestSummonTeamTagJoinsTeam(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("team add red")
	awaitFeedbackWith(t, c, "commands.team.add.success")
	c.ChatCommand(`team modify red prefix {"text":"[R] "}`)
	awaitFeedbackWith(t, c, "commands.team.option.prefix.success")
	c.ChatCommand("team modify red color red")
	awaitFeedbackWith(t, c, "commands.team.option.color.success")

	id := uuid.MustParse("00000001-0000-0002-0000-000300000004")
	c.ChatCommand(`summon zombie ~ ~ ~ {NoAI:1b,UUID:[I;1,2,3,4],Team:"red"}`)
	with := awaitFeedbackWith(t, c, "commands.summon.success")
	name := protocol.Text{Translate: "entity.minecraft.zombie"}
	want := protocol.Text{
		Extra:       []protocol.Text{{Text: "[R] "}, name, {}},
		Color:       "red",
		Insertion:   id.String(),
		HoverEntity: &protocol.HoverEntity{Name: name, ID: "minecraft:zombie", UUID: [4]uint32{1, 2, 3, 4}},
	}
	if len(with) != 1 || !reflect.DeepEqual(with[0], want) {
		t.Fatalf("summon line names %+v, want %+v", with, want)
	}
	c.ChatCommand("team list red")
	with = awaitFeedbackWith(t, c, "commands.team.list.members.success")
	if len(with) != 3 || number(with[1]) != 1 || with[2].Text != id.String() {
		t.Errorf("red's members %+v, want the zombie's UUID alone", with)
	}

	// A team that does not exist leaves the body on none.
	c.ChatCommand(`summon zombie ~ ~ ~ {NoAI:1b,UUID:[I;5,6,7,8],Team:"nope"}`)
	with = awaitFeedbackWith(t, c, "commands.summon.success")
	if len(with) != 1 || with[0].Translate != "entity.minecraft.zombie" {
		t.Errorf("summon line names %+v, want the bare type", with)
	}

	c.ChatCommand("team add blue")
	awaitFeedbackWith(t, c, "commands.team.add.success")
	c.ChatCommand(`data merge entity 00000001-0000-0002-0000-000300000004 {Team:"blue"}`)
	awaitFeedbackWith(t, c, "commands.data.entity.modified")
	c.ChatCommand("team list blue")
	with = awaitFeedbackWith(t, c, "commands.team.list.members.success")
	if len(with) != 3 || with[2].Text != id.String() {
		t.Errorf("blue's members %+v, want the zombie's UUID alone", with)
	}
	c.Disconnect()
}

// TestAKilledMobKeepsItsScoresAndTeamUntilItsBodyGoes checks that a killed
// mob holds its scores and team through its twenty-tick death animation and
// loses them, with a reset_score to the clients, once its body is removed.
// vanilla: LivingEntity.tickDeath, ServerLevel.EntityCallbacks.onDestroyed,
// Scoreboard.entityRemoved
func TestAKilledMobKeepsItsScoresAndTeamUntilItsBodyGoes(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	id := "00000001-0000-0002-0000-000300000004"
	for _, line := range []string{"team add red", "scoreboard objectives add hp dummy",
		`execute at Testificate run summon zombie ~ ~ ~ {NoAI:1b,UUID:[I;1,2,3,4],Team:"red"}`,
		"scoreboard players set " + id + " hp 5"} {
		srv.RunLine(line)
	}
	holds := func() bool {
		v, ok := srv.ScoreOf(id, "hp")
		team := srv.RunLine("team list red")
		return ok && v == 5 && len(team) == 1 && team[0].Translate == "commands.team.list.members.success"
	}
	if !holds() {
		t.Fatal("the zombie has no score of 5 or is not on red before the kill")
	}

	srv.RunLine("kill " + id)
	dead := false
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid != protocol.IDPlayEntityEvent {
			return
		}
		r := protocol.NewReader(data)
		r.ReadI32()
		dead = dead || r.ReadU8() == protocol.EntityStatusDeath
	})
	if !dead {
		t.Fatal("the zombie's death was not sent")
	}
	servertest.RunTicks(t, srv, 19)
	if !holds() {
		t.Fatal("the zombie lost its score or team in its death animation, want them kept until its body goes")
	}

	if !c.ReadTicks(srv, 1, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayResetScore {
			return false
		}
		return protocol.NewReader(data).ReadString(32767) == id
	}) {
		t.Fatal("no reset_score for the zombie on the tick its body went")
	}
	if _, ok := srv.ScoreOf(id, "hp"); ok {
		t.Error("the removed zombie keeps its score")
	}
	if team := srv.RunLine("team list red"); len(team) != 1 || team[0].Translate != "commands.team.list.members.empty" {
		t.Errorf("red lists %+v, want no members", team)
	}
	c.Disconnect()
}

// TestAKilledNonMobLosesItsScoresAndTeam checks that a killed body of every
// kind summonable here but a mob forgets their scores and team, as a mob does when
// its body goes.
// vanilla: ServerLevel.EntityCallbacks.onDestroyed, Scoreboard.entityRemoved
func TestAKilledNonMobLosesItsScoresAndTeam(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	kinds := []string{"armor_stand", "marker", "oak_boat", "minecart", "end_crystal", "item_frame", "painting",
		"item", "experience_orb", "arrow", "snowball", "tnt", "falling_block", "area_effect_cloud",
		"firework_rocket"}
	// extra is the tag a kind needs to summon.
	extra := map[string]string{"item": `Item:{id:"stone",count:1},`, "falling_block": `BlockState:{id:"sand"},`}
	ids := make([]string, len(kinds))
	srv.RunLine("team add red")
	srv.RunLine("scoreboard objectives add hp dummy")
	for i, kind := range kinds {
		ids[i] = fmt.Sprintf("00000001-0000-0002-0000-0003%08x", i+1)
		srv.RunLine(fmt.Sprintf("execute at Testificate run summon %s ~ ~ ~ {%sUUID:[I;1,2,3,%d],Team:\"red\"}", kind, extra[kind], i+1))
		srv.RunLine("scoreboard players set " + ids[i] + " hp 5")
		if v, ok := srv.ScoreOf(ids[i], "hp"); !ok || v != 5 {
			t.Fatalf("the %s has no score of 5 before the kill", kind)
		}
	}
	for _, id := range ids {
		srv.RunLine("kill " + id)
	}
	servertest.UntilTicks(t, srv, 5, "every killed body loses its score", func() bool {
		for _, id := range ids {
			if _, ok := srv.ScoreOf(id, "hp"); ok {
				return false
			}
		}
		return true
	})
	if team := srv.RunLine("team list red"); len(team) != 1 || team[0].Translate != "commands.team.list.members.empty" {
		t.Errorf("red lists %+v, want no members", team)
	}
	c.Disconnect()
}

// TestAPrimedTntLosesItsScoresWhenItsFuseRunsOut checks the tick's own
// removal of a body, not only /kill's.
// vanilla: ServerLevel.EntityCallbacks.onDestroyed
func TestAPrimedTntLosesItsScoresWhenItsFuseRunsOut(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	id := "00000001-0000-0002-0000-000300000004"
	srv.RunLine("scoreboard objectives add hp dummy")
	srv.RunLine("execute at Testificate run summon tnt ~ 200 ~ {fuse:3,UUID:[I;1,2,3,4]}")
	srv.RunLine("scoreboard players set " + id + " hp 5")
	servertest.UntilTicks(t, srv, 10, "the fuse's body forgets its score", func() bool {
		_, ok := srv.ScoreOf(id, "hp")
		return !ok
	})
	c.Disconnect()
}

// TestAnItemOrOrbDestroyedByTheWorldLosesItsScores checks the removals a
// hurt makes: an item burned in lava, and an item and an orb in a blast.
// vanilla: ItemEntity.hurtServer, ExperienceOrb.hurtServer
func TestAnItemOrOrbDestroyedByTheWorldLosesItsScores(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	srv.RunLine("scoreboard objectives add hp dummy")
	srv.RunLine("execute at Testificate run setblock ~ 200 ~ lava")
	summons := []string{
		`summon item ~ 200 ~ {NoGravity:1b,Item:{id:"stone",count:1},UUID:[I;1,2,6,1]}`,
		`summon item ~8 200 ~ {NoGravity:1b,Item:{id:"stone",count:1},UUID:[I;1,2,6,2]}`,
		`summon experience_orb ~8 200 ~ {NoGravity:1b,UUID:[I;1,2,6,3]}`,
	}
	ids := make([]string, len(summons))
	for i, summon := range summons {
		ids[i] = fmt.Sprintf("00000001-0000-0002-0000-0006%08x", i+1)
		srv.RunLine("execute at Testificate run " + summon)
		srv.RunLine("scoreboard players set " + ids[i] + " hp 5")
	}
	srv.RunLine("execute at Testificate run summon tnt ~8 200 ~ {NoGravity:1b,fuse:1}")
	servertest.UntilTicks(t, srv, 40, "every destroyed body loses its score", func() bool {
		for _, id := range ids {
			if _, ok := srv.ScoreOf(id, "hp"); ok {
				return false
			}
		}
		return true
	})
	c.Disconnect()
}

// TestABodyCarriedAcrossDimensionsKeepsItsScores checks that a boat and a
// minecart teleported into another dimension keep their scores: the husk
// left behind hands its UUID to the twin and is not destroyed.
// vanilla: Entity.RemovalReason.CHANGED_DIMENSION, PersistentEntitySectionManager.Callback.onRemove
func TestABodyCarriedAcrossDimensionsKeepsItsScores(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	srv.RunLine("scoreboard objectives add hp dummy")
	kinds := []string{"oak_boat", "minecart"}
	ids := make([]string, len(kinds))
	for i, kind := range kinds {
		ids[i] = fmt.Sprintf("00000001-0000-0002-0000-0004%08x", i+1)
		srv.RunLine(fmt.Sprintf("execute at Testificate run summon %s ~ ~ ~ {UUID:[I;1,2,4,%d]}", kind, i+1))
		srv.RunLine("scoreboard players set " + ids[i] + " hp 5")
		srv.RunLine("execute in minecraft:the_nether run tp " + ids[i] + " 8.5 100 8.5")
	}
	servertest.RunTicks(t, srv, 3)
	for i, id := range ids {
		if v, ok := srv.ScoreOf(id, "hp"); !ok || v != 5 {
			t.Errorf("the %s's score after crossing = %d, %v; want 5", kinds[i], v, ok)
		}
	}
	c.Disconnect()
}

// TestABodyResummonedAfterAKillKeepsItsNewScores checks that /kill's reset
// lands at the kill, so a body summoned with the same UUID right after keeps
// the score set on it.
// vanilla: PersistentEntitySectionManager.Callback.onRemove, ServerLevel.EntityCallbacks.onDestroyed
func TestABodyResummonedAfterAKillKeepsItsNewScores(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	srv.RunLine("scoreboard objectives add hp dummy")
	kinds := []string{"marker", "oak_boat", "minecart"}
	ids := make([]string, len(kinds))
	for i, kind := range kinds {
		ids[i] = fmt.Sprintf("00000001-0000-0002-0000-0005%08x", i+1)
		summon := fmt.Sprintf("execute at Testificate run summon %s ~ ~ ~ {UUID:[I;1,2,5,%d]}", kind, i+1)
		srv.RunLine(summon)
		srv.RunLine("kill " + ids[i])
		srv.RunLine(summon)
		srv.RunLine("scoreboard players set " + ids[i] + " hp 5")
	}
	servertest.RunTicks(t, srv, 2)
	for i, id := range ids {
		if v, ok := srv.ScoreOf(id, "hp"); !ok || v != 5 {
			t.Errorf("the re-summoned %s's score = %d, %v; want 5", kinds[i], v, ok)
		}
	}
	c.Disconnect()
}

// awaitFeedbackWith reads past the team packets a change sends to the next
// System Chat message, failing unless it is key, and answers its arguments.
func awaitFeedbackWith(t *testing.T, c *servertest.Client, key string) []protocol.Text {
	t.Helper()
	for id, data := range c.Packets(10 * time.Second) {
		if id != protocol.IDPlaySystemChat {
			continue
		}
		got, ok := protocol.ReadText(protocol.NewReader(data))
		if !ok || got.Translate != key {
			t.Fatalf("feedback %+v, want %q", got, key)
		}
		return got.With
	}
	t.Fatalf("no %q feedback", key)
	return nil
}

// TestSummonedRiderJoinsItsTagTeam checks that a rider nested in Passengers
// reads its own Team string.
// vanilla: EntityType.loadEntityRecursive, Entity.load
func TestSummonedRiderJoinsItsTagTeam(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("team add red")
	awaitFeedbackWith(t, c, "commands.team.add.success")
	id := uuid.MustParse("00000001-0000-0002-0000-000300000004")
	c.ChatCommand(`summon pig ~ ~ ~ {NoAI:1b,Passengers:[{id:"minecraft:zombie",NoAI:1b,UUID:[I;1,2,3,4],Team:"red"}]}`)
	awaitFeedbackWith(t, c, "commands.summon.success")
	c.ChatCommand("team list red")
	with := awaitFeedbackWith(t, c, "commands.team.list.members.success")
	if len(with) != 3 || number(with[1]) != 1 || with[2].Text != id.String() {
		t.Errorf("red's members %+v, want the rider's UUID alone", with)
	}
	c.Disconnect()
}
