package main

import (
	"fmt"
	"regexp"
	"strings"
)

func init() {
	register(generator{
		name: "entitycategories",
		doc:  "registry/entitycategories_gen.go: the MobCategory of every entity type",
		run:  runEntityCategories,
	})
}

// entityCategoryDecl matches one registration in EntityTypes: the type's
// EntityTypeIds constant and the MobCategory its builder names.
var entityCategoryDecl = regexp.MustCompile(`register\(EntityTypeIds\.([A-Z0-9_]+), EntityType\.Builder\.(?:of\(.*?, |createNothing\()(?:\(MobCategory\))?MobCategory\.([A-Z_]+)\)`)

func runEntityCategories(e *env) error {
	jar, err := e.ref.ClientJar()
	if err != nil {
		return err
	}
	text, err := e.decompile(jar, "net/minecraft/world/entity/EntityTypes.class")
	if err != nil {
		return err
	}
	categories := map[string]string{}
	for _, m := range entityCategoryDecl.FindAllStringSubmatch(text, -1) {
		categories[strings.ToLower(m[1])] = m[2]
	}
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	names, err := registries.NamesByID("minecraft:entity_type")
	if err != nil {
		return err
	}
	f := e.file("registry", "from EntityTypes in the client jar of Minecraft {version}")
	f.WriteString("// entityTypeCategories is each entity type's MobCategory, indexed by\n" +
		"// entity type ID.\nvar entityTypeCategories = [...]MobCategory{\n")
	for _, name := range names {
		c, ok := categories[strings.TrimPrefix(name, "minecraft:")]
		if !ok {
			return fmt.Errorf("entitycategories: no MobCategory found for %s in EntityTypes", name)
		}
		f.Printf("\tMobCategory%s,\n", camel(c))
	}
	f.WriteString("}\n")
	return e.emit("registry/entitycategories_gen.go", f)
}

// camel turns MOB_CATEGORY into MobCategory.
func camel(upper string) string {
	var b strings.Builder
	for part := range strings.SplitSeq(upper, "_") {
		b.WriteString(part[:1] + strings.ToLower(part[1:]))
	}
	return b.String()
}
