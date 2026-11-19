package levelgen

import "testing"

func TestTreeTableSeparatesProviderRegistry(t *testing.T) {
	// Registry namespaces may share an identifier without overwriting each other.
	placed, configured, providers := readTreeFeatures(`# provider fixtures
placed minecraft:same {"feature":"minecraft:same","placement":[]}
configured minecraft:same {"type":"minecraft:simple_block","to_place":"minecraft:same"}
provider minecraft:same {"id":"minecraft:stone"}
`)
	if placed["minecraft:same"] != `{"feature":"minecraft:same","placement":[]}` ||
		configured["minecraft:same"] != `{"type":"minecraft:simple_block","to_place":"minecraft:same"}` ||
		providers["minecraft:same"] != `{"id":"minecraft:stone"}` {
		t.Fatalf("registry collision: placed %v configured %v providers %v", placed, configured, providers)
	}
}
