package main

import (
	"fmt"

	"github.com/golem-mc/golem/tools/internal/gogen"
)

func init() {
	register(generator{
		name: "gameevents",
		doc:  "registry/gameevents_gen.go: typed game-event IDs and names",
		run:  runGameEvents,
	})
}

func runGameEvents(e *env) error {
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	names, err := registries.NamesByID("minecraft:game_event")
	if err != nil {
		return err
	}
	if len(names) == 0 || len(names) >= 256 {
		return fmt.Errorf("%d game events do not fit a nonzero uint8", len(names))
	}
	ids, err := idents("GameEvent", names)
	if err != nil {
		return err
	}
	f := e.file("registry", "from the game-event registry report of a vanilla {version} server")
	f.WriteString(`// GameEvent is an internal game-event ID: the registry ID plus one, reserving zero for none.
type GameEvent uint8

// GameEventNone is the absence of a registered game event.
const GameEventNone GameEvent = 0

`)
	f.Printf("// GameEventCount includes the reserved zero entry.\nconst GameEventCount = %d\n\n", len(names)+1)
	f.WriteString("// The registered game events.\nconst (\n")
	for i, id := range ids {
		f.Printf("\t%s GameEvent = %d\n", id, i+1)
	}
	f.WriteString(")\n\nvar gameEventNames = [GameEventCount]string{\n\t\"\",\n")
	for _, name := range names {
		f.Printf("\t%s,\n", gogen.Quote(name))
	}
	f.WriteString("}\n")
	return e.emit("registry/gameevents_gen.go", f)
}
