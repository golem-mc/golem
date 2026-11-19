package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
)

func init() {
	register(generator{
		name: "jsonrpcschema",
		doc:  "internal/server/management/schema_gen.go: the management protocol's OpenRPC document and each method's parameter",
		run:  runJSONRPCSchema,
	})
}

const notificationPrefix = "minecraft:notification/"

func runJSONRPCSchema(e *env) error {
	raw, err := os.ReadFile(e.ref.ReportPath("json-rpc-api-schema.json"))
	if err != nil {
		return err
	}
	var doc struct {
		Methods []struct {
			Name   string `json:"name"`
			Params []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
			} `json:"params"`
		} `json:"methods"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("jsonrpcschema: %w", err)
	}
	params := map[string]string{}
	var names []string
	for _, m := range doc.Methods {
		if strings.HasPrefix(m.Name, notificationPrefix) {
			continue
		}
		switch {
		case len(m.Params) > 1:
			return fmt.Errorf("jsonrpcschema: %s has %d params, the dispatcher models one", m.Name, len(m.Params))
		case len(m.Params) == 1 && !m.Params[0].Required:
			return fmt.Errorf("jsonrpcschema: %s has an optional param %s, the dispatcher models required ones", m.Name, m.Params[0].Name)
		case len(m.Params) == 1:
			params[m.Name] = m.Params[0].Name
		default:
			params[m.Name] = ""
		}
		names = append(names, m.Name)
	}
	slices.Sort(names)
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return fmt.Errorf("jsonrpcschema: %w", err)
	}

	f := e.file("management", "by tools/gen jsonrpcschema from the vanilla {version} json-rpc-api-schema report")
	f.Printf("\n// discoverJSON is the document rpc.discover returns.\nconst discoverJSON = %s\n", gogen.Quote(compact.String()))
	f.Printf("\n// methodParams maps every request method to the name of its one parameter, empty for none.\nvar methodParams = map[string]string{\n")
	for _, n := range names {
		f.Printf("\t%s: %s,\n", gogen.Quote(n), gogen.Quote(params[n]))
	}
	f.Printf("}\n")
	e.logf("methods: %d", len(names))
	return e.emit("internal/server/management/schema_gen.go", f)
}
