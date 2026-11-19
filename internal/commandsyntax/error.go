package commandsyntax

import "github.com/golem-mc/golem/nbt"

// Error is a grammar failure with its translation arguments and input cursor.
type Error struct {
	Key    string
	With   []string
	Cursor int
	Text   string
}

func nbtParseError(pe *nbt.ParseError, base int) *Error {
	key := pe.Key
	if key == nbt.ErrTrailing {
		key = "command.expected.separator"
	}
	return &Error{Key: key, With: pe.With, Cursor: base + pe.Offset}
}

const (
	keyEntityInvalid           = "argument.entity.invalid"
	keySelectorUnknown         = "argument.entity.selector.unknown"
	keySelectorNotAllowed      = "argument.entity.selector.not_allowed"
	keySelectorMissing         = "argument.entity.selector.missing"
	keyOptionsUnterminated     = "argument.entity.options.unterminated"
	keyOptionsValueless        = "argument.entity.options.valueless"
	keyOptionsUnknown          = "argument.entity.options.unknown"
	keyOptionsInapplicable     = "argument.entity.options.inapplicable"
	keyOptionsDistanceNegative = "argument.entity.options.distance.negative"
	keyOptionsLevelNegative    = "argument.entity.options.level.negative"
	keyOptionsLimitTooSmall    = "argument.entity.options.limit.toosmall"
	keyOptionsModeInvalid      = "argument.entity.options.mode.invalid"
	keyOptionsSortInvalid      = "argument.entity.options.sort.irreversible"
	keyOptionsTypeInvalid      = "argument.entity.options.type.invalid"
	keyRangeEmpty              = "argument.range.empty"
	keyRangeSwapped            = "argument.range.swapped"
)
const (
	keyIDInvalid      = "argument.id.invalid"
	keyBoolExpected   = "parsing.bool.expected"
	keyBoolInvalid    = "parsing.bool.invalid"
	keyExpectedSymbol = "parsing.expected"
	summonBadNBTKey   = "argument.nbt.expected.compound"
)
