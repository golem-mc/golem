package world

import (
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func packTestTemplate(t *testing.T) []byte {
	t.Helper()
	raw, err := nbt.Encode("", nbt.Compound{
		"size":    nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(1), nbt.Int(1), nbt.Int(1)}},
		"palette": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"Name": nbt.String("minecraft:gold_block")}}},
		"blocks": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"pos": nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{nbt.Int(0), nbt.Int(0), nbt.Int(0)}}, "state": nbt.Int(0)}}},
		"DataVersion": nbt.Int(4440),
	})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	return buf.Bytes()
}

// TestPackTemplatePoolReplacesVanillaPool checks that a pack's template pool
// of a vanilla id is what a vanilla structure starts from, and that removing
// the pack brings the vanilla pool back.
func TestPackTemplatePoolReplacesVanillaPool(t *testing.T) {
	const pool = "minecraft:village/plains/town_centers"
	start := func() string {
		def := jigsawStructureNamed("minecraft:village_plains")
		if def == nil || len(def.startPool.templates) == 0 {
			t.Fatal("the plains village has no start templates")
		}
		return def.startPool.templates[0].template.id
	}
	before := start()
	tiny := packTestTemplate(t)
	t.Cleanup(func() { ApplyPackWorldgen(PackWorldgen{}) })
	ApplyPackWorldgen(PackWorldgen{
		TemplatePools: map[string][]byte{pool: []byte(`{"fallback":"minecraft:empty","elements":[{"weight":1,"element":{
			"element_type":"minecraft:single_pool_element","location":"test:tiny","processors":{"processors":[]},"projection":"rigid"}}]}`)},
		Template: func(id string) ([]byte, bool) { return tiny, id == "test:tiny" },
	})
	if got := start(); got != "test:tiny" {
		t.Errorf("the plains village starts from %s with the pack's pool, want test:tiny", got)
	}
	ApplyPackWorldgen(PackWorldgen{})
	if got := start(); got != before {
		t.Errorf("the plains village starts from %s without the pack, want %s", got, before)
	}
}

// TestPackStructureSetRefusesConcentricRings checks that a pack set other
// than random_spread is skipped with an error rather than read.
func TestPackStructureSetRefusesConcentricRings(t *testing.T) {
	_, err := packStructureSet("test:rings", []byte(`{"structures":[{"structure":"minecraft:village_plains","weight":1}],
		"placement":{"type":"minecraft:concentric_rings","distance":32,"spread":3,"count":128}}`), func(string) bool { return true })
	if err == nil {
		t.Fatal("a concentric_rings pack set was read")
	}
}
