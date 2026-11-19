package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
)

func init() {
	register(generator{
		name: "equippable",
		doc:  "registry/equippable_gen.go: the slot of every item that can be worn",
		run:  runEquippable,
	})
}

// Every item whose minecraft:equippable component names a slot is written out with
// the component's swappable flag. The flag separates the two ways an item goes on:
// right-click equips only what the flag allows (a shield or carved pumpkin stays in
// the hand), while shift-click and the armour slots go by the slot alone, as
// InventoryMenu does. Body and saddle slots belong to animals and are included so a
// mob can tell the slots its kind cannot use.
func runEquippable(e *env) error {
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	sounds := registeredSoundHolders(registries)
	dir := e.ref.ReportPath("minecraft/components/item")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("%w; run `mise run reference:reports`", err)
	}
	type worn struct {
		slot      string
		swappable bool
		allowed   string
		sound     string
		sheared   bool
		shearing  string
		// dispensable, damageOnHurt and equipOnInteract, in that order
		flags [3]bool
	}
	items := map[string]worn{}
	bySlot := map[string]int{}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		var report struct {
			Components map[string]json.RawMessage `json:"components"`
		}
		if err := json.Unmarshal(b, &report); err != nil {
			return fmt.Errorf("%s: %w", entry.Name(), err)
		}
		var equippable map[string]json.RawMessage
		if raw, ok := report.Components["minecraft:equippable"]; ok {
			if err := json.Unmarshal(raw, &equippable); err != nil {
				return fmt.Errorf("%s: equippable: %w", entry.Name(), err)
			}
		}
		if len(equippable) == 0 {
			continue
		}
		name := "minecraft:" + strings.TrimSuffix(entry.Name(), ".json")
		var slot string
		if err := json.Unmarshal(equippable["slot"], &slot); err != nil {
			return fmt.Errorf("%s: equippable slot: %w", name, err)
		}
		// swappable defaults to true, and decides whether using the item puts
		// it on rather than doing whatever else it does.
		swappable := true
		if raw, ok := equippable["swappable"]; ok {
			if err := json.Unmarshal(raw, &swappable); err != nil {
				return fmt.Errorf("%s: equippable swappable: %w", name, err)
			}
		}
		allowed, err := allowedEntities(equippable["allowed_entities"])
		if err != nil {
			return fmt.Errorf("%s: equippable allowed_entities: %w", name, err)
		}
		// equip_sound defaults to the generic armor sound.
		sound := "minecraft:item.armor.equip_generic"
		if raw, ok := equippable["equip_sound"]; ok {
			if err := json.Unmarshal(raw, &sound); err != nil {
				return fmt.Errorf("%s: equippable equip_sound: %w", name, err)
			}
		}
		// can_be_sheared defaults to false, shearing_sound to the shears' snip.
		sheared := false
		if raw, ok := equippable["can_be_sheared"]; ok {
			if err := json.Unmarshal(raw, &sheared); err != nil {
				return fmt.Errorf("%s: equippable can_be_sheared: %w", name, err)
			}
		}
		shearing := "minecraft:item.shears.snip"
		if raw, ok := equippable["shearing_sound"]; ok {
			if err := json.Unmarshal(raw, &shearing); err != nil {
				return fmt.Errorf("%s: equippable shearing_sound: %w", name, err)
			}
		}
		// dispensable and damage_on_hurt default to true, equip_on_interact
		// to false.
		flags := [3]bool{true, true, false}
		for i, key := range []string{"dispensable", "damage_on_hurt", "equip_on_interact"} {
			if raw, ok := equippable[key]; ok {
				if err := json.Unmarshal(raw, &flags[i]); err != nil {
					return fmt.Errorf("%s: equippable %s: %w", name, key, err)
				}
			}
		}
		items[name] = worn{slot, swappable, allowed, sound, sheared, shearing, flags}
		bySlot[slot]++
	}

	f := e.file("registry", "from the item reports of a vanilla {version} server")
	f.WriteString("import \"github.com/golem-mc/golem/internal/protocol\"\n\n")
	f.WriteString(`// EquippableItem is what an item's minecraft:equippable component says as far
// as the server needs it: the slot the item goes in, and whether using it
// puts it on.
type EquippableItem struct {
	Slot      string
	Swappable bool
	// Entities is the allowed_entities component: an entity type, a
	// comma-separated list of them or a #tag; empty allows every entity.
	Entities string
	// EquipSound is the equip_sound component, played when a mob or stand is
	// dressed in the item.
	EquipSound protocol.SoundHolder
	// CanBeSheared is can_be_sheared: shears take the piece off a mob
	// wearing it, playing ShearingSound.
	CanBeSheared  bool
	ShearingSound protocol.SoundHolder
	// Dispensable is dispensable: a dispenser puts the piece on an entity
	// in front of it.
	Dispensable bool
	// DamageOnHurt is damage_on_hurt: the worn piece wears when its wearer
	// is hurt.
	DamageOnHurt bool
	// EquipOnInteract is equip_on_interact: using the item on a mob puts it
	// on.
	EquipOnInteract bool
}

// Equippable is that, for every item whose component names a slot, the
// animals' body and saddle slots included: a mob reads the slot to tell the
// pieces it holds or wears from those its kind cannot use, and the player
// code reads it through the player equipment slots alone. The swappable flag
// separates the two ways an item goes on: a shield names the off hand but is
// not swappable, so a shift-click stows it in the off hand while right-clicking
// raises it where it is, and a carved pumpkin shifts onto the head but is not
// put there by being used.
var Equippable = map[string]EquippableItem{
`)
	for _, name := range sortedKeys(items) {
		w := items[name]
		f.Printf("\t%s: {Slot: %s, Swappable: %t, Entities: %s, EquipSound: %s, CanBeSheared: %t, ShearingSound: %s, Dispensable: %t, DamageOnHurt: %t, EquipOnInteract: %t},\n", gogen.Quote(name), gogen.Quote(w.slot), w.swappable, gogen.Quote(w.allowed), sounds.holder(w.sound), w.sheared, sounds.holder(w.shearing), w.flags[0], w.flags[1], w.flags[2])
	}
	f.WriteString("}\n")
	e.logf("equippable items: %d by slot: %v", len(items), bySlot)
	return e.emit("registry/equippable_gen.go", f)
}

// allowedEntities reads an equippable's allowed_entities, a single entry
// (a type or a #tag) or a list of types, as one string; a missing field is
// the empty string, which allows every entity.
func allowedEntities(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		return one, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return "", err
	}
	return strings.Join(many, ","), nil
}
