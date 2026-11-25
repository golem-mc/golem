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
				if _, err := DecodeLootTable(nil, []byte(text)); err != nil {
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
