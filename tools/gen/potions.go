package main

import (
	"fmt"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "potions",
		doc:  "registry/potions_gen.go: the potion registry with the effects drinking each gives",
		run:  runPotions,
	})
}

// Potions and brewings are set in the server's Java code (Potions.java), in no
// report or data pack file, so the helper boots the registries and asks every
// potion. The report's minecraft:potion entries are the cross-check: IDs and names
// must match, and the size must equal internal/protocol/ids.go's RegistrySizePotion, the
// bound the raw component reader checks a potion ID against.

type potionEffect struct {
	id                           string
	amplifier, duration          int
	ambient, particles, showIcon bool
}

type potion struct {
	name    string
	count   int
	effects []potionEffect
}

func runPotions(e *env) error {
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	names, err := registries.NamesByID("minecraft:potion")
	if err != nil {
		return err
	}
	want, err := protocolConst(e, "RegistrySizePotion")
	if err != nil {
		return err
	}
	if len(names) != want {
		return fmt.Errorf("the report carries %d potions; internal/protocol/ids.go's RegistrySizePotion says %d, and the two must move together", len(names), want)
	}
	out, err := e.java("PotionEffects", vanilla.JavaOptions{})
	if err != nil {
		return err
	}
	var potions []potion
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(stdoutLine(line))
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "POTION":
			if len(fields) != 4 {
				return fmt.Errorf("a POTION line of %d fields: %q", len(fields), line)
			}
			if id := atoi(fields[1]); id != len(potions) {
				return fmt.Errorf("the dump numbers %s %d, want %d", fields[2], id, len(potions))
			}
			potions = append(potions, potion{name: fields[2], count: atoi(fields[3])})
		case "EFFECT":
			if len(fields) != 7 || len(potions) == 0 {
				return fmt.Errorf("an EFFECT line that does not follow a potion: %q", line)
			}
			p := &potions[len(potions)-1]
			p.effects = append(p.effects, potionEffect{fields[1], atoi(fields[2]), atoi(fields[3]),
				fields[4] == "true", fields[5] == "true", fields[6] == "true"})
		}
	}
	if len(potions) != len(names) {
		return fmt.Errorf("the dump carries %d potions, the report %d", len(potions), len(names))
	}
	for id, registered := range names {
		p := potions[id]
		if p.name != registered {
			return fmt.Errorf("the dump names potion %d %s, the report %s", id, p.name, registered)
		}
		if p.count != len(p.effects) {
			return fmt.Errorf("%s declares %d effects and lists %d", registered, p.count, len(p.effects))
		}
	}

	literal := func(x potionEffect) string {
		return fmt.Sprintf("{Effect: %s, Amplifier: %d, Duration: %d, Ambient: %t, ShowParticles: %t, ShowIcon: %t}",
			gogen.Quote(x.id), x.amplifier, x.duration, x.ambient, x.particles, x.showIcon)
	}
	f := e.file("registry", "by tools/gen potions from a vanilla {version} server")
	f.WriteString(`// potions is the potion registry the client keeps built into itself, in the
// order its IDs number the entries - the ID a potion_contents component names
// a potion by on the wire - each entry holding the effects drinking it gives,
// as a component writes an effect.
// vanilla: Potion.getEffects (Potions.java's registrations)
var potions = [...]struct {
	Name    string
	Effects []EffectSpec
}{
`)
	withEffects := 0
	for id, registered := range names {
		effects := potions[id].effects
		switch len(effects) {
		case 0:
			f.Printf("\t{\"%s\", nil}, // %d\n", registered, id)
		case 1:
			withEffects++
			f.Printf("\t{\"%s\", []EffectSpec{%s}}, // %d\n", registered, literal(effects[0]), id)
		default:
			withEffects++
			f.Printf("\t{\"%s\", []EffectSpec{\n", registered)
			for _, x := range effects {
				f.Printf("\t\t%s,\n", literal(x))
			}
			f.Printf("\t}}, // %d\n", id)
		}
	}
	f.WriteString("}\n")
	e.logf("potions: %d, with effects: %d", len(names), withEffects)
	return e.emit("registry/potions_gen.go", f)
}
