package main

import (
	"fmt"
	"path"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "damagetypes",
		doc:  "registry/damagetypes_gen.go and registry/damagetypeids_gen.go: typed IDs, tags and each damage type's message_id, scaling, exhaustion, death_message_type and effects",
		run:  runDamageTypes,
	})
}

// The generated names are the registry package's DamageScaling,
// DeathMessageType and DamageEffects constants.
var (
	damageScalings = map[string]string{
		"never":                            "DamageScalesNever",
		"when_caused_by_living_non_player": "DamageScalesWhenCausedByLivingNonPlayer",
		"always":                           "DamageScalesAlways",
	}
	deathMessageTypes = map[string]string{
		"default":                 "DeathMessageDefault",
		"fall_variants":           "DeathMessageFallVariants",
		"intentional_game_design": "DeathMessageIntentionalGameDesign",
	}
	damageEffects = map[string]string{
		"hurt":     "DamageEffectsHurt",
		"thorns":   "DamageEffectsThorns",
		"drowning": "DamageEffectsDrowning",
		"burning":  "DamageEffectsBurning",
		"poking":   "DamageEffectsPoking",
		"freezing": "DamageEffectsFreezing",
	}
)

func runDamageTypes(e *env) error {
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()

	const dir = "minecraft/damage_type"
	files, err := vanilla.Files(dp, dir, ".json")
	if err != nil {
		return err
	}
	f := e.file("registry", "by tools/gen damagetypes from the damage_type definitions of the vanilla {version} data pack")
	f.WriteString(`// DamageTypes holds the fields of each vanilla damage type the server acts on,
// by registry name.
// vanilla: DamageType
var DamageTypes = map[string]DamageTypeInfo{
`)
	for _, rel := range files {
		var t struct {
			MessageID        string  `json:"message_id"`
			Scaling          string  `json:"scaling"`
			Exhaustion       float64 `json:"exhaustion"`
			DeathMessageType string  `json:"death_message_type"`
			Effects          string  `json:"effects"`
		}
		if err := vanilla.ReadJSON(dp, path.Join(dir, rel), &t); err != nil {
			return err
		}
		name := "minecraft:" + strings.TrimSuffix(rel, ".json")
		scaling, ok := damageScalings[t.Scaling]
		if !ok {
			return fmt.Errorf("%s: unknown scaling %q", name, t.Scaling)
		}
		if t.DeathMessageType == "" {
			t.DeathMessageType = "default"
		}
		death, ok := deathMessageTypes[t.DeathMessageType]
		if !ok {
			return fmt.Errorf("%s: unknown death_message_type %q", name, t.DeathMessageType)
		}
		if t.Effects == "" {
			t.Effects = "hurt"
		}
		effects, ok := damageEffects[t.Effects]
		if !ok {
			return fmt.Errorf("%s: unknown effects %q", name, t.Effects)
		}
		f.Printf("\t%s: {MessageID: %s, Scaling: %s, Exhaustion: %s, DeathMessage: %s, Effects: %s},\n",
			gogen.Quote(name), gogen.Quote(t.MessageID), scaling, gogen.Float(t.Exhaustion), death, effects)
	}
	f.WriteString("}\n")
	e.logf("damage types: %d", len(files))
	if err := e.emit("registry/damagetypes_gen.go", f); err != nil {
		return err
	}
	names, err := syncedEntries(e, "minecraft:damage_type")
	if err != nil {
		return err
	}
	constants, err := idents("DamageType", names)
	if err != nil {
		return err
	}
	f = e.file("registry", "by tools/gen damagetypes from the vanilla {version} damage registry and tags")
	f.WriteString("// The damage types, with zero reserved for an absent source.\nconst (\n")
	for i, ident := range constants {
		f.Printf("\t%s DamageType = %d\n", ident, i+1)
	}
	f.Printf(")\n\nconst damageTypeCount = %d\n\nvar damageTypeNames = [...]string{\n\t\"\",\n", len(names)+1)
	for _, name := range names {
		f.Printf("\t%s,\n", gogen.Quote(name))
	}
	f.WriteString("}\n\n")
	tags, err := vanilla.Files(dp, "minecraft/tags/damage_type", ".json")
	if err != nil {
		return err
	}
	f.WriteString("// Damage type tag IDs keep their sorted positions across reloads.\nconst (\n")
	for i, tag := range tags {
		name, err := ident("DamageTypeTagID", "minecraft:"+strings.TrimSuffix(tag, ".json"))
		if err != nil {
			return err
		}
		f.Printf("\t%s DamageTypeTagID = %d\n", name, i)
	}
	f.WriteString(")\n\nvar baseDamageTypeTagNames = []string{\n")
	for _, tag := range tags {
		f.Printf("\t%s,\n", gogen.Quote("minecraft:"+strings.TrimSuffix(tag, ".json")))
	}
	f.WriteString("}\n")
	return e.emit("registry/damagetypeids_gen.go", f)
}

func damageTypeIdent(name string) string {
	id, err := ident("DamageType", name)
	if err != nil {
		panic(err)
	}
	return id
}

func damageTypeTagIdent(name string) string {
	id, err := ident("DamageTypeTagID", name)
	if err != nil {
		fail("damage type tag %s: %v", name, err)
	}
	return id
}
