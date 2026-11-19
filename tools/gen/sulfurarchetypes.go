package main

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "sulfurarchetypes",
		doc:  "registry/sulfurarchetypes_gen.go: every field of each data/minecraft/sulfur_cube_archetype entry",
		run:  runSulfurArchetypes,
	})
}

func runSulfurArchetypes(e *env) error {
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()

	const dir = "minecraft/sulfur_cube_archetype"
	files, err := vanilla.Files(dp, dir, ".json")
	if err != nil {
		return err
	}
	f := e.file("registry", "by tools/gen sulfurarchetypes from the sulfur_cube_archetype definitions of the vanilla {version} data pack")
	f.WriteString(`// builtinSulfurArchetypes are the vanilla archetypes in registry order, which
// decides which of two matching archetypes' knockback and sounds win.
// vanilla: SulfurCubeArchetype.DIRECT_CODEC
var builtinSulfurArchetypes = []SulfurArchetype{
`)
	for _, rel := range files {
		var a struct {
			Items              json.RawMessage `json:"items"`
			AttributeModifiers []struct {
				Attribute string  `json:"attribute"`
				ID        string  `json:"id"`
				Amount    float64 `json:"amount"`
				Operation string  `json:"operation"`
			} `json:"attribute_modifiers"`
			Buoyant   bool `json:"buoyant"`
			Explosion *struct {
				Power      int  `json:"power"`
				CausesFire bool `json:"causes_fire"`
				Fuse       int  `json:"fuse"`
			} `json:"explosion"`
			ContactDamage *struct {
				DamageType        string          `json:"damage_type"`
				Amount            json.RawMessage `json:"amount"`
				AttributeToSource bool            `json:"attribute_to_source"`
			} `json:"contact_damage"`
			Knockback struct {
				Horizontal float64 `json:"horizontal_power"`
				Vertical   float64 `json:"vertical_power"`
			} `json:"knockback_modifiers"`
			Sounds struct {
				Hit       string  `json:"hit_sound"`
				Push      string  `json:"push_sound"`
				Threshold float64 `json:"push_sound_impulse_threshold"`
				Cooldown  float64 `json:"push_sound_cooldown"`
			} `json:"sound_settings"`
		}
		if err := vanilla.ReadJSON(dp, path.Join(dir, rel), &a); err != nil {
			return err
		}
		name := "minecraft:" + strings.TrimSuffix(rel, ".json")
		f.Printf("\t{\n\t\tName: %s,\n", gogen.Quote(name))
		var tag string
		var list []string
		switch {
		case json.Unmarshal(a.Items, &tag) == nil && strings.HasPrefix(tag, "#"):
			f.Printf("\t\tItemTag: %s,\n", gogen.Quote(tag[1:]))
		case json.Unmarshal(a.Items, &tag) == nil:
			list = []string{tag}
		case json.Unmarshal(a.Items, &list) != nil:
			return fmt.Errorf("%s: items is neither a string nor a list", name)
		}
		if list != nil {
			f.WriteString("\t\tItems: []Item{")
			for i, item := range list {
				if i > 0 {
					f.WriteString(", ")
				}
				f.Printf("mustItem(%s)", gogen.Quote(item))
			}
			f.WriteString("},\n")
		}
		f.WriteString("\t\tModifiers: []SulfurModifier{\n")
		for _, m := range a.AttributeModifiers {
			f.Printf("\t\t\t{Attribute: mustAttributeID(%s), ID: %s, Amount: %s, Operation: mustAttributeOperation(%s)},\n",
				gogen.Quote(m.Attribute), gogen.Quote(m.ID), gogen.Float(m.Amount), gogen.Quote(m.Operation))
		}
		f.WriteString("\t\t},\n")
		if a.Buoyant {
			f.WriteString("\t\tBuoyant: true,\n")
		}
		if x := a.Explosion; x != nil {
			f.Printf("\t\tExplosion: &SulfurExplosion{Power: %d, CausesFire: %t, Fuse: %d},\n", x.Power, x.CausesFire, x.Fuse)
		}
		if c := a.ContactDamage; c != nil {
			var amount float64
			if err := json.Unmarshal(c.Amount, &amount); err != nil {
				return fmt.Errorf("%s: contact_damage.amount %s is not a constant; the generator does not model float providers", name, c.Amount)
			}
			damageIdent, err := ident("DamageType", c.DamageType)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			f.Printf("\t\tContact: &SulfurContact{DamageType: %s, Min: %s, Max: %s, AttributeToSource: %t},\n",
				damageIdent, gogen.Float(amount), gogen.Float(amount), c.AttributeToSource)
		}
		f.Printf("\t\tKnockbackHorizontal: %s,\n\t\tKnockbackVertical: %s,\n", gogen.Float(a.Knockback.Horizontal), gogen.Float(a.Knockback.Vertical))
		hit, err := ident("Sound", a.Sounds.Hit)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		push, err := ident("Sound", a.Sounds.Push)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		f.Printf("\t\tHitSound: %s,\n\t\tPushSound: %s,\n", hit, push)
		f.Printf("\t\tPushThreshold: %s,\n\t\tPushCooldown: %s,\n\t},\n", gogen.Float(a.Sounds.Threshold), gogen.Float(a.Sounds.Cooldown))
	}
	f.WriteString("}\n")
	e.logf("sulfur cube archetypes: %d", len(files))
	return e.emit("registry/sulfurarchetypes_gen.go", f)
}
