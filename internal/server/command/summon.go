package command

import (
	"errors"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Summon is the /summon command.
var Summon = Command{
	Name:  "summon",
	Level: PermLevelGamemasters,
	Build: summonNode,
	Run:   runSummon,
}

// The translation keys /summon answers with. An unknown entity type is the
// resource argument's refusal at parse time.
const (
	summonSuccessKey       = "commands.summon.success"
	summonFailedKey        = "commands.summon.failed"
	summonFailedUUIDKey    = "commands.summon.failed.uuid"
	summonPeacefulKey      = "commands.summon.failed.peaceful"
	summonInvalidIDKey     = "argument.id.invalid"
	summonNotSummonableKey = "entity.not_summonable"
	summonBadNBTKey        = "argument.nbt.expected.compound"
	summonTrailingNBTKey   = "argument.nbt.trailing"
	summonInvalidPosKey    = "commands.summon.invalidPosition"
)

// ErrDuplicateUUID is what a tagged summon answers when the tag's UUID is
// already a live mob's in the dimension, the summon vanilla's
// tryAddFreshEntityWithPassengers refuses with commands.summon.failed.uuid.
var ErrDuplicateUUID = errors.New("duplicate uuid")

// entityTypeRegistry is the registry the summon's entity argument is parsed
// against, the one ResourceArgument.resource names.
const entityTypeRegistry = "minecraft:entity_type"

// summonSuggestionType is the suggestion provider vanilla's
// SUMMONABLE_ENTITIES provider goes by in the Commands packet, which the
// client completes locally from its own registry.
const summonSuggestionType = "minecraft:summonable_entities"

// summonNode builds the /summon command tree, matching the vanilla
// report: an entity type by its registry name, then an optional position and
// an optional compound tag after it. The entity argument carries the
// minecraft:resource parser with the entity_type registry as its property.
func summonNode() *Node {
	entityArg := &Node{
		Type:       NodeArgument,
		Name:       "entity",
		Executable: true,
		Parser:     ParserResource,
		Properties: func(b *protocol.Buffer) { b.PutString(entityTypeRegistry) },
		// Checked against the registry while the command is read; whether
		// the type is summonable is the run's own check.
		Resource: &ResourceArg{Registry: entityTypeRegistry},
		Suggests: summonSuggestionType,
	}
	posArg := &Node{
		Type:       NodeArgument,
		Name:       "pos",
		Executable: true,
		Parser:     ParserVec3,
	}
	nbtArg := &Node{
		Type:       NodeArgument,
		Name:       "nbt",
		Executable: true,
		Parser:     ParserNbtCompound,
		Nbt:        true,
	}
	posArg.Children = []*Node{nbtArg}
	entityArg.Children = []*Node{posArg}
	return &Node{
		Type:     NodeLiteral,
		Name:     "summon",
		Required: PermLevelGamemasters,
		Children: []*Node{entityArg},
	}
}

// runSummon runs /summon <entity> [<pos>] [<nbt>]. A type with no
// simulation answers with the same failed-summon message as a creation that
// came to nothing. The tag is read by the argument's reader and applied.
func runSummon(src Source, input string, args []string) error {
	name, ok := parseResourceLocation(args[0])
	if !ok {
		src.Debug("summon entity name is not a resource location", "name", args[0])
		return refuse(src, summonInvalidIDKey)
	}
	if !entityTypeSummonable(name) {
		// vanilla: ResourceArgument.getSummonableEntityType
		return refuse(src, summonNotSummonableKey, protocol.Text{Text: name})
	}
	kind := summonKindOf(name)
	if kind == summonNone {
		// The type exists and is summonable, but this server simulates
		// nothing behind it: the creation comes to nothing, which is the
		// path the vanilla server's own SummonCommand reports
		// commands.summon.failed down.
		src.Debug("summon names a type this server does not simulate", "name", name)
		return refuse(src, summonFailedKey)
	}

	// The position: the runner's own when the command carries none, and
	// otherwise the three words of a minecraft:vec3.
	at := src.Position()
	x, y, z := at.X, at.Y, at.Z
	rest := args[1:]
	if len(rest) >= 3 {
		var ok bool
		if x, y, z, ok = resolveVec3(src, rest[:3]); !ok {
			src.Debug("summon position did not parse", "args", rest[:3])
			return nil
		}
		rest = rest[3:]
	}

	// The tag's entries feed the summoned thing (an orb's Value, an item's
	// Item) and, for a mob, the saved-mob reader. Whether a tag was written
	// at all matters, the empty one included: vanilla runs finalizeSpawn
	// only without one, which decides a sheep's colour.
	scalars := map[string]string{}
	values := map[string]string{}
	var compound nbt.Compound
	tagged := len(rest) > 0
	if len(rest) > 0 {
		verdict, entries, spans, tag := readSummonTag(wordsIn(input, rest, 0, len(rest)))
		switch verdict {
		case nbtNotCompound:
			return refuse(src, summonBadNBTKey)
		case nbtTrailing:
			return refuse(src, summonTrailingNBTKey)
		case nbtMalformed:
			// A compound that does not read as SNBT at all: the nearest
			// refusal this server's own shape answers with is the failed
			// summon.
			src.Debug("summon nbt did not parse", "nbt", wordsIn(input, rest, 0, len(rest)))
			return refuse(src, summonFailedKey)
		}
		scalars, values, compound = entries, spans, tag
		dropInvalidBlockStates(compound, entity.BlockStateNBTKeys, true)
	}

	// The bounds the vanilla command checks the block position against
	// before it stands anything up.
	if !inSpawnableBounds(x, y, z) {
		src.Debug("summon position is out of bounds", "x", x, "y", y, "z", z)
		return refuse(src, summonInvalidPosKey)
	}
	// A peaceful world keeps none of the monsters; the vanilla command
	// refuses the summon rather than standing a monster up into one.
	if src.Difficulty() == 0 && entity.GoneInPeaceful(entity.MobTypeOf(name)) {
		return refuse(src, summonPeacefulKey)
	}

	id, identity, baseName, ok, err := summonBody(src, name, kind, x, y, z, compound, scalars, values, tagged)
	if refused := summonErr(src, err, name); refused != nil {
		return refused
	}
	if !ok {
		src.Debug("summon came to nothing", "name", name)
		return refuse(src, summonFailedKey)
	}
	src.Debug("summoned entity", "name", name, "id", id, "by", src.Name())
	if team, joined := JoinTagTeam(src.Scoreboard(), identity, compound); team != "" && !joined {
		src.Info("unable to add entity to team (that team probably doesn't exist)", "team", team)
	}
	display := protocol.Text{Translate: "entity.minecraft." + strings.TrimPrefix(name, "minecraft:")}
	if baseName != nil {
		display = *baseName
	}
	if custom := summonDisplayName(compound); custom != nil {
		display = *custom
	}
	display = src.EntityDisplayName(name, identity, display)
	return feedbackToAdmins(src, summonSuccessKey, display)
}

// summonBody spawns the body and returns its IDs and optional name component.
// A tag, even an empty one, skips finalizeSpawn for a mob.
// vanilla: SummonCommand.createEntity
func summonBody(src Source, name string, kind summonKind, x, y, z float64, compound nbt.Compound, scalars, values map[string]string, tagged bool) (id int32, identity uuid.UUID, display *protocol.Text, ok bool, err error) {
	switch kind {
	case summonOrb:
		// vanilla: ExperienceOrb.readAdditionalSaveData, Entity.load
		value, _ := nbt.ShortField(compound, "Value")
		id, identity, err = src.SummonOrb(x, y, z, int(value), nbt.BoolField(compound, "NoGravity"), compound)
		ok = err == nil
	case summonItem:
		// Item is read by the ItemStack codec, with the pickup delay. The
		// success line names the item by its stack; vanilla summons an air
		// item that discards itself on its first tick, so here nothing
		// spawns and the line names air.
		stack, pickupDelay, stackOK := summonItemStack(values, scalars)
		if !stackOK {
			display = &protocol.Text{Translate: "block.minecraft.air"}
			id, identity, ok = 0, uuid.New(), true
			break
		}
		// vanilla: ItemEntity.readAdditionalSaveData
		itemTag, _ := nbt.Get[nbt.Compound](compound, "Item")
		if full, whole := registry.StoredStack(itemTag); whole && !full.Empty() {
			stack = full
		}
		itemName := rules.ItemTypeName(stack)
		display = &itemName
		id, identity, err = src.SummonItem(x, y, z, stack, pickupDelay, nbt.BoolField(compound, "NoGravity"), compound)
		ok = err == nil
	case summonLightning:
		id, identity, err = src.SummonSaved(name, x, y, z, compound)
		ok = err == nil
	case summonTNT:
		// Fuse is read as PrimedTnt's short (80 when absent or not a
		// number); zero and below are kept and go off on the first tick.
		fuse := entity.TNTDefaultFuse
		if v, isNumber := nbt.ShortField(compound, "fuse"); isNumber {
			fuse = int(v)
		}
		id, identity, err = src.SummonTNT(x, y, z, fuse, nbt.BoolField(compound, "NoGravity"), compound)
		ok = err == nil
	case summonArmorStand:
		// The stand: made from the compound's own fields — the flags, the
		// pose, the slot mask — the same reader a saved stand's wake
		// reads, its position the command's.
		id, identity, err = src.SummonArmorStand(x, y, z, compound)
		ok = err == nil
	case summonFallingBlock:
		// The fall: the block its tag named under BlockState — sand where
		// the tag named none — read the way a saved fall's wake reads,
		// falling from the position the command points.
		display = &protocol.Text{
			Translate: "entity.minecraft.falling_block_type",
			With:      []protocol.Text{{Translate: summonBlockKey(compound[entity.BlockStateKeyFalling])}},
		}
		id, identity, err = src.SummonFallingBlock(x, y, z, compound)
		ok = err == nil
	case summonArrow:
		// The shaft of the tag's own making — the Motion, the rotation,
		// the pickup rule, the weapon a landed hit owes — flying from
		// the position the command points, drawn as the type named.
		id, identity, err = src.SummonArrow(name, x, y, z, compound)
		ok = err == nil
	case summonThrown:
		// The thrown body the kind names, its tag's own base fields —
		// the Motion, the rotation, the NoGravity flag — riding it.
		id, identity, err = src.SummonThrown(name, x, y, z, compound)
		ok = err == nil
	case summonSaved:
		id, identity, err = src.SummonSaved(name, x, y, z, compound)
		ok = err == nil
	case summonPainting:
		// The painting: hanging from the cell the command's position
		// floors to, facing and made as its tag named.
		id, identity, err = src.SummonPainting(x, y, z, compound)
		ok = err == nil
	case summonFrame:
		// The frame: hanging from the cell the command's position floors
		// to, holding the stack and facing the direction the tag named.
		id, identity, err = src.SummonFrame(name, x, y, z, compound)
		ok = err == nil
	case summonFixture:
		id, identity, err = src.SummonFixture(name, x, y, z, compound)
		ok = err == nil
	case summonMinecart:
		// The cart: its contents and display block among what its tag
		// carries, read the way a saved cart's wake reads them.
		id, identity, err = src.SummonMinecart(name, x, y, z, compound)
		ok = err == nil
	case summonBoat:
		// The hull the id names, the chest boats' own cargo among what its
		// tag carries.
		id, identity, err = src.SummonBoat(entity.BoatKind(name), x, y, z, compound)
		ok = err == nil
	case summonMob:
		// A tag, even an empty one, builds the entity from it and skips
		// finalizeSpawn. Its reader is the saved mob's, so every saved field
		// (name, persistence, variants, age lock) reads the same here.
		if tagged {
			id, identity, err = src.SummonMobTagged(name, x, y, z, compound)
			ok = err == nil
		} else {
			id, identity, ok = src.SummonMob(name, x, y, z)
		}
	}
	return id, identity, display, ok, err
}

// summonErr is what a summon that made its body out of the tag answers with
// the error of it: the duplicate-UUID refusal for a UUID a live body already
// holds, nil for everything the caller carries on through with.
func summonErr(src Source, err error, name string) error {
	if errors.Is(err, ErrDuplicateUUID) {
		src.Debug("summon tag's UUID is already a live body's", "name", name)
		return refuse(src, summonFailedUUIDKey)
	}
	return nil
}

// dropInvalidBlockStates removes block-state NBT entries that fail the 26.3
// BlockState.CODEC (such as the legacy {Name, Properties} form), so command
// inputs leave the entity or block entity at its vanilla default while disk
// loaders keep accepting legacy saves.
// vanilla: BlockState.CODEC
func dropInvalidBlockStates(tag nbt.CompoundTag, keys []string, passengers bool) {
	if tag == nil {
		return
	}
	entries := tag.Entries()
	if entries == nil {
		return
	}
	for _, key := range keys {
		if val, ok := entries[key]; ok {
			if _, _, valid := registry.BlockStateOfCodec(val); !valid {
				tag.Delete(key)
			}
		}
	}
	if !passengers {
		return
	}
	if list, ok := nbt.Get[nbt.List](entries, "Passengers"); ok {
		for _, item := range list.Items {
			if rider, ok := item.(nbt.CompoundTag); ok {
				dropInvalidBlockStates(rider, keys, true)
			}
		}
	}
}

// summonBlockKey is the translation key of the block a falling block's tag
// named — sand where the tag named none or named a block no state answers
// to — the name a fall's display name carries beside its own key.
// vanilla: FallingBlockEntity.getTypeName, DEFAULT_BLOCK_STATE
func summonBlockKey(tag nbt.Tag) string {
	state := registry.BlockSand.DefaultState()
	if name, props, ok := registry.BlockStateOfCodec(tag); ok {
		if resolved, found := registry.FindState(registry.Qualify(name), props); found {
			state = resolved
		}
	}
	namespace, path, _ := strings.Cut(registry.BlockOf(state).Name(), ":")
	return "block." + namespace + "." + path
}

// summonDisplayName reads a tag's CustomName as the component the success
// line names the entity by, or nil where the tag names none or names
// something that is not a component; its click is dropped.
// vanilla: Entity.getName
func summonDisplayName(compound nbt.Compound) *protocol.Text {
	tag, ok := compound["CustomName"]
	if !ok {
		return nil
	}
	text, ok := protocol.StyledTextOf(tag)
	if !ok {
		return nil
	}
	text = text.WithoutClick()
	return &text
}

// inSpawnableBounds is Level.isInSpawnableBounds: the y within the twenty
// million the spawnable height spans and the horizontal within the thirty
// million the world spans, all read off the block the position sits in.
func inSpawnableBounds(x, y, z float64) bool {
	bx, by, bz := rules.BlockOf(x), rules.BlockOf(y), rules.BlockOf(z)
	return by >= -20000000 && by < 20000000 && rules.InWorldBoundsHorizontal(bx, bz)
}

// parseResourceLocation reads a word's longest resource-location run, the
// minecraft namespace supplied when none is written; the rest is ignored.
// vanilla: Identifier.read
func parseResourceLocation(word string) (string, bool) {
	name, _, ok := readIdentifier(word)
	return name, ok
}

// entityTypeExists reports whether an entity type name is one the game's
// entity_type registry carries.
func entityTypeExists(name string) bool {
	return resourceExists(entityTypeRegistry, name)
}

// noSummonEntityTypes are the entity types the game registers with the
// builder's noSummon, the ones ResourceArgument.getSummonableEntityType
// refuses with entity.not_summonable: the player and the fishing bobber.
var noSummonEntityTypes = map[string]bool{
	"minecraft:player":         true,
	"minecraft:fishing_bobber": true,
}

// entityTypeSummonable reports whether the game will stand the entity type up
// at all, EntityType.canSummon behind the vanilla argument's check.
func entityTypeSummonable(name string) bool {
	return !noSummonEntityTypes[name]
}

// summonKind is the family of body a summonable entity type stands up as;
// summonNone is a type this server simulates nothing behind.
type summonKind uint8

const (
	summonNone summonKind = iota
	summonOrb
	summonItem
	summonLightning
	summonTNT
	summonArmorStand
	summonFallingBlock
	summonArrow
	summonThrown
	summonSaved
	summonPainting
	summonFrame
	summonFixture
	summonMinecart
	summonBoat
	summonMob
)

// summonKindOf is the family the entity type name stands up as.
func summonKindOf(name string) summonKind {
	switch name {
	case "minecraft:experience_orb":
		return summonOrb
	case "minecraft:item":
		return summonItem
	case "minecraft:lightning_bolt":
		return summonLightning
	case "minecraft:tnt":
		return summonTNT
	case "minecraft:armor_stand":
		return summonArmorStand
	case "minecraft:falling_block":
		return summonFallingBlock
	case "minecraft:arrow", "minecraft:spectral_arrow", "minecraft:trident":
		return summonArrow
	case "minecraft:snowball", "minecraft:egg", "minecraft:ender_pearl",
		"minecraft:splash_potion", "minecraft:lingering_potion", "minecraft:experience_bottle",
		"minecraft:small_fireball", "minecraft:fireball", "minecraft:dragon_fireball", "minecraft:wither_skull",
		"minecraft:wind_charge", "minecraft:breeze_wind_charge", "minecraft:shulker_bullet", "minecraft:llama_spit":
		return summonThrown
	case "minecraft:end_crystal", "minecraft:area_effect_cloud", "minecraft:evoker_fangs",
		"minecraft:firework_rocket", "minecraft:eye_of_ender", "minecraft:leash_knot", "minecraft:mannequin":
		return summonSaved
	case "minecraft:painting":
		return summonPainting
	case "minecraft:item_frame", "minecraft:glow_item_frame":
		return summonFrame
	}
	switch {
	case entity.FixtureKind(name):
		return summonFixture
	case entity.BoatKind(name) != "":
		return summonBoat
	case entity.SpawnableKind(entity.MobTypeOf(name)):
		return summonMob
	}
	if _, cart := entity.MinecartKindOf(name); cart {
		return summonMinecart
	}
	return summonNone
}

// summonItemStack reads the stack an item's summon carried: the Item
// compound's own id and count, and the pickup delay the outer tag carried
// beside it; the false is a tag that named no item this server knows.
func summonItemStack(values, scalars map[string]string) (protocol.ItemStack, int, bool) {
	verdict, inner, _ := readSummonNBT(values["Item"])
	if verdict != nbtIsCompound {
		return protocol.ItemStack{}, 0, false
	}
	id, ok := registry.ItemID(registry.Qualify(unquoted(inner["id"])))
	if !ok {
		return protocol.ItemStack{}, 0, false
	}
	count := summonInt(inner["count"])
	if count <= 0 {
		count = 1
	}
	return protocol.ItemStack{ItemID: id, Count: int32(count)}, summonInt(scalars["PickupDelay"]), true
}

// summonInt reads one SNBT scalar as an integer: the bare digits, with the
// suffix letters a tag may end in taken off, and nought for anything else.
func summonInt(text string) int {
	for len(text) > 0 {
		last := text[len(text)-1]
		if last >= '0' && last <= '9' {
			break
		}
		if last == '-' || last == '+' {
			return 0
		}
		text = text[:len(text)-1]
	}
	v, err := strconv.ParseInt(text, 10, 32)
	if err != nil {
		return 0
	}
	return int(v)
}
