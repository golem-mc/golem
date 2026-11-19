package vanilla

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
)

// The table is the map a real client answers lookups from, built as
// net.minecraft.locale.Language loads it, in two steps:
//
//	en_us.json      every value run through the loader's rewrite, which turns
//	                the %d and %.Nf formats it does not support into %s
//	deprecated.json renames move a value onto the old key the code still asks
//	                for; removals drop the key
//
// The whole table is kept, so the console can answer every key a client would.
const (
	langPath       = "assets/minecraft/lang/en_us.json"
	deprecatedPath = "assets/minecraft/lang/deprecated.json"
)

// unsupportedFormat is what Language.loadFromJson rewrites: %d, %.1f and
// %2$d are all turned into %s, the one specifier its template reader knows.
var unsupportedFormat = regexp.MustCompile(`%(\d+\$)?[\d.]*[df]`)

// LoadClientLanguage reads the language the vanilla client answers with out of
// its jar. A rename in deprecated.json maps the key that holds the current
// value onto the key the code asks for; a removal drops the key, and a rename
// whose source has no value drops the destination with it, as the client does
// when it warns.
func LoadClientLanguage(jarPath string) (map[string]string, error) {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	read := func(name string) ([]byte, error) {
		f, err := zr.Open(name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(f)
	}
	raw, err := read(langPath)
	if err != nil {
		return nil, err
	}
	var entries map[string]string
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("%s: %w", langPath, err)
	}
	for k, v := range entries {
		entries[k] = unsupportedFormat.ReplaceAllString(v, "%${1}s")
	}
	raw, err = read(deprecatedPath)
	if err != nil {
		return nil, err
	}
	var deprecated struct {
		Removed []string        `json:"removed"`
		Renamed Ordered[string] `json:"renamed"`
	}
	if err := json.Unmarshal(raw, &deprecated); err != nil {
		return nil, fmt.Errorf("%s: %w", deprecatedPath, err)
	}
	for _, key := range deprecated.Removed {
		delete(entries, key)
	}
	for source, destination := range deprecated.Renamed.All() {
		if value, ok := entries[source]; ok {
			delete(entries, source)
			entries[destination] = value
		} else {
			delete(entries, destination)
		}
	}
	return entries, nil
}
