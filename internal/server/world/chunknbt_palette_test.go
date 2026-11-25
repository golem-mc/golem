package world

import (
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// referencePaletteEntry is blockPaletteEntry without the cache.
func referencePaletteEntry(stateID uint16) nbt.Tag {
	name, props, ok := registry.BlockStateName(stateID)
	if !ok {
		return nbt.String("minecraft:air")
	}
	if def, _ := registry.FindState(name, nil); stateID == def {
		return nbt.String(name)
	}
	entry := nbt.Compound{"id": nbt.String(name)}
	if props != "" {
		// BlockStateName sorts properties; the nested pair codecs insert them
		// in reverse order, which matters for colliding compound hash keys.
		// vanilla: StateDefinition.appendPropertyCodec, PairMapCodec.encode
		pairs := strings.Split(props, ",")
		properties := nbt.NewOrderedCompound(len(pairs))
		for _, pair := range slices.Backward(pairs) {
			k, v, _ := strings.Cut(pair, "=")
			properties.Set(k, nbt.String(v))
		}
		entry["properties"] = properties
	}
	return entry
}

func TestChunkNBTPaletteCompactStates(t *testing.T) {
	t.Parallel()
	east := registry.MustState("minecraft:oak_stairs", map[string]string{"facing": "east"})
	for _, mixed := range []bool{false, true} {
		c := newChunk(0, 0)
		c.SetBiome(registry.MustID("minecraft:worldgen/biome", "minecraft:plains"))
		c.SetBlock(0, worldMinY, 0, registry.BlockOakStairs.DefaultState())
		if mixed {
			c.SetBlock(1, worldMinY, 0, east)
		}
		root, err := encodeChunkNBT(c)
		if err != nil {
			t.Fatal(err)
		}
		data, err := nbt.Encode("", root)
		if err != nil {
			t.Fatal(err)
		}
		_, tag, err := nbt.Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		sections := tag.(nbt.Compound)["sections"].(nbt.List)
		blocks := sections.Items[0].(nbt.Compound)["block_states"].(nbt.Compound)
		palette := blocks["palette"].(nbt.List)
		var sawDefault, sawEast bool
		for _, entry := range palette.Items {
			switch value := nbt.ListElement(entry).(type) {
			case nbt.String:
				sawDefault = sawDefault || value == "minecraft:oak_stairs"
			case nbt.Compound:
				name, props := registry.BlockStateOf(value)
				sawEast = sawEast || name == "minecraft:oak_stairs" && props["facing"] == "east"
			}
		}
		if !sawDefault || sawEast != mixed {
			t.Fatalf("mixed=%v: palette = %#v", mixed, palette)
		}
		if !mixed && palette.Elem != nbt.TypeString {
			t.Fatalf("default palette type = %d, want strings", palette.Elem)
		}
		loaded, err := decodeChunkNBT(tag.(nbt.Compound))
		if err != nil {
			t.Fatal(err)
		}
		if got := loaded.BlockAt(0, worldMinY, 0); got != registry.BlockOakStairs.DefaultState() {
			t.Fatalf("default stairs = %d", got)
		}
		if mixed && loaded.BlockAt(1, worldMinY, 0) != east {
			t.Fatal("mixed palette lost east-facing stairs")
		}
	}
}

func TestResolveBlockStateOverrides(t *testing.T) {
	t.Parallel()
	stairs := func(props map[string]string) nbt.Tag {
		p := nbt.Compound{}
		for k, v := range props {
			p[k] = nbt.String(v)
		}
		return nbt.Compound{"id": nbt.String("minecraft:oak_stairs"), "properties": p}
	}
	east := registry.MustState("minecraft:oak_stairs", map[string]string{"facing": "east"})
	for _, tc := range []struct {
		name  string
		entry nbt.Tag
		want  uint16
		ok    bool
	}{
		{"default", nbt.String("minecraft:oak_stairs"), registry.BlockOakStairs.DefaultState(), true},
		{"override", stairs(map[string]string{"facing": "east"}), east, true},
		{"unknown property", stairs(map[string]string{"bogus": "x"}), 0, false},
		{"invalid value", stairs(map[string]string{"facing": "up"}), 0, false},
	} {
		got, ok := resolveBlockState(tc.entry)
		if ok != tc.ok || ok && got != tc.want {
			t.Errorf("%s: got (%d, %v), want (%d, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
}

func BenchmarkResolveChunkPaletteState(b *testing.B) {
	bare := nbt.Tag(nbt.String("minecraft:oak_stairs"))
	override := nbt.Tag(nbt.Compound{
		"id":         nbt.String("minecraft:oak_stairs"),
		"properties": nbt.Compound{"facing": nbt.String("east")},
	})
	for _, bc := range []struct {
		name  string
		entry nbt.Tag
	}{{"bare", bare}, {"override", override}} {
		b.Run(bc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if _, ok := resolveBlockState(bc.entry); !ok {
					b.Fatal("unresolved")
				}
			}
		})
	}
}
