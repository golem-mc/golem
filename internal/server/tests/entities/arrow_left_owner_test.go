package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func expectArrowLeftOwner(t *testing.T, c *servertest.Client, target string, want bool) {
	t.Helper()
	c.ChatCommand("data get entity " + target)
	text, ok := protocol.ReadText(protocol.NewReader(c.ExpectPlay(protocol.IDPlaySystemChat)))
	if !ok || text.Translate != "commands.data.entity.query" || len(text.With) != 2 {
		t.Fatalf("%s query feedback = %+v, want entity data", target, text)
	}
	tag, err := nbt.ParseSNBT(text.With[1].Plain())
	if err != nil {
		t.Fatal(err)
	}
	got, present := tag["LeftOwner"]
	if want && got != nbt.Byte(1) || !want && present {
		t.Fatalf("%s LeftOwner = %v, want true saved as 1b or false omitted (want %t)", target, got, want)
	}
	if got := tag["HasBeenShot"]; got != nbt.Byte(0) {
		t.Fatalf("%s HasBeenShot = %v, want 0b without projectile physics", target, got)
	}
}
