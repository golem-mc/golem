package datapack

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestProviderListRefusesLiteralEmptyKeyCompound(t *testing.T) {
	d := lootDecoder{ResourceDecoder: liveResourceDecoder()}
	decode := func(raw string) string {
		_, msg := d.decodeProviderJSON([]byte(raw), false)
		return msg
	}
	for _, tc := range []struct{ input, want string }{
		{`{"type":"minecraft:add","inputs":[{"":7}]}`, `No key type in MapLike[{"":7}]`},
		{`{"type":"minecraft:add","inputs":[{"":7},{"type":"minecraft:constant","value":1}]}`, `No key type in MapLike[{"":7}]`},
		{`{"type":"minecraft:number_dispatcher","cases":[{"":7},{"condition":{"type":"minecraft:random_chance","chance":1},"value":1}]}`,
			`No key value in MapLike[{"":7}]; No key condition in MapLike[{"":7}]`},
	} {
		if msg := decode(tc.input); !strings.Contains(msg, tc.want) {
			t.Errorf("%s: error = %q, want %q", tc.input, msg, tc.want)
		}
	}
	if msg := decode(`{"type":"minecraft:add","inputs":[7,{"type":"minecraft:constant","value":1}]}`); msg != "" {
		t.Errorf("mixed list refused: %s", msg)
	}
}

func TestValidatePackContextProvidersRejectsInvalidFiles(t *testing.T) {
	// JsonOps includes the bad value in the number error.
	// vanilla: JsonOps.getNumberValue, Weighted.codec
	const badWeight = `{"type":"minecraft:weighted_list","distribution":[{"data":1,"weight":"abc"},{"data":2,"weight":1}]}`
	for _, tc := range []struct {
		input string
		float bool
		want  string
	}{
		{"1 2", false, "context_int_provider p:bad: trailing data after the JSON value"},
		{badWeight, false, `context_int_provider p:bad: Not a number: "abc"`},
		{badWeight, true, `context_float_provider p:bad: Not a number: "abc"`},
	} {
		var ints, floats map[string][]byte
		files := map[string][]byte{"p:bad": []byte(tc.input)}
		if tc.float {
			floats = files
		} else {
			ints = files
		}
		layers := registry.StagePackLayers()
		layers.SetContextProviders(ints, floats)
		if err := ValidatePackContextProviders(layers, ints, floats); err == nil || err.Error() != tc.want {
			t.Errorf("validation returned %v, want %q", err, tc.want)
		}
	}
}
