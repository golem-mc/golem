package datapack

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: LootTable.DIRECT_CODEC, LootItemFunctions.TYPED_CODEC
func TestInlineLootTableFunctionDiscriminators(t *testing.T) {
	const leaf = `{"KEY":"set_count","count":2}`
	for name, body := range map[string]string{
		"table":           `{"modifier":LEAF}`,
		"pool":            `{"pools":[{"rolls":1,"modifier":LEAF}]}`,
		"entry":           `{"pools":[{"rolls":1,"entries":[{"type":"item","name":"stone","modifier":LEAF}]}]}`,
		"child":           `{"pools":[{"rolls":1,"entries":[{"type":"group","children":[{"type":"item","name":"stone","modifier":LEAF}]}]}]}`,
		"list":            `{"modifier":[LEAF]}`,
		"sequence":        `{"modifier":{"type":"sequence","functions":[LEAF]}}`,
		"filtered pass":   `{"modifier":{"type":"filtered","item_filter":{},"on_pass":LEAF}}`,
		"filtered fail":   `{"modifier":{"type":"filtered","item_filter":{},"on_fail":LEAF}}`,
		"modify contents": `{"modifier":{"type":"modify_contents","component":"container","modifier":LEAF}}`,
		"set contents":    `{"modifier":{"type":"set_contents","component":"container","entries":[{"type":"item","name":"stone","modifier":LEAF}]}}`,
	} {
		t.Run(name, func(t *testing.T) {
			for _, key := range []string{"type", "function"} {
				text := strings.ReplaceAll(strings.ReplaceAll(body, "LEAF", leaf), "KEY", key)
				_, err := DecodeLootTableValue(parseJSON([]byte(text)))
				if key == "type" {
					if err != nil {
						t.Errorf("current inline function: %v", err)
					}
				} else if err == nil || !strings.Contains(err.Error(), "No key type") {
					t.Errorf("legacy inline function: %v, want missing type", err)
				}
				if _, err := DecodeLootTable([]byte(text)); err != nil {
					t.Errorf("pack function keyed by %s: %v", key, err)
				}
			}
		})
	}
}

func TestInlineLootTableKeepsFunctionData(t *testing.T) {
	table, err := DecodeLootTableValue(parseJSON([]byte(`{"modifier":{"type":"set_custom_data","function":"discard","tag":"{function:\"set_count\"}"}}`)))
	if err != nil {
		t.Fatal(err)
	}
	if len(table.Functions) != 1 {
		t.Fatalf("functions = %v, want one custom-data modifier", table.Functions)
	}
	if got, ok := table.Functions[0].(registry.LootSetCustomData); !ok || len(got.Tag.Entries()) != 1 || got.Tag.Entries()["function"] != nbt.String("set_count") {
		t.Errorf("modifier = %#v, want unchanged custom data", table.Functions[0])
	}
}

// vanilla: LootTable.DIRECT_CODEC, LootPool.CODEC, UniformContainerBase.commonFields
func TestInlineLootTableIgnoresLegacyFunctions(t *testing.T) {
	for name, body := range map[string]string{
		"legacy list": `{"functions":[{"function":"set_count","count":2}]}`,
		"pool":        `{"pools":[{"rolls":1,"functions":[{"function":"set_count","count":2}]}]}`,
		"entry":       `{"pools":[{"rolls":1,"entries":[{"type":"item","name":"stone","functions":[{"function":"set_count","count":2}]}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			functions := func(table registry.LootTable) []registry.LootFunction {
				switch name {
				case "pool":
					return table.Pools[0].Functions
				case "entry":
					return table.Pools[0].Entries[0].(registry.LootItemEntry).Functions
				default:
					return table.Functions
				}
			}
			table, err := DecodeLootTableValue(parseJSON([]byte(body)))
			if err != nil {
				t.Fatal(err)
			}
			if got := functions(table); len(got) != 0 {
				t.Errorf("inline functions = %v, want none", got)
			}
			table, err = DecodeLootTable([]byte(body))
			if err != nil {
				t.Fatal(err)
			}
			if got := functions(table); len(got) != 1 {
				t.Errorf("pack functions = %v, want one", got)
			}
		})
	}
}

// vanilla: LootItemFunctions.DIRECT_CODEC, HolderSetCodec.directCodec
func TestInlineLootTableModifierErrors(t *testing.T) {
	const missing = `No key type in MapLike[{count:2,function:"set_count"}]`
	const listError = `Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a list: {count:2,function:"set_count"}; Second: ` + missing
	const directError = `Failed to parse either. First: ` + missing + `; Second: ` + listError
	const sequenceError = `Failed to parse either. First: ` + listError +
		`; Second: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a list: {functions:{count:2,function:"set_count"},type:"sequence"}; Second: ` + listError
	const contentsError = `Failed to parse either. First: ` + directError +
		`; Second: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Not a list: {component:"container",modifier:{count:2,function:"set_count"},type:"modify_contents"}; Second: ` + directError
	for _, tc := range []struct{ modifier, want string }{
		{`{"function":"set_count","count":2}`, directError},
		{`{"type":"modify_contents","component":"container","modifier":{"function":"set_count","count":2}}`, contentsError},
		{`[{"function":"set_count","count":2}]`, missing},
		{`{"type":"sequence","functions":{"function":"set_count","count":2}}`, sequenceError},
		{`{"type":"sequence","functions":[{"function":"set_count","count":2}]}`, missing},
		{`{"type":"filtered","item_filter":{},"on_pass":{"function":"set_count","count":2}}`, directError},
	} {
		_, err := DecodeLootTableValue(parseJSON([]byte(`{"modifier":` + tc.modifier + `}`)))
		if err == nil || err.Error() != tc.want {
			t.Errorf("modifier %s: %v, want %s", tc.modifier, err, tc.want)
		}
	}
}
