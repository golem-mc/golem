package main

import (
	"bytes"
	"encoding/json"
	"path"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "contextproviders",
		doc:  "registry/contextproviders_gen.go: the data pack's context_int_provider, context_float_provider and predicate entries as compact JSON",
		run:  runContextProviders,
	})
}

// The provider decoder lives in internal/server/command, so the table keeps each entry
// as the JSON the data pack ships and leaves the reading to that decoder.
// vanilla: ContextIntProviders, ContextFloatProviders, Registries.PREDICATE
func runContextProviders(e *env) error {
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()

	f := e.file("registry", "by tools/gen contextproviders from the context_int_provider, context_float_provider and predicate entries of the vanilla {version} data pack")
	for _, kind := range []struct{ dir, varName, doc string }{
		{"context_int_provider", "contextIntProviderJSON", "the built-in context int providers"},
		{"context_float_provider", "contextFloatProviderJSON", "the built-in context float providers"},
		{"predicate", "predicateJSON", "the built-in predicates"},
	} {
		files, err := vanilla.Files(dp, path.Join("minecraft", kind.dir), ".json")
		if err != nil {
			return err
		}
		f.Printf("// %s holds %s by name, as JSON.\nvar %s = map[string]string{\n", kind.varName, kind.doc, kind.varName)
		for _, rel := range files {
			var raw json.RawMessage
			if err := vanilla.ReadJSON(dp, path.Join("minecraft", kind.dir, rel), &raw); err != nil {
				return err
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, raw); err != nil {
				return err
			}
			f.Printf("\t%s: %s,\n", gogen.Quote("minecraft:"+strings.TrimSuffix(rel, ".json")), gogen.Quote(compact.String()))
		}
		f.WriteString("}\n\n")
		e.logf("%s: %d", kind.dir, len(files))
	}
	return e.emit("registry/contextproviders_gen.go", f)
}
