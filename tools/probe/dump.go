package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// dumped* mirror what the server sent, for regenerating the registry package.
type dumpedRegistry struct {
	Name    string   `json:"name"`
	Entries []string `json:"entries"`
}

type dumpedTag struct {
	Name string  `json:"name"`
	IDs  []int32 `json:"ids"`
}

type dumpedTagRegistry struct {
	Registry string      `json:"registry"`
	Tags     []dumpedTag `json:"tags"`
}

var dump struct {
	Version    string              `json:"version"`
	Protocol   int32               `json:"protocol"`
	Registries []dumpedRegistry    `json:"registries"`
	Tags       []dumpedTagRegistry `json:"tags"`
}

// writeDump saves the registries and tags the server sent, if DUMP is set.
func writeDump() {
	path := os.Getenv("DUMP")
	if path == "" || len(dump.Registries) == 0 {
		return
	}
	data, err := json.MarshalIndent(dump, "", "  ")
	if err != nil {
		fail("marshal dump: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		fail("write dump: %v", err)
	}
	fmt.Printf("wrote %s\n", path)
}
