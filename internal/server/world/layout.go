package world

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Since 26.1 a world keeps each dimension's region, entities and poi folders
// and its saved data under dimensions/<namespace>/<path>/, the overworld
// included; level.dat and the server-wide data/ stay at the root.
// vanilla: DimensionType.getStorageFolder

// DimensionDir returns the folder of the dimension called name in the world
// under root.
func DimensionDir(root, name string) string {
	namespace, path, ok := strings.Cut(name, ":")
	if !ok {
		namespace, path = "minecraft", name
	}
	return filepath.Join(root, "dimensions", namespace, path)
}

// legacyMoves are the moves from the pre-26.1 layout: overworld at the root,
// nether under DIM-1, end under DIM1, player files in advancements, playerdata
// and stats, and server-wide saved data loose in data/. Paths are relative to
// the world folder.
// vanilla: PlayerStorageFileFix, DimensionStorageFileFix
var legacyMoves = func() [][2]string {
	moves := [][2]string{
		{"advancements", filepath.Join("players", "advancements")},
		{"playerdata", filepath.Join("players", "data")},
		{"stats", filepath.Join("players", "stats")},
		{filepath.Join("data", "scoreboard.dat"), filepath.Join("data", "minecraft", "scoreboard.dat")},
		{filepath.Join("data", "stopwatches.dat"), filepath.Join("data", "minecraft", "stopwatches.dat")},
		{filepath.Join("data", "idcounts.dat"), filepath.Join("data", "minecraft", "maps", "last_id.dat")},
		{filepath.Join("data", "random_sequences.dat"), filepath.Join("data", "minecraft", "random_sequences.dat")},
	}
	for _, d := range []struct{ old, name string }{
		{"", DimensionName}, {"DIM-1", NetherName}, {"DIM1", EndName},
	} {
		dst := DimensionDir("", d.name)
		for _, folder := range []string{"region", "entities", "poi"} {
			moves = append(moves, [2]string{filepath.Join(d.old, folder), filepath.Join(dst, folder)})
		}
		for _, file := range [][2]string{
			{"chunks.dat", "chunk_tickets.dat"},
			{"raids.dat", "raids.dat"},
			{"raids_end.dat", "raids.dat"},
			{"world_border.dat", "world_border.dat"},
		} {
			moves = append(moves, [2]string{
				filepath.Join(d.old, "data", file[0]),
				filepath.Join(dst, "data", "minecraft", file[1]),
			})
		}
	}
	return moves
}()

// legacyDataMoves are the old data/ moves whose names carry a namespace or map
// number the fixed list cannot name, so they are read off the folder.
// vanilla: DimensionStorageFileFix (RegexMove)
func legacyDataMoves(root string) ([][2]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "data"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var moves [][2]string
	for _, entry := range entries {
		name := entry.Name()
		if namespace, ok := strings.CutPrefix(name, "command_storage_"); ok {
			if namespace, ok := strings.CutSuffix(namespace, ".dat"); ok && namespaceName(namespace) {
				moves = append(moves, [2]string{
					filepath.Join("data", name),
					filepath.Join("data", namespace, "command_storage.dat"),
				})
			}
			continue
		}
		if number, ok := strings.CutPrefix(name, "map_"); ok {
			if number, ok := strings.CutSuffix(number, ".dat"); ok && mapNumber(number) {
				moves = append(moves, [2]string{
					filepath.Join("data", name),
					filepath.Join("data", "minecraft", "maps", number+".dat"),
				})
			}
		}
	}
	return moves, nil
}

// namespaceName reports a command storage namespace, the [a-z0-9_.-]+ in old
// file names.
func namespaceName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '.' && r != '-' {
			return false
		}
	}
	return true
}

// mapNumber reports a map number, the \d+ in old file names.
func mapNumber(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// MigrateLayout moves a pre-26.1 world under root into the current places. Each move
// is a rename skipped when its source is gone, so a cut-short migration
// finishes on the next start; a move whose target exists is left with a
// warning. The emptied DIM-1 and DIM1 folders are removed.
// vanilla: FileFixUtil.moveFile
func MigrateLayout(root string) error {
	for _, move := range legacyMoves {
		if err := moveLegacy(root, move[0], move[1]); err != nil {
			return err
		}
	}
	scanned, err := legacyDataMoves(root)
	if err != nil {
		return fmt.Errorf("migrate the world layout: %w", err)
	}
	for _, move := range scanned {
		if err := moveLegacy(root, move[0], move[1]); err != nil {
			return err
		}
	}
	for _, dir := range []string{"DIM-1/data", "DIM1/data", "DIM-1", "DIM1"} {
		// Remove fails on a non-empty folder, which is then kept.
		if err := os.Remove(filepath.Join(root, dir)); err == nil {
			slog.Info("removed an emptied folder of the old world layout", "dir", filepath.Join(root, dir))
		}
	}
	return nil
}

// moveLegacy renames one old-layout path, an error leaving the rest to the next
// run.
func moveLegacy(root, from, to string) error {
	from, to = filepath.Join(root, from), filepath.Join(root, to)
	if _, err := os.Lstat(from); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("migrate the world layout: %w", err)
	}
	if _, err := os.Lstat(to); err == nil {
		slog.Warn("not moving a file of the old world layout, its new place is taken",
			"from", from, "to", to)
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("migrate the world layout: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return fmt.Errorf("migrate the world layout: %w", err)
	}
	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("migrate the world layout: %w", err)
	}
	slog.Info("moved to the current world layout", "from", from, "to", to)
	return nil
}
