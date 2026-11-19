package command

import (
	"math"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
)

// Data is /data: get, merge, modify and remove on an entity's, block
// entity's or command storage's NBT. Reads see the saved form and writes go
// back the same way: storages to data/<namespace>/command_storage.dat,
// blocks through the placement tag, bodies through their saved compounds.
// vanilla: DataCommands
var Data = Command{
	Name:   "data",
	Level:  PermLevelGamemasters,
	Run:    discardResult(runData),
	Result: runData,
	Build:  dataNode,
}

// The translation keys /data answers with beyond the path's own.
const (
	dataBlockQueryKey       = "commands.data.block.query"
	dataBlockGetKey         = "commands.data.block.get"
	dataBlockModifiedKey    = "commands.data.block.modified"
	dataEntityQueryKey      = "commands.data.entity.query"
	dataEntityGetKey        = "commands.data.entity.get"
	dataEntityInvalidKey    = "commands.data.entity.invalid"
	dataEntityModifiedKey   = "commands.data.entity.modified"
	dataStorageQueryKey     = "commands.data.storage.query"
	dataStorageGetKey       = "commands.data.storage.get"
	dataStorageModifiedKey  = "commands.data.storage.modified"
	dataMergeFailedKey      = "commands.data.merge.failed"
	dataGetInvalidKey       = "commands.data.get.invalid"
	dataGetMultipleKey      = "commands.data.get.multiple"
	dataExpectedObjectKey   = "commands.data.modify.expected_object"
	dataExpectedValueKey    = "commands.data.modify.expected_value"
	dataInvalidSubstringKey = "commands.data.modify.invalid_substring"
)

// dataNode builds the /data tree: merge, get and remove for each target kind
// (a single entity, a position, a storage identifier), then modify.
// vanilla: DataCommands.register
func dataNode() *Node {
	target := func(kind string, children ...*Node) *Node {
		var child *Node
		switch kind {
		case "entity":
			child = &Node{Type: NodeArgument, Name: "target", Parser: ParserEntity,
				Properties: func(b *protocol.Buffer) { b.PutU8(1) },
				EntityArg:  &EntityArg{Single: true}}
		case "block":
			child = &Node{Type: NodeArgument, Name: "target", Parser: ParserBlockPos}
		default:
			child = &Node{Type: NodeArgument, Name: "target", Parser: ParserResourceLoc}
		}
		child.Children = children
		return &Node{Type: NodeLiteral, Name: kind, Children: []*Node{child}}
	}
	scale := func() *Node {
		return &Node{Type: NodeArgument, Name: "scale", Parser: ParserDouble,
			Properties: func(b *protocol.Buffer) { b.PutU8(0) },
			Check:      checkDoubleArg, Executable: true}
	}
	// Each branch builds its own nodes, the way the tree's callers never
	// share one below two parents.
	getPath := func() *Node {
		node := pathArg("path")
		node.Executable = true
		node.Children = []*Node{scale()}
		return node
	}
	mergeNbt := func() *Node {
		return &Node{Type: NodeArgument, Name: "nbt", Parser: ParserNbtCompound,
			Nbt: true, Executable: true}
	}
	removePath := func() *Node {
		node := pathArg("path")
		node.Executable = true
		return node
	}
	// The get's target answers on its own — the whole-target query runs
	// bare — with the path behind it and the scale behind that.
	getBranch := func(kind string) *Node {
		branch := target(kind, getPath())
		branch.Children[0].Executable = true
		return branch
	}
	return &Node{
		Type:     NodeLiteral,
		Name:     "data",
		Required: PermLevelGamemasters,
		Children: []*Node{
			{Type: NodeLiteral, Name: "merge", Children: []*Node{
				target("entity", mergeNbt()),
				target("block", mergeNbt()),
				target("storage", mergeNbt()),
			}},
			{Type: NodeLiteral, Name: "get", Children: []*Node{
				getBranch("entity"),
				getBranch("block"),
				getBranch("storage"),
			}},
			{Type: NodeLiteral, Name: "remove", Children: []*Node{
				target("entity", removePath()),
				target("block", removePath()),
				target("storage", removePath()),
			}},
			dataModifyNode(target),
		},
	}
}

// dataModifyNode builds modify: the target path, the five operations (insert
// reads its index first), and four source forms each: a target kind with
// an optional path, a string from one with an optional range, a literal, or
// a context provider's value.
// vanilla: DataCommands.decorateModification
func dataModifyNode(target func(string, ...*Node) *Node) *Node {
	// One source kind: the position, the single target or the identifier
	// its own target form reads, with the source path behind it. The
	// source head answers on its own — the whole source compound — so it
	// carries an executable; the string's range sits behind its path.
	sourceKind := func(kind string, executable bool) *Node {
		sourcePath := pathArg("sourcePath")
		sourcePath.Executable = true
		var head *Node
		switch kind {
		case "entity":
			head = &Node{Type: NodeArgument, Name: "source", Parser: ParserEntity,
				Properties: func(b *protocol.Buffer) { b.PutU8(1) },
				EntityArg:  &EntityArg{Single: true}, Executable: executable}
		case "block":
			head = &Node{Type: NodeArgument, Name: "source", Parser: ParserBlockPos,
				Executable: executable}
		default:
			head = &Node{Type: NodeArgument, Name: "source", Parser: ParserResourceLoc,
				Executable: executable}
		}
		head.Children = []*Node{sourcePath}
		return &Node{Type: NodeLiteral, Name: kind, Children: []*Node{head}}
	}
	sourceKinds := func(executable bool) []*Node {
		return []*Node{sourceKind("entity", executable), sourceKind("block", executable), sourceKind("storage", executable)}
	}
	// The string's range: the start, and the end behind it. Each source
	// path a string reads reaches it, so every kind's path carries it —
	// one closing per kind, the tree node for each being its own.
	stringKind := func(kind string) *Node {
		end := &Node{Type: NodeArgument, Name: "end", Parser: ParserInteger,
			Properties: func(b *protocol.Buffer) { b.PutU8(0) },
			Int:        &IntArg{}, Executable: true}
		start := &Node{Type: NodeArgument, Name: "start", Parser: ParserInteger,
			Properties: func(b *protocol.Buffer) { b.PutU8(0) },
			Int:        &IntArg{}, Executable: true, Children: []*Node{end}}
		sourcePath := pathArg("sourcePath")
		sourcePath.Executable = true
		sourcePath.Children = []*Node{start}
		var head *Node
		switch kind {
		case "entity":
			head = &Node{Type: NodeArgument, Name: "source", Parser: ParserEntity,
				Properties: func(b *protocol.Buffer) { b.PutU8(1) },
				EntityArg:  &EntityArg{Single: true}, Executable: true, Children: []*Node{sourcePath}}
		case "block":
			head = &Node{Type: NodeArgument, Name: "source", Parser: ParserBlockPos,
				Executable: true, Children: []*Node{sourcePath}}
		default:
			head = &Node{Type: NodeArgument, Name: "source", Parser: ParserResourceLoc,
				Executable: true, Children: []*Node{sourcePath}}
		}
		return &Node{Type: NodeLiteral, Name: kind, Children: []*Node{head}}
	}
	value := func() *Node {
		return &Node{Type: NodeArgument, Name: "value", Parser: ParserNbtTag,
			NbtValue: true, Executable: true}
	}
	// The from and the string, each with the three kinds their own words
	// name, and the literal value: the choices every operation offers.
	forms := func() []*Node {
		return []*Node{
			{Type: NodeLiteral, Name: "from", Children: sourceKinds(true)},
			{Type: NodeLiteral, Name: "string",
				Children: []*Node{stringKind("entity"), stringKind("block"), stringKind("storage")}},
			{Type: NodeLiteral, Name: "value", Children: []*Node{value()}},
			{Type: NodeLiteral, Name: "compute", Children: computeSources(false)},
		}
	}
	index := func() *Node {
		return &Node{Type: NodeArgument, Name: "index", Parser: ParserInteger,
			Properties: func(b *protocol.Buffer) { b.PutU8(0) }, Int: &IntArg{}, Children: forms()}
	}
	targetPath := func() *Node {
		node := pathArg("targetPath")
		node.Children = []*Node{
			{Type: NodeLiteral, Name: "insert", Children: []*Node{index()}},
			{Type: NodeLiteral, Name: "prepend", Children: forms()},
			{Type: NodeLiteral, Name: "append", Children: forms()},
			{Type: NodeLiteral, Name: "set", Children: forms()},
			{Type: NodeLiteral, Name: "merge", Children: forms()},
		}
		return node
	}
	return &Node{Type: NodeLiteral, Name: "modify", Children: []*Node{
		target("entity", targetPath()),
		target("block", targetPath()),
		target("storage", targetPath()),
	}}
}

// readDoubleWord reads one word as the brigadier double the scale
// argument takes, refusing a word that is no double where the vanilla
// reader throws from.
// vanilla: DoubleArgumentType.parse
func readDoubleWord(word string) (float64, *ParseError) {
	r := &coordReader{s: word}
	v, err := r.readDouble()
	if err != nil {
		return 0, err
	}
	if r.canRead() {
		return 0, &ParseError{Key: keyExpectedSeparator, Cursor: r.cur}
	}
	return v, nil
}

// checkDoubleArg refuses a word that is no double, where the vanilla
// reader throws from.
// vanilla: DoubleArgumentType.parse
func checkDoubleArg(word string) *ParseError {
	_, err := readDoubleWord(word)
	return err
}

// readNBTValue reads one SNBT value of any shape — the tag a data
// modify's value argument carries — whole and to itself, with the
// summon reader's own verdicts.
// vanilla: NbtTagArgument.parse
func readNBTValue(text string) (nbtRead, nbt.Tag) {
	return readNBTValueWith(text, false)
}

func readOrderedNBTValue(text string) (nbtRead, nbt.Tag) { return readNBTValueWith(text, true) }

func readNBTValueWith(text string, ordered bool) (nbtRead, nbt.Tag) {
	parse := nbt.ParseValueTag
	if ordered {
		parse = nbt.ParseOrderedValueTag
	}
	value, n, err := parse(text)
	if err != nil {
		return nbtMalformed, nil
	}
	if strings.Trim(text[n:], " \t\r\n") != "" {
		return nbtTrailing, value
	}
	return nbtIsCompound, value
}

// DataTarget is a body whose saved compound /data reads (entity-file form for
// mobs, player-file for players); false for a kind with no save form, which
// the command refuses.
// vanilla: EntityDataAccessor.getData
type DataTarget interface {
	DataTag() (nbt.CompoundTag, bool)
}

// DataWriter is a DataTarget the /data command can write back: the
// compound loaded onto the live body. A target that is no DataWriter — a
// player — refuses the write the way the command refuses a player's.
// vanilla: EntityDataAccessor.setData
type DataWriter interface {
	SetDataTag(data nbt.CompoundTag) bool
}

// runData runs a /data command the tree accepted. The modify form reads
// its source before its target; the rest resolve the target's words first
// and the form's own words after.
func runData(src Source, input string, args []string) (int32, error) {
	if len(args) < 2 {
		src.Debug("data command without a target")
		return 0, nil
	}
	tokens := Tokenize(input)
	if args[0] == "modify" {
		return runDataModify(src, input, args, tokens)
	}
	target, span, err := resolveDataTarget(src, args, 1, tokens, 2)
	if err != nil {
		return 0, err
	}
	rest := args[2+span:]
	switch args[0] {
	case "get":
		return runDataGet(src, target, rest, tokens)
	case "merge":
		return runDataMerge(src, target, rest, tokens)
	case "remove":
		return runDataRemove(src, target, rest, tokens)
	}
	src.Debug("data form the walk did not know", "form", args[0])
	return 0, nil
}

// dataTargetKind is which of the three a /data command resolved its
// target to.
type dataTargetKind int

const (
	dataTargetEntity dataTargetKind = iota
	dataTargetBlock
	dataTargetStorage
)

// dataTarget is a command's resolved target: where its reads go, how its
// write goes back, and everything the messages name the target with.
type dataTarget struct {
	src  Source
	kind dataTargetKind
	// storage: the id the command named.
	id string
	// block: the block entity's position.
	x, y, z int32
	// entity: the single body the selector named, and how its merged
	// compound goes back onto it; nil for a body whose writes refuse.
	entity      Entity
	writeEntity func(nbt.CompoundTag) bool
}

// resolveDataTarget resolves the target after the kind word at at (token
// tokenAt): an entity, a position or a storage id, plus the words taken. A
// refusal has already been sent when ErrRefused comes back.
// vanilla: EntityDataAccessor.access, BlockDataAccessor.access,
// StorageDataAccessor.access
func resolveDataTarget(src Source, args []string, at int, tokens []Token, tokenAt int) (dataTarget, int, error) {
	var target dataTarget
	target.src = src
	if len(args) <= at {
		return target, 0, nil
	}
	switch kind := args[at]; kind {
	case "entity":
		word, span := selectorArgAt(tokens, tokenAt+1)
		bodies, notFound := resolveRequiredEntities(src, word)
		if notFound != "" {
			return target, 0, refuse(src, notFound)
		}
		if len(bodies) == 0 {
			return target, 0, refuse(src, argumentEntityNotFoundKey)
		}
		// A body this server keeps no save form of refuses what the
		// command would read of it; every body it simulates carries one.
		if _, ok := bodies[0].(DataTarget); !ok {
			return target, 0, refuse(src, dataEntityInvalidKey)
		}
		target.kind, target.entity = dataTargetEntity, bodies[0]
		if writer, ok := bodies[0].(DataWriter); ok {
			body := bodies[0]
			target.writeEntity = func(data nbt.CompoundTag) bool {
				if !writer.SetDataTag(data) {
					return false
				}
				// The write is Entity.load of the merged tag, Team included.
				JoinTagTeam(src.Scoreboard(), body.UUID(), data.Entries())
				return true
			}
		}
		return target, span, nil
	case "block":
		if len(args)-at < 4 {
			return target, 0, nil
		}
		x, y, z, ok := resolveBlockPos(src, args[at+1:at+4])
		if !ok {
			src.Debug("data block position did not parse", "args", args[at+1:at+4])
			return target, 0, ErrRefused
		}
		if key := loadedBlockPosRefusal(src, x, y, z); key != "" {
			return target, 0, refuse(src, key)
		}
		if _, has := src.BlockEntityTagAt(x, y, z); !has {
			return target, 0, refuse(src, dataBlockInvalidKey)
		}
		target.kind, target.x, target.y, target.z = dataTargetBlock, x, y, z
		return target, 3, nil
	case "storage":
		// The messages spell the id with its namespace, the way an
		// Identifier's own text does.
		name, _, _ := readIdentifier(args[at+1])
		target.kind, target.id = dataTargetStorage, name
		return target, 1, nil
	}
	src.Debug("data target the walk did not know", "kind", args[at])
	return target, 0, ErrRefused
}

// dataTargetSpan counts the words a target's own argument takes, read
// the way resolveDataTarget reads them but resolving nothing.
func dataTargetSpan(args []string, at int, tokens []Token, tokenAt int) int {
	if at >= len(args) {
		return 0
	}
	switch args[at] {
	case "entity":
		_, span := selectorArgAt(tokens, tokenAt+1)
		return span
	case "block":
		if len(args)-at >= 4 {
			return 3
		}
	case "storage":
		if len(args)-at >= 2 {
			return 1
		}
	}
	return 0
}

// getData is the target's whole compound, the answer the vanilla
// accessor's getData hands over; a storage nobody has written reads as
// the empty compound.
// vanilla: DataAccessor.getData
func (t dataTarget) getData() nbt.CompoundTag {
	switch t.kind {
	case dataTargetBlock:
		tag, _ := t.src.BlockEntityTagAt(t.x, t.y, t.z)
		// A carried tag can alias the world: edits must reach setData before
		// changing it, or the block write sees no change and skips its update.
		return nbt.Snapshot(tag).(nbt.CompoundTag)
	case dataTargetEntity:
		tagged := t.entity.(DataTarget)
		tag, _ := tagged.DataTag()
		return tag
	default:
		var read nbt.CompoundTag
		t.src.ReadCommandStorage(t.id, func(storage nbt.CompoundTag) {
			read, _ = nbt.Snapshot(storage).(nbt.CompoundTag)
		})
		return read
	}
}

// readData keeps storage reads under their lock; fn must not retain tags or
// call Source methods.
func (t dataTarget) readData(fn func(nbt.CompoundTag)) {
	if t.kind == dataTargetStorage {
		t.src.ReadCommandStorage(t.id, fn)
	} else {
		fn(t.getData())
	}
}

// getTag snapshots only the tag at path before releasing the storage lock.
func (t dataTarget) getTag(path nbtPath) (nbt.Tag, *pathError) {
	if t.kind != dataTargetStorage {
		return getSingleTag(path, t.getData())
	}
	var tag nbt.Tag
	var refusal *pathError
	t.readData(func(data nbt.CompoundTag) {
		tag, refusal = getSingleTag(path, data)
		if refusal == nil {
			tag = nbt.Snapshot(tag)
		}
	})
	return tag, refusal
}

// modifyData commits fn's path edits when it reports a change. A storage edit
// copies only the selected root entry; fn must not retain tags or call Source methods.
func (t dataTarget) modifyData(path nbtPath, fn func(nbtPath, nbt.CompoundTag) bool) error {
	if key, scoped := path.rootKey(); t.kind == dataTargetStorage && scoped {
		t.src.ModifyCommandStorage(t.id, func(live nbt.CompoundTag) bool {
			editPath := path
			if match, ok := path.nodes[0].(matchRootObjectNode); ok {
				if !tagsMatchPattern(match.pattern, live) {
					// A failed root match cannot mutate; let the operation report its refusal.
					return fn(path, live)
				}
				// Check the predicate against the full root, retaining the node for
				// depth limits and error offsets when editing the detached entry.
				editPath.nodes = slices.Clone(path.nodes)
				editPath.nodes[0] = matchRootObjectNode{pattern: nbt.Compound{}}
			}
			data := nbt.Compound{}
			if child, had := live.Entries()[key]; had {
				data[key] = nbt.Snapshot(child)
			}
			if !fn(editPath, data) {
				return false
			}
			if child, has := data[key]; has {
				live.Set(key, child)
			} else {
				live.Delete(key)
			}
			return true
		})
		return nil
	}
	data := t.getData()
	if fn(path, data) {
		return t.setData(data)
	}
	return nil
}

// setData puts a modified compound back where the target keeps it, a whole
// write. An entity that keeps no writable form — a player — refuses here,
// after the command has found a change to make.
// vanilla: DataAccessor.setData
func (t dataTarget) setData(data nbt.CompoundTag) error {
	switch t.kind {
	case dataTargetBlock:
		t.src.SetBlockTagged(t.x, t.y, t.z, t.src.BlockAt(t.x, t.y, t.z), nil, data, false, false)
		return nil
	case dataTargetEntity:
		if t.writeEntity == nil || !t.writeEntity(data) {
			return refuse(t.src, dataEntityInvalidKey)
		}
		return nil
	default:
		t.src.SetCommandStorage(t.id, data)
		return nil
	}
}

// runDataGet runs the get form: the bare target first, the path behind
// it, the scale behind that. The scaled read answers only with numeric
// tags — the path names one, and the product, floored.
// vanilla: DataCommands.getData
func runDataGet(src Source, target dataTarget, rest []string, tokens []Token) (int32, error) {
	if len(rest) == 0 {
		return 1, dataQuery(src, target, target.getData())
	}
	span := pathSpanOf(rest)
	path, err := parseNBTPath(wordsInTokens(tokens, rest, 0, span))
	if err != nil {
		src.Debug("data get path did not parse", "path", rest[:span])
		return 0, nil
	}
	tag, refusal := target.getTag(path)
	if refusal != nil {
		return 0, refuse(src, refusal.key, tagTexts(refusal)...)
	}
	if span >= len(rest) {
		return tagResult(tag), dataQuery(src, target, tag)
	}
	scale, perr := readDoubleWord(rest[span])
	if perr != nil {
		src.Debug("data get scale did not parse", "scale", rest[span])
		return 0, nil
	}
	value, numeric := numericDouble(tag)
	if !numeric {
		return 0, refuse(src, dataGetInvalidKey, protocol.Text{Text: path.original})
	}
	result := floorToInt32(value * scale)
	return result, dataGetScaled(src, target, path.original, scale, int64(result))
}

// tagResult is the return value of a get that names one tag: a number
// floored, a list's, an array's or a compound's size, a string's length in
// UTF-16 units.
// vanilla: DataCommands.getData(CommandSourceStack, DataAccessor, NbtPath)
func tagResult(tag nbt.Tag) int32 {
	switch v := tag.(type) {
	case nbt.List:
		return int32(len(v.Items))
	case nbt.ByteArray:
		return int32(len(v))
	case nbt.IntArray:
		return int32(len(v))
	case nbt.LongArray:
		return int32(len(v))
	case nbt.CompoundTag:
		return int32(len(v.Entries()))
	case nbt.String:
		return int32(len(utf16.Encode([]rune(string(v)))))
	}
	value, _ := numericDouble(tag)
	return floorToInt32(value)
}

// runDataMerge runs the merge form: the compound argument over the
// target's own, every entry of it replacing the entry it names, itself
// refused where nothing changed.
// vanilla: DataCommands.mergeData
func runDataMerge(src Source, target dataTarget, rest []string, tokens []Token) (int32, error) {
	verdict, value := readOrderedNBTValue(wordsInTokens(tokens, rest, 0, len(rest)))
	compound, isCompound := value.(nbt.CompoundTag)
	if verdict == nbtIsCompound && !isCompound {
		verdict = nbtNotCompound
	}
	switch verdict {
	case nbtNotCompound:
		return 0, refuse(src, summonBadNBTKey)
	case nbtTrailing:
		return 0, refuse(src, summonTrailingNBTKey)
	case nbtMalformed:
		src.Debug("data merge compound did not parse", "nbt", rest)
		return 0, nil
	}
	data := target.getData()
	if nbtTooDeep(compound, 0) {
		return 0, refuse(src, pathTooDeepKey)
	}
	merged := nbt.OrderedCopy(data).(nbt.CompoundTag)
	mergeCompound(merged, compound)
	if nbt.Equal(merged, data) {
		return 0, refuse(src, dataMergeFailedKey)
	}
	if err := target.setData(merged); err != nil {
		return 0, err
	}
	return 1, dataModified(src, target)
}

// runDataRemove runs the remove form: every place the path names in the
// target, taken out.
// vanilla: DataCommands.removeData
func runDataRemove(src Source, target dataTarget, rest []string, tokens []Token) (int32, error) {
	path, err := parseNBTPath(wordsInTokens(tokens, rest, 0, len(rest)))
	if err != nil {
		src.Debug("data remove path did not parse", "path", rest)
		return 0, nil
	}
	removed := 0
	writeErr := target.modifyData(path, func(path nbtPath, data nbt.CompoundTag) bool {
		removed = path.remove(data)
		return removed > 0
	})
	if removed == 0 {
		return 0, refuse(src, dataMergeFailedKey)
	}
	if writeErr != nil {
		return 0, writeErr
	}
	return int32(removed), dataModified(src, target)
}

// runDataModify runs modify: a path, one of five operations, and a value from
// the source form after it. The source resolves before the target.
// vanilla: DataCommands.manipulateData
func runDataModify(src Source, input string, args []string, tokens []Token) (int32, error) {
	const at, tokenAt = 1, 2
	targetSpan := dataTargetSpan(args, at, tokens, tokenAt)
	rest := args[at+1+targetSpan:]
	restTokenAt := at + targetSpan + 2
	if len(rest) < 3 {
		src.Debug("data modify without its operation")
		return 0, nil
	}
	span := pathSpanOf(rest)
	path, err := parseNBTPath(wordsInTokens(tokens, rest, 0, span))
	if err != nil {
		src.Debug("data modify path did not parse", "path", rest[:span])
		return 0, nil
	}
	i := span
	if i >= len(rest) {
		src.Debug("data modify without its operation")
		return 0, nil
	}
	op := rest[i]
	i++
	index := 0
	if op == "insert" {
		if i >= len(rest) {
			src.Debug("data modify insert without its index")
			return 0, nil
		}
		parsed, perr := strconv.Atoi(rest[i])
		if perr != nil {
			src.Debug("data modify insert index did not parse", "index", rest[i])
			return 0, nil
		}
		index, i = parsed, i+1
	}
	if i >= len(rest) {
		src.Debug("data modify without its source")
		return 0, nil
	}
	form := rest[i]
	i++
	sources := rest[i:]
	var values []nbt.Tag
	switch form {
	case "value":
		if verdict, tag := readOrderedNBTValue(wordsInTokens(tokens, sources, 0, len(sources))); verdict == nbtIsCompound {
			values = []nbt.Tag{tag}
		} else {
			src.Debug("data modify value did not parse", "nbt", sources)
			return 0, nil
		}
	case "from", "string":
		source, sourceArgSpan, sourceErr := resolveDataTarget(src, rest, i, tokens, restTokenAt+i)
		if sourceErr != nil {
			return 0, sourceErr
		}
		// The source's own kind word plus the words its argument took.
		sourceRest := rest[i+1+sourceArgSpan:]
		var sourcePath nbtPath
		if len(sourceRest) > 0 {
			sourceSpan := pathSpanOf(sourceRest)
			sourcePath, err = parseNBTPath(wordsInTokens(tokens, sourceRest, 0, sourceSpan))
			if err != nil {
				src.Debug("data modify source path did not parse", "path", sourceRest[:sourceSpan])
				return 0, nil
			}
			sourceRest = sourceRest[sourceSpan:]
		}
		if form == "string" {
			processor, _ := substringProcessor(src, sourceRest)
			if processor == nil {
				return 0, nil
			}
			var stringRefusal *pathError
			source.readData(func(data nbt.CompoundTag) {
				values, stringRefusal = stringSources(data, sourcePath, processor)
			})
			if stringRefusal != nil {
				return 0, refuse(src, stringRefusal.key, tagTexts(stringRefusal)...)
			}
		} else {
			var sourceRefusal *pathError
			source.readData(func(data nbt.CompoundTag) {
				values, sourceRefusal = fromSourcesOf(data, sourcePath)
				if source.kind == dataTargetStorage {
					for i, value := range values {
						values[i] = nbt.Snapshot(value)
					}
				}
			})
			if sourceRefusal != nil {
				return 0, refuse(src, sourceRefusal.key, tagTexts(sourceRefusal)...)
			}
		}
	case "compute":
		tag, err := dataComputeValue(src, input, sources)
		if tag == nil {
			return 0, err
		}
		values = []nbt.Tag{tag}
	default:
		src.Debug("data modify source the walk did not know", "form", form)
		return 0, nil
	}
	if len(values) == 0 {
		src.Debug("data modify without a value to write")
		return 0, nil
	}
	target, _, targetErr := resolveDataTarget(src, args, at, tokens, tokenAt)
	if targetErr != nil {
		return 0, targetErr
	}
	switch op {
	case "set", "insert", "append", "prepend", "merge":
	default:
		src.Debug("data modify operation the walk did not know", "op", op)
		return 0, nil
	}
	var changed int
	var refusal *pathError
	writeErr := target.modifyData(path, func(path nbtPath, data nbt.CompoundTag) bool {
		switch op {
		case "set":
			changed, refusal = path.set(data, values[len(values)-1])
		case "insert":
			changed, refusal = path.insert(index, data, values)
		case "append":
			changed, refusal = path.insert(-1, data, values)
		case "prepend":
			changed, refusal = path.insert(0, data, values)
		case "merge":
			changed, refusal = modifyMerge(path, data, values)
		}
		return changed > 0 && refusal == nil
	})
	if refusal != nil {
		return 0, refuse(src, refusal.key, tagTexts(refusal)...)
	}
	if changed == 0 {
		return 0, refuse(src, dataMergeFailedKey)
	}
	if writeErr != nil {
		return 0, writeErr
	}
	return int32(changed), dataModified(src, target)
}

// dataComputeValue evaluates a compute source's provider in the context it
// names: an int tag or a float tag. A nil tag means the command stops with
// the error. An arithmetic failure stores 0, as does a NaN or infinite
// float, and a float is stored unscaled.
// vanilla: DataCommands.decorateModification (getInt / getFloat)
func dataComputeValue(src Source, input string, words []string) (nbt.Tag, error) {
	lc, rest, err := computeContext(src, input, words)
	if lc == nil {
		return nil, err
	}
	p, kind, _ := readComputeProvider(src, input, rest)
	switch kind {
	case "integer":
		v, err := rules.EvalIntProvider(p, lc, src.Random())
		if err != nil {
			return nbt.Int(0), nil
		}
		return nbt.Int(v), nil
	case "float":
		v, err := rules.EvalFloatProvider(p, lc, src.Random())
		if f := float64(v); err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nbt.Float(0), nil
		}
		return nbt.Float(v), nil
	}
	return nil, nil
}

// mergeCompound merges src into dst: a compound under a name both hold is
// merged the same way into the one already there, anything else takes the
// incoming value.
// vanilla: CompoundTag.merge
func mergeCompound(dst, src nbt.CompoundTag) {
	for _, name := range nbt.HashOrderedKeys(src) {
		value := src.Entries()[name]
		if incoming, ok := value.(nbt.CompoundTag); ok {
			if existing, ok := dst.Entries()[name].(nbt.CompoundTag); ok {
				mergeCompound(existing, incoming)
				continue
			}
		}
		dst.Set(name, nbt.OrderedCopy(value))
	}
}

// modifyMerge merges the sources' compound into each place the path names
// (created where absent), answering how many changed; a non-compound source
// or place is vanilla's refusal.
// vanilla: DataCommands.mergeData's manipulator
func modifyMerge(path nbtPath, data nbt.CompoundTag, values []nbt.Tag) (int, *pathError) {
	combined := &nbt.OrderedCompound{}
	for _, value := range values {
		compound, ok := value.(nbt.CompoundTag)
		if !ok {
			return 0, pathRefusal(dataExpectedObjectKey, nbt.Stringify(value))
		}
		mergeCompound(combined, compound)
	}
	places, refusal := path.placesCreated(data, func() nbt.Tag { return &nbt.OrderedCompound{} })
	if refusal != nil {
		return 0, refusal
	}
	changed := 0
	for _, place := range places {
		compound, ok := place.get.(nbt.CompoundTag)
		if !ok {
			return 0, pathRefusal(dataExpectedObjectKey, nbt.Stringify(place.get))
		}
		before := nbt.Copy(compound)
		mergeCompound(compound, combined)
		if !nbt.Equal(before, compound) {
			changed++
		}
	}
	return changed, nil
}

// substringProcessor reads the range a string's source keeps: none, the
// start, or the start and the end, negatives counting from the end of
// the value.
// vanilla: DataCommands.substring
func substringProcessor(src Source, words []string) (func(string) (string, *pathError), *ParseError) {
	start, end := 0, 0
	hasStart, hasEnd := false, false
	if len(words) > 0 {
		parsed, err := strconv.Atoi(words[0])
		if err != nil {
			src.Debug("data modify string start did not parse", "start", words[0])
			return nil, nil
		}
		start, hasStart = parsed, true
	}
	if len(words) > 1 {
		parsed, err := strconv.Atoi(words[1])
		if err != nil {
			src.Debug("data modify string end did not parse", "end", words[1])
			return nil, nil
		}
		end, hasEnd = parsed, true
	}
	if len(words) > 2 {
		src.Debug("data modify string with more than its range")
		return nil, nil
	}
	if !hasStart {
		return func(text string) (string, *pathError) { return text, nil }, nil
	}
	return func(text string) (string, *pathError) {
		units := utf16.Encode([]rune(text)) // vanilla: String.substring counts UTF-16 units
		from := substringOffset(start, len(units))
		to := len(units)
		if hasEnd {
			to = substringOffset(end, len(units))
		}
		if from < 0 || to > len(units) || from > to {
			return "", pathRefusal(dataInvalidSubstringKey, strconv.Itoa(from), strconv.Itoa(to))
		}
		return string(utf16.Decode(units[from:to])), nil
	}, nil
}

// substringOffset is where an index of a range falls in a value of the
// length given, negatives counting from its end.
// vanilla: DataCommands.getOffset
func substringOffset(offset, length int) int {
	if offset < 0 {
		return length + offset
	}
	return offset
}

// tagAsText reads a tag the way the string's source does: a string as its
// own text, a number as its own compact SNBT, anything else refusing.
// vanilla: DataCommands.getAsText
func tagAsText(tag nbt.Tag) (string, bool) {
	switch v := tag.(type) {
	case nbt.String:
		return string(v), true
	case nbt.Byte, nbt.Short, nbt.Int, nbt.Long, nbt.Float, nbt.Double:
		return nbt.Stringify(tag), true
	}
	return "", false
}

// fromSourcesOf answers the tags a from's source path names in the
// source's data, or the source's whole compound where the path was left
// out.
// vanilla: DataCommands.resolveSourcePath, getSingletonSource
func fromSourcesOf(data nbt.CompoundTag, path nbtPath) ([]nbt.Tag, *pathError) {
	if path.nodes == nil {
		return []nbt.Tag{data}, nil
	}
	return path.get(data)
}

// stringSources answers the strings the string's source path names: each
// tag as its own text, a number as its compact SNBT, cut to the range the
// processor takes of each.
// vanilla: DataCommands.getAsText, stringifyTagList
func stringSources(data nbt.CompoundTag, path nbtPath, processor func(string) (string, *pathError)) ([]nbt.Tag, *pathError) {
	tags, err := path.get(data)
	if err != nil {
		return nil, err
	}
	var out []nbt.Tag
	for _, tag := range tags {
		text, ok := tagAsText(tag)
		if !ok {
			return nil, pathRefusal(dataExpectedValueKey, nbt.Stringify(tag))
		}
		processed, err := processor(text)
		if err != nil {
			return nil, err
		}
		out = append(out, nbt.String(processed))
	}
	return out, nil
}

// getSingleTag answers the one tag the path names; none or several are
// the vanilla single-value refusal, which carries no values of its own.
// vanilla: DataCommands.getSingleTag
func getSingleTag(path nbtPath, data nbt.CompoundTag) (nbt.Tag, *pathError) {
	tags, err := path.get(data)
	if err != nil {
		return nil, err
	}
	if len(tags) > 1 {
		return nil, pathRefusal(dataGetMultipleKey)
	}
	return tags[0], nil
}

// numericDouble is a numeric tag's value as the game's own reader takes
// it; ok is false for a tag no number reads.
func numericDouble(tag nbt.Tag) (float64, bool) {
	switch v := tag.(type) {
	case nbt.Byte:
		return float64(v), true
	case nbt.Short:
		return float64(v), true
	case nbt.Int:
		return float64(v), true
	case nbt.Long:
		return float64(v), true
	case nbt.Float:
		return float64(v), true
	case nbt.Double:
		return float64(v), true
	}
	return 0, false
}

// floorToInt32 floors f into an int as Java's (int) Math.floor does: NaN
// reads as nought, the cast saturates at the int's bounds, and a negative
// fraction steps the truncation down one.
// vanilla: Mth.floor
func floorToInt32(f float64) int32 {
	switch {
	case math.IsNaN(f):
		return 0
	case f >= math.MaxInt32:
		return math.MaxInt32
	case f <= math.MinInt32:
		return math.MinInt32
	}
	i := int32(math.Trunc(f))
	if f < float64(i) {
		return i - 1
	}
	return i
}

// javaFixed is Java's String.format(Locale.ROOT, "%.<prec>f", v): it rounds
// the shortest decimal form of v, halves away from zero, keeps the sign of a
// negative that rounds to zero, and spells out NaN and the infinities.
func javaFixed(v float64, prec int) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "Infinity"
	case math.IsInf(v, -1):
		return "-Infinity"
	}
	exact, _ := new(big.Rat).SetString(strconv.FormatFloat(v, 'g', -1, 64))
	s := exact.FloatString(prec)
	if math.Signbit(v) && s[0] != '-' {
		s = "-" + s
	}
	return s
}

// dataQuery answers a bare target or path query with the target's name and
// the tag's pretty text, to the runner only: a get never informs operators.
// vanilla: DataAccessor.getPrintSuccess(Tag)
func dataQuery(src Source, target dataTarget, tag nbt.Tag) error {
	switch target.kind {
	case dataTargetBlock:
		return src.Feedback(dataBlockQueryKey, append(coordinates(target), prettyText(tag))...)
	case dataTargetEntity:
		return src.Feedback(dataEntityQueryKey, target.entity.DisplayName(), prettyText(tag))
	}
	return src.Feedback(dataStorageQueryKey, protocol.Text{Text: target.id}, prettyText(tag))
}

// dataGetScaled answers the scaled read: the path, the target named its
// own way, the scale at two decimals and the floored product.
// vanilla: DataAccessor.getPrintSuccess(NbtPath, double, int)
func dataGetScaled(src Source, target dataTarget, path string, scale float64, value int64) error {
	scaleText := protocol.Text{Text: javaFixed(scale, 2)}
	valueText := protocol.Text{Number: &value}
	pathText := protocol.Text{Text: path}
	switch target.kind {
	case dataTargetBlock:
		with := append([]protocol.Text{pathText}, coordinates(target)...)
		with = append(with, scaleText, valueText)
		return src.Feedback(dataBlockGetKey, with...)
	case dataTargetEntity:
		return src.Feedback(dataEntityGetKey, pathText, target.entity.DisplayName(), scaleText, valueText)
	}
	return src.Feedback(dataStorageGetKey, pathText, protocol.Text{Text: target.id}, scaleText, valueText)
}

// dataModified answers the line every write sends, which the vanilla
// server also broadcasts to the other operators.
// vanilla: DataAccessor.getModifiedSuccess
func dataModified(src Source, target dataTarget) error {
	if src.Silent() {
		return nil
	}
	switch target.kind {
	case dataTargetBlock:
		return feedbackToAdmins(src, dataBlockModifiedKey, coordinates(target)...)
	case dataTargetEntity:
		return feedbackToAdmins(src, dataEntityModifiedKey, target.entity.DisplayName())
	}
	return feedbackToAdmins(src, dataStorageModifiedKey, protocol.Text{Text: target.id})
}

// coordinates are the block position as the plain texts the block
// messages carry it in.
func coordinates(target dataTarget) []protocol.Text {
	return []protocol.Text{
		{Text: strconv.FormatInt(int64(target.x), 10)},
		{Text: strconv.FormatInt(int64(target.y), 10)},
		{Text: strconv.FormatInt(int64(target.z), 10)},
	}
}

// tagTexts are the values a path refusal's message takes.
func tagTexts(refusal *pathError) []protocol.Text {
	with := make([]protocol.Text, 0, len(refusal.with))
	for _, value := range refusal.with {
		with = append(with, protocol.Text{Text: value})
	}
	return with
}

// prettyText is a tag as its own message text.
func prettyText(tag nbt.Tag) protocol.Text {
	return PrettyNBTText(tag, true)
}

// prettyNBTMaxDepth and prettyNBTMaxLength are where the pretty form folds
// a tag to <...>: nesting depth, and elements shown per array.
// vanilla: TextComponentTagVisitor.MAX_DEPTH, MAX_LENGTH
const (
	prettyNBTMaxDepth  = 64
	prettyNBTMaxLength = 128
)

// prettyWriter collects the pieces of a pretty tag; plain drops the colours.
type prettyWriter struct {
	pieces []protocol.Text
	rich   bool
}

func (w *prettyWriter) add(text, color string) {
	if !w.rich {
		color = ""
	}
	w.pieces = append(w.pieces, protocol.Text{Text: text, Color: color})
}

// PrettyNBTText renders a tag as the chat's NBT component does: a space
// after every colon and comma, syntax highlighting when rich, and arrays and
// nesting folded to <...>. Keys follow Java HashMap bucket order; the empty
// indentation pieces vanilla sends are left out.
// vanilla: TextComponentTagVisitor
func PrettyNBTText(tag nbt.Tag, rich bool) protocol.Text {
	w := prettyWriter{rich: rich}
	w.tag(tag, 0)
	return protocol.Text{Extra: w.pieces}
}

// number writes a number's digits gold and its type suffix red, the digits
// being what nbt.Stringify writes less the suffix.
func (w *prettyWriter) number(tag nbt.Tag, suffix string) {
	w.add(strings.TrimSuffix(nbt.Stringify(tag), suffix), protocol.ColorGold)
	if suffix != "" {
		w.add(suffix, protocol.ColorRed)
	}
}

// quoted writes a string, or a key, with the quotes unstyled.
func (w *prettyWriter) quoted(s, color string) {
	q := nbt.Stringify(nbt.String(s))
	w.add(q[:1], "")
	w.add(q[1:len(q)-1], color)
	w.add(q[:1], "")
}

func (w *prettyWriter) folded() { w.add("<...>", protocol.ColorGray) }

// array writes a typed array's prefix, then up to the first 128 elements.
func (w *prettyWriter) array(prefix string, n int, element func(int)) {
	w.add("[", "")
	w.add(prefix, protocol.ColorRed)
	w.add(";", "")
	for i := 0; i < n && i < prettyNBTMaxLength; i++ {
		w.add(" ", "")
		element(i)
		if i != n-1 {
			w.add(",", "")
		}
	}
	if n > prettyNBTMaxLength {
		w.folded()
	}
	w.add("]", "")
}

func (w *prettyWriter) tag(tag nbt.Tag, depth int) {
	switch v := tag.(type) {
	case nbt.Byte:
		w.number(v, "b")
	case nbt.Short:
		w.number(v, "s")
	case nbt.Int:
		w.number(v, "")
	case nbt.Long:
		w.number(v, "L")
	case nbt.Float:
		w.number(v, "f")
	case nbt.Double:
		w.number(v, "d")
	case nbt.String:
		w.quoted(string(v), protocol.ColorGreen)
	case nbt.ByteArray:
		w.array("B", len(v), func(i int) { w.number(nbt.Byte(int8(v[i])), "b") })
	case nbt.IntArray:
		w.array("I", len(v), func(i int) { w.number(nbt.Int(v[i]), "") })
	case nbt.LongArray:
		w.array("L", len(v), func(i int) { w.number(nbt.Long(v[i]), "L") })
	case nbt.List:
		w.add("[", "")
		if len(v.Items) > 0 && depth >= prettyNBTMaxDepth {
			w.folded()
		}
		if depth < prettyNBTMaxDepth {
			for i, item := range v.Items {
				if i != 0 {
					w.add(",", "")
					w.add(" ", "")
				}
				w.tag(nbt.ListElement(item), depth+1)
			}
		}
		w.add("]", "")
	case nbt.CompoundTag:
		w.add("{", "")
		if len(v.Entries()) > 0 && depth >= prettyNBTMaxDepth {
			w.folded()
		}
		if depth < prettyNBTMaxDepth {
			for i, key := range nbt.HashOrderedKeys(v) {
				if i != 0 {
					w.add(",", "")
					w.add(" ", "")
				}
				if prettySimpleKey(key) {
					w.add(key, protocol.ColorAqua)
				} else {
					w.quoted(key, protocol.ColorAqua)
				}
				w.add(":", "")
				w.add(" ", "")
				w.tag(v.Entries()[key], depth+1)
			}
		}
		w.add("}", "")
	}
}

// prettySimpleKey reports whether a key is written without quotes.
// vanilla: TextComponentTagVisitor.SIMPLE_VALUE
func prettySimpleKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case c == '.', c == '_', c == '+', c == '-':
		default:
			return false
		}
	}
	return true
}
