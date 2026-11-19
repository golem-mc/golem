package servertest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// SnowyFlatWorld stores the flat generator a snowy-plains world keeps in
// world_gen_settings.dat — the plains-or-not the rest of the suite builds on
// would never freeze — with the same stone ground the harness's own flat
// level-type grows.
func SnowyFlatWorld(t *testing.T, cfg *config.Config) {
	t.Helper()
	dir := t.TempDir()
	data, err := nbt.Encode("", nbt.Compound{
		"DataVersion": nbt.Int(4440),
		"data": nbt.Compound{
			"seed":                nbt.Long(0),
			"generate_structures": nbt.Byte(0),
			"dimensions": nbt.Compound{
				world.DimensionName: nbt.Compound{
					"type": nbt.String(world.DimensionType),
					"generator": nbt.Compound{
						"type": nbt.String("minecraft:flat"),
						"settings": nbt.Compound{
							"biome": nbt.String("minecraft:snowy_plains"),
							"layers": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
								nbt.Compound{"block": nbt.String("minecraft:stone"), "height": nbt.Int(128)},
							}},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "data", "minecraft", "world_gen_settings.dat")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteGzip(path, data); err != nil {
		t.Fatal(err)
	}
	cfg.LevelType = "minecraft:flat"
	cfg.LevelName = dir
}
