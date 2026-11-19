package registry

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// vanilla: CommonButtonData.MAP_CODEC, OptionalFieldCodec.decode
func TestDialogActionErrorsKeepOptionalFieldPartials(t *testing.T) {
	for _, button := range []nbt.Compound{
		{"label": nbt.String("ok"), "width": nbt.Int(0)},
		{"label": nbt.String("ok"), "tooltip": nbt.Compound{}},
		{"label": nbt.String("ok"), "action": nbt.Compound{"type": nbt.String("run_command")}},
		{"label": nbt.Compound{"text": nbt.String("ok"), "extra": nbt.Int(5)}},
	} {
		dialog := nbt.Compound{
			"type":    nbt.String("multi_action"),
			"title":   nbt.String("x"),
			"actions": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{button}},
		}
		message := DialogMalformed(dialog)
		if message == "" || strings.Contains(message, "List must have contents") {
			t.Errorf("optional-field error discarded the partial button %s: %q", nbt.Stringify(button), message)
		}
	}
}
