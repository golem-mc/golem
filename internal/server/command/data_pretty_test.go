package command

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestPrettyNBTTextFoldsLongArrays checks an array over 128 elements shows
// 128 and a gray <...>.
// vanilla: TextComponentTagVisitor.visitIntArray
func TestPrettyNBTTextFoldsLongArrays(t *testing.T) {
	t.Parallel()
	text := PrettyNBTText(make(nbt.IntArray, 200), true)
	if got := text.Plain(); !strings.HasSuffix(got, ",<...>]") {
		t.Fatalf("text ends %q, want ,<...>]", got[len(got)-10:])
	}
	folded := text.Extra[len(text.Extra)-2]
	if folded.Text != "<...>" || folded.Color != protocol.ColorGray {
		t.Fatalf("fold = %+v, want gray <...>", folded)
	}
}

// TestPrettyNBTTextFoldsDeepNesting checks a compound nested 65 deep folds
// its innermost level to {<...>}.
// vanilla: TextComponentTagVisitor.visitCompound
func TestPrettyNBTTextFoldsDeepNesting(t *testing.T) {
	t.Parallel()
	var tag nbt.Tag = nbt.Compound{"x": nbt.Byte(1)}
	for range 64 {
		tag = nbt.Compound{"a": tag}
	}
	if got := PrettyNBTText(tag, false).Plain(); !strings.Contains(got, "{<...>}") || strings.Contains(got, "1b") {
		t.Fatalf("text = %q, want the innermost compound folded to {<...>}", got)
	}
}
