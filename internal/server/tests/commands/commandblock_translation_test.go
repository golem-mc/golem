package commands

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestCommandBlockNameMixedTranslationIndexes(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)

	for i, test := range []struct {
		template, want string
	}{
		{"%2$s %s", "second first"},
		{"%2$s %2$s %s %s", "second second first second"},
		{"%s %1$s %s", "first first second"},
	} {
		servertest.Setblock(t, c, int32(2+i), 64, 0, fmt.Sprintf(
			`command_block{CustomName:{translate:%q,with:["first","second"]},Command:"say mixed indexes",auto:1b}`,
			test.template))
		if got := c.ExpectWithinTicks(srv, 2, protocol.IDPlayDisguisedChat); !bytes.Equal(got, servertest.SayPayload(protocol.Text{Text: test.want}, "mixed indexes")) {
			t.Fatalf("broadcast for %q = %x, want sender %q", test.template, got, test.want)
		}
	}
}
