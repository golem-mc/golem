package commandsyntax

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

const (
	selP = 'p'
	selA = 'a'
	selR = 'r'
	selS = 's'
	selE = 'e'
	selN = 'n'
)

// Selector ordering before applying the result limit.
const (
	OrderArbitrary = iota
	OrderNearest
	OrderFurthest
	OrderRandom

	// MaxAll is the limit a selector that takes everything carries:
	// EntitySelectorParser's Integer.MAX_VALUE.
	MaxAll = math.MaxInt32
)

// Bounds retains optional ends for numeric and rotation ranges.
type Bounds struct {
	Min, Max float64
	HasMin   bool
	HasMax   bool
}

// MatchesSqr compares a squared measure against squared bounds.
func (b *Bounds) MatchesSqr(value float64) bool {
	return !(b.HasMin && value < b.Min*b.Min || b.HasMax && value > b.Max*b.Max)
}

// Matches compares a measure against optional bounds.
func (b *Bounds) Matches(value float64) bool {
	return !(b.HasMin && value < b.Min || b.HasMax && value > b.Max)
}

// Selector retains parsed target data without executable predicates.
type Selector struct {
	// Bare names and UUIDs bypass bracket filters and dimension limits.
	Name                                          string
	UUID                                          uuid.UUID
	HasUUID                                       bool
	CurrentEntity, IncludesEntities, WorldLimited bool
	ExactType                                     string
	Tags                                          []TagFilter
	MaxResults, Order                             int
	Distance, Level                               *Bounds
	X, Y, Z, Dx, Dy, Dz                           *float64
	RotX, RotY                                    *Bounds
	Scores                                        map[string]*Bounds
	Teams                                         []TeamWant
	Filters                                       []Filter
	LootPredicates                                []LootPredicateWant
}

// selReader tracks byte offsets relative to the selector's start.
type selReader struct {
	input string
	pos   int
}

func (r *selReader) canRead() bool { return r.pos < len(r.input) }

func (r *selReader) peek() byte { return r.input[r.pos] }

func (r *selReader) canReadAhead() bool { return r.pos+1 < len(r.input) }

func (r *selReader) skip() { r.pos++ }

func (r *selReader) skipWhitespace() {
	for r.canRead() && IsWhitespace(r.peek()) {
		r.pos++
	}
}

// IsWhitespace is the whitespace Brigadier skips.
func IsWhitespace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func (r *selReader) readString() (string, *Error) {
	if !r.canRead() {
		return "", nil
	}
	if q := r.peek(); q == '"' || q == '\'' {
		return r.readQuoted(q)
	}
	return r.readUnquoted(), nil
}

func (r *selReader) readUnquoted() string {
	start := r.pos
	for r.canRead() && IsAllowedInUnquotedString(r.peek()) {
		r.pos++
	}
	return r.input[start:r.pos]
}

func (r *selReader) readQuoted(quote byte) (string, *Error) {
	start := r.pos
	r.pos++ // the opening quote
	var out []byte
	for r.canRead() {
		c := r.peek()
		if c == '\\' {
			r.pos++
			if !r.canRead() {
				return "", &Error{Key: "parsing.quote.escape", Cursor: r.pos}
			}
			e := r.peek()
			if e != quote && e != '\\' {
				return "", &Error{Key: "parsing.quote.escape", Cursor: r.pos}
			}
			out = append(out, e)
			r.pos++
			continue
		}
		if c == quote {
			r.pos++
			return string(out), nil
		}
		out = append(out, c)
		r.pos++
	}
	return "", &Error{Key: "parsing.quote.expected.end", Cursor: start}
}

// IsAllowedInUnquotedString reports Brigadier's unquoted string character set.
func IsAllowedInUnquotedString(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' ||
		c == '_' || c == '.' || c == '+' || c == '-'
}

func isIdentifierChar(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' ||
		c == '.' || c == '_' || c == '-' || c == ':'
}

// readIdentifier accepts implicit and empty minecraft namespaces.
func (r *selReader) readIdentifier() (string, *Error) {
	start := r.pos
	for r.canRead() && isIdentifierChar(r.peek()) {
		r.pos++
	}
	id := r.input[start:r.pos]
	if id == "" {
		return "", &Error{Key: "argument.id.invalid", Cursor: start}
	}
	return qualify(id), nil
}

// readPredicateID allows an empty prefix and path slashes, unlike type values.
// vanilla: Identifier.read
func (r *selReader) readPredicateID() (string, *Error) {
	start := r.pos
	name, n, ok := ReadIdentifier(r.input[start:])
	r.pos += n
	if !ok {
		r.pos = start
		return "", &Error{Key: keyIDInvalid, Cursor: start}
	}
	return name, nil
}

// isRangeInputChar excludes plus signs and the range's ".." delimiter.
func (r *selReader) isRangeInputChar() bool {
	c := r.peek()
	if c >= '0' && c <= '9' || c == '-' {
		return true
	}
	if c != '.' {
		return false
	}
	return !r.canReadAhead() || r.input[r.pos+1] != '.'
}

func (r *selReader) readRangeNumber() (string, bool) {
	start := r.pos
	for r.canRead() && r.isRangeInputChar() {
		r.pos++
	}
	return r.input[start:r.pos], start != r.pos
}

// parseRange allows swapped ends only for rotation ranges.
func (r *selReader) parseRange(parse func(string) (float64, bool), invalid string, allowSwapped bool) (*Bounds, *Error) {
	start := r.pos
	if !r.canRead() {
		return nil, &Error{Key: keyRangeEmpty, Cursor: start}
	}
	readEnd := func() (float64, bool, *Error) {
		text, any := r.readRangeNumber()
		if !any {
			return 0, false, nil
		}
		value, ok := parse(text)
		if !ok {
			return 0, false, &Error{Key: invalid, With: []string{text}, Cursor: start}
		}
		return value, true, nil
	}
	minValue, HasMin, err := readEnd()
	if err != nil {
		return nil, err
	}
	var maxValue float64
	HasMax := HasMin
	if HasMin {
		maxValue = minValue
	}
	if r.canReadAhead() && r.peek() == '.' && r.input[r.pos+1] == '.' {
		r.pos += 2
		maxValue, HasMax, err = readEnd()
		if err != nil {
			return nil, err
		}
	}
	if !HasMin && !HasMax {
		return nil, &Error{Key: keyRangeEmpty, Cursor: start}
	}
	if !allowSwapped && HasMin && HasMax && minValue > maxValue {
		return nil, &Error{Key: keyRangeSwapped, Cursor: start}
	}
	return &Bounds{Min: minValue, Max: maxValue, HasMin: HasMin, HasMax: HasMax}, nil
}

// ParseFloatEnd reads a double range endpoint.
func ParseFloatEnd(text string) (float64, bool) {
	value, err := strconv.ParseFloat(text, 64)
	return value, err == nil
}

// ParseIntEnd reads an int32 range endpoint.
func ParseIntEnd(text string) (float64, bool) {
	value, err := strconv.ParseInt(text, 10, 32)
	return float64(value), err == nil
}

// InvertState closes after a positive value, allowing multiple negative values first.
type InvertState uint8

const (
	InvertNone     InvertState = iota
	InvertSingle               // a positive value was parsed; the option is closed
	InvertMultiple             // only negative values so far
)

func (s InvertState) canParse(invert bool) bool {
	if invert {
		return s != InvertSingle
	}
	return s == InvertNone
}

func (s InvertState) canParseAny() bool { return s != InvertSingle }

func (s *InvertState) mark(invert bool) {
	if invert {
		*s = InvertMultiple
	} else {
		*s = InvertSingle
	}
}

// selParser retains option applicability and the grammar's last completion stage.
type selParser struct {
	CompletionState
	r                                        selReader
	inputStart                               int
	entityTypeExists                         func(string) bool
	sel                                      Selector
	limitSet, sortSet, scoresSet, advanceSet bool
}

func (p *selParser) parseSelectorBody() *Error {
	if !p.r.canRead() {
		return &Error{Key: keySelectorMissing, Cursor: p.absolute()}
	}
	letterPos := p.r.pos
	letter := p.r.peek()
	p.r.skip()
	addAlive := false
	switch letter {
	case selP:
		p.sel.MaxResults = 1
		p.sel.Order = OrderNearest
	case selA:
		p.sel.MaxResults = MaxAll
		p.sel.Order = OrderArbitrary
	case selR:
		p.sel.MaxResults = 1
		p.sel.Order = OrderRandom
	case selS:
		p.sel.MaxResults = 1
		p.sel.IncludesEntities = true
		p.sel.CurrentEntity = true
	case selE:
		p.sel.MaxResults = MaxAll
		p.sel.IncludesEntities = true
		p.sel.Order = OrderArbitrary
		addAlive = true
	case selN:
		p.sel.MaxResults = 1
		p.sel.IncludesEntities = true
		p.sel.Order = OrderNearest
		addAlive = true
	default:
		p.r.pos = letterPos
		return &Error{Key: keySelectorUnknown, With: []string{"@" + string(letter)}, Cursor: p.absolute()}
	}
	if addAlive {
		// The alive filter the @e and @n letters add: the dead are
		// never picked by either.
		p.addFilter(Filter{Kind: FilterAlive})
	}
	p.Stage = SuggestOpenOptions
	if p.r.canRead() && p.r.peek() == '[' {
		p.r.skip()
		p.Stage = SuggestOptionsKeyOrClose
		return p.parseOptions()
	}
	return nil
}

func (p *selParser) parseNameOrUUID() *Error {
	if p.r.canRead() {
		p.Stage = SuggestName
	}
	start := p.r.pos
	word, err := p.r.readString()
	if err != nil {
		return err
	}
	if id, err := ParseUUIDStrict(word); err == nil {
		p.sel.UUID = id
		p.sel.HasUUID = true
		p.sel.IncludesEntities = true
		p.sel.MaxResults = 1
		return nil
	}
	if word == "" || len(word) > 16 {
		p.r.pos = start
		return &Error{Key: keyEntityInvalid, Cursor: p.absolute()}
	}
	p.sel.Name = word
	p.sel.IncludesEntities = false
	p.sel.MaxResults = 1
	return nil
}

var errNotAUUID = errors.New("not a UUID")

// ParseUUIDStrict accepts only dashed UUIDs, as Java's UUID.fromString does.
func ParseUUIDStrict(word string) (uuid.UUID, error) {
	if len(word) != 36 || word[8] != '-' || word[13] != '-' || word[18] != '-' || word[23] != '-' {
		return uuid.Nil, errNotAUUID
	}
	return uuid.Parse(word)
}

func (p *selParser) absolute() int { return p.inputStart + p.r.pos }

// vanilla: EntitySelectorParser.parseOptions
func (p *selParser) parseOptions() *Error {
	p.Stage = SuggestOptionsKey
	for {
		p.r.skipWhitespace()
		// Where a key would begin, past the whitespace the list tolerates,
		// which is where the key Suggestions replace from.
		p.OptionStart = p.r.pos
		if !p.r.canRead() || p.r.peek() == ']' {
			break
		}
		mark := p.r.pos
		key, err := p.r.readString()
		if err != nil {
			return err
		}
		opt := lookupOption(key)
		if opt == nil {
			return &Error{Key: keyOptionsUnknown, With: []string{key}, Cursor: p.inputStart + mark}
		}
		if !opt.canUse(p) {
			return &Error{Key: keyOptionsInapplicable, With: []string{key}, Cursor: p.inputStart + mark}
		}
		p.r.skipWhitespace()
		if !p.r.canRead() || p.r.peek() != '=' {
			return &Error{Key: keyOptionsValueless, With: []string{key}, Cursor: p.inputStart + mark}
		}
		p.r.skip()
		p.r.skipWhitespace()
		p.Stage = SuggestNothing
		if err := opt.handle(p); err != nil {
			return err
		}
		p.r.skipWhitespace()
		p.Stage = SuggestOptionsNextOrClose
		if p.r.canRead() && p.r.peek() == ',' {
			p.r.skip()
			// A comma re-arms the option-name Suggestions, as the vanilla
			// loop does before it reads the next option.
			p.Stage = SuggestOptionsKey
			continue
		}
		if p.r.canRead() && p.r.peek() == ']' {
			break
		}
		return &Error{Key: keyOptionsUnterminated, Cursor: p.absolute()}
	}
	if p.r.canRead() {
		p.r.skip() // the ]
		p.Stage = SuggestNothing
		return nil
	}
	return &Error{Key: keyOptionsUnterminated, Cursor: p.absolute()}
}

func (p *selParser) shouldInvertValue() bool {
	p.r.skipWhitespace()
	if p.r.canRead() && p.r.peek() == '!' {
		p.r.skip()
		p.r.skipWhitespace()
		return true
	}
	return false
}

func (p *selParser) isTag() bool {
	p.r.skipWhitespace()
	if p.r.canRead() && p.r.peek() == '#' {
		p.r.skip()
		p.r.skipWhitespace()
		return true
	}
	return false
}

func (p *selParser) addFilter(f Filter) {
	p.sel.Filters = append(p.sel.Filters, f)
}

func (p *selParser) setWorldLimited() { p.sel.WorldLimited = true }

type option struct {
	name   string
	canUse func(*selParser) bool
	handle func(*selParser) *Error
}

// vanilla: EntitySelectorOptions
var options = []*option{
	{
		name: "name",
		canUse: func(p *selParser) bool {
			return p.NameState.canParseAny()
		},
		handle: func(p *selParser) *Error {
			mark := p.r.pos
			invert := p.shouldInvertValue()
			value, err := p.r.readString()
			if err != nil {
				return err
			}
			if !p.NameState.canParse(invert) {
				return p.rollbackInapplicable(mark, "name")
			}
			p.NameState.mark(invert)
			// The comparison is against the entity's plain name, the text
			// Entity.getPlainTextName reads off its display name.
			p.addFilter(Filter{Kind: FilterName, Name: value, Invert: invert})
			return nil
		},
	},
	{
		name:   "distance",
		canUse: func(p *selParser) bool { return p.sel.Distance == nil },
		handle: func(p *selParser) *Error {
			mark := p.r.pos
			value, err := p.r.parseRange(ParseFloatEnd, "parsing.double.invalid", false)
			if err != nil {
				err.Cursor += p.inputStart // the range reads within the selector
				return err
			}
			if value.HasMin && value.Min < 0 || value.HasMax && value.Max < 0 {
				return &Error{Key: keyOptionsDistanceNegative, Cursor: p.inputStart + mark}
			}
			p.sel.Distance = value
			p.setWorldLimited()
			return nil
		},
	},
	{
		name:   "level",
		canUse: func(p *selParser) bool { return p.sel.Level == nil },
		handle: func(p *selParser) *Error {
			mark := p.r.pos
			value, err := p.r.parseRange(ParseIntEnd, "parsing.int.invalid", false)
			if err != nil {
				err.Cursor += p.inputStart // the range reads within the selector
				return err
			}
			if value.HasMin && value.Min < 0 || value.HasMax && value.Max < 0 {
				return &Error{Key: keyOptionsLevelNegative, Cursor: p.inputStart + mark}
			}
			p.sel.Level = value
			// The level belongs to players alone, so the pick is pinned to
			// the players.
			p.sel.IncludesEntities = false
			p.addFilter(Filter{Kind: FilterLevel, Bounds: value})
			return nil
		},
	},
	coordinateOption("x", func(p *selParser) **float64 { return &p.sel.X }),
	coordinateOption("y", func(p *selParser) **float64 { return &p.sel.Y }),
	coordinateOption("z", func(p *selParser) **float64 { return &p.sel.Z }),
	coordinateOption("dx", func(p *selParser) **float64 { return &p.sel.Dx }),
	coordinateOption("dy", func(p *selParser) **float64 { return &p.sel.Dy }),
	coordinateOption("dz", func(p *selParser) **float64 { return &p.sel.Dz }),
	{
		name:   "x_rotation",
		canUse: func(p *selParser) bool { return p.sel.RotX == nil },
		handle: func(p *selParser) *Error {
			value, err := p.r.parseRange(ParseFloatEnd, "parsing.float.invalid", true)
			if err != nil {
				err.Cursor += p.inputStart // the range reads within the selector
				return err
			}
			p.sel.RotX = value
			return nil
		},
	},
	{
		name:   "y_rotation",
		canUse: func(p *selParser) bool { return p.sel.RotY == nil },
		handle: func(p *selParser) *Error {
			value, err := p.r.parseRange(ParseFloatEnd, "parsing.float.invalid", true)
			if err != nil {
				err.Cursor += p.inputStart // the range reads within the selector
				return err
			}
			p.sel.RotY = value
			return nil
		},
	},
	{
		name: "limit",
		canUse: func(p *selParser) bool {
			return !p.sel.CurrentEntity && !p.limitSet
		},
		handle: func(p *selParser) *Error {
			mark := p.r.pos
			p.r.skipWhitespace()
			text, any := p.r.readRangeNumber()
			if !any {
				// Brigadier's readInt refuses an empty read with the
				// expected-integer message.
				return &Error{Key: "parsing.int.expected", Cursor: p.absolute()}
			}
			value, ok := strconv.ParseInt(text, 10, 32)
			if ok != nil {
				return &Error{Key: "parsing.int.invalid", With: []string{text}, Cursor: p.inputStart + mark}
			}
			if value < 1 {
				return &Error{Key: keyOptionsLimitTooSmall, Cursor: p.inputStart + mark}
			}
			p.sel.MaxResults = int(value)
			p.limitSet = true
			return nil
		},
	},
	{
		name: "sort",
		canUse: func(p *selParser) bool {
			return !p.sel.CurrentEntity && !p.sortSet
		},
		handle: func(p *selParser) *Error {
			mark := p.r.pos
			p.r.skipWhitespace()
			p.Stage = SuggestSortValues
			p.ValueStart = mark
			value := p.r.readUnquoted()
			switch value {
			case "nearest":
				p.sel.Order = OrderNearest
			case "furthest":
				p.sel.Order = OrderFurthest
			case "random":
				p.sel.Order = OrderRandom
			case "arbitrary":
				p.sel.Order = OrderArbitrary
			default:
				return &Error{Key: keyOptionsSortInvalid, With: []string{value}, Cursor: p.inputStart + mark}
			}
			p.sortSet = true
			return nil
		},
	},
	{
		name: "gamemode",
		canUse: func(p *selParser) bool {
			return p.GamemodeState.canParseAny()
		},
		handle: func(p *selParser) *Error {
			mark := p.r.pos
			p.Stage = SuggestGamemodeValues
			p.ValueStart = mark
			invert := p.shouldInvertValue()
			if !p.GamemodeState.canParse(invert) {
				return p.rollbackInapplicable(mark, "gamemode")
			}
			p.r.skipWhitespace()
			value := p.r.readUnquoted()
			mode, ok := gameModeByName(value)
			if !ok {
				return &Error{Key: keyOptionsModeInvalid, With: []string{value}, Cursor: p.inputStart + mark}
			}
			p.sel.IncludesEntities = false
			p.addFilter(Filter{Kind: FilterGamemode, Mode: mode, Invert: invert})
			p.GamemodeState.mark(invert)
			return nil
		},
	},
	{
		name: "team",
		canUse: func(p *selParser) bool {
			return p.TeamState.canParseAny()
		},
		handle: func(p *selParser) *Error {
			mark := p.r.pos
			invert := p.shouldInvertValue()
			p.r.skipWhitespace()
			name := p.r.readUnquoted()
			if !p.TeamState.canParse(invert) {
				return p.rollbackInapplicable(mark, "team")
			}
			p.sel.Teams = append(p.sel.Teams, TeamWant{Name: name, Invert: invert})
			p.TeamState.mark(invert)
			return nil
		},
	},
	{
		name: "type",
		canUse: func(p *selParser) bool {
			return p.TypeState.canParseAny()
		},
		handle: func(p *selParser) *Error {
			mark := p.r.pos
			invert := p.shouldInvertValue()
			p.Stage = SuggestTypeValues
			p.ValueStart = mark
			if p.isTag() {
				// A tag narrows the pick to the entity types it groups; tags
				// may be listed any number of times, each once.
				id, err := p.r.readIdentifier()
				if err != nil {
					// A bare # reads as the empty tag id, which groups no
					// types.
					id = ""
				}
				if p.TypeState == InvertSingle || p.SeenTypeTags[id] {
					return p.rollbackInapplicable(mark, "type")
				}
				// An unknown tag parses all the same and matches nothing.
				p.addFilter(Filter{Kind: FilterTypeTag, Name: id, Invert: invert})
				p.TypeState = InvertMultiple
				if p.SeenTypeTags == nil {
					p.SeenTypeTags = make(map[string]bool)
				}
				p.SeenTypeTags[id] = true
				return nil
			}
			if !p.TypeState.canParse(invert) {
				return p.rollbackInapplicable(mark, "type")
			}
			id, err := p.r.readIdentifier()
			if err != nil {
				return err
			}
			if !p.entityTypeExists(id) {
				return &Error{Key: keyOptionsTypeInvalid, With: []string{id}, Cursor: p.inputStart + mark}
			}
			if id == "minecraft:player" && !invert {
				// A positive player type pins the pick to the players.
				p.sel.IncludesEntities = false
			}
			if !invert {
				p.sel.ExactType = id
			}
			p.addFilter(Filter{Kind: FilterType, Name: id, Invert: invert})
			p.TypeState.mark(invert)
			return nil
		},
	},
	{
		name:   "tag",
		canUse: func(*selParser) bool { return true },
		handle: func(p *selParser) *Error {
			invert := p.shouldInvertValue()
			p.r.skipWhitespace()
			tag := p.r.readUnquoted()
			f := TagFilter{Name: tag, Invert: invert}
			p.sel.Tags = append(p.sel.Tags, f)
			p.addFilter(Filter{Kind: FilterTag, Name: tag, Invert: invert})
			return nil
		},
	},
	{
		name:   "nbt",
		canUse: func(*selParser) bool { return true },
		handle: func(p *selParser) *Error {
			invert := p.shouldInvertValue()
			mark := p.r.pos
			value, end, err := nbt.ParseValueTag(p.r.input[mark:])
			if err != nil {
				return nbtParseError(err, p.inputStart+mark)
			}
			want, ok := value.(nbt.Compound)
			if !ok {
				return &Error{Key: summonBadNBTKey, Cursor: p.inputStart + mark + end}
			}
			p.r.pos = mark + end
			// Matched against saveWithoutId; a body with no save form is
			// the empty compound, so an empty pattern still Matches.
			// vanilla: EntitySelectorOptions' nbt option, Entity.saveWithoutId
			p.addFilter(Filter{Kind: FilterNBT, Pattern: want, Invert: invert})
			return nil
		},
	},
	{
		name:   "scores",
		canUse: func(p *selParser) bool { return !p.scoresSet },
		handle: (*selParser).parseScores,
	},
	{
		name:   "advancements",
		canUse: func(p *selParser) bool { return !p.advanceSet },
		handle: (*selParser).parseAdvancements,
	},
	{
		name:   "predicate",
		canUse: func(*selParser) bool { return true },
		handle: func(p *selParser) *Error {
			invert := p.shouldInvertValue()
			mark := p.r.pos
			id, err := p.r.readPredicateID()
			if err != nil {
				return &Error{Key: err.Key, Cursor: p.inputStart + mark}
			}
			// An unknown id never Matches either way; a known one is read
			// when the selector runs, so a reload is seen.
			// vanilla: EntitySelectorOptions' predicate option
			p.sel.LootPredicates = append(p.sel.LootPredicates, LootPredicateWant{Name: qualify(id), Invert: invert})
			return nil
		},
	},
}

func coordinateOption(name string, field func(*selParser) **float64) *option {
	return &option{
		name:   name,
		canUse: func(p *selParser) bool { return *field(p) == nil },
		handle: func(p *selParser) *Error {
			p.setWorldLimited()
			p.r.skipWhitespace()
			text, any := p.r.readRangeNumber()
			if !any {
				return &Error{Key: "parsing.double.expected", Cursor: p.absolute()}
			}
			value, ok := ParseFloatEnd(text)
			if !ok {
				return &Error{Key: "parsing.double.invalid", With: []string{text}, Cursor: p.absolute()}
			}
			*field(p) = &value
			return nil
		},
	}
}

// rollbackInapplicable refuses at the original option value's cursor.
func (p *selParser) rollbackInapplicable(mark int, name string) *Error {
	return &Error{Key: keyOptionsInapplicable, With: []string{name}, Cursor: p.inputStart + mark}
}

func lookupOption(name string) *option {
	for _, opt := range options {
		if opt.name == name {
			return opt
		}
	}
	return nil
}

func gameModeByName(value string) (int32, bool) {
	for mode, name := range GameModeNames {
		if strings.EqualFold(name, value) {
			return int32(mode), true
		}
	}
	return 0, false
}

func identifierPrefix(word string) int {
	n := 0
	for n < len(word) && isCharValid(word[n]) {
		n++
	}
	return n
}

// readIdentifier accepts implicit and empty minecraft namespaces.
// vanilla: Identifier.read
func ReadIdentifier(word string) (name string, n int, ok bool) {
	n = identifierPrefix(word)
	name, ok = ParseIdentifier(word[:n])
	return name, n, ok
}

// ParseIdentifier allows empty parts but refuses a slash in the namespace.
// vanilla: Identifier.parse
func ParseIdentifier(prefix string) (string, bool) {
	if before, after, ok := strings.Cut(prefix, ":"); ok {
		namespace, path := before, after
		if !isIdentifierPart(namespace, false) && namespace != "" ||
			!isIdentifierPart(path, true) && path != "" {
			return "", false
		}
		if namespace == "" {
			namespace = "minecraft"
		}
		return namespace + ":" + path, true
	}
	return "minecraft:" + prefix, true
}

func isIdentifierPart(s string, path bool) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
		case r == '/' && path:
		default:
			return false
		}
	}
	return true
}

func isCharValid(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
		c == '_' || c == ':' || c == '/' || c == '.' || c == '-'
}

// Suggestion stages: the points a parse stands at when it stops, each one the
// tab completion reads its matches off — the suggesters EntitySelectorParser
// hands its Suggestions field. SuggestNothing offers nothing.
type SuggestStage int

const (
	SuggestNothing SuggestStage = iota
	SuggestNameOrSelector
	SuggestName
	SuggestSelector
	SuggestOpenOptions
	SuggestOptionsKeyOrClose
	SuggestOptionsKey
	SuggestOptionsNextOrClose
	SuggestTypeValues
	SuggestSortValues
	SuggestGamemodeValues
)

// FilterKind identifies an executable selector predicate's captured data.
type FilterKind uint8

const (
	FilterAlive FilterKind = iota
	FilterName
	FilterLevel
	FilterGamemode
	FilterType
	FilterTypeTag
	FilterTag
	FilterNBT
	FilterAdvancements
)

// Filter retains one predicate in the order the selector parsed it.
type Filter struct {
	Kind         FilterKind
	Name         string
	Invert       bool
	Mode         int32
	Bounds       *Bounds
	Pattern      nbt.Compound
	Advancements map[string]*AdvancementWant
}

// LootPredicateWant is one predicate option, evaluated against the current pack.
type LootPredicateWant struct {
	Name   string
	Invert bool
}

// TagFilter is one tag option, also used before snapshotting entities.
type TagFilter struct {
	Name   string
	Invert bool
}

// Passes tests a tag collection through its size and membership lookup.
func (f TagFilter) Passes(size int, contains func(string) bool) bool {
	held := size == 0
	if f.Name != "" {
		held = contains(f.Name)
	}
	return held != f.Invert
}

// CompletionState retains the grammar's last completion stage and option states.
type CompletionState struct {
	Stage                                          SuggestStage
	Position, OptionStart, ValueStart              int
	NameState, GamemodeState, TeamState, TypeState InvertState
	SeenTypeTags                                   map[string]bool
	ApplicableOptions                              []string
}

func (p *selParser) completion() CompletionState {
	p.Position = p.r.pos
	for _, opt := range options {
		if opt.canUse(p) {
			p.ApplicableOptions = append(p.ApplicableOptions, opt.name)
		}
	}
	return p.CompletionState
}

// ParseSelector reads one selector without requiring consumption of the whole input.
// entityTypeExists must look up the caller's current entity registry.
func ParseSelector(text string, start int, allowSelectors bool, entityTypeExists func(string) bool) (*Selector, CompletionState, int, *Error) {
	p := &selParser{r: selReader{input: text}, inputStart: start, entityTypeExists: entityTypeExists}
	p.Stage = SuggestNameOrSelector
	var err *Error
	if p.r.canRead() && p.r.peek() == '@' {
		if !allowSelectors {
			return nil, p.completion(), 0, &Error{Key: keySelectorNotAllowed, Cursor: start}
		}
		p.r.skip()
		p.Stage = SuggestSelector
		err = p.parseSelectorBody()
	} else {
		err = p.parseNameOrUUID()
	}
	if err != nil {
		return nil, p.completion(), p.r.pos, err
	}
	return &p.sel, p.completion(), p.r.pos, nil
}

// ParseRange reads a numeric range using the caller's number and error forms.
func ParseRange(text string, parse func(string) (float64, bool), invalid string, allowSwapped bool) (*Bounds, int, *Error) {
	r := selReader{input: text}
	b, err := r.parseRange(parse, invalid, allowSwapped)
	return b, r.pos, err
}

func qualify(id string) string {
	if strings.HasPrefix(id, ":") {
		return "minecraft" + id
	}
	if !strings.Contains(id, ":") {
		return "minecraft:" + id
	}
	return id
}

// GameModeNames lists the game modes in protocol order.
var GameModeNames = []string{"survival", "creative", "adventure", "spectator"}
