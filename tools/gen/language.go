package main

import (
	"strconv"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "language",
		doc:  "internal/protocol/language_gen.go: the vanilla client's en_us table",
		run:  runLanguage,
	})
}

func runLanguage(e *env) error {
	jar, err := e.ref.ClientJar()
	if err != nil {
		return err
	}
	entries, err := vanilla.LoadClientLanguage(jar)
	if err != nil {
		return err
	}
	f := e.file("protocol", "from the vanilla client jar of Minecraft {version}")
	f.WriteString(`// vanillaLanguage holds the en_us templates the vanilla client ships, the
// way its language loads them: every value with the format rewrite
// Language.loadFromJson applies - the %d and %.Nf specifiers its template
// reader does not understand turned into %s - and the deprecated.json
// renames and removals applied over them, so a key the code asks for
// answers with the template a client of this version would show.
var vanillaLanguage = map[string]string{
`)
	// Sorted by key, one entry per line; gofmt lines the values up.
	for _, key := range sortedKeys(entries) {
		f.Printf("\t%s: %s,\n", strconv.Quote(key), strconv.Quote(entries[key]))
	}
	f.WriteString("}\n")
	e.logf("language: %d keys from %s", len(entries), jar)
	return e.emit("internal/protocol/language_gen.go", f)
}
