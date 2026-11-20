package command

import (
	"maps"
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Place is the /place command.
var Place = Command{
	Name:  "place",
	Level: PermLevelGamemasters,
	Build: placeNode,
	Run:   runPlace,
}

// The translation keys /place answers with.
const (
	placeFeatureFailedKey    = "commands.place.feature.failed"
	placeFeatureSuccessKey   = "commands.place.feature.success"
	placeJigsawInvalidKey    = "commands.place.jigsaw.invalid"
	placeJigsawFailedKey     = "commands.place.jigsaw.failed"
	placeJigsawSuccessKey    = "commands.place.jigsaw.success"
	placeStructureInvalidKey = "commands.place.structure.invalid"
	placeStructureFailedKey  = "commands.place.structure.failed"
	placeStructureSuccessKey = "commands.place.structure.success"
	placeTemplateInvalidKey  = "commands.place.template.invalid"
	placeTemplateFailedKey   = "commands.place.template.failed"
	placeTemplateSuccessKey  = "commands.place.template.success"
	enumInvalidKey           = "argument.enum.invalid"
)

// The registries the client's resource_key parser reads, and the one a
// feature argument names in its refusal.
const (
	featureRegistry      = "minecraft:worldgen/feature"
	templatePoolRegistry = "minecraft:worldgen/template_pool"
	structureRegistry    = "minecraft:worldgen/structure"
)

// placeNode builds the /place tree.
// vanilla: PlaceCommand.register
func placeNode() *Node {
	pos := func(name string, children ...*Node) *Node {
		return &Node{Type: NodeArgument, Name: name, Parser: ParserBlockPos, Executable: true, Children: children}
	}
	key := func(name, reg string, children ...*Node) *Node {
		return &Node{
			Type: NodeArgument, Name: name, Parser: ParserResourceKey, Executable: true,
			Properties: func(b *protocol.Buffer) { b.PutString(reg) }, Children: children,
		}
	}
	feature := &Node{Type: NodeLiteral, Name: "feature", Children: []*Node{{
		Type: NodeArgument, Name: "feature", Parser: ParserFeature, Executable: true,
		Check: checkFeatureArgument, Children: []*Node{pos("pos")},
	}}}
	jigsaw := &Node{Type: NodeLiteral, Name: "jigsaw", Children: []*Node{{
		Type: NodeArgument, Name: "pool", Parser: ParserResourceKey,
		Properties: func(b *protocol.Buffer) { b.PutString(templatePoolRegistry) },
		Children: []*Node{{
			Type: NodeArgument, Name: "target", Parser: ParserResourceLoc,
			Children: []*Node{{
				Type: NodeArgument, Name: "max_depth", Parser: ParserInteger, Executable: true,
				Int:      &IntArg{Min: 1, Max: 20, HasMin: true, HasMax: true},
				Children: []*Node{pos("position")},
			}},
		}},
	}}}
	structure := &Node{Type: NodeLiteral, Name: "structure", Children: []*Node{
		key("structure", structureRegistry, pos("pos")),
	}}
	strict := &Node{Type: NodeLiteral, Name: "strict", Executable: true}
	seed := &Node{Type: NodeArgument, Name: "seed", Parser: ParserInteger, Executable: true, Children: []*Node{strict}}
	integrity := &Node{
		Type: NodeArgument, Name: "integrity", Parser: ParserFloat, Executable: true,
		Float:    &FloatArg{Min: 0, Max: 1, HasMin: true, HasMax: true},
		Children: []*Node{seed},
	}
	mirror := &Node{
		Type: NodeArgument, Name: "mirror", Parser: ParserTemplateMirror, Executable: true,
		Check: checkMirrorArg, Children: []*Node{integrity},
	}
	rotation := &Node{
		Type: NodeArgument, Name: "rotation", Parser: ParserTemplateRotation, Executable: true,
		Check: checkRotationArg, Children: []*Node{mirror},
	}
	template := &Node{Type: NodeLiteral, Name: "template", Children: []*Node{{
		Type: NodeArgument, Name: "template", Parser: ParserResourceLoc, Executable: true,
		Suggests: AskServerSuggestion, Completions: templateCompletions,
		Children: []*Node{pos("pos", rotation)},
	}}}
	return &Node{Type: NodeLiteral, Name: "place", Required: PermLevelGamemasters,
		Children: []*Node{feature, jigsaw, structure, template}}
}

// templateCompletions offers the ids of the structure templates.
// vanilla: PlaceCommand.SUGGEST_TEMPLATES
func templateCompletions(ctx any, partial string) (int, []string) {
	src, ok := ctx.(Source)
	if !ok {
		return 0, nil
	}
	return 0, ResourceSuggestions(partial, src.ListTemplates()...)
}

func checkRotationArg(word string) *ParseError {
	if _, ok := world.ParseRotation(word); ok {
		return nil
	}
	return &ParseError{Key: enumInvalidKey, With: []string{word}, Cursor: len(word)}
}

func checkMirrorArg(word string) *ParseError {
	if _, ok := world.ParseMirror(word); ok {
		return nil
	}
	return &ParseError{Key: enumInvalidKey, With: []string{word}, Cursor: len(word)}
}

// placePos reads the optional block position at words, defaulting to the
// block the source stands in, and holds it to the loaded world.
// vanilla: BlockPosArgument.getLoadedBlockPos
func placePos(src Source, words []string) (x, y, z int32, ok bool, err error) {
	if len(words) == 0 {
		at := src.Position()
		return rules.BlockOf(at.X), rules.BlockOf(at.Y), rules.BlockOf(at.Z), true, nil
	}
	x, y, z, ok = resolveBlockPos(src, words)
	if !ok {
		return 0, 0, 0, false, nil
	}
	if key := loadedBlockPosRefusal(src, x, y, z); key != "" {
		return 0, 0, 0, true, refuse(src, key)
	}
	return x, y, z, true, nil
}

// runPlace runs /place.
// vanilla: PlaceCommand
func runPlace(src Source, _ string, args []string) error {
	if len(args) < 2 {
		src.Debug("incomplete place command")
		return nil
	}
	switch args[0] {
	case "feature":
		return runPlaceFeature(src, args[1:])
	case "jigsaw":
		return runPlaceJigsaw(src, args[1:])
	case "structure":
		return runPlaceStructure(src, args[1:])
	case "template":
		return runPlaceTemplate(src, args[1:])
	}
	src.Debug("place branch is not one the tree offers", "word", args[0])
	return nil
}

// chunkSpan reports whether every chunk from the one holding (x0, z0) to
// the one holding (x1, z1) is loaded.
// vanilla: PlaceCommand.checkLoaded
func chunkSpan(src Source, x0, z0, x1, z1 int32) bool {
	return src.BlockBoxLoaded(min(x0, x1), min(z0, z1), max(x0, x1), max(z0, z1))
}

// checkFeatureArgument holds a feature argument to a feature the data pack
// names, or an inline one that reads as SNBT.
// vanilla: ResourceOrIdArgument.parse
func checkFeatureArgument(text string) *ParseError {
	if name, n, ok := readIdentifier(text); ok && n == len(text) {
		if name = registry.Qualify(name); !registry.FeatureKeys[name] {
			return &ParseError{Key: lootTableNotFoundKey, With: []string{name, featureRegistry}, Cursor: len(text)}
		}
		return nil
	}
	_, n, pe := nbt.ParseValueTag(text)
	if pe != nil {
		return snbtRefusal(pe, joinSelector([]Token{{Text: text}}), pe.Offset)
	}
	if n != len(text) {
		return &ParseError{Key: "command.unknown.argument", Cursor: n}
	}
	return nil
}

func runPlaceFeature(src Source, args []string) error {
	id := registry.Qualify(args[0])
	if !registry.FeatureKeys[id] {
		// An inline feature is not built here, so it places nothing.
		return refuse(src, placeFeatureFailedKey)
	}
	x, y, z, ok, err := placePos(src, args[1:])
	if !ok {
		src.Debug("place position did not parse", "args", args[1:])
		return nil
	}
	if err != nil {
		return err
	}
	if !chunkSpan(src, (x>>4-1)*16, (z>>4-1)*16, (x>>4+1)*16+15, (z>>4+1)*16+15) {
		return refuse(src, keyPosUnloaded)
	}
	if !src.PlaceFeature(id, x, y, z) {
		return refuse(src, placeFeatureFailedKey)
	}
	return feedbackToAdmins(src, placeFeatureSuccessKey, protocol.Text{Text: id}, intText(x), intText(y), intText(z))
}

func runPlaceJigsaw(src Source, args []string) error {
	pool := registry.Qualify(args[0])
	if !registry.TemplatePoolKeys[pool] {
		return refuse(src, placeJigsawInvalidKey, protocol.Text{Text: pool})
	}
	var posWords []string
	if len(args) > 3 {
		posWords = args[3:]
	}
	x, y, z, ok, err := placePos(src, posWords)
	if !ok {
		src.Debug("place position did not parse", "args", posWords)
		return nil
	}
	if err != nil {
		return err
	}
	// vanilla: PlaceCommand.placeJigsaw checks the chunk of the position.
	if !chunkSpan(src, x, z, x, z) {
		return refuse(src, keyPosUnloaded)
	}
	depth, err := strconv.Atoi(args[2])
	if err != nil {
		src.Debug("place jigsaw depth did not parse", "word", args[2])
		return nil
	}
	switch src.PlaceJigsaw(pool, registry.Qualify(args[1]), depth, x, y, z) {
	case world.StructureNoStart:
		return refuse(src, placeJigsawFailedKey)
	case world.StructureUnloaded:
		return refuse(src, keyPosUnloaded)
	}
	return feedbackToAdmins(src, placeJigsawSuccessKey, intText(x), intText(y), intText(z))
}

func runPlaceStructure(src Source, args []string) error {
	id := registry.Qualify(args[0])
	if !registry.StructureKeys[id] && !registry.StructureExists(id) {
		return refuse(src, placeStructureInvalidKey, protocol.Text{Text: id})
	}
	x, y, z, ok, err := placePos(src, args[1:])
	if !ok {
		src.Debug("place position did not parse", "args", args[1:])
		return nil
	}
	if err != nil {
		return err
	}
	switch src.PlaceStructure(id, x, z) {
	case world.StructureNoStart:
		return refuse(src, placeStructureFailedKey)
	case world.StructureUnloaded:
		return refuse(src, keyPosUnloaded)
	}
	return feedbackToAdmins(src, placeStructureSuccessKey, protocol.Text{Text: id}, intText(x), intText(y), intText(z))
}

// TemplateEntityHost is the part of a Source that stands up template entities.
type TemplateEntityHost interface {
	SummonOrb(x, y, z float64, value int, noGravity bool, tag nbt.Compound) (int32, uuid.UUID, error)
	SummonTNT(x, y, z float64, fuse int, noGravity bool, tag nbt.Compound) (int32, uuid.UUID, error)
	SummonItem(x, y, z float64, stack protocol.ItemStack, pickupDelay int, noGravity bool, tag nbt.Compound) (int32, uuid.UUID, error)
	SummonLightning(x, y, z float64) (int32, uuid.UUID, bool)
	SummonArmorStand(x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	SummonFallingBlock(x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	SummonArrow(name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	SummonThrown(name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	SummonSaved(name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	SummonPainting(x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	SummonFrame(name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	SummonFixture(name string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	SummonMinecart(kind string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	SummonBoat(kind string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
	SummonMobTagged(kind string, x, y, z float64, tag nbt.Compound) (int32, uuid.UUID, error)
}

// PlaceTemplateEntities stands up each of the template's entities that the
// summon command can make, from its saved tag; other kinds are skipped.
// vanilla: StructureTemplate.placeEntities
func PlaceTemplateEntities(src TemplateEntityHost, entities []world.PlacedEntity, debug func(msg string, args ...any)) {
	for _, e := range entities {
		// Template creation loads only the root; the summon adapters also load riders.
		// vanilla: StructureTemplate.createEntityIgnoreException, EntityType.create
		if _, ok := e.Tag["Passengers"]; ok {
			e.Tag = maps.Clone(e.Tag)
			delete(e.Tag, "Passengers")
		}
		x, y, z := e.Pos[0], e.Pos[1], e.Pos[2]
		var err error
		switch summonKindOf(e.ID) {
		case summonOrb:
			// vanilla: ExperienceOrb.readAdditionalSaveData
			value, _ := nbt.IntField(e.Tag, "Value")
			_, _, err = src.SummonOrb(x, y, z, int(value), nbt.BoolField(e.Tag, "NoGravity"), e.Tag)
		case summonItem:
			item, ok := nbt.Get[nbt.Compound](e.Tag, "Item")
			stack, stacked := registry.StoredStack(item)
			if !ok || !stacked {
				continue
			}
			delay, _ := nbt.LongValue(e.Tag["PickupDelay"])
			noGravity, _ := nbt.LongValue(e.Tag["NoGravity"])
			_, _, err = src.SummonItem(x, y, z, stack, int(delay), noGravity != 0, e.Tag)
		case summonLightning:
			src.SummonLightning(x, y, z)
		case summonTNT:
			// vanilla: PrimedTnt.readAdditionalSaveData
			fuse := entity.TNTDefaultFuse
			if v, ok := nbt.ShortField(e.Tag, "fuse"); ok {
				fuse = int(v)
			}
			_, _, err = src.SummonTNT(x, y, z, fuse, nbt.BoolField(e.Tag, "NoGravity"), e.Tag)
		case summonArmorStand:
			_, _, err = src.SummonArmorStand(x, y, z, e.Tag)
		case summonFallingBlock:
			_, _, err = src.SummonFallingBlock(x, y, z, e.Tag)
		case summonArrow:
			_, _, err = src.SummonArrow(e.ID, x, y, z, e.Tag)
		case summonThrown:
			_, _, err = src.SummonThrown(e.ID, x, y, z, e.Tag)
		case summonSaved:
			_, _, err = src.SummonSaved(e.ID, x, y, z, e.Tag)
		case summonPainting:
			_, _, err = src.SummonPainting(x, y, z, e.Tag)
		case summonFrame:
			_, _, err = src.SummonFrame(e.ID, x, y, z, e.Tag)
		case summonFixture:
			_, _, err = src.SummonFixture(e.ID, x, y, z, e.Tag)
		case summonMinecart:
			_, _, err = src.SummonMinecart(e.ID, x, y, z, e.Tag)
		case summonBoat:
			_, _, err = src.SummonBoat(entity.BoatKind(e.ID), x, y, z, e.Tag)
		case summonMob:
			_, _, err = src.SummonMobTagged(e.ID, x, y, z, e.Tag)
		default:
			debug("place template entity is not one this server places", "id", e.ID)
			continue
		}
		if err != nil {
			debug("place template entity failed", "id", e.ID, "error", err)
		}
	}
}

func intText(v int32) protocol.Text {
	n := int64(v)
	return protocol.Text{Number: &n}
}

func runPlaceTemplate(src Source, args []string) error {
	id := registry.Qualify(args[0])
	words := args[1:]
	posWords := words[:min(3, len(words))]
	if len(posWords) < 3 {
		posWords = nil
	}
	x, y, z, ok, err := placePos(src, posWords)
	if !ok {
		src.Debug("place position did not parse", "args", posWords)
		return nil
	}
	if err != nil {
		return err
	}
	rest := words[len(posWords):]
	p := world.TemplatePlacement{Origin: [3]int32{x, y, z}, Integrity: 1}
	strict := false
	if len(rest) > 0 {
		p.Rotation, _ = world.ParseRotation(rest[0])
	}
	if len(rest) > 1 {
		p.Mirror, _ = world.ParseMirror(rest[1])
	}
	if len(rest) > 2 {
		f, perr := strconv.ParseFloat(rest[2], 32)
		if perr != nil {
			src.Debug("place integrity did not parse", "word", rest[2])
			return nil
		}
		p.Integrity = float32(f)
	}
	if len(rest) > 3 {
		s, perr := strconv.ParseInt(rest[3], 10, 32)
		if perr != nil {
			src.Debug("place seed did not parse", "word", rest[3])
			return nil
		}
		p.Seed = s
	}
	if len(rest) > 4 {
		strict = rest[4] == "strict"
	}

	tpl, found := src.LoadTemplate(id)
	if !found {
		if registry.BuiltinTemplates[id] {
			// Known templates whose bytes are not embedded still report failure.
			src.Debug("place template is built in and not loaded", "template", id)
			return refuse(src, placeTemplateFailedKey)
		}
		return refuse(src, placeTemplateInvalidKey, protocol.Text{Text: id})
	}
	// vanilla: PlaceCommand.placeTemplate checks the chunks from the origin
	// to the origin offset by the unrotated size.
	if !chunkSpan(src, x, z, x+tpl.Size[0], z+tpl.Size[2]) {
		return refuse(src, keyPosUnloaded)
	}
	placed, ok := tpl.Place(p)
	if !ok {
		return refuse(src, placeTemplateFailedKey)
	}
	src.PlaceTemplate(placed, strict)
	PlaceTemplateEntities(src, placed.Entities, src.Debug)
	return feedbackToAdmins(src, placeTemplateSuccessKey, protocol.Text{Text: id}, intText(x), intText(y), intText(z))
}
