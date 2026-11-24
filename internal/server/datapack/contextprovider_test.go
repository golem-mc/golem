package datapack

import (
	"strings"
	"testing"
)

func TestProviderListRefusesLiteralEmptyKeyCompound(t *testing.T) {
	decode := func(raw string) string {
		tag, err := JSONTag([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		_, msg := DecodeIntProvider(tag)
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

func TestValidatePackContextProvidersRejectsTrailingJSON(t *testing.T) {
	err := ValidatePackContextProviders(map[string][]byte{"p:extra": []byte("1 2")}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "context_int_provider p:extra: trailing data") {
		t.Fatalf("provider with trailing JSON returned %v", err)
	}
}
