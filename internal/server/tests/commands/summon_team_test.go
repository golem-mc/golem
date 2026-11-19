package commands

import (
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
