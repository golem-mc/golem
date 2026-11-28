package command

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// The errors the coordinate arguments throw while they are read, and the one
// a block_pos is refused with when a command asks for its spawnable position.
// vanilla: WorldCoordinate, Vec3Argument, RotationArgument, BlockPosArgument, BuiltInExceptions
const (
	keyPosMixed          = "argument.pos.mixed"
	keyPosMissingDouble  = "argument.pos.missing.double"
	keyPosMissingInt     = "argument.pos.missing.int"
	keyPos3DIncomplete   = "argument.pos3d.incomplete"
	keyPos2DIncomplete   = "argument.pos2d.incomplete"
	keyRotIncomplete     = "argument.rotation.incomplete"
	keyPosUnloaded       = "argument.pos.unloaded"
	keyPosOutOfWorld     = "argument.pos.outofworld"
	keyPosOutOfBounds    = "argument.pos.outofbounds"
	keyDoubleExpected    = "parsing.double.expected"
	keyDoubleInvalid     = "parsing.double.invalid"
	keyFloatExpected     = "parsing.float.expected"
	keyFloatInvalid      = "parsing.float.invalid"
	keyIntExpected       = "parsing.int.expected"
	keyIntInvalid        = "parsing.int.invalid"
	keyExpectedSeparator = "command.expected.separator"
)

// loadedBlockPosRefusal is the key a loaded block position is refused with in
// in's dimension: argument.pos.unloaded, then argument.pos.outofworld, or "".
// vanilla: BlockPosArgument.getLoadedBlockPos
func loadedBlockPosRefusal(in Source, x, y, z int32) string {
	switch {
	case !in.BlockLoaded(x, y, z):
		return keyPosUnloaded
	case !in.InWorldBounds(x, y, z):
		return keyPosOutOfWorld
	}
	return ""
}

// coord is one component of a position or a rotation as a command gives it:
// an absolute value, or a tilde form relative to whatever it is applied to.
type coord struct {
	relative bool
	value    float64
}

// resolve applies a coordinate to whatever it is relative to: an absolute
// coordinate is itself, a relative one the offset added to the current value.
func (c coord) resolve(current float64) float64 {
	if c.relative {
		return current + c.value
	}
	return c.value
}

// position is a minecraft:vec3 or minecraft:block_pos argument: world
// coordinates, each absolute or relative, or local ones, where x, y and z
// are the left, up and forwards offsets of LocalCoordinates.
type position struct {
	local   bool
	x, y, z coord
}

// at resolves the position against a source: world coordinates against its
// position, local ones against its anchor and rotation.
// vanilla: WorldCoordinates.getPosition, LocalCoordinates.getPosition
func (p position) at(src Source) (x, y, z float64) {
	pos := src.Position()
	if !p.local {
		return p.x.resolve(pos.X), p.y.resolve(pos.Y), p.z.resolve(pos.Z)
	}
	from := pos
	if src.AnchorEyes() {
		from = src.EyePosition()
	}
	dx, dy, dz := localOffset(pos.Yaw, pos.Pitch, p.x.value, p.y.value, p.z.value)
	return from.X + dx, from.Y + dy, from.Z + dz
}

// localOffset turns left, up and forwards offsets into world ones for a
// facing, in the float steps and sine table vanilla works it out with.
// vanilla: Vec3.applyLocalCoordinatesToRotation
func localOffset(yaw, pitch float32, left, up, forwards float64) (x, y, z float64) {
	const rad = float32(math.Pi) / 180
	yCos := world.MthCos(float64(float32(float32(yaw+90) * rad)))
	ySin := world.MthSin(float64(float32(float32(yaw+90) * rad)))
	xCos := world.MthCos(float64(float32(-pitch * rad)))
	xSin := world.MthSin(float64(float32(-pitch * rad)))
	xCosUp := world.MthCos(float64(float32(float32(-pitch+90) * rad)))
	xSinUp := world.MthSin(float64(float32(float32(-pitch+90) * rad)))
	fx, fy, fz := float64(float32(yCos*xCos)), float64(xSin), float64(float32(ySin*xCos))
	ux, uy, uz := float64(float32(yCos*xCosUp)), float64(xSinUp), float64(float32(ySin*xCosUp))
	// left is forwards × up, scaled by -1
	lx := -(float64(fy*uz) - float64(fz*uy))
	ly := -(float64(fz*ux) - float64(fx*uz))
	lz := -(float64(fx*uy) - float64(fy*ux))
	x = float64(fx*forwards) + float64(ux*up) + float64(lx*left)
	y = float64(fy*forwards) + float64(uy*up) + float64(ly*left)
	z = float64(fz*forwards) + float64(uz*up) + float64(lz*left)
	return x, y, z
}

// asinTab and cosTab are the arcsine tables vanilla's atan2 interpolates
// over, the asine and cosine of i/256; fracBias is the constant whose low
// thirty-two bits end up holding the table index of a scaled ordinate.
// vanilla: Mth.ASIN_TAB, Mth.COS_TAB, Mth.FRAC_BIAS
var (
	asinTab  [257]float64
	cosTab   [257]float64
	fracBias = math.Float64frombits(4805340802404319232)
)

func init() {
	for i := range asinTab {
		d := float64(i) / 256
		e := math.Asin(d)
		asinTab[i] = e
		cosTab[i] = math.Cos(e)
	}
}

// fastInvSqrt is vanilla's own fast inverse square root: the magic-bit seed
// followed by one Newton step, kept bit for bit because its error is what
// atan2's answers land where they do on.
// vanilla: Mth.fastInvSqrt
func fastInvSqrt(d float64) float64 {
	e := 0.5 * d
	l := math.Float64bits(d)
	l = 6910469410427058090 - l>>1
	d = math.Float64frombits(l)
	d *= 1.5 - e*d*d
	return d
}

// mthAtan2 is vanilla's table-driven atan2. Its answers are within a table
// step of the exact one, and which side of a step an input lands on decides
// where vanilla leaves a turned body.
// vanilla: Mth.atan2
func mthAtan2(d, e float64) float64 {
	f := e*e + d*d
	if math.IsNaN(f) {
		return math.NaN()
	}
	bl := d < 0
	if bl {
		d = -d
	}
	bl2 := e < 0
	if bl2 {
		e = -e
	}
	// Past here the first argument is the smaller of the two, which is
	// the one the arcsine table reads.
	bl3 := d > e
	if bl3 {
		d, e = e, d
	}
	g := fastInvSqrt(f)
	e *= g
	d *= g
	// fracBias + d keeps d's leading eight fraction bits in the low
	// thirty-two bits of the double, where the integer narrowing reads
	// them as the table index.
	h := fracBias + d
	i := int32(math.Float64bits(h))
	j := asinTab[i]
	k := cosTab[i]
	l := h - fracBias
	m := d*k - e*l
	n := (6.0 + m*m) * m * 0.16666666666666666
	o := j + n
	if bl3 {
		o = 1.5707963267948966 - o
	}
	if bl2 {
		o = math.Pi - o
	}
	if bl {
		o = -o
	}
	return o
}

// coordReader is the part of brigadier's StringReader the coordinate
// arguments read with, over the argument's words joined by single spaces;
// a ParseError's cursor is an offset in that text.
type coordReader struct {
	s   string
	cur int
}

func (r *coordReader) canRead() bool { return r.cur < len(r.s) }
func (r *coordReader) peek() byte    { return r.s[r.cur] }

func (r *coordReader) fail(key string, with ...string) *ParseError {
	return &ParseError{Key: key, With: with, Cursor: r.cur}
}

// readNumber takes the run of characters brigadier allows in a number:
// digits, '.' and '-', so no exponent, '+', or inf/NaN.
func (r *coordReader) readNumber() (start int, number string) {
	start = r.cur
	for r.canRead() && (r.peek() >= '0' && r.peek() <= '9' || r.peek() == '.' || r.peek() == '-') {
		r.cur++
	}
	return start, r.s[start:r.cur]
}

// readDouble is StringReader.readDouble: Java's Double.parseDouble over the
// run, which overflows to an infinity rather than failing.
func (r *coordReader) readDouble() (float64, *ParseError) {
	return r.readReal(keyDoubleExpected, keyDoubleInvalid)
}

// readFloat is StringReader.readFloat, which reads the run readDouble takes
// and words its refusals as a float's.
func (r *coordReader) readFloat() (float64, *ParseError) {
	return r.readReal(keyFloatExpected, keyFloatInvalid)
}

// readReal reads a number, failing with the expected key where no number
// begins and the invalid key where the run is no number in Java's eyes, the
// reader rewound to the run's start for the latter.
func (r *coordReader) readReal(expectedKey, invalidKey string) (float64, *ParseError) {
	start, number := r.readNumber()
	if number == "" {
		return 0, r.fail(expectedKey)
	}
	v, err := strconv.ParseFloat(number, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		r.cur = start
		return 0, r.fail(invalidKey, number)
	}
	return v, nil
}

// readInt is StringReader.readInt: a Java int.
func (r *coordReader) readInt() (int32, *ParseError) {
	start, number := r.readNumber()
	if number == "" {
		return 0, r.fail(keyIntExpected)
	}
	v, err := strconv.ParseInt(number, 10, 32)
	if err != nil {
		r.cur = start
		return 0, r.fail(keyIntInvalid, number)
	}
	return int32(v), nil
}

// hasValue says a number follows a '~' or a '^' rather than the space or
// the end that stands for zero.
func (r *coordReader) hasValue() bool { return r.canRead() && r.peek() != ' ' }

// worldDouble reads one world coordinate of a vec3 or a rotation. centre is
// centerCorrect: an absolute number written without a '.' moves to the
// middle of its block. vanilla: WorldCoordinate.parseDouble
func (r *coordReader) worldDouble(centre bool) (coord, *ParseError) {
	if r.canRead() && r.peek() == '^' {
		return coord{}, r.fail(keyPosMixed)
	}
	if !r.canRead() {
		return coord{}, r.fail(keyPosMissingDouble)
	}
	relative := r.peek() == '~'
	if relative {
		r.cur++
	}
	start := r.cur
	var v float64
	if r.hasValue() {
		var err *ParseError
		if v, err = r.readDouble(); err != nil {
			return coord{}, err
		}
	}
	if !relative && centre && !strings.Contains(r.s[start:r.cur], ".") {
		v += 0.5
	}
	return coord{relative: relative, value: v}, nil
}

// worldInt reads one world coordinate of a block_pos: an absolute one must
// be an int, while a relative offset may still be a fraction.
// vanilla: WorldCoordinate.parseInt
func (r *coordReader) worldInt() (coord, *ParseError) {
	if r.canRead() && r.peek() == '^' {
		return coord{}, r.fail(keyPosMixed)
	}
	if !r.canRead() {
		return coord{}, r.fail(keyPosMissingInt)
	}
	relative := r.peek() == '~'
	if relative {
		r.cur++
	}
	if !r.hasValue() {
		return coord{relative: relative}, nil
	}
	if relative {
		v, err := r.readDouble()
		return coord{relative: true, value: v}, err
	}
	v, err := r.readInt()
	return coord{value: float64(v)}, err
}

// localDouble reads one component of a local position, which must be one
// too. vanilla: LocalCoordinates.readDouble
func (r *coordReader) localDouble(start int) (coord, *ParseError) {
	if !r.canRead() {
		return coord{}, r.fail(keyPosMissingDouble)
	}
	if r.peek() != '^' {
		r.cur = start
		return coord{}, r.fail(keyPosMixed)
	}
	r.cur++
	if !r.hasValue() {
		return coord{}, nil
	}
	v, err := r.readDouble()
	return coord{value: v}, err
}

// components reads n space-separated components with read, i being the
// component's index; a component that does not end at a space before the
// next one is the incomplete error, at the start of the argument.
func (r *coordReader) components(n int, incomplete string, read func(i, start int) (coord, *ParseError)) ([]coord, *ParseError) {
	start := r.cur
	out := make([]coord, n)
	for i := range out {
		if i > 0 {
			if !r.canRead() || r.peek() != ' ' {
				r.cur = start
				return nil, r.fail(incomplete)
			}
			r.cur++
		}
		c, err := read(i, start)
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}

// readPosition reads a vec3 (block false), whose whole x and z are centred,
// or a block_pos (block true), whose absolute coordinates are ints.
// vanilla: Vec3Argument.parse, BlockPosArgument.parse
func (r *coordReader) readPosition(block bool) (position, *ParseError) {
	return r.readPositionCentred(block, true)
}

// readPositionCentred is readPosition with the centre correction of a
// vec3's whole x and z switched by centre.
func (r *coordReader) readPositionCentred(block, centre bool) (position, *ParseError) {
	local := r.canRead() && r.peek() == '^'
	cs, err := r.components(3, keyPos3DIncomplete, func(i, start int) (coord, *ParseError) {
		switch {
		case local:
			return r.localDouble(start)
		case block:
			return r.worldInt()
		default:
			return r.worldDouble(centre && i != 1)
		}
	})
	if err != nil {
		return position{}, err
	}
	return position{local: local, x: cs[0], y: cs[1], z: cs[2]}, nil
}

// readColumnPos reads a column_pos: an x and a z, each an int or a tilde
// form. vanilla: ColumnPosArgument.parse
func (r *coordReader) readColumnPos() (x, z coord, err *ParseError) {
	if !r.canRead() {
		return coord{}, coord{}, r.fail(keyPos2DIncomplete)
	}
	cs, err := r.components(2, keyPos2DIncomplete, func(int, int) (coord, *ParseError) {
		return r.worldInt()
	})
	if err != nil {
		return coord{}, coord{}, err
	}
	return cs[0], cs[1], nil
}

// readRotation reads a rotation: a yaw then a pitch, neither centred.
// vanilla: RotationArgument.parse
func (r *coordReader) readRotation() (yaw, pitch coord, err *ParseError) {
	if !r.canRead() {
		return coord{}, coord{}, r.fail(keyRotIncomplete)
	}
	cs, err := r.components(2, keyRotIncomplete, func(int, int) (coord, *ParseError) {
		return r.worldDouble(false)
	})
	if err != nil {
		return coord{}, coord{}, err
	}
	return cs[0], cs[1], nil
}

// readVec2 reads a vec2: an x then a z, both centred when absolute whole
// numbers, a '^' refused as mixed.
// vanilla: Vec2Argument.parse
func (r *coordReader) readVec2() (x, z coord, err *ParseError) {
	if !r.canRead() {
		return coord{}, coord{}, r.fail(keyPos2DIncomplete)
	}
	cs, err := r.components(2, keyPos2DIncomplete, func(int, int) (coord, *ParseError) {
		return r.worldDouble(true)
	})
	if err != nil {
		return coord{}, coord{}, err
	}
	return cs[0], cs[1], nil
}

// parseCoordinateArgument reads the words of a vec2, vec3, block_pos or rotation
// argument while the tree walk is reading them. A number running on into
// the last word's tail is the dispatcher's missing separator.
func parseCoordinateArgument(parser int32, tokens []Token) *ParseError {
	span := joinSelector(tokens)
	r := &coordReader{s: span.text}
	var err *ParseError
	switch parser {
	case ParserRotation:
		_, _, err = r.readRotation()
	case ParserVec2:
		_, _, err = r.readVec2()
	case ParserColumnPos:
		_, _, err = r.readColumnPos()
	default:
		_, err = r.readPosition(parser == ParserBlockPos)
	}
	if err == nil && r.canRead() {
		err = r.fail(keyExpectedSeparator)
	}
	if err != nil {
		err.Cursor = span.absolute(err.Cursor)
	}
	return err
}

// parseIntegerArgument reads an integer argument: a non-integer or
// out-of-range word stops the branch at its start, and a number running into
// a tail is the dispatcher's separator error at the tail.
// vanilla: IntegerArgumentType.parse
func parseIntegerArgument(tokens []Token, bounds *IntArg) *ParseError {
	span := joinSelector(tokens)
	r := &coordReader{s: span.text}
	v, err := r.readInt()
	if err != nil {
		err.Cursor = span.absolute(err.Cursor)
		return err
	}
	// The range is the parser's own check, made while it still holds the
	// reader at the number; the tail is the dispatcher's separator check,
	// which only sees what is left when the parser is done.
	if bounds != nil {
		if bounds.HasMin && v < bounds.Min {
			return &ParseError{
				Key:    keyIntegerLow,
				With:   []string{strconv.Itoa(int(bounds.Min)), strconv.Itoa(int(v))},
				Cursor: span.absolute(0),
			}
		}
		if bounds.HasMax && v > bounds.Max {
			return &ParseError{
				Key:    keyIntegerBig,
				With:   []string{strconv.Itoa(int(bounds.Max)), strconv.Itoa(int(v))},
				Cursor: span.absolute(0),
			}
		}
	}
	if r.canRead() {
		return &ParseError{Key: keyExpectedSeparator, Cursor: span.absolute(r.cur)}
	}
	return nil
}

// parseFloatArgument is parseIntegerArgument for brigadier:float, the value
// held as the Java float the reader yields.
// vanilla: FloatArgumentType.parse
func parseFloatArgument(tokens []Token, bounds *FloatArg) *ParseError {
	span := joinSelector(tokens)
	r := &coordReader{s: span.text}
	v, err := r.readFloat()
	if err != nil {
		err.Cursor = span.absolute(err.Cursor)
		return err
	}
	f := float32(v)
	if bounds != nil {
		if bounds.HasMin && f < bounds.Min {
			return &ParseError{
				Key:    keyFloatLow,
				With:   []string{formatJavaFloat(bounds.Min), formatJavaFloat(f)},
				Cursor: span.absolute(0),
			}
		}
		if bounds.HasMax && f > bounds.Max {
			return &ParseError{
				Key:    keyFloatBig,
				With:   []string{formatJavaFloat(bounds.Max), formatJavaFloat(f)},
				Cursor: span.absolute(0),
			}
		}
	}
	if r.canRead() {
		return &ParseError{Key: keyExpectedSeparator, Cursor: span.absolute(r.cur)}
	}
	return nil
}

// positionCache keeps the last successful position parses by joined text: a
// recursive function parses the same coordinates once per step. A position
// depends only on its text, and one cache serves one reading flavour, since
// the same text parses differently in each.
type positionCache struct {
	sync.Mutex
	byText map[string]position
}

const positionCacheMax = 1024

// read is the cached parse of words, or parse's result on a miss; failures are
// not stored.
func (c *positionCache) read(words []string, parse func(*coordReader) (position, *ParseError)) (position, bool) {
	// The key is built in a stack buffer: a map lookup by string(buf) does not
	// allocate, so a hit costs none.
	var buf [64]byte
	key := buf[:0]
	for i, w := range words {
		if i > 0 {
			key = append(key, ' ')
		}
		key = append(key, w...)
	}
	c.Lock()
	p, ok := c.byText[string(key)]
	c.Unlock()
	if ok {
		return p, true
	}
	text := string(key)
	p, err := parse(&coordReader{s: text})
	if err != nil {
		return p, false
	}
	c.Lock()
	if c.byText == nil || len(c.byText) >= positionCacheMax {
		c.byText = make(map[string]position, positionCacheMax)
	}
	c.byText[text] = p
	c.Unlock()
	return p, true
}

var vec3Cache, vec3ExactCache, blockPosCache positionCache

// parseVec3 reads a minecraft:vec3 argument from the first three words.
func parseVec3(words []string) (position, bool) {
	return vec3Cache.read(words, func(r *coordReader) (position, *ParseError) { return r.readPosition(false) })
}

// resolveVec3Exact is a vec3 argument registered with centerCorrect false,
// so whole x and z stay whole.
// vanilla: Vec3Argument.vec3(false)
func resolveVec3Exact(src Source, words []string) (x, y, z float64, ok bool) {
	p, ok := vec3ExactCache.read(words, func(r *coordReader) (position, *ParseError) { return r.readPositionCentred(false, false) })
	if !ok {
		return 0, 0, 0, false
	}
	x, y, z = p.at(src)
	return x, y, z, true
}

// parseBlockPos reads a minecraft:block_pos argument from the first three words.
func parseBlockPos(words []string) (position, bool) {
	return blockPosCache.read(words, func(r *coordReader) (position, *ParseError) { return r.readPosition(true) })
}

// parseRotation reads a rotation argument, a yaw and a pitch, from the first two words.
func parseRotation(words []string) (yaw, pitch coord, ok bool) {
	r := &coordReader{s: strings.Join(words, " ")}
	yaw, pitch, err := r.readRotation()
	return yaw, pitch, err == nil
}

// resolveVec3 is a minecraft:vec3 argument resolved against a source.
func resolveVec3(src Source, words []string) (x, y, z float64, ok bool) {
	p, ok := parseVec3(words)
	if !ok {
		return 0, 0, 0, false
	}
	x, y, z = p.at(src)
	return x, y, z, true
}

// resolveBlockPos is a minecraft:block_pos argument resolved against a
// source and floored to the block it lands in.
// vanilla: Coordinates.getBlockPos
func resolveBlockPos(src Source, words []string) (x, y, z int32, ok bool) {
	p, ok := parseBlockPos(words)
	if !ok {
		return 0, 0, 0, false
	}
	x, y, z = p.block(src)
	return x, y, z, true
}

// block is the position resolved against a source and floored to the block
// it lands in.
func (p position) block(src Source) (x, y, z int32) {
	fx, fy, fz := p.at(src)
	return rules.BlockOf(fx), rules.BlockOf(fy), rules.BlockOf(fz)
}

// resolveColumnPos is a minecraft:column_pos argument resolved against a
// source and floored to the block column it lands in.
// vanilla: ColumnPosArgument.getColumnPos
func resolveColumnPos(src Source, words []string) (x, z int32, ok bool) {
	r := &coordReader{s: strings.Join(words, " ")}
	cx, cz, err := r.readColumnPos()
	if err != nil {
		return 0, 0, false
	}
	at := src.Position()
	return rules.BlockOf(cx.resolve(at.X)), rules.BlockOf(cz.resolve(at.Z)), true
}

// isCoordish reports whether a word could begin a position: a number, a
// sign, a decimal point, or the '~' or '^' a relative or local coordinate
// starts with. It is what tells the argument of /tp that could be either apart.
func isCoordish(word string) bool {
	if strings.HasPrefix(word, "~") || strings.HasPrefix(word, "^") {
		return true
	}
	r := &coordReader{s: word}
	_, err := r.readDouble()
	return err == nil && !r.canRead()
}

// formatCoord renders one coordinate of a position the way the vanilla
// server's teleport feedback does: Java's String.format("%f") in the root
// locale, six decimal places whatever the value.
func formatCoord(v float64) string {
	return strconv.FormatFloat(v, 'f', 6, 64)
}

// formatJavaFloat renders a float32 the way Java's Float.toString does, which
// is what a command feedback message carries: the infinities and NaN by name,
// plain notation from 1e-3 up to below 1e7, and d.dddE±n outside that range,
// always with a decimal point.
func formatJavaFloat(v float32) string { return formatJavaReal(float64(v), 32) }

// formatJavaDouble renders a double as Double.toString does.
func formatJavaDouble(v float64) string { return formatJavaReal(v, 64) }

// formatJavaReal renders v in the notation of Float.toString (bits 32) or
// Double.toString (bits 64).
func formatJavaReal(v float64, bits int) string { return nbt.JavaFloatText(v, bits) }

// parseInteger reads one word as a brigadier:integer argument would: a
// base-10 integer with an optional sign; the range checks are the commands'.
func parseInteger(word string) (int64, bool) {
	v, err := strconv.ParseInt(word, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

// formatInt prints an integer without decoration, which is what the feedback
// messages carry their counts as.
func formatInt(v int64) string {
	return strconv.FormatInt(v, 10)
}
