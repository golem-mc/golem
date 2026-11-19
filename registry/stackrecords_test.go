package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// componentsAwaitingReaders are the persistent components whose readers are
// planned in the named issue; a reader that lands must leave this list.
var componentsAwaitingReaders = map[string]int{}

// TestEveryPersistentComponentIsModelled fails for a component the stored
// stack reader does not know, which /give and a save would refuse silently.
func TestEveryPersistentComponentIsModelled(t *testing.T) {
	t.Parallel()
	for name := range ItemComponents {
		if TransientItemComponents[name] {
			continue
		}
		// A reader may be modelled for one shape only, so try each.
		modeled := false
		for _, probe := range []nbt.Tag{nbt.Compound{}, nbt.Int(0), nbt.String(""), nbt.List{Elem: nbt.TypeCompound}} {
			_, known := readComponentKnown(new(protocol.ItemStack), name, probe)
			modeled = modeled || known
		}
		issue, awaiting := componentsAwaitingReaders[name]
		switch {
		case !modeled && !awaiting:
			t.Errorf("%s has no reader in readComponentKnown", name)
		case modeled && awaiting:
			t.Errorf("%s has a reader: drop it from componentsAwaitingReaders (#%d)", name, issue)
		}
	}
}

func TestRecordComponentsRoundTrip(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, in, want string }{
		{"use_effects", "{}", "{}"},
		{"use_effects", "{can_sprint:true,interact_vibrations:false,speed_multiplier:0.5f}", "{can_sprint:1b,interact_vibrations:0b,speed_multiplier:0.5f}"},
		{"attack_animation", "{type:'stab',duration:12}", "{duration:12,type:\"stab\"}"},
		{"interact_animation", "{}", "{}"},
		{"kinetic_weapon", "{delay_ticks:4,damage_conditions:{max_duration_ticks:30,min_speed:1.5f},sound:'minecraft:item.trident.throw',forward_movement:0.5f}",
			"{damage_conditions:{max_duration_ticks:30,min_speed:1.5f},delay_ticks:4,forward_movement:0.5f,sound:\"minecraft:item.trident.throw\"}"},
		{"piercing_weapon", "{deals_knockback:false,dismounts:true,hit_sound:{sound_id:'pk:hit',range:8f}}",
			"{deals_knockback:0b,dismounts:1b,hit_sound:{range:8.0f,sound_id:\"pk:hit\"}}"},
		{"cooking_fuel", "{burn_time:200,speed_multiplier:2.0f}", "{burn_time:200,speed_multiplier:2.0f}"},
		{"cooking_fuel", "{burn_time:'pk:burn',speed_multiplier:'pk:speed'}", "{burn_time:\"pk:burn\",speed_multiplier:\"pk:speed\"}"},
		{"brewing_fuel", "{uses:3,speed_multiplier:1.5f}", "{speed_multiplier:1.5f,uses:3}"},
		{"villager_food", "{nutrition:4}", "{nutrition:4}"},
		{"mob_visibility", "{targeting_entity_types:'minecraft:zombie',visibility:0.5f}", "{targeting_entity_types:\"minecraft:zombie\",visibility:0.5f}"},
		{"mob_visibility", "{targeting_entity_types:['minecraft:zombie','minecraft:husk'],visibility:10f}", "{targeting_entity_types:[\"minecraft:zombie\",\"minecraft:husk\"],visibility:10.0f}"},
	} {
		key := "minecraft:" + c.name
		value := snbtValue(t, c.in)
		stack, ok := StoredStack(nbt.Compound{"id": nbt.String("minecraft:stick"), "count": nbt.Int(1), "components": nbt.Compound{key: value}})
		if !ok {
			t.Errorf("%s %s refused", c.name, c.in)
			continue
		}
		if got := nbt.Stringify(StackComponents(stack)[key]); got != c.want {
			t.Errorf("%s %s stored as %s, want %s", c.name, c.in, got, c.want)
		}
		back, ok := StoredStack(StackTag(stack))
		if !ok || !stack.Equal(back) {
			t.Errorf("%s save round trip = %+v, %v", c.name, back, ok)
		}
		var b protocol.Buffer
		b.PutSlot(stack)
		wire, ok := protocol.NewReader(b.Bytes()).ReadSlot()
		if !ok || !stack.Equal(wire) {
			t.Errorf("%s wire round trip = %+v, %v", c.name, wire, ok)
		}
	}
}

func TestRecordComponentRemoval(t *testing.T) {
	t.Parallel()
	stack, ok := StoredStack(nbt.Compound{"id": nbt.String("minecraft:stick"), "count": nbt.Int(1), "components": nbt.Compound{"!minecraft:cooking_fuel": nbt.Compound{}}})
	if !ok || !stack.Removes(protocol.DataComponentCookingFuel) {
		t.Fatalf("removal = %+v, %v", stack, ok)
	}
}

func TestRecordComponentMalformed(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, in, want string }{
		{"cooking_fuel", "{}", "No key speed_multiplier in MapLike[{}]; No key burn_time in MapLike[{}]"},
		{"cooking_fuel", "{burn_time:{},speed_multiplier:1f}", "Failed to parse either. First: Not a number; Second: Not a string"},
		{"cooking_fuel", "5", "Not a map: 5"},
		{"villager_food", "{nutrition:0}", "Value must be positive: 0"},
		{"use_effects", "{speed_multiplier:2f}", "Value must be within range [0.0;1.0]: 2.0"},
		{"attack_animation", "{type:'spin',duration:-1}", "Value must be non-negative: -1; Unknown element name:spin"},
		{"kinetic_weapon", "{damage_conditions:{}}", "No key max_duration_ticks in MapLike[{}]"},
	} {
		value := snbtValue(t, c.in)
		if got := ComponentValueMalformed("minecraft:"+c.name, value); got != c.want {
			t.Errorf("%s %s = %q, want %q", c.name, c.in, got, c.want)
		}
		if ReadComponent(new(protocol.ItemStack), "minecraft:"+c.name, value) {
			t.Errorf("%s %s accepted", c.name, c.in)
		}
	}
}

func snbtValue(t *testing.T, text string) nbt.Tag {
	t.Helper()
	wrapped, err := nbt.ParseSNBT("{v:" + text + "}")
	if err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	return wrapped["v"]
}
