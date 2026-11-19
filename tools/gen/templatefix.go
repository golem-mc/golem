package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{name: "templatefix", doc: "internal/server/world/templatefix_gen.go: structure data-fixer schema paths and recipe renames", run: runTemplateFix})
}

func runTemplateFix(e *env) error {
	out, err := e.java("TemplateFixSchema", vanilla.JavaOptions{Xmx: "1g"})
	if err != nil {
		return err
	}
	schemas := map[string]map[string][][2]string{}
	hooks := map[string]bool{}
	var version, owner string
	var renames [][2]string
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(stdoutLine(line))
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "TYPE":
			if len(fields) != 3 {
				return fmt.Errorf("bad schema line: %s", line)
			}
			version, owner = fields[1], fields[2]
			if _, err := strconv.Atoi(version); err != nil {
				return err
			}
			if schemas[version] == nil {
				schemas[version] = map[string][][2]string{}
			}
			schemas[version][owner] = nil
		case "EDGE":
			if len(fields) != 5 || fields[1] != version || fields[2] != owner {
				return fmt.Errorf("bad edge: %s", line)
			}
			path := strings.TrimPrefix(fields[3], "/")
			if path == "." {
				path = ""
			}
			schemas[version][owner] = append(schemas[version][owner], [2]string{path, fields[4]})
		case "HOOK":
			if len(fields) != 3 || fields[1] != version || fields[2] != "plain_item_id" {
				return fmt.Errorf("bad hook: %s", line)
			}
			hooks[version] = true
		case "RECIPE":
			if len(fields) != 3 {
				return fmt.Errorf("bad recipe: %s", line)
			}
			renames = append(renames, [2]string{fields[1], fields[2]})
		}
	}
	wantStages := 7
	if e.ref.Version == "26.3" {
		wantStages = 16
	}
	if len(schemas) != wantStages || len(renames) != 11 || len(schemas["45350"]) < 15 {
		return fmt.Errorf("incomplete structure fixer dump")
	}
	f := e.file("world", "Java {version} DataFixers STRUCTURE schemas and StonecutterRecipeRenameFix")
	stages := sortedKeys(schemas)
	f.Printf("var templateFixStages = [...]int32{")
	for _, stage := range stages {
		f.Printf("%s,", stage)
	}
	f.Printf("}\nvar templateFixSchemas = map[int32]templateFixer{\n")
	var unique []string
	for _, stage := range stages {
		shared := stage
		for _, prev := range unique {
			if hooks[stage] == hooks[prev] && reflect.DeepEqual(schemas[stage], schemas[prev]) {
				shared = prev
				break
			}
		}
		if shared == stage {
			unique = append(unique, stage)
		}
		f.Printf("%s: templateFixSchema%s,\n", stage, shared)
	}
	f.Printf("}\n")
	for _, stage := range unique {
		f.Printf("var templateFixSchema%s = templateFixer{unpackItems: %t, paths: map[string][]templateFixEdge{\n", stage, hooks[stage])
		for _, name := range sortedKeys(schemas[stage]) {
			f.Printf("%s: {\n", gogen.Quote(name))
			for _, edge := range schemas[stage][name] {
				f.Printf("{%s, %s},\n", gogen.Quote(edge[0]), gogen.Quote(edge[1]))
			}
			f.Printf("},\n")
		}
		f.Printf("}}\n")
	}
	f.Printf("var templateRecipeRenames = map[string]string{\n")
	for _, r := range renames {
		f.Printf("%s: %s,\n", gogen.Quote(r[0]), gogen.Quote(r[1]))
	}
	f.Printf("}\n")
	if err := e.emit("internal/server/world/templatefix_gen.go", f); err != nil {
		return err
	}
	golden, err := e.java("TemplateFixGolden", vanilla.JavaOptions{Xmx: "1g", Args: []string{filepath.Join(e.root, "internal/server/world/testdata/templatefix_input.snbt")}})
	if err != nil {
		return err
	}
	var expected strings.Builder
	for line := range strings.SplitSeq(golden, "\n") {
		if line, ok := strings.CutPrefix(stdoutLine(line), "FIXED "); ok {
			expected.WriteString(line + "\n")
		}
	}
	if expected.Len() == 0 {
		return fmt.Errorf("no template fixer golden output")
	}
	if e.ref.Version == "26.3" {
		type fixtureSchema struct {
			Paths       map[string][][2]string `json:"paths"`
			UnpackItems bool                   `json:"unpack_items"`
		}
		fixture := struct {
			Stages  map[string]string        `json:"stages"`
			Schemas map[string]fixtureSchema `json:"schemas"`
		}{Stages: map[string]string{}, Schemas: map[string]fixtureSchema{}}
		for _, stage := range stages {
			for _, shared := range unique {
				if hooks[stage] == hooks[shared] && reflect.DeepEqual(schemas[stage], schemas[shared]) {
					fixture.Stages[stage] = shared
					break
				}
			}
		}
		for _, stage := range unique {
			fixture.Schemas[stage] = fixtureSchema{Paths: schemas[stage], UnpackItems: hooks[stage]}
		}
		data, err := json.MarshalIndent(fixture, "", "  ")
		if err != nil {
			return err
		}
		if err := e.emitBytes("internal/server/world/testdata/templatefix_26.3_schemas.json", append(data, '\n')); err != nil {
			return err
		}
		if err := e.emitBytes("internal/server/world/testdata/templatefix_26.3_expected.snbt", []byte(expected.String())); err != nil {
			return err
		}
	}
	return e.emitBytes("internal/server/world/testdata/templatefix_expected.snbt", []byte(expected.String()))
}
