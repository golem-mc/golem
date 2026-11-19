package network

import (
	"errors"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

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
