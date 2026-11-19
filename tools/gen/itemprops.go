package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func init() {
	register(generator{
		name: "itemprops",
		doc:  "registry/itemprops_gen.go: stack sizes, durability, tools, food and combat data per item",
		run:  runItemProps,
	})
}

// One JSON file per item in the reports holds its default data components: stack
// size, durability, food, tool rules, attribute modifiers. This moves them into Go
// in item registry order, so the table follows the wire IDs.
//
// Tool rules name the blocks they cover (a block, a list, or a # tag, which may
// hold tags). Tags are expanded into block-name sets, and sets several rules share
// are written once. Rules keep their order: the first that covers a block decides.
// Whether a block needs the correct tool at all is the block's own fact (blockdrops
// reads it beside the hardness).

// attributeOperations are AttributeModifier.Operation's names, as the
// registry's constants for them.
var attributeOperations = map[string]string{
	"add_value":            "AttributeAddValue",
	"add_multiplied_base":  "AttributeAddMultipliedBase",
	"add_multiplied_total": "AttributeAddMultipliedTotal",
}

const defaultConsumeSound = "minecraft:entity.generic.eat"

// blockTagMembers lists the blocks of a block tag, following the # tag
// references inside it. The reference is what a rule or another tag names one
// by: a # followed by the tag's own name, or by a fully qualified minecraft:
// one.
func blockTagMembers(dp *vanilla.DataPack, reference string, seen []string) []string {
	name := strings.TrimPrefix(strings.TrimPrefix(reference, "#"), "minecraft:")
	if slices.Contains(seen, name) {
		fail("the block tag %s contains itself", name)
	}
	seen = append(slices.Clone(seen), name)
	tagFile := path.Join("minecraft/tags/block", name+".json")
	var tag obj
	b, err := dpRead(dp, tagFile)
	if err != nil {
		fail("%v", err)
	}
	tag = decode(b, tagFile)
	var members []string
	for _, v := range tag.list("values") {
		value := asStr(v, "a tag value")
		if strings.HasPrefix(value, "#") {
			members = append(members, blockTagMembers(dp, value, seen)...)
		} else {
			members = append(members, qualified(value))
		}
	}
	return members
}

func effectSpec(effect obj) string {
	// A MobEffectInstance as a component writes it, with the codec's
	// defaults: show_icon follows show_particles when left out.
	particles := effect.boolOr("show_particles", true)
	return fmt.Sprintf("{Effect: %s, Amplifier: %d, Duration: %d, Ambient: %t, ShowParticles: %t, ShowIcon: %t}",
		gogen.Quote(effect.str("id")), effect.intOr("amplifier", 0), effect.intOr("duration", 0),
		effect.boolOr("ambient", false), particles, effect.boolOr("show_icon", particles))
}

// consumeEffect writes one on_consume_effects entry as a ConsumeEffect literal.
func (r *itemPropsReader) consumeEffect(item string, effect obj) string {
	switch effect.str("type") {
	case "minecraft:apply_effects":
		var specs []string
		for _, v := range effect.list("effects") {
			specs = append(specs, effectSpec(asObj(v, "an effect")))
		}
		return fmt.Sprintf("{Type: ApplyEffects, Probability: %s, Effects: []EffectSpec{%s}}",
			gogen.Float(effect.numOr("probability", 1.0)), strings.Join(specs, ", "))
	case "minecraft:remove_effects":
		var names []string
		switch v := effect["effects"].(type) {
		case string:
			names = []string{v}
		case []any:
			for _, n := range v {
				names = append(names, asStr(n, "an effect name"))
			}
		default:
			fail("%s removes effects of a shape the generator does not read: %v", item, v)
		}
		quoted := make([]string, len(names))
		for i, n := range names {
			if strings.HasPrefix(n, "#") {
				fail("%s removes an effect tag, which the generator does not expand", item)
			}
			quoted[i] = gogen.Quote(qualified(n))
		}
		return fmt.Sprintf("{Type: RemoveEffects, Remove: []string{%s}}", strings.Join(quoted, ", "))
	case "minecraft:clear_all_effects":
		return "{Type: ClearAllEffects}"
	case "minecraft:teleport_randomly":
		return fmt.Sprintf("{Type: TeleportRandomly, Diameter: %s, Directional: %t}", gogen.Float(effect.numOr("diameter", 16.0)), effect.boolOr("directional_particles", true))
	case "minecraft:play_sound":
		return fmt.Sprintf("{Type: PlaySound, Sound: %s}", r.sounds.holder(effect.str("sound")))
	}
	fail("%s has a consume effect the generator does not read: %v", item, map[string]any(effect))
	return ""
}

// goName is an item's name as a Go identifier: each underscore-separated part
// capitalised, the rest lowered.
func goName(name string) string {
	var b strings.Builder
	for part := range strings.SplitSeq(strings.TrimPrefix(name, "minecraft:"), "_") {
		if part != "" {
			b.WriteString(strings.ToUpper(part[:1]) + strings.ToLower(part[1:]))
		}
	}
	return b.String()
}

type attributeModifier struct{ attr, id, amount, op, slot string }

type toolRule struct {
	set     int
	speed   *float64
	correct *bool
}

type toolSpec struct {
	rules        []toolRule
	defaultSpeed float64
	perBlock     int
	creative     bool
}

type foodSpec struct {
	nutrition      int
	saturation     string
	canAlwaysEat   bool
	consumeSeconds string
}

type consumableSpec struct {
	seconds   string
	effects   []string
	remainder string
	sound     string
	drink     bool
}

// itemPropsReader reads the item component reports into the tables the
// generated file is made of.
type itemPropsReader struct {
	dp          *vanilla.DataPack
	blockReport vanilla.Blocks
	sounds      soundHolders

	// The expanded block sets the tool rules name, deduplicated so that a set
	// several rules share is written out once and pointed at by each of them.
	blockSets     [][]string
	blockSetIndex map[string]int

	// Tools and foods become named values of their own so the table reads as
	// one line per item; everything else is a number.
	tools            map[string]toolSpec
	foods            map[string]foodSpec
	consumables      map[string]consumableSpec
	deathProtections map[string][]string
}

type itemEntry struct {
	name  string
	parts []string
	multi bool
}

func (r *itemPropsReader) blockSet(names []string) int {
	members := slices.Clone(names)
	slices.Sort(members)
	members = slices.Compact(members)
	key := strings.Join(members, "\x00")
	if i, ok := r.blockSetIndex[key]; ok {
		return i
	}
	for _, m := range members {
		if _, ok := r.blockReport.Get(m); !ok {
			fail("%s is named by a tool rule but not in the block report", m)
		}
	}
	r.blockSetIndex[key] = len(r.blockSets)
	r.blockSets = append(r.blockSets, members)
	return r.blockSetIndex[key]
}

// ruleBlocks are the block names one tool rule covers: an entry that is a tag
// is expanded, an entry that is a block is qualified. A rule may cover nothing
// at all: the incorrect_for_diamond_tool tag is empty in this version, a
// diamond tool being wrong for no block. A rule that matches no block decides
// nothing, which is what an empty set says.
func (r *itemPropsReader) ruleBlocks(blocks any) []string {
	var entries []string
	switch v := blocks.(type) {
	case string:
		entries = []string{v}
	case []any:
		for _, x := range v {
			entries = append(entries, asStr(x, "a tool rule block"))
		}
	default:
		fail("a tool rule has blocks of a shape the generator does not read: %v", blocks)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry, "#") {
			names = append(names, blockTagMembers(r.dp, entry, nil)...)
		} else {
			names = append(names, qualified(entry))
		}
	}
	return names
}

func (r *itemPropsReader) tool(tool obj) toolSpec {
	// A rule may leave out speed or correct_for_drops; vanilla's
	// getMiningSpeed and isCorrectForDrops then skip it for that question.
	spec := toolSpec{
		defaultSpeed: tool.numOr("default_mining_speed", 1.0),
		perBlock:     tool.intOr("damage_per_block", 1),
		creative:     tool.boolOr("can_destroy_blocks_in_creative", true),
	}
	for _, v := range tool.list("rules") {
		rule := asObj(v, "a tool rule")
		rl := toolRule{set: r.blockSet(r.ruleBlocks(rule["blocks"]))}
		if rule.has("speed") {
			s := rule.num("speed")
			rl.speed = &s
		}
		if rule.has("correct_for_drops") {
			c := rule.boolOr("correct_for_drops", false)
			rl.correct = &c
		}
		spec.rules = append(spec.rules, rl)
	}
	return spec
}

func attributeModifiers(name string, components obj) []attributeModifier {
	var modifiers []attributeModifier
	for _, v := range components.listOr("minecraft:attribute_modifiers") {
		// Every modifier, in the component's order: an equipped item's go onto
		// the wearer's attributes in that order. The display only decides the
		// tooltip, which is the client's.
		mod := asObj(v, "an attribute modifier")
		op, known := attributeOperations[mod.str("operation")]
		if !known {
			fail("%s has a modifier of an unknown operation: %v", name, map[string]any(mod))
		}
		modifiers = append(modifiers, attributeModifier{
			qualified(mod.str("type")), qualified(mod.str("id")),
			gogen.Float(mod.num("amount")), op, mod.strOr("slot", "any"),
		})
	}
	return modifiers
}

// combatParts are the literals of the attack range, the piercing weapon, the
// kinetic weapon, the minimum attack charge and the damage type, in the order the table lists them.
func (r *itemPropsReader) combatParts(components obj) []string {
	var parts []string
	if ar, ok := components.obj("minecraft:attack_range"); ok {
		// The codec's defaults, vanilla's AttackRange.CODEC.
		var fields []string
		for _, kd := range []struct {
			key, field string
			def        float64
		}{{"min_reach", "MinReach", 0.0}, {"max_reach", "MaxReach", 3.0}, {"min_creative_reach", "MinCreativeReach", 0.0},
			{"max_creative_reach", "MaxCreativeReach", 5.0}, {"hitbox_margin", "HitboxMargin", 0.3}, {"mob_factor", "MobFactor", 1.0}} {
			fields = append(fields, kd.field+": "+gogen.Float(ar.numOr(kd.key, kd.def)))
		}
		parts = append(parts, fmt.Sprintf("AttackRange: &AttackRange{%s}", strings.Join(fields, ", ")))
	}
	if pw, ok := components.obj("minecraft:piercing_weapon"); ok {
		// The codec's defaults, vanilla's PiercingWeapon.CODEC.
		sound, hitSound := "SoundNone", "SoundNone"
		if pw.has("sound") {
			sound = r.sounds.sound(pw.str("sound"))
		}
		if pw.has("hit_sound") {
			hitSound = r.sounds.sound(pw.str("hit_sound"))
		}
		parts = append(parts, fmt.Sprintf("PiercingWeapon: &PiercingWeapon{DealsKnockback: %t, Dismounts: %t, Sound: %s, HitSound: %s}",
			pw.boolOr("deals_knockback", true), pw.boolOr("dismounts", false), sound, hitSound))
	}
	if kw, ok := components.obj("minecraft:kinetic_weapon"); ok {
		// The codec's defaults, vanilla's KineticWeapon.CODEC.
		cond := func(key string) string {
			c, ok := kw.obj(key)
			if !ok {
				return "nil"
			}
			return fmt.Sprintf("&KineticCondition{MaxDuration: %d, MinSpeed: %s, MinRelativeSpeed: %s}",
				c.intOr("max_duration_ticks", 0), gogen.Float(c.numOr("min_speed", 0)), gogen.Float(c.numOr("min_relative_speed", 0)))
		}
		kwSound := func(key string) string {
			if !kw.has(key) {
				return "SoundNone"
			}
			return r.sounds.sound(kw.str(key))
		}
		parts = append(parts, fmt.Sprintf("KineticWeapon: &KineticWeapon{ContactCooldown: %d, Delay: %d, Dismount: %s, Knockback: %s, Damage: %s, DamageMultiplier: %s, Sound: %s, HitSound: %s}",
			kw.intOr("contact_cooldown_ticks", 10), kw.intOr("delay_ticks", 0),
			cond("dismount_conditions"), cond("knockback_conditions"), cond("damage_conditions"),
			gogen.Float(kw.numOr("damage_multiplier", 1)),
			kwSound("sound"), kwSound("hit_sound")))
	}
	if anim, ok := components.obj("minecraft:attack_animation"); ok && (anim.has("type") || anim.has("duration")) {
		// The codec's defaults, vanilla's SwingAnimation.CODEC.
		kind := map[string]int{"none": 0, "whack": 1, "stab": 2}
		t, known := kind[strings.TrimPrefix(anim.strOr("type", "whack"), "minecraft:")]
		if !known {
			fail("%s has an attack animation type the generator does not know: %v", "an item", anim["type"])
		}
		parts = append(parts, fmt.Sprintf("AttackAnimation: &SwingAnimation{Type: %d, Duration: %d}", t, anim.intOr("duration", 6)))
	}
	if components.has("minecraft:minimum_attack_charge") {
		parts = append(parts, "MinimumAttackCharge: "+gogen.Float(components.num("minecraft:minimum_attack_charge")))
	}
	if components.has("minecraft:damage_type") {
		parts = append(parts, "DamageType: "+damageTypeIdent(qualified(components.str("minecraft:damage_type"))))
	}
	return parts
}

// meal reads the food and the consumable of an item, recording the ones that
// need a value of their own. A meal with nothing but its seconds needs no
// Consumable of its own: ConsumableOf reads those off the Food.
func (r *itemPropsReader) meal(name string, components obj) (inConsumables bool) {
	food, hasFood := components.obj("minecraft:food")
	if hasFood {
		consumable, _ := components.obj("minecraft:consumable")
		r.foods[name] = foodSpec{
			nutrition:      int(food.num("nutrition")),
			saturation:     gogen.Float(food.num("saturation")),
			canAlwaysEat:   food.boolOr("can_always_eat", false),
			consumeSeconds: gogen.Float(consumable.numOr("consume_seconds", 1.6)),
		}
	}
	consumable, ok := components.obj("minecraft:consumable")
	if !ok {
		return false
	}
	remainder := ""
	if ur, ok := components.obj("minecraft:use_remainder"); ok {
		remainder = ur.strOr("id", "")
		if remainder == "" || ur.numOr("count", 1) != 1 || len(ur) != 1 {
			fail("%s has a use_remainder the generator does not read: %v", name, ur)
		}
	}
	var effects []string
	for _, v := range consumable.listOr("on_consume_effects") {
		effects = append(effects, r.consumeEffect(name, asObj(v, "a consume effect")))
	}
	c := consumableSpec{
		seconds:   gogen.Float(consumable.numOr("consume_seconds", 1.6)),
		effects:   effects,
		remainder: remainder,
		sound:     consumable.strOr("sound", defaultConsumeSound),
		drink:     consumable.strOr("animation", "eat") == "drink",
	}
	if hasFood && len(c.effects) == 0 && c.remainder == "" && c.sound == defaultConsumeSound && !c.drink {
		return false
	}
	r.consumables[name] = c
	return true
}

// item reads one item's components into its line of the table.
func (r *itemPropsReader) item(name string, components obj) itemEntry {
	parts := []string{fmt.Sprintf("MaxStack: %d", components.intOr("minecraft:max_stack_size", 64))}
	if components.has("minecraft:max_damage") {
		parts = append(parts, fmt.Sprintf("MaxDamage: %d", components.intOr("minecraft:max_damage", 0)))
	}
	if components.has("minecraft:block_transformer") {
		transformer := components.str("minecraft:block_transformer")
		switch transformer {
		case "minecraft:axe", "minecraft:hoe", "minecraft:shovel":
			parts = append(parts, "BlockTransformer: BlockTransformer"+goName(transformer))
		default:
			fail("%s has an unmodelled block transformer %s", name, transformer)
		}
	}
	if enchantable, ok := components.obj("minecraft:enchantable"); ok {
		parts = append(parts, fmt.Sprintf("Enchantable: %d", int(enchantable.num("value"))))
	}
	if weapon, ok := components.obj("minecraft:weapon"); ok {
		// The codec's defaults, vanilla's Weapon.CODEC.
		parts = append(parts, fmt.Sprintf("Weapon: &Weapon{DamagePerAttack: %d, DisableBlockingSeconds: %s}",
			weapon.intOr("item_damage_per_attack", 1), gogen.Float(weapon.numOr("disable_blocking_for_seconds", 0))))
	}
	modifiers := attributeModifiers(name, components)
	if len(modifiers) > 0 {
		lines := make([]string, len(modifiers))
		for i, m := range modifiers {
			lines[i] = fmt.Sprintf("\t\t{Attribute: Attribute%s, ID: %s, Amount: %s, Operation: %s, Slot: %s},",
				goName(m.attr), gogen.Quote(m.id), m.amount, m.op, gogen.Quote(m.slot))
		}
		parts = append(parts, "Modifiers: []AttributeModifier{\n"+strings.Join(lines, "\n")+"\n\t}")
	}
	if tool, ok := components.obj("minecraft:tool"); ok {
		r.tools[name] = r.tool(tool)
		parts = append(parts, fmt.Sprintf("Tool: %sTool", goName(name)))
	}
	inConsumables := r.meal(name, components)
	if _, ok := components.obj("minecraft:food"); ok {
		parts = append(parts, fmt.Sprintf("Food: %sFood", goName(name)))
	}
	if inConsumables {
		parts = append(parts, fmt.Sprintf("Consumable: %sConsumable", goName(name)))
	}
	return r.finish(name, components, parts, modifiers, inConsumables)
}

// finish appends what follows the tool, food and consumable on an item's line.
func (r *itemPropsReader) finish(name string, components obj, parts []string, modifiers []attributeModifier, inConsumables bool) itemEntry {
	if rep, ok := components.obj("minecraft:repairable"); ok {
		// The items the anvil repairs the item with: a single reference, a #
		// tag or a plain item, kept raw: the tag resolves at runtime against
		// the registry's own item tags, the plain name against the item
		// registry. Anything else in a report is refused rather than guessed
		// at.
		items, isString := rep["items"].(string)
		if !isString {
			fail("%s has a repairable of a shape the generator does not read: %v", name, rep["items"])
		}
		parts = append(parts, "Repairable: "+gogen.Quote(qualified(items)))
	}
	if ba, ok := components.obj("minecraft:blocks_attacks"); ok {
		parts = append(parts, "BlocksAttacks: "+r.blocksAttacksLiteral(name, ba))
	}
	// An item without use_effects posts no use vibration at all.
	// vanilla: ItemStack.causeUseVibration, UseEffects.DEFAULT
	if ue, ok := components.obj("minecraft:use_effects"); !ok || !ue.boolOr("interact_vibrations", true) {
		parts = append(parts, "NoInteractVibrations: true")
	}
	if components.has("minecraft:glider") {
		parts = append(parts, "Glider: true")
	}
	if dp, ok := components.obj("minecraft:death_protection"); ok {
		var effects []string
		for _, v := range dp.list("death_effects") {
			effects = append(effects, r.consumeEffect(name, asObj(v, "a death effect")))
		}
		r.deathProtections[name] = effects
		parts = append(parts, fmt.Sprintf("DeathProtection: %sDeathProtection", goName(name)))
	}
	parts = append(parts, r.combatParts(components)...)
	// The consumable rides on its food's line, so adding one does not break
	// the alignment of the table around it.
	consumableLine := 0
	if inConsumables {
		consumableLine = 1
	}
	return itemEntry{name, parts, len(parts)-consumableLine > 2 || len(modifiers) > 0}
}

func runItemProps(e *env) error {
	registries, err := e.ref.Registries()
	if err != nil {
		return err
	}
	itemRegistry, err := registries.NamesByID("minecraft:item")
	if err != nil {
		return err
	}
	// minecraft:damage is the component protocol.DataComponentDamage names.
	// The registry it belongs to is built into the client, so a version bump
	// that renumbers it has to move the constant too; regenerating after the
	// bump is what catches that.
	componentRegistry, ok := registries["minecraft:data_component_type"]
	if !ok {
		return fmt.Errorf("the registries report has no minecraft:data_component_type")
	}
	damage, ok := componentRegistry.Entries.Get("minecraft:damage")
	if !ok {
		return fmt.Errorf("minecraft:damage is not a data component in this version")
	}
	want, err := protocolConst(e, "DataComponentDamage")
	if err != nil {
		return err
	}
	if damage.ProtocolID != want {
		return fmt.Errorf("minecraft:damage is data component %d in this version; update protocol.DataComponentDamage in internal/protocol/ids.go", damage.ProtocolID)
	}
	blockReport, err := e.ref.Blocks()
	if err != nil {
		return err
	}
	dp, err := e.ref.DataPack()
	if err != nil {
		return err
	}
	defer dp.Close()

	r := &itemPropsReader{
		dp: dp, blockReport: blockReport, blockSetIndex: map[string]int{},
		sounds: registeredSoundHolders(registries),
		tools:  map[string]toolSpec{}, foods: map[string]foodSpec{},
		consumables: map[string]consumableSpec{}, deathProtections: map[string][]string{},
	}
	// One pass over every item's components, in the item registry's order.
	var props []itemEntry
	for _, name := range itemRegistry {
		// The report files are named by the item's own path; the namespace the
		// registry carries is left off them.
		file := filepath.Join(e.ref.ReportPath("minecraft/components/item"), name[strings.Index(name, ":")+1:]+".json")
		raw, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			return fmt.Errorf("%s is in the item registry but has no component report", name)
		} else if err != nil {
			return err
		}
		components, _ := decode(raw, file).obj("components")
		props = append(props, r.item(name, components))
	}
	f := e.file("registry", "from the item reports and block tags of a vanilla {version} server")
	r.write(f, props)
	e.logf("items with component properties: %d", len(props))
	e.logf("tools: %d, foods: %d, consumables: %d, death protections: %d, shared block sets: %d",
		len(r.tools), len(r.foods), len(r.consumables), len(r.deathProtections), len(r.blockSets))
	return e.emit("registry/itemprops_gen.go", f)
}

// write lays out the table and the named values it points at.
func (r *itemPropsReader) write(f *gogen.File, props []itemEntry) {
	f.WriteString("import \"github.com/golem-mc/golem/internal/protocol\"\n\n")
	f.WriteString(`// The per-item component data the lookups in itemprops.go answer from:
// everything a stack's default data components decide about it. The table
// follows the item registry's order; the named values below hold the tools
// and the foods, which are the only parts longer than a line.

// ItemProperties holds what an item's default data components say about it,
// indexed by item name. See itemprops.go for the fields' meaning; the values
// here are the components' own, read out of the vanilla server's report.
var ItemProperties = map[string]ItemProps{
`)
	for _, it := range props {
		if it.multi {
			f.Printf("\t%s: {\n\t\t%s,\n\t},\n", gogen.Quote(it.name), strings.Join(it.parts, ",\n\t\t"))
		} else {
			f.Printf("\t%s: {%s},\n", gogen.Quote(it.name), strings.Join(it.parts, ", "))
		}
	}
	f.WriteString("}\n")

	for _, name := range sortedKeys(r.tools) {
		tool, gn := r.tools[name], goName(name)
		f.Printf(`
// %sTool is the tool component of %s: the rules naming
// the blocks it mines faster and is the right tool for - the first one
// covering a block is the one that decides - and, by its damage per block,
// what breaking one costs the item.
var %sTool = &Tool{
`, gn, name, gn)
		if len(tool.rules) > 0 {
			f.WriteString("\tRules: []ToolRule{\n")
			for _, rl := range tool.rules {
				fields := []string{fmt.Sprintf("Blocks: &itemBlockSet%d", rl.set)}
				if rl.speed != nil {
					fields = append(fields, "Speed: "+gogen.Float(*rl.speed))
				}
				if rl.correct != nil {
					if *rl.correct {
						fields = append(fields, "CorrectForDrops: true")
					} else {
						fields = append(fields, "DeniesDrops: true")
					}
				}
				f.Printf("\t\t{%s},\n", strings.Join(fields, ", "))
			}
			f.WriteString("\t},\n")
		}
		f.Printf("\tDefaultMiningSpeed: %s,\n", gogen.Float(tool.defaultSpeed))
		f.Printf("\tDamagePerBlock: %d,\n", tool.perBlock)
		// Written every time: the component's default is true, which is not
		// the Go zero the field left out would read back as.
		f.Printf("\tCanDestroyBlocksInCreative: %t,\n}\n", tool.creative)
	}

	for _, name := range sortedKeys(r.foods) {
		food, gn := r.foods[name], goName(name)
		f.Printf(`
// %sFood is what a meal of %s restores, and how long the meal takes.
var %sFood = &Food{
	Nutrition:      %d,
	Saturation:     %s,
	CanAlwaysEat:   %t,
	ConsumeSeconds: %s,
}
`, gn, name, gn, food.nutrition, food.saturation, food.canAlwaysEat, food.consumeSeconds)
	}

	for _, name := range sortedKeys(r.consumables) {
		c, gn := r.consumables[name], goName(name)
		f.Printf(`
// %sConsumable is how %s is consumed.
var %sConsumable = &Consumable{
	Seconds: %s,
`, gn, name, gn, c.seconds)
		if len(c.effects) > 0 {
			f.WriteString("\tEffects: []ConsumeEffect{\n")
			for _, eff := range c.effects {
				f.Printf("\t\t%s,\n", eff)
			}
			f.WriteString("\t},\n")
		}
		if c.remainder != "" {
			f.Printf("\tRemainder: protocol.ItemStack{ItemID: int32(Item%s), Count: 1},\n", goName(c.remainder))
		}
		f.Printf("\tSound: %s,\n", r.sounds.holder(c.sound))
		if c.drink {
			f.WriteString("\tDrink: true,\n")
		}
		f.WriteString("}\n")
	}

	for _, name := range sortedKeys(r.deathProtections) {
		gn := goName(name)
		f.Printf(`
// %sDeathProtection is the death effects of %s, run when
// it saves its holder from a death.
var %sDeathProtection = []ConsumeEffect{
`, gn, name, gn)
		for _, eff := range r.deathProtections[name] {
			f.Printf("\t%s,\n", eff)
		}
		f.WriteString("}\n")
	}

	f.WriteString(`
// The expanded block sets the tool rules name, each written out once however
// many rules share it. A rule's set holds the members of the tag it named at
// generation time, so nothing at runtime has to know a tag from a list.
`)
	for i, members := range r.blockSets {
		symbols, err := idents("Block", members)
		if err != nil {
			fail("%v", err)
		}
		f.Printf("var itemBlockSet%d = [BlockCount]bool{\n", i)
		for _, sym := range symbols {
			f.Printf("\t%s: true,\n", sym)
		}
		f.WriteString("}\n")
	}
}

// blocksAttacksLiteral is the Go literal of an item's blocks_attacks, with
// the codec's defaults filled in. vanilla: BlocksAttacks.CODEC
func (r *itemPropsReader) blocksAttacksLiteral(item string, ba obj) string {
	reductions := []string{"{Angle: 90.0, Base: 0.0, Factor: 1.0}"}
	if ba.has("damage_reductions") {
		reductions = nil
		for _, v := range ba.list("damage_reductions") {
			dr, ok := v.(map[string]any)
			if !ok {
				fail("%s has a damage reduction of a shape the generator does not read: %v", item, v)
			}
			o := obj(dr)
			typ := ""
			if o.has("type") {
				typ = ", Type: " + damageTypeSetLiteral(item, o["type"])
			}
			reductions = append(reductions, fmt.Sprintf("{Angle: %s, Base: %s, Factor: %s%s}",
				gogen.Float(o.numOr("horizontal_blocking_angle", 90)), gogen.Float(o.num("base")), gogen.Float(o.num("factor")), typ))
		}
	}
	itemDamage := "ItemDamageFunction{Threshold: 1.0, Base: 0.0, Factor: 1.0}"
	if id, ok := ba.obj("item_damage"); ok {
		itemDamage = fmt.Sprintf("ItemDamageFunction{Threshold: %s, Base: %s, Factor: %s}",
			gogen.Float(id.num("threshold")), gogen.Float(id.num("base")), gogen.Float(id.num("factor")))
	}
	parts := []string{
		"BlockDelaySeconds: " + gogen.Float(ba.numOr("block_delay_seconds", 0)),
		"DisableCooldownScale: " + gogen.Float(ba.numOr("disable_cooldown_scale", 1)),
		"Reductions: []DamageReduction{" + strings.Join(reductions, ", ") + "}",
		"ItemDamage: " + itemDamage,
	}
	if ba.has("bypassed_by") {
		parts = append(parts, "BypassedBy: "+damageTypeSetLiteral(item, ba["bypassed_by"]))
	}
	if ba.has("block_sound") {
		parts = append(parts, "BlockSound: new("+r.sounds.holder(ba.str("block_sound"))+")")
	}
	if ba.has("disabled_sound") {
		parts = append(parts, "DisableSound: new("+r.sounds.holder(ba.str("disabled_sound"))+")")
	}
	return "&BlocksAttacks{" + strings.Join(parts, ", ") + "}"
}

// damageTypeSetLiteral is the literal of a damage type holder set: a "#tag",
// one name or a list of names.
func damageTypeSetLiteral(item string, v any) string {
	switch set := v.(type) {
	case string:
		if strings.HasPrefix(set, "#") {
			return fmt.Sprintf("&DamageTypeSet{Tag: %s, IsTag: true}", damageTypeTagIdent(qualified(set[1:])))
		}
		return fmt.Sprintf("&DamageTypeSet{Types: []DamageType{%s}}", damageTypeIdent(qualified(set)))
	case []any:
		names := make([]string, len(set))
		for i, n := range set {
			s, ok := n.(string)
			if !ok {
				fail("%s has a damage type set of a shape the generator does not read: %v", item, v)
			}
			names[i] = damageTypeIdent(qualified(s))
		}
		return fmt.Sprintf("&DamageTypeSet{Types: []DamageType{%s}}", strings.Join(names, ", "))
	}
	fail("%s has a damage type set of a shape the generator does not read: %v", item, v)
	return ""
}
