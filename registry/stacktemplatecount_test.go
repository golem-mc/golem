package registry

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func countlessEntry(id string) nbt.Compound { return nbt.Compound{"id": nbt.String(id)} }

func TestTemplateStacksLeaveOutACountOfOne(t *testing.T) {
	t.Parallel()
	dirt := nbt.Compound{"id": nbt.String("minecraft:dirt"), "count": nbt.Int(5)}
	list := func(entries ...nbt.Tag) nbt.List { return nbt.List{Elem: nbt.TypeCompound, Items: entries} }
	slot := func(i int, item nbt.Compound) nbt.Compound {
		return nbt.Compound{"slot": nbt.Int(i), "item": item}
	}
	for name, value := range map[string]nbt.Tag{
		"minecraft:bundle_contents":     list(countlessEntry("minecraft:stone"), dirt),
		"minecraft:charged_projectiles": list(countlessEntry("minecraft:arrow"), dirt),
		"minecraft:container":           list(slot(0, countlessEntry("minecraft:stone")), slot(1, dirt)),
		"minecraft:use_remainder":       countlessEntry("minecraft:bowl"),
		"minecraft:sulfur_cube_content": countlessEntry("minecraft:stone"),
	} {
		stack, ok := StoredStack(nbt.Compound{
			"id": nbt.String("minecraft:stick"), "count": nbt.Int(1),
			"components": nbt.Compound{name: value},
		})
		if !ok {
			t.Fatalf("%s: count-less entry refused", name)
		}
		got := StackTag(stack).Entries()["components"].(nbt.CompoundTag).Entries()[name]
		if name == "minecraft:use_remainder" || name == "minecraft:sulfur_cube_content" {
			if _, has := got.(nbt.CompoundTag).Entries()["count"]; has {
				t.Errorf("%s writes a count of 1", name)
			}
			continue
		}
		items := got.(nbt.List).Items
		for i, it := range items {
			entry := it.(nbt.CompoundTag).Entries()
			if inner, ok := entry["item"].(nbt.CompoundTag); ok {
				entry = inner.Entries()
			}
			count, has := entry["count"]
			if i == 0 && has {
				t.Errorf("%s: count-1 entry writes count %v", name, count)
			}
			if i == 1 && count != nbt.Int(5) {
				t.Errorf("%s: count-5 entry writes %v", name, count)
			}
		}
		back, ok := StoredStack(StackTag(stack))
		if !ok || !stack.Equal(back) {
			t.Errorf("%s: round trip = %v", name, ok)
		}
	}
}
