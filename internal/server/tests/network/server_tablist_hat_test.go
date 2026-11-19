package network

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

const (
	hatTestActionHat = 0x80
	hatTestHatBit    = 0x40
)

// TestJoinTabEntryCarriesHat checks that the join Player Info Update names the
// UPDATE_HAT action and each entry's hat bit.
func TestJoinTabEntryCarriesHat(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")
	watcher.SetSkinParts(0x7F &^ hatTestHatBit)
	watcher.Settle()

	joiner := connect()
	joiner.Login("Joiner")
	joiner.Configure()

	info := protocol.NewReader(joiner.AwaitPlay(protocol.IDPlayPlayerInfoUpdate))
	actions := info.ReadU8()
	if actions&hatTestActionHat == 0 {
		t.Fatalf("join actions = %#02x, want the UPDATE_HAT bit 0x80", actions)
	}
	n := info.ReadVarInt()
	hats := map[string]bool{}
	for range n {
		info.ReadUUID()
		name := info.ReadString(16)
		info.SkipProfileProperties()
		if info.ReadBool() {
			t.Fatal("offline player unexpectedly has a chat session")
		}
		info.ReadVarInt() // game mode
		info.ReadBool()   // listed
		info.ReadVarInt() // latency
		hats[name] = info.ReadBool()
	}
	if err := info.Err(); err != nil {
		t.Fatalf("player info update: %v", err)
	}
	if hats["Joiner"] != true || hats["Watcher"] != false || len(hats) != 2 {
		t.Errorf("hats = %v, want Joiner true, Watcher false", hats)
	}
}

// TestHatToggleBroadcastsUpdateHat checks that switching the hat layer sends
// UPDATE_HAT to everyone, the sender included, and that changing another layer
// does not.
func TestHatToggleBroadcastsUpdateHat(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, connect := servertest.Start(t, cfg)

	alpha := connect().Join("Alpha")
	beta := connect().Join("Beta")
	alpha.ExpectPlayerAdded("Beta")

	expectHat := func(c *servertest.Client, shown bool) {
		t.Helper()
		info := protocol.NewReader(c.AwaitPlay(protocol.IDPlayPlayerInfoUpdate))
		if got := info.ReadU8(); got != hatTestActionHat {
			t.Fatalf("actions = %#02x, want 0x80", got)
		}
		if got := info.ReadVarInt(); got != 1 {
			t.Fatalf("entries = %d, want 1", got)
		}
		if got, want := info.ReadUUID(), protocol.OfflineUUID("Alpha"); got != want {
			t.Errorf("entry UUID = %s, want Alpha's", got)
		}
		if got := info.ReadBool(); got != shown {
			t.Errorf("hat shown = %v, want %v", got, shown)
		}
	}

	// Another layer changing is not a hat change.
	alpha.SetSkinParts(0x7F &^ 0x01)
	alpha.SetSkinParts(0x7F &^ 0x01 &^ hatTestHatBit)
	expectHat(alpha, false)
	expectHat(beta, false)

	alpha.SetSkinParts(0x7F)
	expectHat(alpha, true)
	expectHat(beta, true)
}
