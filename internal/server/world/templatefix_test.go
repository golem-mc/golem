package world

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func readTemplateFixFixtures(t *testing.T, file string) map[string]nbt.Compound {
	t.Helper()
	data, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]nbt.Compound{}
	scan := bufio.NewScanner(bytes.NewReader(data))
	for scan.Scan() {
		line := scan.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, text, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("bad fixture: %s", line)
		}
		tag, err := nbt.ParseSNBT(text)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = tag
	}
	if err := scan.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTemplateDataFixVanilla26_3(t *testing.T) {
	// Generated separately with REFERENCE_VERSION=26.3 so the next release's
	// schema and fixtures can be checked without activating its production data.
	data, err := os.ReadFile("testdata/templatefix_26.3_schemas.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Stages  map[string]string `json:"stages"`
		Schemas map[string]struct {
			Paths       map[string][][2]string `json:"paths"`
			UnpackItems bool                   `json:"unpack_items"`
		} `json:"schemas"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	schemas := map[int32]templateFixer{}
	var stages []int32
	for key, shared := range fixture.Stages {
		stage, err := strconv.ParseInt(key, 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		paths, ok := fixture.Schemas[shared]
		if !ok {
			t.Fatalf("stage %s refers to missing schema %s", key, shared)
		}
		schema := templateFixer{paths: map[string][]templateFixEdge{}, unpackItems: paths.UnpackItems}
		for kind, edges := range paths.Paths {
			for _, edge := range edges {
				schema.paths[kind] = append(schema.paths[kind], templateFixEdge{edge[0], edge[1]})
			}
		}
		schemas[int32(stage)] = schema
		stages = append(stages, int32(stage))
	}
	slices.Sort(stages)
	if len(stages) != 16 {
		t.Fatalf("got %d stages, want 16", len(stages))
	}
	input := readTemplateFixFixtures(t, "templatefix_input.snbt")
	expected := readTemplateFixFixtures(t, "templatefix_26.3_expected.snbt")
	if len(input) != len(expected) {
		t.Fatal("fixture counts differ")
	}
	for name, root := range input {
		t.Run(name, func(t *testing.T) {
			version, _ := nbt.IntField(root, "DataVersion")
			for _, stage := range stages {
				if int64(version)*10 < int64(stage) {
					schemas[stage].value(root, "structure", stage)
				}
			}
			if !reflect.DeepEqual(root, expected[name]) {
				t.Errorf("fixed = %#v\nwant %#v", root, expected[name])
			}
		})
	}
}

func TestParseTemplateDataFix(t *testing.T) {
	root, err := nbt.ParseSNBT(`{DataVersion:4440,size:[2,1,1],palette:[{Name:"minecraft:chain",Properties:{axis:"x"}},{Name:"minecraft:chest"}],blocks:[{pos:[0,0,0],state:0},{pos:[1,0,0],state:1,nbt:{id:"minecraft:chest",Items:[{Slot:0b,id:"minecraft:chain",count:1}]}}],entities:[{pos:[0.5d,1.0d,0.5d],blockPos:[0,1,0],nbt:{id:"minecraft:villager"}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := nbt.Encode("", root)
	if err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{raw, compressed.Bytes()} {
		tpl, err := ParseTemplate(data)
		if err != nil {
			t.Fatal(err)
		}
		chain, ok := registry.FindState("minecraft:iron_chain", map[string]string{"axis": "x"})
		if !ok || tpl.Palettes[0][0].State != chain {
			t.Fatalf("old chain state was not upgraded: %+v", tpl.Palettes)
		}
		items, _ := nbt.Get[nbt.List](tpl.Palettes[0][1].NBT, "Items")
		item := items.Items[0].(nbt.Compound)
		if item["id"] != nbt.String("minecraft:iron_chain") {
			t.Errorf("item = %v", item)
		}
		if tpl.Entities[0].NBT["VillagerDataFinalized"] != nbt.Byte(1) {
			t.Error("entity was not upgraded")
		}
		saved, err := tpl.Encode(dataVersion)
		if err != nil {
			t.Fatal(err)
		}
		again, err := ParseTemplate(saved)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(tpl, again) {
			t.Error("upgraded template changed after saving and reloading")
		}
	}
}

// TestParseTemplateBlockStateForms checks that a pre-26.3 Name/Properties
// palette and a 26.3 id/properties one load to the same state.
func TestParseTemplateBlockStateForms(t *testing.T) {
	forms := map[string]string{
		"legacy": `{DataVersion:4440,size:[1,1,1],palette:[{Name:"minecraft:bone_block",Properties:{axis:"x"}}],blocks:[{pos:[0,0,0],state:0}]}`,
		"26.3":   `{DataVersion:5023,size:[1,1,1],palette:[{id:"minecraft:bone_block",properties:{axis:"x"}}],blocks:[{pos:[0,0,0],state:0}]}`,
	}
	want, ok := registry.FindState("minecraft:bone_block", map[string]string{"axis": "x"})
	if !ok {
		t.Fatal("no bone_block state")
	}
	for name, snbt := range forms {
		root, err := nbt.ParseSNBT(snbt)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := nbt.Encode("", root)
		if err != nil {
			t.Fatal(err)
		}
		tpl, err := ParseTemplate(raw)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := tpl.Palettes[0][0].State; got != want {
			t.Errorf("%s: state = %d, want %d", name, got, want)
		}
	}
}
