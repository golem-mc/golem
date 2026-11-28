package server

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The Pos and UUID shortcut must read exactly what the whole saved tag holds,
// for every kind a summon accepts that offers it.
func TestBaseDataTagMatchesDataTag(t *testing.T) {
	s, _ := newPluginHandleTestWorld(t)
	for i := range registry.EntityTypeCount {
		name := registry.EntityType(i).Name()
		if name == "minecraft:player" {
			continue
		}
		s.RunLine(fmt.Sprintf("summon %s %d.5 -60 3.25", name, i%8))
	}
	s.RunLine(`summon minecraft:item 1.5 -60 1.5 {Item:{id:"minecraft:stone",count:1}}`)
	shortcut := map[string]bool{}
	for _, e := range (worldScope{srv: s}).Entities(command.EntityQuery{}) {
		full, ok := e.(command.DataTarget).DataTag()
		base, isBase := e.(command.BaseDataTarget)
		if !ok || !isBase {
			continue
		}
		for _, key := range []string{"Pos", "UUID"} {
			got, ok := base.BaseDataTag(key)
			if !ok {
				continue
			}
			shortcut[e.EntityType()+" "+key] = true
			if !nbt.Equal(got, full.Entries()[key]) {
				t.Errorf("%s %s = %v, want %v", e.EntityType(), key, got, full.Entries()[key])
			}
		}
	}
	for _, want := range []string{"minecraft:marker Pos", "minecraft:zombie Pos", "minecraft:armor_stand Pos", "minecraft:item Pos", "minecraft:zombie UUID"} {
		if !shortcut[want] {
			t.Errorf("no shortcut checked for %s", want)
		}
	}
}
