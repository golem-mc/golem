package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
)

func init() {
	register(generator{name: "sculk-features", doc: "internal/server/world/levelgen/sculk_features.txt: 26.3 sculk feature compositions", run: runSculkFeatures})
}

func runSculkFeatures(e *env) error {
	if e.ref.Version != "26.3" {
		e.logf("skipped: needs 26.3")
		return nil
	}
	pack, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer pack.Close()
	data, err := fs.Sub(pack, "minecraft/worldgen")
	if err != nil {
		return err
	}
	r := featureReader{fsys: data}
	var out bytes.Buffer
	for _, name := range []string{"sculk_patch_deep_dark", "sculk_patch_ancient_city"} {
		doc, err := r.placed(name)
		if err != nil {
			return err
		}
		if bad := unmodeled(doc); len(bad) != 0 {
			return fmt.Errorf("%s: unmodeled %v", name, bad)
		}
		line, err := json.Marshal(doc)
		if err != nil {
			return err
		}
		fmt.Fprintf(&out, "%s\t%s\n", name, line)
	}
	return e.emitBytes("internal/server/world/levelgen/sculk_features.txt", out.Bytes())
}
