package main

import "fmt"

// The registries the client keeps to itself: the report lists their entries
// by protocol ID, and these generators write the names out in that order, so
// a name's position is its ID.

// nameTable is one Go variable holding a registry's names.
type nameTable struct {
	registry string // key in registries.json
	variable string
	doc      string // the variable's comment, verbatim
	set      bool   // a map[string]bool instead of a []string
	// idType, when set, also declares an int32 type of that name with one
	// constant per entry, named by ident, valued at its ID (plus one for reserveZero).
	idType      string
	idDoc       string // idType's comment, verbatim
	reserveZero bool   // Registry IDs are offset by one, reserving zero for none.
}

// nameFile is one generated file.
type nameFile struct {
	out    string
	pkg    string
	from   string // completes the header; {version} is the Minecraft version
	tables []nameTable
}

type nameGenerator struct {
	name  string
	doc   string
	files []nameFile
}

var nameGenerators = []nameGenerator{
	{
		name: "particles",
		doc:  "registry/particles_gen.go: the minecraft:particle_type registry",
		files: []nameFile{{
			out: "registry/particles_gen.go", pkg: "registry",
			from: "from the particle type registry of a vanilla {version} server",
			tables: []nameTable{{
				registry: "minecraft:particle_type", variable: "ParticleTypes",
				doc: `// ParticleTypes lists every particle type; the position of a name is the ID
// the Level Particles packet refers to it by.
`,
			}},
		}},
	},
	{
		name: "mobeffects",
		doc:  "registry/mobeffects_gen.go: the minecraft:mob_effect registry",
		files: []nameFile{{
			out: "registry/mobeffects_gen.go", pkg: "registry",
			from: "from the mob effect registry of a vanilla {version} server",
			tables: []nameTable{{
				registry: "minecraft:mob_effect", variable: "MobEffects",
				doc: `// MobEffects lists every status effect in ID order: an effect's index here is
// its minecraft:mob_effect ID.
`,
			}},
		}},
	},
	{
		name: "stats",
		doc:  "registry/stats_gen.go: the minecraft:stat_type and custom_stat registries",
		files: []nameFile{{
			out: "registry/stats_gen.go", pkg: "registry",
			from: "from the statistic registries of a vanilla {version} server",
			tables: []nameTable{{
				registry: "minecraft:stat_type", variable: "StatTypes", idType: "StatType",
				idDoc: "// StatType is a statistic type's ID in minecraft:stat_type.\n",
				doc: `// StatTypes lists every statistic type, in the order that gives each its
// numeric ID in the minecraft:stat_type registry, the category ID the Award
// Statistics packet carries. The registry is built into the client and never
// sent, so these IDs are only valid for the exact version above.
`,
			}, {
				registry: "minecraft:custom_stat", variable: "CustomStats", idType: "CustomStat",
				idDoc: "// CustomStat is a minecraft:custom statistic's key, its ID in\n// minecraft:custom_stat.\n",
				doc: `// CustomStats lists every entry of the minecraft:custom_stat registry, the
// keys of the minecraft:custom statistic type, in ID order.
`,
			}},
		}},
	},
	{
		name: "soundevents",
		doc:  "registry/soundevents_gen.go: the minecraft:sound_event registry",
		files: []nameFile{{
			out: "registry/soundevents_gen.go", pkg: "registry",
			from: "from the sound event registry of a vanilla {version} server",
			tables: []nameTable{{
				registry: "minecraft:sound_event", variable: "SoundEvents", idType: "Sound", reserveZero: true,
				idDoc: "// Sound is a registered sound holder: its registry ID plus one; zero means none.\n",
				doc: `// SoundEvents lists every sound event, in the order that gives each its
// numeric ID: the position of a name in this slice is the ID the Sound Effect
// and Entity Sound Effect packets refer to it by. The registry is built into
// the client and never sent, so these IDs are only valid for the exact version
// above.
`,
			}},
		}},
	},
	{
		name: "entitytypes",
		doc:  "registry/entitytypes_gen.go and internal/protocol/entitytypes_gen.go: the minecraft:entity_type registry",
		files: []nameFile{{
			out: "registry/entitytypes_gen.go", pkg: "registry",
			from: "from the entity type registry of a vanilla {version} server",
			tables: []nameTable{{
				registry: "minecraft:entity_type", variable: "EntityTypes",
				doc: `// EntityTypes lists every entity type, in the order that gives each its
// numeric ID: the position of a name in this slice is the ID a Spawn Entity
// packet refers to it by. The registry is built into the client and never
// sent, so these IDs are only valid for the exact version above.
`,
			}},
		}, {
			out: "internal/protocol/entitytypes_gen.go", pkg: "protocol",
			from: "from the entity type registry of a vanilla {version} server",
			tables: []nameTable{{
				registry: "minecraft:entity_type", variable: "entityTypeNames", set: true,
				doc: `// entityTypeNames are the names a text component's show-entity hover may
// carry in its id: the client resolves one against its own entity type
// registry while it decodes the component, so an id the registry does not
// carry takes the client down with the text. EntityTypes in the registry
// package holds the same names paired with their numeric IDs; this copy
// exists because the protocol cannot import the package that imports it.
`,
			}},
		}},
	},
	{
		name: "items",
		doc:  "registry/items_gen.go: the minecraft:item registry",
		files: []nameFile{{
			out: "registry/items_gen.go", pkg: "registry",
			from: "from the item registry of a vanilla {version} server",
			tables: []nameTable{{
				registry: "minecraft:item", variable: "Items",
				doc: `// Items lists every item, in the order that gives each its numeric ID: the
// position of a name in this slice is the ID that packets refer to it by.
//
// The item registry is built into the client and never sent, so these IDs are
// only valid for the exact version above. Anything written to disk stores the
// name instead, which is what makes a world outlive them.
`,
			}},
		}},
	},
}

func init() {
	for _, g := range nameGenerators {
		register(generator{name: g.name, doc: g.doc, run: func(e *env) error { return runNames(e, g) }})
	}
}

func runNames(e *env, g nameGenerator) error {
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	for _, spec := range g.files {
		f := e.file(spec.pkg, spec.from)
		for _, t := range spec.tables {
			names, err := registries.NamesByID(t.registry)
			if err != nil {
				return err
			}
			f.WriteString(t.doc)
			if t.set {
				f.Set(t.variable, names)
			} else {
				f.Strings(t.variable, names)
			}
			f.WriteString("\n")
			if t.idType != "" {
				f.WriteString(t.idDoc)
				symbols, err := idents(t.idType, names)
				if err != nil {
					return err
				}
				f.Printf("type %s int32\n\nconst (\n", t.idType)
				if t.reserveZero {
					f.Printf("\t%sNone %s = 0\n", t.idType, t.idType)
				}
				for id, symbol := range symbols {
					if t.reserveZero {
						if symbol == t.idType+"None" {
							return fmt.Errorf("%s conflicts with reserved zero", symbol)
						}
						id++
					}
					f.Printf("\t%s %s = %d\n", symbol, t.idType, id)
				}
				f.WriteString(")\n\n")
			}
			e.logf("%s: %d", t.registry, len(names))
		}
		if err := e.emit(spec.out, f); err != nil {
			return err
		}
	}
	return nil
}
