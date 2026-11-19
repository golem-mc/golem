package network

import (
	"errors"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func answerKnownPacks(c *servertest.Client) {
	c.T.Helper()
	c.Expect(protocol.IDConfigPluginMessage)
	c.Expect(protocol.IDKnownPacks)
	c.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutString("minecraft")
		b.PutString("core")
		b.PutString(registry.Version)
	})
}

// readConduct reads configuration packets up to Code of Conduct and returns
// its text; Finish Configuration before it fails the test.
func readConduct(c *servertest.Client) string {
	c.T.Helper()
	for {
		id, data, err := c.Read()
		if err != nil {
			c.T.Fatalf("read configuration packet: %v", err)
		}
		switch id {
		case protocol.IDConfigCodeOfConduct:
			return protocol.NewReader(data).ReadString(32767)
		case protocol.IDConfigFinish:
			c.T.Fatal("Finish Configuration came before Code of Conduct")
		}
	}
}

func sendClientInfo(c *servertest.Client, locale string) {
	c.Send(protocol.IDClientInformation, func(b *protocol.Buffer) {
		b.PutString(locale)
		b.PutI8(8)
		b.PutVarInt(0)
		b.PutBool(true)
		b.PutU8(0x7f)
		b.PutVarInt(1)
		b.PutBool(false)
		b.PutBool(true)
		b.PutVarInt(0)
	})
}

// TestCodeOfConductHoldsFinishUntilAccepted checks that the text goes out,
// colour codes stripped, and Finish Configuration waits for Accept.
func TestCodeOfConductHoldsFinishUntilAccepted(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, servertest.ConductConfig(t, map[string]string{"en_us.txt": "Be §akind.\r\nBe fair.\n"}))
	c.Login("Testificate")
	answerKnownPacks(c)
	if got, want := readConduct(c), "Be kind.\nBe fair."; got != want {
		t.Errorf("code of conduct = %q, want %q", got, want)
	}
	c.Send(protocol.IDConfigAcceptCodeOfConduct, nil)
	c.Expect(protocol.IDConfigFinish)
	c.Send(protocol.IDConfigFinishAck, nil)
	c.Playing = true
	c.EnterWorld("Testificate")
}

// TestCodeOfConductPicksTheClientLanguage checks the language fallback chain.
func TestCodeOfConductPicksTheClientLanguage(t *testing.T) {
	t.Parallel()
	files := map[string]string{"en_us.txt": "english", "de_de.txt": "deutsch", "fr_fr.txt": "francais"}
	for locale, want := range map[string]string{"DE_de": "deutsch", "ja_jp": "english"} {
		_, c := servertest.Dial(t, servertest.ConductConfig(t, files))
		c.Login("Testificate")
		c.Expect(protocol.IDConfigPluginMessage)
		sendClientInfo(c, locale)
		c.Expect(protocol.IDKnownPacks)
		c.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) {
			b.PutVarInt(1)
			b.PutString("minecraft")
			b.PutString("core")
			b.PutString(registry.Version)
		})
		if got := readConduct(c); got != want {
			t.Errorf("locale %s: text = %q, want %q", locale, got, want)
		}
	}
}

// TestFinishBeforeCodeOfConductAcceptIsRefused checks that the player cannot
// join without accepting.
func TestFinishBeforeCodeOfConductAcceptIsRefused(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, servertest.ConductConfig(t, map[string]string{"en_us.txt": "rules"}))
	c.Login("Testificate")
	answerKnownPacks(c)
	readConduct(c)
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
			t.Fatal("the player joined without accepting")
		}
	}
}

// TestCodeOfConductPrecedesTheResourcePack checks that the pack is offered
// only after the accept.
func TestCodeOfConductPrecedesTheResourcePack(t *testing.T) {
	t.Parallel()
	cfg := servertest.ConductConfig(t, map[string]string{"en_us.txt": "rules"})
	pc := packConfig()
	cfg.ResourcePack, cfg.ResourcePackID, cfg.ResourcePackSHA1 = pc.ResourcePack, pc.ResourcePackID, pc.ResourcePackSHA1
	_, c := servertest.Dial(t, cfg)
	c.Login("Testificate")
	answerKnownPacks(c)
	readConduct(c)
	c.Send(protocol.IDConfigAcceptCodeOfConduct, nil)
	c.Expect(protocol.IDConfigResourcePackPush)
}

// TestUnsolicitedCodeOfConductAcceptDisconnects checks that an Accept without
// a Code of Conduct task is refused.
func TestUnsolicitedCodeOfConductAcceptDisconnects(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, packConfigNoPack())
	c.Login("Testificate")
	answerKnownPacks(c)
	c.Send(protocol.IDConfigAcceptCodeOfConduct, nil)
	for {
		id, _, err := c.Read()
		if errors.Is(err, servertest.ErrWaitOver) {
			t.Fatal("the connection stayed open")
		}
		if err != nil {
			return
		}
		if id == protocol.IDPlayLogin {
			t.Fatal("the player joined")
		}
	}
}

func packConfigNoPack() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	return cfg
}

// TestAConfigurationPacketAtTheTopOfTheRangeIsNotKicked sends Accept Code of
// Conduct, configuration id 0x09, the last of the serverbound range, while
// the server waits for it: the top of the range is not an unknown id and the
// configuration still finishes.
func TestAConfigurationPacketAtTheTopOfTheRangeIsNotKicked(t *testing.T) {
	t.Parallel()
	cfg := servertest.ConductConfig(t, map[string]string{"en_us.txt": "rules"})
	_, c := servertest.Dial(t, cfg)
	c.Login("Sender")
	answerKnownPacks(c)
	readConduct(c)
	c.Send(protocol.IDConfigAcceptCodeOfConduct, nil)
	if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDConfigFinish); !ok {
		t.Fatal("the top-of-range id cost its sender the connection")
	}
}
