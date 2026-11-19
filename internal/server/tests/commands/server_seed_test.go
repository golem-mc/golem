package commands_test

import (
	"encoding/hex"
	"reflect"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanillaSeedFeedback is the System Chat payload (text component, then the
// overlay flag) a vanilla server sent for /seed with seed 5000000000,
// captured with tools/probe (COMMAND=seed) against the reference server.
const vanillaSeedFeedback = "0a090004776974680a00000001090004776974680a000000010a000b636c69636b5f6576656e74080006616374696f6e0011636f70795f746f5f636c6970626f61726408000576616c7565000a3530303030303030303000080005636f6c6f720005677265656e080009696e73657274696f6e000a3530303030303030303008000474657874000a353030303030303030300a000b686f7665725f6576656e74080006616374696f6e000973686f775f746578740a000576616c75650800097472616e736c617465000f636861742e636f70792e636c69636b0000000800097472616e736c6174650014636861742e7371756172655f627261636b657473000800097472616e736c6174650015636f6d6d616e64732e736565642e7375636365737300" + "00"

// TestSeed runs the /seed checks against one flat world of seed 5000000000,
// which does not fit 32 bits. The subtests share the player and run in turn.
// vanilla: SeedCommand
func TestSeed(t *testing.T) {
	t.Parallel()
	const seed = int64(5000000000) // (int)seed = 705032704
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelSeed = strconv.FormatInt(seed, 10)
	_, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	// The /seed message, as decoded, is the one a vanilla server sent.
	c.Run(t, "FeedbackMatchesVanilla", func(t *testing.T) {
		c.ChatCommand("seed")
		want, err := hex.DecodeString(vanillaSeedFeedback)
		if err != nil {
			t.Fatal(err)
		}
		got := c.ExpectPlay(protocol.IDPlaySystemChat)
		// Vanilla writes a compound's keys in another order than we do; the
		// client reads them by name, so the decoded components are compared.
		gotText, ok1 := protocol.ReadText(protocol.NewReader(got))
		wantText, ok2 := protocol.ReadText(protocol.NewReader(want))
		if !ok1 || !ok2 || !reflect.DeepEqual(gotText, wantText) {
			t.Errorf("/seed feedback = %+v, want vanilla %+v", gotText, wantText)
		}
		if got[len(got)-1] != want[len(want)-1] {
			t.Errorf("/seed overlay flag = %d, want %d", got[len(got)-1], want[len(want)-1])
		}
	})

	// /execute store result takes (int)seed.
	c.Run(t, "ResultIsTheSeedTruncatedToInt", func(t *testing.T) {
		c.ChatCommand("execute store result storage t:s r int 1 run seed")
		if got := c.ExpectSeed(); got != seed {
			t.Fatalf("/seed named %d, want %d", got, seed)
		}
		c.ChatCommand("data get storage t:s r")
		c.ExpectCommandFeedback("commands.data.storage.query", "t:s", "705032704")
	})
}
