package servertest

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// PlaySoundFeedback builds the System Chat payload a /playsound feedback
// carries.
func PlaySoundFeedback(key string, with ...protocol.Text) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{Translate: key, With: with})
	b.PutBool(false)
	return b.Bytes()
}

// BellState is the state of a bell with the attachment and facing named.
func BellState(t *testing.T, attachment, facing string, powered bool) uint16 {
	t.Helper()
	poweredStr := "false"
	if powered {
		poweredStr = "true"
	}
	return BlockStateWith(t, "minecraft:bell",
		"attachment="+attachment+",facing="+facing+",powered="+poweredStr)
}
