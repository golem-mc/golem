package command

import (
	"math"
	"math/rand"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/golem-mc/golem/internal/commandsyntax"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The translation keys the vanilla server's EntitySelectorParser and
// EntitySelectorOptions throw while reading a target selector, and the ones
// EntityArgument throws when the argument's shape and the selector disagree.
const (
	keySelectorUnknown      = "argument.entity.selector.unknown"
	keySelectorNotAllowed   = "argument.entity.selector.not_allowed"
	keySelectorMissing      = "argument.entity.selector.missing"
	keyOptionsUnterminated  = "argument.entity.options.unterminated"
	keyOptionsLimitTooSmall = "argument.entity.options.limit.toosmall"
	keyRangeEmpty           = "argument.range.empty"
	keyRangeSwapped         = "argument.range.swapped"
	keyTooManyEntities      = "argument.entity.toomany"
	keyTooManyPlayers       = "argument.player.toomany"
	keyOnlyPlayers          = "argument.player.entities"
)

// ParseError is a Brigadier parse failure, which the client renders as a red
// message and a context line pointing at Cursor, an offset in the whole
// input. With fills a dynamic message's %s; a failure the client has no key
// for carries prose in Text instead.
type ParseError struct {
	Key    string
	With   []string
	Cursor int
	Text   string
	// NoContext says the refusal carries no input, so the caller sends the
	// message alone with no context line. A message argument's over-length
	// refusal is built this way. vanilla: MessageArgument.Message.TOO_LONG
	NoContext bool
}

// Error makes ParseError an error, spelling the message the way the client
// will read it once the translation lands.
func (e *ParseError) Error() string {
	if e.Key != "" {
		return messageOf(e.Key, e.With...)
	}
	return e.Text
}

// EntityArg is an entity parser's properties byte: single or multiple
// targets, players only or any entity. vanilla's EntityArgument checks the
// selector against it while parsing.
type EntityArg struct {
	Single      bool
	PlayersOnly bool

	// Profile says the argument is a game profile one, whose players-only
	// check has no exception for @s and reports where the selector ended
	// rather than at the input's start.
	Profile bool
}

// Selector is a parsed target selector: which players and simulated entities
// it names and its bracket filters. vanilla: EntitySelector
type Selector struct {
	commandsyntax.Selector
	predicates []func(Entity) bool
}

type selectorCacheKey struct {
	word  string
	allow bool
}

var selectorCache = struct {
	sync.Mutex
	entries map[selectorCacheKey]*Selector
}{entries: make(map[selectorCacheKey]*Selector)}

// parsedSelector shares successful, immutable parses between runtime resolvers.
// Permission is part of the key so a privileged parse cannot authorize a selector.
func parsedSelector(word string, allowSelectors bool) (*Selector, *ParseError) {
	key := selectorCacheKey{word, allowSelectors}
	selectorCache.Lock()
	defer selectorCache.Unlock()
	if sel := selectorCache.entries[key]; sel != nil {
		return sel, nil
	}
	sel, _, err := parseSelectorWord(word, 0, allowSelectors)
	if err != nil {
		return nil, err
	}
	selectorCache.entries[key] = sel
	if len(selectorCache.entries) > 1024 {
		clear(selectorCache.entries)
	}
	return sel, nil
}

type bounds = commandsyntax.Bounds

var parseFloatEnd = commandsyntax.ParseFloatEnd
var parseIntEnd = commandsyntax.ParseIntEnd
var isAllowedInUnquotedString = commandsyntax.IsAllowedInUnquotedString

func syntaxError(err *commandsyntax.Error) *ParseError {
	if err == nil {
		return nil
	}
	return &ParseError{Key: err.Key, With: err.With, Cursor: err.Cursor, Text: err.Text}
}

// selReader adapts range callers to the shared grammar.
type selReader struct {
	input string
	pos   int
}

func (r *selReader) canRead() bool { return r.pos < len(r.input) }
func (r *selReader) parseRange(parse func(string) (float64, bool), invalid string, swapped bool) (*bounds, *ParseError) {
	b, n, err := commandsyntax.ParseRange(r.input[r.pos:], parse, invalid, swapped)
	fail := syntaxError(err)
	if fail != nil {
		fail.Cursor += r.pos
	}
	r.pos += n
	return b, fail
}

func parseSelectorWord(text string, start int, allow bool) (*Selector, int, *ParseError) {
	parsed, _, n, err := commandsyntax.ParseSelector(text, start, allow, entityTypeExists)
	if err != nil {
		return nil, 0, syntaxError(err)
	}
	sel := &Selector{Selector: *parsed}
	for _, f := range parsed.Filters {
		sel.predicates = append(sel.predicates, selectorFilter(f))
	}

	return sel, n, nil
}

// selectorFilter binds parsed values once; registry tags and progress stay live.
func selectorFilter(f commandsyntax.Filter) func(Entity) bool {
	switch f.Kind {
	case commandsyntax.FilterAlive:
		return func(e Entity) bool { return e.Alive() }
	case commandsyntax.FilterName:
		return func(e Entity) bool { return (e.PlainName() == f.Name) != f.Invert }
	case commandsyntax.FilterLevel:
		return func(e Entity) bool {
			player, ok := e.(Target)
			return ok && f.Bounds.Matches(float64(player.ExperienceLevels()))
		}
	case commandsyntax.FilterGamemode:
		return func(e Entity) bool {
			target, ok := e.(Target)
			if !ok {
				return f.Invert
			}
			return (target.GameModeNumber() == f.Mode) != f.Invert
		}
	case commandsyntax.FilterType:
		return func(e Entity) bool { return (e.EntityType() == f.Name) != f.Invert }
	case commandsyntax.FilterTypeTag:
		return func(e Entity) bool {
			typ, known := registry.EntityTypeID(e.EntityType())
			return (known && registry.EntityTypeInTag(typ, f.Name)) != f.Invert
		}
	case commandsyntax.FilterTag:
		tag := TagFilter{Name: f.Name, Invert: f.Invert}
		return func(e Entity) bool {
			tags := e.Tags()
			return tag.Passes(len(tags), func(name string) bool { return slices.Contains(tags, name) })
		}
	case commandsyntax.FilterNBT:
		return func(e Entity) bool {
			var data nbt.CompoundTag
			if tagged, ok := e.(DataTarget); ok {
				data, _ = tagged.DataTag()
			}
			return tagsMatchPattern(f.Pattern, data) != f.Invert
		}
	case commandsyntax.FilterAdvancements:
		return advancementsFilter(f.Advancements)
	default:
		panic("unknown selector filter")
	}
}

// anchorPosition is the point the distance and the volume are measured from:
// the x, y, z options over the runner's own coordinates, which is how
// EntitySelector's position function resolves them against its source.
func (sel *Selector) anchorPosition(src Source) player.Position {
	pos := src.Position()
	if sel.X != nil {
		pos.X = *sel.X
	}
	if sel.Y != nil {
		pos.Y = *sel.Y
	}
	if sel.Z != nil {
		pos.Z = *sel.Z
	}
	return pos
}

// volume is the volume options' box: each axis from 0 to the delta plus a
// block, or from the delta to a block for a negative delta, or, with only a maximum
// distance, its cube grown a block each way; false for neither.
// vanilla: EntitySelectorParser.createAabb
func (sel *Selector) volume(pos player.Position) (minX, minY, minZ, maxX, maxY, maxZ float64, ok bool) {
	if sel.Dx != nil || sel.Dy != nil || sel.Dz != nil {
		dx, dy, dz := 0.0, 0.0, 0.0
		if sel.Dx != nil {
			dx = *sel.Dx
		}
		if sel.Dy != nil {
			dy = *sel.Dy
		}
		if sel.Dz != nil {
			dz = *sel.Dz
		}
		low := func(d float64) float64 {
			if d < 0 {
				return d
			}
			return 0
		}
		high := func(d float64) float64 {
			if d < 0 {
				return 1
			}
			return d + 1
		}
		return pos.X + low(dx), pos.Y + low(dy), pos.Z + low(dz),
			pos.X + high(dx), pos.Y + high(dy), pos.Z + high(dz), true
	}
	if sel.Distance != nil && sel.Distance.HasMax {
		max := sel.Distance.Max
		return pos.X - max, pos.Y - max, pos.Z - max,
			pos.X + max + 1, pos.Y + max + 1, pos.Z + max + 1, true
	}
	return 0, 0, 0, 0, 0, 0, false
}

// predicate builds the whole test an entity passes: the options' filters, the
// volume's box, then the distance — EntitySelector.getPredicate's order.
func (sel *Selector) predicate(src Source, pos player.Position) func(Entity) bool {
	minX, minY, minZ, maxX, maxY, maxZ, hasBox := sel.volume(pos)
	var filters []func(Entity) bool
	filters = append(filters, sel.predicates...)
	for _, want := range sel.LootPredicates {
		filters = append(filters, lootPredicateWant(want).filter(src))
	}
	if sel.Scores != nil {
		filters = append(filters, sel.scoresFilter(src))
	}
	if sel.Teams != nil {
		filters = append(filters, sel.teamsFilter(src))
	}
	if hasBox {
		filters = append(filters, func(e Entity) bool {
			bMinX, bMinY, bMinZ, bMaxX, bMaxY, bMaxZ := e.Box()
			return bMaxX > minX && bMinX < maxX &&
				bMaxY > minY && bMinY < maxY &&
				bMaxZ > minZ && bMinZ < maxZ
		})
	}
	if sel.Distance != nil {
		filters = append(filters, func(e Entity) bool {
			p := e.Position()
			dx, dy, dz := p.X-pos.X, p.Y-pos.Y, p.Z-pos.Z
			return sel.Distance.MatchesSqr(dx*dx + dy*dy + dz*dz)
		})
	}
	if sel.RotX != nil {
		filters = append(filters, rotationFilter(sel.RotX, func(e Entity) float32 {
			return e.Position().Pitch
		}))
	}
	if sel.RotY != nil {
		filters = append(filters, rotationFilter(sel.RotY, func(e Entity) float32 {
			return e.Position().Yaw
		}))
	}
	switch len(filters) {
	case 0:
		return acceptAll
	case 1:
		return filters[0]
	}
	return func(e Entity) bool {
		for _, filter := range filters {
			if !filter(e) {
				return false
			}
		}
		return true
	}
}

func acceptAll(Entity) bool { return true }

// rotationFilter wraps the angle and range ends into a turn.
// vanilla: EntitySelectorParser.createRotationPredicate
func rotationFilter(b *bounds, angle func(Entity) float32) func(Entity) bool {
	min := wrapAngle(0)
	if b.HasMin {
		min = wrapAngle(float64(float32(b.Min)))
	}
	max := wrapAngle(359)
	if b.HasMax {
		max = wrapAngle(float64(float32(b.Max)))
	}
	return func(e Entity) bool {
		a := wrapAngle(float64(angle(e)))
		if min > max {
			return a >= min || a <= max
		}
		return a >= min && a <= max
	}
}

// wrapDegrees brings an angle into the range -180 to 180 the way
// Mth.wrapDegrees does.
func wrapAngle(a float64) float64 {
	a = math.Mod(a, 360)
	if a >= 180 {
		a -= 360
	}
	if a < -180 {
		a += 360
	}
	return a
}

// collectLimit is EntitySelector.getResultLimit: everything for a sorted
// selector — the sort decides what survives — the limit itself for an
// arbitrary one.
func (sel *Selector) collectLimit() int {
	if sel.Order == commandsyntax.OrderArbitrary {
		return sel.MaxResults
	}
	return commandsyntax.MaxAll
}

// sortAndLimit orders the picks the selector's sort asks for and cuts them to
// the limit, EntitySelector.sortAndLimit's own two steps.
func (sel *Selector) sortAndLimit(src Source, pos player.Position, entities []Entity) []Entity {
	if len(entities) > 1 {
		switch sel.Order {
		case commandsyntax.OrderNearest, commandsyntax.OrderFurthest:
			type pick struct {
				d float64
				e Entity
			}
			picks := make([]pick, len(entities))
			for i, e := range entities {
				p := e.Position()
				picks[i] = pick{entity.Dist2(p.X, p.Y, p.Z, pos.X, pos.Y, pos.Z), e}
			}
			direction := 1
			if sel.Order == commandsyntax.OrderFurthest {
				direction = -1
			}
			compare := func(a, b pick) int {
				if a.d < b.d {
					return -direction
				}
				if a.d > b.d {
					return direction
				}
				return 0
			}
			// Both operations keep the first pick on ties (entity ID order).
			// vanilla: EntitySelectorParser.ORDER_NEAREST / ORDER_FURTHEST
			if sel.MaxResults == 1 {
				entities[0] = slices.MinFunc(picks, compare).e
			} else {
				slices.SortStableFunc(picks, compare)
				for i, p := range picks {
					entities[i] = p.e
				}
			}
		case commandsyntax.OrderRandom:
			src.Random().Shuffle(len(entities), func(i, j int) {
				entities[i], entities[j] = entities[j], entities[i]
			})
		}
	}
	n := min(sel.MaxResults, len(entities))
	return entities[:n:n]
}

// findEntities resolves the selector: players-only through the player list,
// a bare name or UUID straight to its entity ignoring filters, the rest
// swept from the world. Empty is the caller's not-found.
// vanilla: EntitySelector.findEntities
func (sel *Selector) findEntities(src Source) []Entity {
	if !sel.IncludesEntities {
		return asEntities(sel.findPlayers(src))
	}
	if sel.Name != "" {
		for _, candidate := range src.OnlineTargets() {
			if strings.EqualFold(candidate.Name(), sel.Name) { // vanilla: PlayerList.getPlayerByName
				return []Entity{candidate}
			}
		}
		return nil
	}
	if sel.HasUUID {
		query := EntityQuery{UUID: &sel.UUID, IncludeDead: true}
		for _, candidate := range src.Entities(query) {
			if candidate.UUID() == sel.UUID {
				return []Entity{candidate}
			}
		}
		return nil
	}
	if sel.CurrentEntity {
		return sel.self(src)
	}
	pos := sel.anchorPosition(src)
	q := sel.query(src, pos)
	if sel.Order == commandsyntax.OrderArbitrary && sel.MaxResults < commandsyntax.MaxAll {
		q.Limit, q.Accept = sel.MaxResults, sel.predicate(src, pos)
		return src.Entities(q)
	}
	return sel.sweep(src, asEntities(src.OnlineTargets()), src.Entities(q))
}

// query is what the world can rule out before snapshotting: the exact type,
// the tags, the runner's dimension when world-limited and the volume's box. The sweep
// still runs every filter on what comes back.
// vanilla: EntitySelector.addEntities
func (sel *Selector) query(src Source, pos player.Position) EntityQuery {
	q := EntityQuery{Type: sel.ExactType, Tags: sel.Tags}
	if sel.WorldLimited {
		q.Dimension = src.DimensionName()
	}
	if minX, minY, minZ, maxX, maxY, maxZ, ok := sel.volume(pos); ok {
		q.Box = &[6]float64{minX, minY, minZ, maxX, maxY, maxZ}
	}
	return q
}

// findPlayers resolves the selector against the players of the world, the way
// EntitySelector.findPlayers does: a bare name or UUID straight to the player
// it names, and everything else swept out of the player list.
func (sel *Selector) findPlayers(src Source) []Target {
	if sel.Name != "" {
		for _, candidate := range src.OnlineTargets() {
			if strings.EqualFold(candidate.Name(), sel.Name) { // vanilla: PlayerList.getPlayerByName
				return []Target{candidate}
			}
		}
		return nil
	}
	if sel.HasUUID {
		for _, candidate := range src.OnlineTargets() {
			if candidate.UUID() == sel.UUID {
				return []Target{candidate}
			}
		}
		return nil
	}
	if sel.CurrentEntity {
		// The runner is a player, so the @s the players-only arguments
		// take answers with them the same way; anything else here is not
		// one of the players the argument could carry.
		for _, candidate := range sel.self(src) {
			if target, ok := candidate.(Target); ok {
				return []Target{target}
			}
		}
		return nil
	}
	result := sel.sweep(src, asEntities(src.OnlineTargets()), nil)
	targets := make([]Target, len(result))
	for i, e := range result {
		targets[i] = e.(Target)
	}
	return targets
}

// self answers @s: the runner's body if it passes the filters, unsorted and
// unlimited.
// vanilla: EntitySelector
func (sel *Selector) self(src Source) []Entity {
	self := src.SelfEntity()
	if self == nil {
		return nil
	}
	if !sel.predicate(src, sel.anchorPosition(src))(self) {
		return nil
	}
	return []Entity{self}
}

// sweep is both resolves' common half: the dimension searched, filters, sort
// and limit. Players are searched first, as entities too; a nil entity pool
// searches players alone.
func (sel *Selector) sweep(src Source, players []Entity, entities []Entity) []Entity {
	pos := sel.anchorPosition(src)
	test := sel.predicate(src, pos)
	limit := sel.collectLimit()
	if sel.WorldLimited {
		here := src.DimensionName()
		// A world-limited selector iterates the runner's own dimension's
		// player list, where a player who crossed in is at the end, rather
		// than the global player list filtered to the dimension.
		players = asEntities(src.OnlineTargetsIn(here))
		if entities != nil {
			entities = filterDimension(entities, here)
		}
	}
	if entities == nil {
		return sel.sortAndLimit(src, pos, filterUpTo(players, test, limit))
	}
	return sel.sortAndLimit(src, pos, filterUpTo(entities, test, limit))
}

// filterDimension keeps the entities of one dimension, which is what a
// world-limited selector searches.
func filterDimension(entities []Entity, dimension string) []Entity {
	kept := make([]Entity, 0, len(entities))
	for _, e := range entities {
		if e.DimensionName() == dimension {
			kept = append(kept, e)
		}
	}
	return kept
}

// filterUpTo gathers the entities a test passes, stopping once the limit is
// full the way the vanilla sweep's own early exit does.
func filterUpTo(entities []Entity, test func(Entity) bool, limit int) []Entity {
	var kept []Entity
	for _, e := range entities {
		if !test(e) {
			continue
		}
		kept = append(kept, e)
		if len(kept) >= limit {
			break
		}
	}
	return kept
}

// asEntities widens targets into entities, which is what a players-only pool
// hands the sweep and what a players-only answer hands back to a command.
func asEntities(targets []Target) []Entity {
	entities := make([]Entity, len(targets))
	for i, t := range targets {
		entities[i] = t
	}
	return entities
}

// SuggestSelector completes a partial selector by running the parser and
// reading the stage it stopped at. It returns the matches and their offset
// in the word: option keys from the cursor inside the brackets, values after
// '=', the comma-or-bracket stage from the cursor, other stages the whole
// word. Before any @, online player names are offered.
func SuggestSelector(src Source, partial string) (int, []string) {
	_, state, _, _ := commandsyntax.ParseSelector(partial, 0, true, entityTypeExists)
	p := selectorCompletion{CompletionState: state, source: src}
	return p.stageSuggestions(partial)
}

// stageSuggestions reads the matches the parser's final stage offers, and the
// offset in the typed word they replace from.
func (p *selectorCompletion) stageSuggestions(partial string) (int, []string) {
	switch p.Stage {
	case commandsyntax.SuggestNameOrSelector:
		// Only a word with nothing typed yet stands here: the selectors
		// and the players are both offered, the selectors first.
		return 0, append(selectorSuggestions(), playerNameSuggestions(p.source, partial)...)
	case commandsyntax.SuggestName:
		return 0, playerNameSuggestions(p.source, partial)
	case commandsyntax.SuggestSelector:
		return 0, selectorSuggestions()
	case commandsyntax.SuggestOpenOptions:
		return 0, []string{partial + "["}
	case commandsyntax.SuggestOptionsKeyOrClose, commandsyntax.SuggestOptionsKey:
		// vanilla: EntitySelectorParser.suggestOptionsKey
		return p.OptionStart, optionSuggestions(p, partial[p.OptionStart:])
	case commandsyntax.SuggestOptionsNextOrClose:
		// vanilla: EntitySelectorParser.suggestOptionsNextOrClose
		return p.Position, []string{",", "]"}
	case commandsyntax.SuggestTypeValues:
		// vanilla: EntitySelectorParser.suggestTypeValues
		return p.typeValueSuggestions(partial)
	case commandsyntax.SuggestSortValues:
		// vanilla: EntitySelectorOptions sort option
		return p.ValueStart, Suggestions(strings.ToLower(partial[p.ValueStart:]), "arbitrary", "furthest", "nearest", "random")
	case commandsyntax.SuggestGamemodeValues:
		return p.ValueStart, p.gamemodeSuggestions(partial[p.ValueStart:])
	}
	return 0, nil
}

// gamemodeSuggestions are the modes the option could still read, plain and
// negated as its state allows; a typed '!' leaves only negated forms.
//
// vanilla: EntitySelectorOptions gamemode option
func (p *selectorCompletion) gamemodeSuggestions(typed string) []string {
	prefix := strings.ToLower(typed)
	addNormal := p.GamemodeState == commandsyntax.InvertNone
	addInverted := p.GamemodeState != commandsyntax.InvertSingle
	if prefix != "" {
		if prefix[0] == '!' {
			addNormal = false
			prefix = prefix[1:]
		} else {
			addInverted = false
		}
	}
	var matches []string
	for _, name := range gameModeNames {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if addInverted {
			matches = append(matches, "!"+name)
		}
		if addNormal {
			matches = append(matches, name)
		}
	}
	return matches
}

// selectorSuggestions are the six selectors, in the order the vanilla
// suggester lists them.
func selectorSuggestions() []string {
	return []string{"@a", "@e", "@n", "@p", "@r", "@s"}
}

// playerNameSuggestions are the online players whose names the typed fragment
// could still become, matched the way SharedSuggestionProvider matches: the
// fragment must begin one of the runs the name splits into.
func playerNameSuggestions(src Source, partial string) []string {
	if src == nil {
		return nil
	}
	var names []string
	for _, profile := range src.OnlinePlayers() {
		if matchesSubStr(partial, profile.Name) {
			names = append(names, profile.Name)
		}
	}
	return names
}

// optionSuggestions are the option names (with '=') the parser may still
// read here, unused and starting with the typed fragment.
//
// vanilla: EntitySelectorParser.suggestNames
func optionSuggestions(p *selectorCompletion, partial string) []string {
	var names []string
	for _, name := range p.ApplicableOptions {
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(partial)) {
			names = append(names, name+"=")
		}
	}
	return names
}

// typeValueSuggestions are a type option's remaining values (negated and
// plain types, tags), replacing from the value's start without the '=',
// each form with its marks before the id.
//
// vanilla: EntitySelectorParser.suggestTypeValues
func (p *selectorCompletion) typeValueSuggestions(partial string) (int, []string) {
	at := p.ValueStart
	typed := partial[at:]
	var matches []string
	add := func(prefix, id string) {
		if typeValueMatches(typed, prefix, id) {
			matches = append(matches, prefix+id)
		}
	}
	if p.TypeState != commandsyntax.InvertSingle {
		for _, name := range entityTypeNames {
			add("!", name)
		}
		for _, tag := range entityTypeTagNames {
			// vanilla: InvertableSetOptionState.canParseTag refuses a tag
			// already read, whatever its polarity.
			if p.SeenTypeTags[tag] {
				continue
			}
			add("#", tag)
			add("!#", tag)
		}
	}
	if p.TypeState == commandsyntax.InvertNone {
		for _, name := range entityTypeNames {
			add("", name)
		}
	}
	sort.Strings(matches)
	return at, matches
}

// typeValueMatches reports whether a type form matches the typed value: the
// marks they share are stripped and the rest matched against the id. A
// value sharing no mark with a marked form never matches it, so "c" keeps
// out !minecraft:cow while '!' reaches the !# tags.
//
// vanilla: SharedSuggestionProvider.filterResources
func typeValueMatches(typed, prefix, id string) bool {
	common := commonPrefix(typed, prefix)
	if common == "" && typed != "" && prefix != "" {
		return false
	}
	return resourceMatches(typed[len(common):], id)
}

// commonPrefix is the longest run two strings begin with.
func commonPrefix(a, b string) string {
	n := 0
	for n < len(a) && n < len(b) && a[n] == b[n] {
		n++
	}
	return a[:n]
}

// resourceMatches is SharedSuggestionProvider's own resource match: a
// fragment carrying a namespace is matched against the whole id, one
// without against the namespace and the path each.
func resourceMatches(partial, id string) bool {
	lower := strings.ToLower(partial)
	if strings.Contains(lower, ":") {
		return matchesSubStr(lower, strings.ToLower(id))
	}
	namespace, path, _ := strings.Cut(id, ":")
	return matchesSubStr(lower, strings.ToLower(namespace)) ||
		matchesSubStr(lower, strings.ToLower(path))
}

// matchesSubStr is the match SharedSuggestionProvider filters its suggestions
// with: the fragment must begin the candidate, or begin whatever follows one
// of the separators a name or a path carries.
func matchesSubStr(partial, candidate string) bool {
	lowerC, lowerP := strings.ToLower(candidate), strings.ToLower(partial)
	if strings.HasPrefix(lowerC, lowerP) {
		return true
	}
	for i := 0; i < len(candidate); i++ {
		switch candidate[i] {
		case '.', '_', '/':
			if strings.HasPrefix(lowerC[i+1:], lowerP) {
				return true
			}
		}
	}
	return false
}

// entityTypeNames and entityTypeTagNames are the entity-type names and tag
// names the type option's suggestions offer, read from the registry.
var (
	entityTypeNames    = registry.EntityTypes
	entityTypeTagNames = builtinEntityTypeTagNames()
)

// builtinEntityTypeTagNames collects the names of the entity-type tags the
// built-in tags carry, which are the # values the type option suggests.
func builtinEntityTypeTagNames() []string {
	var names []string
	for _, tagged := range registry.BuiltinTags {
		if tagged.Registry != "minecraft:entity_type" {
			continue
		}
		for _, tag := range tagged.Tags {
			names = append(names, tag.Name)
		}
	}
	return names
}

// selectorDice is the dice a predicate option rolls: a selector carries no
// source to take them from. Seeding a source costs microseconds, so they are
// pooled instead of built per entity.
var selectorDice = sync.Pool{New: func() any { return rand.New(rand.NewSource(rand.Int63())) }}

type selectorCompletion struct {
	commandsyntax.CompletionState
	source Source
}
