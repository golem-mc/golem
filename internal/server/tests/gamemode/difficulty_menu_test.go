package gamemode

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

func sendChangeDifficulty(c *servertest.Client, d int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayChangeDifficultyIn, func(b *protocol.Buffer) { b.PutVarInt(d) })
}

func sendLockDifficulty(c *servertest.Client, locked bool) {
	c.T.Helper()
	c.Send(protocol.IDPlayLockDifficulty, func(b *protocol.Buffer) { b.PutBool(locked) })
}

// expectDifficulty reads the next Change Difficulty and checks its fields.
func expectDifficulty(c *servertest.Client, wantD uint8, wantLocked bool) {
	c.T.Helper()
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayDifficulty))
	if d, locked := r.ReadU8(), r.ReadBool(); d != wantD || locked != wantLocked {
		c.T.Errorf("change difficulty = (%d, %v), want (%d, %v)", d, locked, wantD, wantLocked)
	}
}

func difficultyMenuWorld(t *testing.T) (*servertest.Client, *servertest.Client, func() uint8, func() bool) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate"))
	op := connect().Join("Testificate")
	other := connect().Join("Alex")
	op.Settle() // the op's view of Alex joining
	return op, other, srv.Difficulty, srv.DifficultyLocked
}

// TestMenuChangeDifficultyReachesEveryone checks an op's difficulty button:
// every client is sent the new difficulty with the lock flag. The id wraps
// like Difficulty.BY_ID.
// vanilla: ServerGamePacketListenerImpl.handleChangeDifficulty
func TestMenuChangeDifficultyReachesEveryone(t *testing.T) {
	t.Parallel()
	op, other, difficulty, _ := difficultyMenuWorld(t)
	sendChangeDifficulty(op, 3)
	expectDifficulty(op, 3, false)
	expectDifficulty(other, 3, false)
	if got := difficulty(); got != 3 {
		t.Errorf("difficulty = %d, want 3", got)
	}
	sendChangeDifficulty(op, 6)
	expectDifficulty(op, 2, false)
	expectDifficulty(other, 2, false)
}

// TestMenuChangeDifficultyRefusesNonOp checks a non-op's request changes
// nothing and tells nobody.
func TestMenuChangeDifficultyRefusesNonOp(t *testing.T) {
	t.Parallel()
	op, other, difficulty, _ := difficultyMenuWorld(t)
	sendChangeDifficulty(other, 3)
	sendLockDifficulty(other, true)
	other.Settle()
	if got := difficulty(); got != 1 {
		t.Errorf("difficulty = %d, want it left at 1", got)
	}
	sendChangeDifficulty(op, 2)
	expectDifficulty(op, 2, false) // the first Change Difficulty the op sees
}

// TestMenuLockDifficulty checks the lock button sets the flag for everyone
// and that a locked difficulty refuses the menu, but not /difficulty.
func TestMenuLockDifficulty(t *testing.T) {
	t.Parallel()
	op, other, difficulty, locked := difficultyMenuWorld(t)
	sendLockDifficulty(op, true)
	expectDifficulty(op, 1, true)
	expectDifficulty(other, 1, true)
	if !locked() {
		t.Fatal("difficulty is free after Lock Difficulty")
	}

	sendChangeDifficulty(op, 3)
	op.Settle()
	if got := difficulty(); got != 1 {
		t.Errorf("locked difficulty = %d, want it left at 1", got)
	}

	op.ChatCommand("difficulty hard")
	op.ExpectComponentFeedback("commands.difficulty.success",
		protocol.Text{Translate: "options.difficulty.hard"})
	expectDifficulty(op, 3, true)
	expectDifficulty(other, 3, true)

	sendLockDifficulty(op, false)
	expectDifficulty(op, 3, false)
	sendChangeDifficulty(op, 0)
	expectDifficulty(op, 0, false)
}
