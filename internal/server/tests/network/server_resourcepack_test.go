package network

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/resourcepack"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

const (
	testPackURL  = "https://example.org/pack.zip"
	testPackID   = "11111111-2222-3333-4444-555555555555"
	testPackSHA1 = "0123456789abcdef0123456789abcdef01234567"
)

func packConfig() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ResourcePack = testPackURL
	cfg.ResourcePackID = testPackID
	cfg.ResourcePackSHA1 = testPackSHA1
	cfg.ResourcePackPrompt = `{"text":"Please take the pack"}`
	return cfg
}

// configureUntilPack answers the known packs and reads the configuration
// packets up to Add Resource Pack, returning its body. Finish Configuration
// before it fails the test.
func configureUntilPack(c *servertest.Client) []byte {
	c.T.Helper()
	c.Expect(protocol.IDConfigPluginMessage)
	c.Expect(protocol.IDKnownPacks)
	c.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutString("minecraft")
		b.PutString("core")
		b.PutString(registry.Version)
	})
	for {
		id, data, err := c.Read()
		if err != nil {
			c.T.Fatalf("read configuration packet: %v", err)
		}
		switch id {
		case protocol.IDConfigResourcePackPush:
			return data
		case protocol.IDConfigFinish:
			c.T.Fatal("Finish Configuration came before Add Resource Pack")
		}
	}
}

func sendPackResponse(c *servertest.Client, id uuid.UUID, action resourcepack.Action) {
	c.T.Helper()
	pid := int32(protocol.IDConfigResourcePackIn)
	if c.Playing {
		pid = protocol.IDPlayResourcePackIn
	}
	c.Send(pid, func(b *protocol.Buffer) {
		b.PutUUID(id)
		b.PutVarInt(int32(action))
	})
}

// TestConfigurationOffersTheResourcePack checks that Add Resource Pack, with
// the url, hash, id and prompt, comes before Finish Configuration.
func TestConfigurationOffersTheResourcePack(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, packConfig())
	c.Login("Testificate")

	r := protocol.NewReader(configureUntilPack(c))
	id := r.ReadUUID()
	if want := uuid.MustParse(testPackID); id != want {
		t.Errorf("pack id = %s, want %s", id, want)
	}
	if got := r.ReadString(32767); got != testPackURL {
		t.Errorf("url = %q, want %q", got, testPackURL)
	}
	if got := r.ReadString(40); got != testPackSHA1 {
		t.Errorf("hash = %q, want %q", got, testPackSHA1)
	}
	if r.ReadBool() {
		t.Error("pack is forced, want not required")
	}
	if !r.ReadBool() {
		t.Fatal("no prompt sent")
	}
	prompt, ok := protocol.ReadText(r)
	if !ok || prompt.Text != "Please take the pack" {
		t.Errorf("prompt = %+v, want the configured text", prompt)
	}
	if r.Remaining() != 0 || r.Err() != nil {
		t.Errorf("leftover=%d err=%v", r.Remaining(), r.Err())
	}
}

// TestResourcePackLoadedLetsThePlayerIn checks that a "successfully loaded"
// response lets a required pack's player finish configuration and join.
func TestResourcePackLoadedLetsThePlayerIn(t *testing.T) {
	t.Parallel()
	cfg := packConfig()
	cfg.RequireResourcePack = true
	_, c := servertest.Dial(t, cfg)
	c.Login("Testificate")

	r := protocol.NewReader(configureUntilPack(c))
	id := r.ReadUUID()
	r.ReadString(32767)
	r.ReadString(40)
	if !r.ReadBool() {
		t.Error("pack is not forced, want required")
	}

	sendPackResponse(c, id, resourcepack.Accepted)
	sendPackResponse(c, id, resourcepack.SuccessfullyLoaded)
	c.Expect(protocol.IDConfigFinish)
	c.Send(protocol.IDConfigFinishAck, nil)
	c.Playing = true
	c.EnterWorld("Testificate")
}

// TestRequiredResourcePackDeclinedDisconnects checks the kick for declining a
// required pack in configuration.
func TestRequiredResourcePackDeclinedDisconnects(t *testing.T) {
	t.Parallel()
	cfg := packConfig()
	cfg.RequireResourcePack = true
	_, c := servertest.Dial(t, cfg)
	c.Login("Testificate")

	id := protocol.NewReader(configureUntilPack(c)).ReadUUID()
	sendPackResponse(c, id, resourcepack.Declined)

	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: "multiplayer.requiredTexturePrompt.disconnect"})
	if got := c.Expect(protocol.IDConfigDisconnect); !bytes.Equal(got, want.Bytes()) {
		t.Errorf("disconnect = % X, want % X", got, want.Bytes())
	}
}

func TestOptionalResourcePackDeclinedLetsThePlayerIn(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, packConfig())
	c.Login("Testificate")

	id := protocol.NewReader(configureUntilPack(c)).ReadUUID()
	sendPackResponse(c, id, resourcepack.Declined)
	c.Expect(protocol.IDConfigFinish)
	c.Send(protocol.IDConfigFinishAck, nil)
	c.Playing = true
	c.EnterWorld("Testificate")
}

// readPackPrompt reads Add Resource Pack up to its prompt and returns the
// prompt's tag, nil when none is sent.
func readPackPrompt(t *testing.T, data []byte) nbt.Tag {
	t.Helper()
	r := protocol.NewReader(data)
	r.ReadUUID()
	r.ReadString(32767)
	r.ReadString(40)
	r.ReadBool()
	if !r.ReadBool() {
		if r.Remaining() != 0 || r.Err() != nil {
			t.Errorf("leftover=%d err=%v", r.Remaining(), r.Err())
		}
		return nil
	}
	rest := r.Rest()
	tag, n, err := nbt.DecodeNetwork(rest)
	if err != nil || r.Err() != nil {
		t.Fatalf("prompt: %v %v", err, r.Err())
	}
	if n != len(rest) {
		t.Errorf("leftover=%d", len(rest)-n)
	}
	return tag
}
