package network

import (
	"bytes"
	"errors"
	"github.com/golem-mc/golem/internal/server/servertest"
	"reflect"
	"testing"

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
// the url, hash, id and prompt, comes before Finish Configuration, and that
// the phase waits for the answer.
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

	// Still downloading: Finish Configuration keeps waiting.
	sendPackResponse(c, id, resourcepack.Accepted)
	sendPackResponse(c, id, resourcepack.Downloaded)
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout/4, protocol.IDConfigFinish); ok {
		t.Fatal("Finish Configuration arrived before the pack was resolved")
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

// TestRequiredResourcePackDeclinedInPlay checks that the play phase handles
// the response too, and kicks a required decline.
func TestRequiredResourcePackDeclinedInPlay(t *testing.T) {
	t.Parallel()
	cfg := packConfig()
	cfg.RequireResourcePack = true
	_, c := servertest.Dial(t, cfg)
	c.Login("Testificate")
	id := protocol.NewReader(configureUntilPack(c)).ReadUUID()
	sendPackResponse(c, id, resourcepack.SuccessfullyLoaded)
	c.Expect(protocol.IDConfigFinish)
	c.Send(protocol.IDConfigFinishAck, nil)
	c.Playing = true
	c.EnterWorld("Testificate")

	sendPackResponse(c, id, resourcepack.Declined)
	servertest.ExpectDisconnect(t, c, protocol.Text{Translate: "multiplayer.requiredTexturePrompt.disconnect"})
}

// TestNoResourcePackWithoutTheURL checks that with resource-pack unset the
// phase is the plain one.
func TestNoResourcePackWithoutTheURL(t *testing.T) {
	t.Parallel()
	cfg := packConfig()
	cfg.ResourcePack = ""
	_, c := servertest.Dial(t, cfg)
	c.Login("Testificate")
	c.Configure()
}

// TestResourcePackIDDefaultsToTheURLsNameUUID checks the id a pack without
// resource-pack-id gets: Java's UUID.nameUUIDFromBytes of the URL.
func TestResourcePackIDDefaultsToTheURLsNameUUID(t *testing.T) {
	t.Parallel()
	cfg := packConfig()
	cfg.ResourcePackID = ""
	_, c := servertest.Dial(t, cfg)
	c.Login("Testificate")

	got := protocol.NewReader(configureUntilPack(c)).ReadUUID()
	if want := uuid.MustParse("44a09017-ad23-3c41-983a-70248eaafdb5"); got != want {
		t.Errorf("id = %s, want %s", got, want)
	}
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

// TestResourcePackPromptWithAnInvalidColorIsNotSent checks that a prompt
// whose colour the client's codec cannot read is dropped, so the client
// still decodes Add Resource Pack.
func TestResourcePackPromptWithAnInvalidColorIsNotSent(t *testing.T) {
	t.Parallel()
	cfg := packConfig()
	cfg.ResourcePackPrompt = `{"text":"x","color":"notacolor"}`
	_, c := servertest.Dial(t, cfg)
	c.Login("Testificate")

	if tag := readPackPrompt(t, configureUntilPack(c)); tag != nil {
		t.Errorf("prompt = %#v, want none", tag)
	}
}

// TestResourcePackPromptKeepsItsStyle checks that the prompt is relayed with
// the styles and content types the Text model has no field for.
func TestResourcePackPromptKeepsItsStyle(t *testing.T) {
	t.Parallel()
	cfg := packConfig()
	cfg.ResourcePackPrompt = `{"text":"Take it","bold":true,"strikethrough":false,"color":"#FFAA00",` +
		`"extra":[{"keybind":"key.jump","font":"minecraft:alt"}]}`
	_, c := servertest.Dial(t, cfg)
	c.Login("Testificate")

	want := nbt.Compound{
		"text": nbt.String("Take it"), "bold": nbt.Byte(1), "strikethrough": nbt.Byte(0), "color": nbt.String("#FFAA00"),
		"extra": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"keybind": nbt.String("key.jump"), "font": nbt.String("minecraft:alt")},
		}},
	}
	if got := readPackPrompt(t, configureUntilPack(c)); !reflect.DeepEqual(got, want) {
		t.Errorf("prompt = %#v, want %#v", got, want)
	}
}

// TestFinishBeforeThePackAnswerIsRefused checks that a client acknowledging
// Finish Configuration while the pack answer is still due never joins.
func TestFinishBeforeThePackAnswerIsRefused(t *testing.T) {
	t.Parallel()
	cfg := packConfig()
	cfg.RequireResourcePack = true
	_, c := servertest.Dial(t, cfg)
	c.Login("Testificate")

	configureUntilPack(c)
	c.Send(protocol.IDConfigFinishAck, nil)
	for {
		id, _, err := c.Read()
		if errors.Is(err, servertest.ErrWaitOver) {
			t.Fatal("the connection stayed open")
		}
		if err != nil {
			return
		}
		if id == protocol.IDPlayLogin {
			t.Fatal("the player joined without answering the pack")
		}
	}
}

// TestOverlongPackHashFailsConfiguration checks that a resource-pack-sha1
// longer than the packet takes kicks with the configuration error instead
// of sending the pack.
func TestOverlongPackHashFailsConfiguration(t *testing.T) {
	t.Parallel()
	cfg := packConfig()
	cfg.ResourcePackSHA1 = testPackSHA1 + "0"
	_, c := servertest.Dial(t, cfg)
	c.Login("Testificate")
	c.Expect(protocol.IDConfigPluginMessage)
	c.Expect(protocol.IDKnownPacks)
	c.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutString("minecraft")
		b.PutString("core")
		b.PutString(registry.Version)
	})

	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: "multiplayer.disconnect.configuration_error"})
	for {
		id, data, err := c.Read()
		if err != nil {
			t.Fatalf("no disconnect: %v", err)
		}
		switch id {
		case protocol.IDConfigResourcePackPush, protocol.IDConfigFinish:
			t.Fatalf("packet 0x%02X sent, want the configuration error", id)
		case protocol.IDConfigDisconnect:
			if !bytes.Equal(data, want.Bytes()) {
				t.Errorf("disconnect = % X, want % X", data, want.Bytes())
			}
			return
		}
	}
}
