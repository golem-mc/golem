package sounds

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"bytes"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// runPlaysound runs one /playsound line and returns the Sound Effect the
// runner was sent and the System Chat line it answered with. The packet goes
// through the player's mailbox and the feedback straight out, so the two are
// collected in whichever order they arrive.
func runPlaysound(c *servertest.Client, command string) (sound, chat []byte) {
	c.T.Helper()
	c.ChatCommand(command)
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySoundEffect:
			sound = data
		case protocol.IDPlaySystemChat:
			chat = data
		}
	})
	return sound, chat
}

// TestPlaySoundCommandPlaysTheSound runs the full /playsound form and checks
// the Sound Effect packet the target receives: the sound as the inline
// definition vanilla's PlaySoundCommand sends, the master category, the
// position, the volume and pitch, and the seed.
func TestPlaySoundCommandPlaysTheSound(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "DJ", servertest.OpsListing(t, "DJ"))
	c.Settle()

	pos := srv.PlayerPosition("DJ")

	sound, chat := runPlaysound(c, "playsound minecraft:entity.creeper.primed master @s ~ ~ ~ 1 0.5")
	got := servertest.DecodePlaySoundEffect(t, sound)
	if got.RegistryID != -1 {
		t.Errorf("sound travelled as registry ID %d, want the inline definition", got.RegistryID)
	}
	if got.Name != "minecraft:entity.creeper.primed" {
		t.Errorf("sound inline name = %q, want minecraft:entity.creeper.primed", got.Name)
	}
	if got.Category != protocol.SoundCategoryMaster {
		t.Errorf("category = %d, want %d (master)", got.Category, protocol.SoundCategoryMaster)
	}
	if got.X != int32(pos.X*8) || got.Y != int32(pos.Y*8) || got.Z != int32(pos.Z*8) {
		t.Errorf("position = (%d %d %d), want (%d %d %d)",
			got.X, got.Y, got.Z, int32(pos.X*8), int32(pos.Y*8), int32(pos.Z*8))
	}
	if got.Volume != 1 || got.Pitch != 0.5 {
		t.Errorf("volume, pitch = %v, %v, want 1, 0.5", got.Volume, got.Pitch)
	}
	if !got.HasSeed {
		t.Error("the sound packet carried no seed long")
	}
	if want := servertest.PlaySoundFeedback("commands.playsound.success.single",
		protocol.Text{Text: "minecraft:entity.creeper.primed"}, servertest.PlayerDisplayComponent("DJ")); !bytes.Equal(chat, want) {
		t.Errorf("feedback = % X, want % X", chat, want)
	}
}

// TestPlaySoundCommandDefaults runs /playsound with only the sound: master,
// the runner as the target, its own position, and volume and pitch one.
func TestPlaySoundCommandDefaults(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "DJ", servertest.OpsListing(t, "DJ"))
	c.Settle()

	pos := srv.PlayerPosition("DJ")
	sound, _ := runPlaysound(c, "playsound minecraft:block.note_block.pling")
	got := servertest.DecodePlaySoundEffect(t, sound)
	if got.Category != protocol.SoundCategoryMaster {
		t.Errorf("default category = %d, want %d (master)", got.Category, protocol.SoundCategoryMaster)
	}
	if got.X != int32(pos.X*8) || got.Y != int32(pos.Y*8) || got.Z != int32(pos.Z*8) {
		t.Errorf("default position = (%d %d %d), want the runner's (%d %d %d)",
			got.X, got.Y, got.Z, int32(pos.X*8), int32(pos.Y*8), int32(pos.Z*8))
	}
	if got.Volume != 1 || got.Pitch != 1 {
		t.Errorf("default volume, pitch = %v, %v, want 1, 1", got.Volume, got.Pitch)
	}
}

// TestPlaySoundCommandTooFarFails plays from out of range with no minimum
// volume: the target hears nothing and the runner is told the sound is too
// far away.
func TestPlaySoundCommandTooFarFails(t *testing.T) {
	t.Parallel()
	_, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "DJ", servertest.OpsListing(t, "DJ"))
	c.Settle()

	sound, chat := runPlaysound(c, "playsound minecraft:block.note_block.pling master @s ~100 ~ ~ 1 1 0")
	if sound != nil {
		t.Errorf("a sound played for a target out of range with no minimum volume: % X", sound)
	}
	if want := servertest.CommandErrorMessageForTest("commands.playsound.failed"); !bytes.Equal(chat, want) {
		t.Errorf("failure = % X, want % X", chat, want)
	}
}

// childCommand returns the named child of a decoded command node, failing if
// it has none.
func childCommand(t *testing.T, nodes []servertest.TestCommandNode, parent servertest.TestCommandNode, name string) servertest.TestCommandNode {
	t.Helper()
	for _, index := range parent.Children {
		if nodes[index].Name == name {
			return nodes[index]
		}
	}
	t.Fatalf("node %q has no child %q", parent.Name, name)
	return servertest.TestCommandNode{}
}

// TestPlaySoundCommandTree checks the /playsound tree: a sound argument with
// one literal per source category, each carrying the optional targets, pos,
// volume, pitch and minVolume behind it.
func TestPlaySoundCommandTree(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "DJ"))
	c.Login("DJ")
	c.Configure()

	nodes := servertest.DecodeCommands(t, c.AwaitPlay(protocol.IDPlayCommands))
	playsound := childCommand(t, nodes, nodes[0], "playsound")
	sound := childCommand(t, nodes, playsound, "sound")
	if sound.Typ != 2 {
		t.Fatalf("sound node type = %d, want 2 (argument)", sound.Typ)
	}
	var sources []string
	for _, index := range sound.Children {
		if nodes[index].Typ == 1 {
			sources = append(sources, nodes[index].Name)
		}
	}
	want := []string{"master", "music", "record", "weather", "block", "hostile", "neutral", "player", "ambient", "voice", "ui"}
	if !slices.Equal(sources, want) {
		t.Errorf("source literals = %v, want %v", sources, want)
	}

	// The tail hangs off each source literal, deep enough to reach
	// minVolume.
	master := childCommand(t, nodes, sound, "master")
	targets := childCommand(t, nodes, master, "targets")
	pos := childCommand(t, nodes, targets, "pos")
	volume := childCommand(t, nodes, pos, "volume")
	pitch := childCommand(t, nodes, volume, "pitch")
	minVolume := childCommand(t, nodes, pitch, "minVolume")
	for _, node := range []servertest.TestCommandNode{targets, pos, volume, pitch, minVolume} {
		if node.Typ != 2 {
			t.Errorf("node %q type = %d, want 2 (argument)", node.Name, node.Typ)
		}
	}

	c.SkipJoin()
	c.Disconnect()
}
