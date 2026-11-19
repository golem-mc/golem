package commandsyntax

import (
	"reflect"
	"slices"
	"testing"
)

func TestParseSelectorData(t *testing.T) {
	var lookedUp []string
	lookup := func(id string) bool { lookedUp = append(lookedUp, id); return id == "minecraft:cow" }
	text := `@e[name=!Bob,level=1..5,gamemode=!creative,type=#unknown,type=!cow,tag=kept,nbt={Health:2f},advancements={t:a={c=true}},scores={o=2..},team=!red,predicate=!t:p,x=1,y_rotation=170..-170] tail`
	sel, state, n, err := ParseSelector(text, 7, true, lookup)
	if err != nil {
		t.Fatal(err)
	}
	if text[n:] != " tail" || state.Position != n || state.Stage != SuggestNothing {
		t.Fatalf("consumption=%d, state=%+v", n, state)
	}
	var kinds []FilterKind
	for _, f := range sel.Filters {
		kinds = append(kinds, f.Kind)
	}
	want := []FilterKind{FilterAlive, FilterName, FilterLevel, FilterGamemode, FilterTypeTag, FilterType, FilterTag, FilterNBT, FilterAdvancements}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("filter order=%v, want %v", kinds, want)
	}
	if !reflect.DeepEqual(lookedUp, []string{"minecraft:cow"}) {
		t.Fatalf("lookup=%v", lookedUp)
	}
	if sel.ExactType != "" || !sel.WorldLimited || sel.X == nil || *sel.X != 1 || sel.RotY.Min != 170 || sel.RotY.Max != -170 || !sel.Scores["o"].HasMin || sel.Scores["o"].HasMax {
		t.Fatalf("selector=%+v", sel)
	}
	if !sel.Teams[0].Invert || sel.Teams[0].Name != "red" || !sel.LootPredicates[0].Invert || sel.LootPredicates[0].Name != "t:p" || !sel.Filters[8].Advancements["t:a"].Criteria["c"] {
		t.Fatal("captured option values changed")
	}
}

func TestParseSelectorCompletionState(t *testing.T) {
	lookup := func(string) bool { return true }
	for _, tc := range []struct {
		text                    string
		stage                   SuggestStage
		optionStart, valueStart int
	}{
		{"", SuggestNameOrSelector, 0, 0}, {"@", SuggestSelector, 0, 0}, {"@e", SuggestOpenOptions, 0, 0},
		{"@e[", SuggestOptionsKey, 3, 0}, {"@e[li", SuggestOptionsKey, 3, 0},
		{"@e[sort=", SuggestSortValues, 3, 8}, {"@e[gamemode=!", SuggestGamemodeValues, 3, 12},
		{"@e[type=!", SuggestTypeValues, 3, 8},
	} {
		_, state, _, _ := ParseSelector(tc.text, 0, true, lookup)
		if state.Stage != tc.stage || state.OptionStart != tc.optionStart || state.ValueStart != tc.valueStart {
			t.Errorf("%q: %+v", tc.text, state)
		}
	}
	_, state, _, _ := ParseSelector("@e[type=!#t:a,gamemode=!creative,limit=2,", 0, true, lookup)
	if state.TypeState != InvertMultiple || state.GamemodeState != InvertMultiple || !state.SeenTypeTags["t:a"] || slices.Contains(state.ApplicableOptions, "limit") || !slices.Contains(state.ApplicableOptions, "type") {
		t.Fatalf("state=%+v", state)
	}
	_, state, _, _ = ParseSelector("@e[type=cow,gamemode=creative,", 0, true, lookup)
	if state.TypeState != InvertSingle || state.GamemodeState != InvertSingle || slices.Contains(state.ApplicableOptions, "type") || slices.Contains(state.ApplicableOptions, "gamemode") {
		t.Fatalf("state=%+v", state)
	}
}

func TestParseSelectorErrors(t *testing.T) {
	for _, tc := range []struct {
		text, key string
		cursor    int
		with      []string
	}{
		{"@", keySelectorMissing, 5, nil}, {"@z", keySelectorUnknown, 5, []string{"@z"}},
		{"@e[type=missing]", keyOptionsTypeInvalid, 12, []string{"minecraft:missing"}},
		{"@e[distance=5..1]", keyRangeSwapped, 16, nil},
		{"@e[limit=0]", keyOptionsLimitTooSmall, 13, nil},
		{"@e[nbt=42]", summonBadNBTKey, 13, nil},
	} {
		_, _, _, err := ParseSelector(tc.text, 4, true, func(string) bool { return false })
		if err == nil || err.Key != tc.key || err.Cursor != tc.cursor || !reflect.DeepEqual(err.With, tc.with) {
			t.Errorf("%q: %+v, want %s at %d with %v", tc.text, err, tc.key, tc.cursor, tc.with)
		}
	}
	_, _, _, err := ParseSelector("@s", 12, false, func(string) bool { return true })
	if err == nil || err.Key != keySelectorNotAllowed || err.Cursor != 12 {
		t.Fatalf("permission error=%+v", err)
	}
}
