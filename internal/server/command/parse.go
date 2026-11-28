package command

import (
	"cmp"
	"errors"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"

	"github.com/golem-mc/golem/internal/commandsyntax"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Token is one word of a command, with the byte offset in the input where it
// starts, which is what the cursor in a command error points at.
type Token struct {
	Text  string
	Start int
	// Gap is the whitespace between the previous word and this one, which a
	// quoted value spanning words must keep.
	Gap string
}

// lineTokens holds Tokenize's answer for each line of the loaded functions,
// which a tick function reads again every tick. The slices are shared, so
// they are stored clipped and never written to.
var lineTokens sync.Map // string -> []Token

// Tokenize splits the input into words the way strings.Fields does, while
// keeping the byte offset of each word in the original input.
func Tokenize(input string) []Token {
	if t, ok := lineTokens.Load(input); ok {
		return t.([]Token)
	}
	if t, ok := cachedTokens.Load(input); ok {
		return t.([]Token)
	}
	return tokenize(input)
}

// maxCachedTokenLines bounds cachedTokens.
const maxCachedTokenLines = 4096

// cachedTokens holds the tokens of command-block lines and of each tail after
// a "run", which execute tokenizes again for every context it forks over.
// Slices are shared, stored clipped and never written to.
var (
	cachedTokens     sync.Map // string -> []Token
	cachedTokenCount atomic.Int32
)

// CacheLineTokens keeps the tokens of a line and of every tail after a "run"
// for Tokenize. The cache is cleared whenever it passes its bound.
func CacheLineTokens(line string) {
	if _, ok := cachedTokens.Load(line); ok {
		return
	}
	if cachedTokenCount.Load() > maxCachedTokenLines {
		cachedTokens.Clear()
		cachedTokenCount.Store(0)
	}
	walkRunTails(line, tokenize(line), func(l string, t []Token) {
		if _, loaded := cachedTokens.LoadOrStore(l, slices.Clip(t)); !loaded {
			cachedTokenCount.Add(1)
		}
	})
}

// walkRunTails calls store for the line and every tail after a "run".
func walkRunTails(line string, tokens []Token, store func(string, []Token)) {
	store(line, tokens)
	for i, t := range tokens[:max(len(tokens)-1, 0)] {
		if t.Text == "run" {
			tail := line[tokens[i+1].Start:]
			walkRunTails(tail, tokenize(tail), store)
		}
	}
}

func tokenize(input string) []Token {
	var tokens []Token
	start, prevEnd := -1, 0
	add := func(end int) {
		tokens = append(tokens, Token{Text: input[start:end], Start: start, Gap: input[prevEnd:start]})
		prevEnd = end
		start = -1
	}
	for i, r := range input {
		if unicode.IsSpace(r) {
			if start >= 0 {
				add(i)
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		add(len(input))
	}
	return tokens
}

// TokenEnd is where the tokens' last word ends in the input, which is where
// the reader of a parse that consumed every word stands: the cursor its
// refusal of the input behind that read points at.
func TokenEnd(tokens []Token) int {
	last := tokens[len(tokens)-1]
	return last.Start + len(last.Text)
}

// TrailingCursor is where the dispatcher points its trailing refusal: at
// the last word's end, or one space past it when the input holds more
// behind that word, the separator vanilla skips before it reads on.
// vanilla: CommandDispatcher.parseNodes
func TrailingCursor(input string, tokens []Token) int {
	end := TokenEnd(tokens)
	if len(input) > end+1 {
		return end + 1
	}
	return end
}

// Failure describes the branch a walk ended on when no command was found: At
// is the token it stopped at; UnknownCommand marks vanilla's two
// unknown-command shapes (no literal names the first word, or every word
// matched with the reader spent and still no command — a redirect's single
// trailing space counts as the separator its read skips); Trailing marks
// every word matched with input left unread behind the winning read, which
// the dispatcher's separator rules refuse as an unknown argument; Err is the
// typed parse error the branch died on, if any, sent with the context line
// vanilla's parse errors carry.
type Failure struct {
	At             int
	UnknownCommand bool
	Trailing       bool
	Err            *ParseError
}

// Match walks tokens through the tree from node, following redirects and
// letting each argument consume the words its parser reads. It returns the
// node an executable path ends at, or nil with vanilla's dispatcher failure:
// the winning branch's one typed error, an unknown command (first word, or
// all words matched without a command), an unknown argument at the stopping
// token or where a read left input behind it. Without allowSelectors a
// selector is refused at the @.
func Match(node *Node, input string, tokens []Token, allowSelectors bool) (*Node, *Failure) {
	path, fail := MatchPath(node, input, tokens, allowSelectors)
	if path != nil {
		return path[len(path)-1], fail
	}
	return nil, fail
}

// MatchPath is Match that also returns the winning parse's node chain, from
// the command literal through the argument nodes (aliases followed) to
// Match's node; /teleport reads it to tell a position from a target.
func MatchPath(node *Node, input string, tokens []Token, allowSelectors bool) ([]*Node, *Failure) {
	path, _, fail := matchPath(node, input, tokens, allowSelectors)
	return path, fail
}

// MessageArgumentRange names a ParserMessage argument's byte range in the
// original input, before selector expansion; greedy ranges include trailing whitespace.
type MessageArgumentRange struct {
	Name       string
	Start, End int
}

// MatchPathArguments also returns message arguments from the winning parse,
// including aliases, redirects and nested execute commands. A failed parse can
// still contain parsed arguments; signature validation precedes execution.
// vanilla: SignableCommand.of, ArgumentVisitor.visitNodeArguments
func MatchPathArguments(node *Node, input string, tokens []Token, allowSelectors bool) ([]*Node, []MessageArgumentRange, *Failure) {
	path, steps, fail := matchPath(node, input, tokens, allowSelectors)
	return path, steps.messageArguments(nil), fail
}

// SignableArguments collects message ranges up to the first redirect back to
// the dispatcher's root, as vanilla's signing visitor requires. In particular,
// execute run's nested arguments remain parseable but are not client-signed.
// vanilla: SignableCommand.of, ArgumentVisitor.visitArguments(rejectRootRedirects=true)
func SignableArguments(node *Node, input string, tokens []Token, allowSelectors bool) ([]MessageArgumentRange, *Failure) {
	_, steps, fail := matchPath(node, input, tokens, allowSelectors)
	return steps.messageArguments(node), fail
}

func matchPath(node *Node, input string, tokens []Token, allowSelectors bool) ([]*Node, *pathStep, *Failure) {
	won := walkNode(node, input, tokens, 0, allowSelectors, nil)
	finish := func(path []*Node, fail *Failure) ([]*Node, *pathStep, *Failure) {
		return path, won.path, fail
	}
	if won.idx == len(tokens) {
		if len(won.errs) == 1 {
			return finish(nil, &Failure{At: won.idx, Err: won.errs[0]})
		}
		// Every word was matched. The winning read must reach the end of
		// the input too: a greedy message or phrase reader takes its last
		// trailing spaces with it, any other read stops where its last
		// word does, and input behind that read is the argument the
		// dispatcher's separator rules refuse — unless the word's node
		// redirects, whose continuation vanilla steps into by skipping the
		// one space behind it. Explicit redirects need a child parse; aliases may
		// inherit executability. vanilla: Commands.getParseException
		path := won.path.nodes()
		hopped := won.node.Redirect != nil
		for !won.node.Executable && won.node.Redirect != nil && won.node.RedirectName == "" {
			won.node = won.node.Redirect
			hopped = true
			path[len(path)-1] = won.node
		}
		trailing := len(tokens) > 0 && TokenEnd(tokens) < len(input)
		if won.node.Executable && (!trailing || won.readsRest) {
			return finish(path, &Failure{})
		}
		if !trailing || (hopped && len(input)-TokenEnd(tokens) == 1) {
			// No command was reached, and vanilla's reader stands at the
			// input's end: spent, or past the only trailing space a
			// redirect's skip reaches.
			// vanilla: ContextChain.tryFlatten
			return finish(nil, &Failure{At: won.idx, UnknownCommand: true})
		}
		return finish(nil, &Failure{At: won.idx, Trailing: true})
	}
	// Words left over: a single typed error on the winning branch is what
	// vanilla rethrows, with its reader's cursor; a winner stuck on the
	// first word is an unknown command, any other an unknown argument at
	// its stopping token. vanilla: CommandDispatcher.execute
	if len(won.errs) == 1 {
		return finish(nil, &Failure{At: won.idx, Err: won.errs[0]})
	}
	if won.idx == 0 {
		return finish(nil, &Failure{At: 0, UnknownCommand: true})
	}
	return finish(nil, &Failure{At: won.idx})
}

// attempt is what walking one node's subtree left: the last node, the
// stopping token, the typed errors its argument children threw, and the
// path from the command literal (meaningless for a branch with no command).
// readsRest says the argument that consumed the last word also read the
// input's end with it, trailing spaces included, which only the greedy
// readers that read to the end of the line do. As in Brigadier's
// parseNodes, a node whose argument children all fail hands back its own
// start and their errors; one with a parsing child hands back the best child
// attempt, its own errors forgotten. vanilla: ParseResults
type attempt struct {
	node *Node
	idx  int
	errs []*ParseError
	path *pathStep
	// readsRest holds only for the winning attempt of a walk that consumed
	// every word, and says the read took the whole rest of the input.
	readsRest bool
}

// pathStep is one node of a walk's path, linked to the step before it, so
// that sibling branches share the steps above them instead of each copying
// them, which keeps a walk down a long chain linear in its words.
type pathStep struct {
	node     *Node
	readNode *Node
	prev     *pathStep
	message  MessageArgumentRange
	// redirectFrom retains the parent context's last parsed node for /help.
	redirectFrom *Node
}

// messageArguments preserves parse order without copying sibling paths.
func (s *pathStep) messageArguments(stopAt *Node) []MessageArgumentRange {
	var arguments []MessageArgumentRange
	for at := s; at != nil; at = at.prev {
		if at.node == stopAt {
			// Walking backward, discard the child context collected so far;
			// the redirecting node's own argument belongs to its parent.
			arguments = nil
		}
		if at.message.Name != "" {
			arguments = append(arguments, at.message)
		}
	}
	slices.Reverse(arguments)
	return arguments
}

// nodes is the path from its first step to s; nil for no steps.
func (s *pathStep) nodes() []*Node {
	n := 0
	for at := s; at != nil; at = at.prev {
		n++
	}
	if n == 0 {
		return nil
	}
	path := make([]*Node, n)
	for at := s; at != nil; at = at.prev {
		n--
		path[n] = at.node
	}
	return path
}

// argumentNodes retains the reader's node when a redirect replaces its path entry.
func (s *pathStep) argumentNodes() []*Node {
	path := s.nodes()
	i := len(path) - 1
	for at := s; at != nil; at = at.prev {
		if at.readNode != nil {
			path[i] = at.readNode
		}
		i--
	}
	return path
}

// walkNode walks one node's subtree: the literal child naming the word at
// idx, else the argument children, each consuming its words and walking on.
// path ends on this node (a redirect hop replaces it with a step on the
// target); steps are never changed in place, since siblings share them. The
// best clean child attempt wins; with none, the walk stops here with its
// start token and the children's errors. vanilla: CommandDispatcher.parseNodes
func walkNode(node *Node, input string, tokens []Token, idx int, allowSelectors bool, path *pathStep) attempt {
	// Out of words: the walk ends here, and whether it runs is read off the
	// node. A node that both runs and redirects (a conditional or store)
	// redirects only for words past it.
	if idx == len(tokens) {
		// Brigadier skips one separator, so further whitespace can itself
		// be a message even though Tokenize has no word left to represent it.
		// vanilla: CommandDispatcher.parseNodes, MessageArgument.parse
		if idx > 0 && len(input)-TokenEnd(tokens) > 1 && node.Redirect == nil {
			for _, child := range node.Children {
				if child.Type != NodeArgument || child.Parser != ParserMessage || !child.Greedy {
					continue
				}
				start := TokenEnd(tokens) + 1
				if err := checkMessageLength(input[start:]); err != nil {
					return attempt{node: node, idx: idx, path: path, errs: []*ParseError{err}}
				}
				step := &pathStep{node: child, prev: path,
					message: MessageArgumentRange{Name: child.Name, Start: start, End: len(input)}}
				return attempt{node: child, idx: idx, path: step, readsRest: true}
			}
		}
		return attempt{node: node, idx: idx, path: path}
	}
	if node.Redirect != nil {
		redirectFrom := node
		for node.Redirect != nil {
			node = node.Redirect
		}
		if path != nil {
			readNode := path.node
			if path.readNode != nil {
				readNode = path.readNode
			}
			path = &pathStep{node: node, readNode: readNode, prev: path.prev, message: path.message, redirectFrom: redirectFrom}
		}
	}

	// A literal child whose name is the word beats everything: Brigadier
	// walks that literal alone and never looks at this node's arguments.
	word := tokens[idx].Text
	if child := FindLiteralChild(node, word); child != nil {
		return walkNode(child, input, tokens, idx+1, allowSelectors, &pathStep{node: child, prev: path})
	}

	// Then the arguments, each taking the words its parser reads and
	// contributing an attempt when it parses cleanly.
	var pots []attempt
	var errs []*ParseError
	for _, child := range node.Children {
		if child.Type != NodeArgument {
			continue
		}
		arity := ArgumentArity(child, tokens[idx:])
		switch child.Parser {
		case ParserVec3, ParserVec2, ParserBlockPos, ParserRotation, ParserColumnPos:
			// Read while the walk is reading it, so that too few words
			// are the argument's own incomplete error.
			if err := parseCoordinateArgument(child.Parser, tokens[idx:min(idx+arity, len(tokens))]); err != nil {
				errs = append(errs, err)
				continue
			}
		case ParserInteger:
			// An integer is read, and held to the range the argument
			// declares, while the walk is reading it.
			// vanilla: IntegerArgumentType.parse
			if err := parseIntegerArgument(tokens[idx:idx+arity], child.Int); err != nil {
				errs = append(errs, err)
				continue
			}
		case ParserFloat:
			// A float is read, and held to the range the argument
			// declares, while the walk is reading it.
			// vanilla: FloatArgumentType.parse
			if err := parseFloatArgument(tokens[idx:idx+arity], child.Float); err != nil {
				errs = append(errs, err)
				continue
			}
		case ParserBool:
			// vanilla: BoolArgumentType.parse
			if err := parseBoolArgument(tokens[idx : idx+arity]); err != nil {
				errs = append(errs, err)
				continue
			}
		case ParserTime:
			// minecraft:time is read as number and unit during the walk;
			// the tick count and multiplier are the handler's.
			// vanilla: TimeArgument.parse
			if err := parseTimeArgument(tokens[idx]); err != nil {
				errs = append(errs, err)
				continue
			}
		case ParserResourceLoc, ParserResourceKey:
			// An identifier is read as its longest alphabet run, which
			// the dispatcher then demands whitespace behind.
			// vanilla: IdentifierArgument.parse
			if err := parseResourceLocArgument(tokens[idx]); err != nil {
				errs = append(errs, err)
				continue
			}
		case ParserItemStack:
			// An item stack is read during the walk; a refusal this server
			// has no wording for is left to the command, which is silent.
			// vanilla: ItemParser
			if err := parseItemStackArgument(tokens[idx : idx+arity]); err != nil {
				errs = append(errs, err)
				continue
			}
		case ParserItemPred:
			// An item predicate is read during the walk, its grammar
			// skipping whitespace between a name and its bracket.
			// vanilla: ItemPredicateArgument
			if _, err := parseItemPredicateArgument(tokens[idx : idx+arity]); err != nil {
				errs = append(errs, err)
				continue
			}
		case ParserBlockState:
			// A block entity tag behind the properties spans words, so
			// the argument's own span is what its parser reads.
			// vanilla: BlockStateParser
			if err := parseBlockStateArgument(tokens[idx : idx+arity]); err != nil {
				errs = append(errs, err)
				continue
			}
		case ParserBlockPredicate:
			// vanilla: BlockPredicateArgument
			if err := parseBlockPredicateArgument(tokens[idx : idx+arity]); err != nil {
				errs = append(errs, err)
				continue
			}
		case ParserParticle:
			// A particle's type and SNBT options are read during the walk,
			// before coordinates. vanilla: ParticleArgument.readParticle
			if err := parseParticleArgument(tokens[idx : idx+arity]); err != nil {
				errs = append(errs, err)
				continue
			}
		case ParserMessage:
			// A message reads its selectors during the walk: a refused
			// selector letter is stepped over, any other refusal stops the
			// branch, and text past the cap is refused first.
			// vanilla: MessageArgument.Message.parseText
			if err := parseMessageArgument(input, tokens[idx:idx+arity], allowSelectors); err != nil {
				errs = append(errs, err)
				continue
			}
		case ParserString:
			// A quotable phrase or single word is read while the walk is
			// reading it; nothing may stand behind the value.
			// vanilla: StringArgumentType.parse
			if child.Quotable {
				if err := parseQuotablePhraseArgument(tokens[idx : idx+arity]); err != nil {
					errs = append(errs, err)
					continue
				}
			} else if !child.Greedy {
				if err := parseWordArgument(tokens[idx]); err != nil {
					errs = append(errs, err)
					continue
				}
			}
		}
		if child.EntityArg != nil {
			// An entity argument parses its selector during the walk; a
			// bad selector, or one the argument refuses, stops the branch.
			// vanilla: EntityArgument.parse
			if err := parseEntityArgument(child, tokens[idx:idx+arity], allowSelectors); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if child.Selector != nil {
			// A score holder reads the same way, but its refusal lands at
			// the word's end, where vanilla's reader stood.
			// vanilla: ScoreHolderArgument.parse
			if err := parseSelectorArgument(child, tokens[idx:idx+arity], allowSelectors); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if child.Check != nil {
			// An argument naming something it knows (dimension, anchor,
			// function) checks the word alone, so the cursor is at the
			// word's end, offset by its start.
			// vanilla: e.g. DimensionArgument.parse
			word := tokens[idx].Text
			if child.Parser == ParserNbtPath || child.Parser == ParserComponent || child.Parser == ParserLootModifier || child.Parser == ParserSlotSource || child.Parser == ParserLootPredicate || child.Parser == ParserLootTable || child.Parser == ParserContextIntProvider || child.Parser == ParserContextFloatProvider || child.Parser == ParserDialog || child.Parser == ParserFeature {
				// A path's own check reads the raw input its quotes and
				// matchers spanned, so cursors keep runs of spaces.
				last := tokens[idx+arity-1]
				word = input[tokens[idx].Start : last.Start+len(last.Text)]
			}
			if err := child.Check(word); err != nil {
				err.Cursor += tokens[idx].Start
				errs = append(errs, err)
				continue
			}
		}
		if child.Resource != nil {
			// A registry entry is looked up during the walk; an unknown
			// name stops the branch. vanilla: ResourceArgument.parse
			if err := parseResourceArgument(child, tokens[idx]); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if child.Parser == ParserGamemode {
			// A game mode is read while the walk is reading it: a word
			// that names no mode is refused where the word ends.
			// vanilla: GameModeArgument.parse
			if _, ok := parseGameMode(tokens[idx].Text); !ok {
				errs = append(errs, &ParseError{
					Key:    "argument.gamemode.invalid",
					With:   []string{tokens[idx].Text},
					Cursor: tokens[idx].Start + len(tokens[idx].Text),
				})
				continue
			}
		}
		if child.Nbt {
			// An SNBT compound is read during the walk, its braces
			// spanning words; a shape vanilla's grammar names no
			// refusal for is left to the command.
			// vanilla: CompoundTagArgument.parse
			if err := parseNbtArgument(tokens[idx : idx+arity]); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if child.NbtValue {
			if err := parseNbtTagArgument(tokens[idx:idx+arity], false); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		end := min(idx+arity,
			// No parser here reads more words than the command has
			// left; this is only a guard for the walk's arithmetic.
			len(tokens))
		step := &pathStep{node: child, prev: path}
		if child.Parser == ParserMessage {
			argumentEnd := TokenEnd(tokens[idx:end])
			if child.Greedy {
				argumentEnd = len(input)
			}
			step.message = MessageArgumentRange{Name: child.Name, Start: messageArgumentStart(tokens[idx]), End: argumentEnd}
		}
		pot := walkNode(child, input, tokens, end, allowSelectors, step)
		if end == len(tokens) {
			// The child consumed the last word. A greedy message or
			// phrase reader consumes the input's end with it, trailing
			// spaces included; every other reader stops where its last
			// word does, and the dispatcher's separator rules read
			// behind it. vanilla: MessageArgument.parse,
			// StringArgumentType.greedyString
			pot.readsRest = child.Greedy && (child.Parser == ParserMessage || child.Parser == ParserString)
		}
		pots = append(pots, pot)
	}
	if len(pots) > 0 {
		// Brigadier prefers the attempt that read the whole input, then
		// one whose branch failed without a typed error, and otherwise
		// keeps the tree's order; this node's own errors are dropped.
		sort.SliceStable(pots, func(i, j int) bool {
			a, b := pots[i].idx == len(tokens), pots[j].idx == len(tokens)
			if a != b {
				return a
			}
			return len(pots[i].errs) == 0 && len(pots[j].errs) != 0
		})
		return pots[0]
	}
	return attempt{node: node, idx: idx, errs: errs, path: path}
}

// parseEntityArgument reads an entity argument's selector and checks it as
// EntityArgument.parse does: single-target refuses a multi-target selector,
// players-only one that may answer anything else, both with the cursor at
// the input's start, where vanilla rewinds.
func parseEntityArgument(node *Node, tokens []Token, allowSelectors bool) *ParseError {
	span := joinSelector(tokens)
	sel, end, err := parseSelectorWord(span.text, tokens[0].Start, allowSelectors)
	if err != nil {
		return err
	}
	arg := node.EntityArg
	if arg.Single && sel.MaxResults > 1 {
		key := keyTooManyEntities
		if arg.PlayersOnly {
			key = keyTooManyPlayers
		}
		return &ParseError{Key: key, Cursor: 0}
	}
	if arg.PlayersOnly && sel.IncludesEntities {
		if !arg.Profile && sel.CurrentEntity {
			// @s names the runner, who is a player, and is the one
			// selector a players-only argument accepts anyway.
			return nil
		}
		if arg.Profile {
			// The profile argument has no exception for @s, and reports
			// where its selector ended rather than at the input's start.
			return &ParseError{Key: keyOnlyPlayers, Cursor: span.absolute(end - 1)}
		}
		return &ParseError{Key: keyOnlyPlayers, Cursor: 0}
	}
	return nil
}

// parseMessageArgument reads a message's text: each selector-starting @ is
// parsed, an unknown or missing selector letter stepped over, any other
// refusal fails, and text past the cap is refused before selectors are read.
// Without the selector permission every @ stays as typed.
// vanilla: MessageArgument.Message.parseText
func parseMessageArgument(input string, tokens []Token, allowSelectors bool) *ParseError {
	if err := checkMessageLength(input[messageArgumentStart(tokens[0]):]); err != nil {
		return err
	}
	if !allowSelectors {
		return nil
	}
	span := joinSelector(tokens)
	for at := 0; at < len(span.text); {
		if span.text[at] != '@' {
			at++
			continue
		}
		_, n, err := parseSelectorWord(span.text[at:], span.absolute(at), true)
		if err != nil {
			if err.Key != keySelectorMissing && err.Key != keySelectorUnknown {
				return err
			}
			at++
			continue
		}
		at += n
	}
	return nil
}

// messageArgumentStart retains whitespace beyond the one separator skipped
// by Brigadier; signed input must not be reconstructed from token text.
func messageArgumentStart(token Token) int {
	if len(token.Gap) > 0 {
		return token.Start - len(token.Gap) + 1
	}
	return token.Start
}

func checkMessageLength(message string) *ParseError {
	// The limit counts UTF-16 units, including whitespace; the refusal has
	// no input context. vanilla: MessageArgument.Message.parseText
	if remaining := utf16Length(message); remaining > maxMessageLength {
		return &ParseError{
			Key:       keyMessageTooLong,
			With:      []string{strconv.Itoa(remaining), strconv.Itoa(maxMessageLength)},
			NoContext: true,
		}
	}
	return nil
}

// utf16Length returns the UTF-16 code units s takes: one per rune and a
// second for each above U+FFFF, the length a Java string reports.
func utf16Length(s string) int {
	units := 0
	for _, r := range s {
		units++
		if r > 0xFFFF {
			units++
		}
	}
	return units
}

// parseSelectorArgument reads a score holder's selector as
// ScoreHolderArgument.parse does: single-holder refuses a multi-target
// selector at the end of the word it spanned.
func parseSelectorArgument(node *Node, tokens []Token, allowSelectors bool) *ParseError {
	// Anything but a selector is a holder's name: any word without a space.
	// vanilla: ScoreHolderArgument.parse
	if !strings.HasPrefix(tokens[0].Text, "@") {
		return nil
	}
	span := joinSelector(tokens)
	sel, end, err := parseSelectorWord(span.text, tokens[0].Start, allowSelectors)
	if err != nil {
		return err
	}
	if node.Selector.Single && sel.MaxResults > 1 {
		return &ParseError{Key: keyTooManyEntities, Cursor: span.absolute(end-1) + 1}
	}
	return nil
}

// parseResourceArgument reads a registry entry's resource location and looks
// it up. Identifier.read stops at the first character outside the alphabet,
// so an unknown name refuses before that character, and a leftover tail is
// the dispatcher's missing separator.
// vanilla: ResourceArgument.parse, Identifier.read
func parseResourceArgument(node *Node, token Token) *ParseError {
	name, n, ok := readIdentifier(token.Text)
	if !ok {
		return &ParseError{Key: keyIDInvalid, Cursor: token.Start}
	}
	if !resourceExists(node.Resource.Registry, name) {
		return &ParseError{
			Key:    resourceNotFoundKey,
			With:   []string{name, node.Resource.Registry},
			Cursor: token.Start + n,
		}
	}
	if n < len(token.Text) {
		return &ParseError{Key: keyExpectedSeparator, Cursor: token.Start + n}
	}
	return nil
}

// resourceExists reports whether a registry carries an entry. Entity types
// are not a synchronized registry, so they have their own table.
func resourceExists(registryName, entry string) bool {
	if registryName == entityTypeRegistry {
		_, ok := registry.EntityTypeID(entry)
		return ok
	}
	if registryName == attributeRegistry {
		_, ok := registry.AttributeByName(entry)
		return ok
	}
	if registryName == mobEffectRegistry {
		_, ok := registry.MobEffectID(entry)
		return ok
	}
	r, ok := registry.Get(registryName)
	return ok && r.ID(entry) >= 0
}

// parseBoolArgument reads a brigadier:bool's joined, possibly quoted, text:
// only "true" and "false" are booleans. An empty read is expected-boolean
// at the read's end, an unknown value invalid-boolean at its start (where
// vanilla rewinds), a bad quote its own refusal; a tail is left for the
// separator check. vanilla: BoolArgumentType.parse
func parseBoolArgument(tokens []Token) *ParseError {
	span := joinSelector(tokens)
	value, n, err := readStringValue(span.text)
	if err != nil {
		err.Cursor = span.absolute(err.Cursor)
		return err
	}
	if value == "" {
		return &ParseError{Key: keyBoolExpected, Cursor: span.absolute(n)}
	}
	if value != "true" && value != "false" {
		return &ParseError{Key: keyBoolInvalid, With: []string{value}, Cursor: tokens[0].Start}
	}
	if n < len(span.text) {
		return &ParseError{Key: keyExpectedSeparator, Cursor: span.absolute(n)}
	}
	return nil
}

// readStringValue reads a quoted string, or the unquoted run, answering the
// value, how much was read, and a quote refusal with its cursor. The walk
// and handlers share it, so they cannot disagree.
// vanilla: StringReader.readString, StringReader.readStringUntil
func readStringValue(word string) (value string, n int, err *ParseError) {
	if word == "" {
		return "", 0, nil
	}
	if q := word[0]; q == '"' || q == '\'' {
		n = 1 // the opening quote
		var out []byte
		for n < len(word) {
			c := word[n]
			n++
			if c == '\\' {
				if n == len(word) {
					break
				}
				e := word[n]
				if e != q && e != '\\' {
					// the vanilla reader rewinds onto the character it refused
					return "", n, &ParseError{Key: keyQuoteEscape, With: []string{string(e)}, Cursor: n}
				}
				out = append(out, e)
				n++
				continue
			}
			if c == q {
				return string(out), n, nil
			}
			out = append(out, c)
		}
		return "", n, &ParseError{Key: keyQuoteExpectedEnd, Cursor: n}
	}
	for n < len(word) && isAllowedInUnquotedString(word[n]) {
		n++
	}
	return word[:n], n, nil
}

// boolArgValue joins a boolean's words back into vanilla's read text (a
// quoted value standing for its contents); only "true" and "false" are
// booleans. vanilla: BoolArgumentType.parse
func boolArgValue(words []string) (string, bool) {
	value, _, err := readStringValue(strings.Join(words, " "))
	if err != nil || value != "true" && value != "false" {
		return "", false
	}
	return value, true
}

// parseQuotablePhraseArgument reads a quotable phrase, whose opening quote
// must close; a tail is the separator check's.
// vanilla: StringArgumentType.parse
func parseQuotablePhraseArgument(tokens []Token) *ParseError {
	span := joinSelector(tokens)
	_, n, err := readStringValue(span.text)
	if err != nil {
		err.Cursor = span.absolute(err.Cursor)
		return err
	}
	if n < len(span.text) {
		return &ParseError{Key: keyExpectedSeparator, Cursor: span.absolute(n)}
	}
	return nil
}

// parseWordArgument reads a single-word string's allowed unquoted run; any
// leftover tail (or a leading disallowed byte) is the separator check's.
// vanilla: StringArgumentType.parse, StringReader.readUnquotedString
func parseWordArgument(token Token) *ParseError {
	n := 0
	for n < len(token.Text) && isAllowedInUnquotedString(token.Text[n]) {
		n++
	}
	if n < len(token.Text) {
		return &ParseError{Key: keyExpectedSeparator, Cursor: token.Start + n}
	}
	return nil
}

// parseTimeArgument reads minecraft:time: a word not starting with a number,
// or with an unknown unit, stops the branch; a tail is the separator
// check's. The unit is the unquoted-string run, so a digit or sign after it
// is part of the refused unit. Ticks and the minimum are the handler's.
// vanilla: TimeArgument.parse, StringReader.readUnquotedString
func parseTimeArgument(token Token) *ParseError {
	r := &coordReader{s: token.Text}
	if _, err := r.readFloat(); err != nil {
		err.Cursor += token.Start
		return err
	}
	n := r.cur
	for n < len(token.Text) && isAllowedInUnquotedString(token.Text[n]) {
		n++
	}
	if _, ok := timeUnits[token.Text[r.cur:n]]; !ok {
		// The vanilla reader stands behind the unit it read when it
		// refuses it, at the run's end rather than the word's.
		return &ParseError{Key: keyTimeInvalidUnit, Cursor: token.Start + n}
	}
	if n < len(token.Text) {
		return &ParseError{Key: keyExpectedSeparator, Cursor: token.Start + n}
	}
	return nil
}

// parseResourceLocArgument parses the longest identifier-alphabet prefix of
// the word: anything left over is the dispatcher's missing separator, and a
// prefix that is no location stops the branch at the word's start.
// vanilla: IdentifierArgument.parse
func parseResourceLocArgument(token Token) *ParseError {
	_, n, ok := readIdentifier(token.Text)
	if !ok {
		return &ParseError{Key: keyIDInvalid, Cursor: token.Start}
	}
	if n < len(token.Text) {
		return &ParseError{Key: keyExpectedSeparator, Cursor: token.Start + n}
	}
	return nil
}

// isCharValid reports whether the vanilla identifier's greedy reader takes
// c: the lowercase letters, digits, underscore, colon, slash, dot and hyphen.
// vanilla: Identifier.isCharValid
func isCharValid(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' ||
		c == '_' || c == ':' || c == '/' || c == '.' || c == '-'
}

// identifierPrefix is how much of a word Identifier.read reads: the longest
// run of characters isCharValid takes from the word's start.
func identifierPrefix(word string) int {
	n := 0
	for n < len(word) && isCharValid(word[n]) {
		n++
	}
	return n
}

// readIdentifier reads word's longest resource location prefix: the name,
// its length, and whether it parses.
// vanilla: Identifier.read
func readIdentifier(word string) (string, int, bool) { return commandsyntax.ReadIdentifier(word) }
func parseIdentifier(prefix string) (string, bool)   { return commandsyntax.ParseIdentifier(prefix) }

// parseNbtArgument reads an SNBT compound with the shared strict grammar.
// vanilla: CompoundTagArgument.parse, TagParser
func parseNbtArgument(tokens []Token) *ParseError {
	return parseNbtTagArgument(tokens, true)
}

// parseNbtTagArgument preserves the grammar's refusal and its cursor.
// vanilla: NbtTagArgument.parse, TagParser.parseCompoundAsArgument
func parseNbtTagArgument(tokens []Token, compound bool) *ParseError {
	if len(tokens) == 0 {
		return nil
	}
	span := joinSelector(tokens)
	value, n, pe := nbt.ParseValueTag(span.text)
	if pe != nil {
		return snbtRefusal(pe, span, pe.Offset)
	}
	if _, ok := value.(nbt.CompoundTag); compound && !ok {
		return &ParseError{Key: summonBadNBTKey, Cursor: span.absolute(n)}
	}
	if n < len(span.text) {
		if !strings.ContainsRune(" \t\n\r", rune(span.text[n])) {
			return &ParseError{Key: keyExpectedSeparator, Cursor: span.absolute(n)}
		}
		return &ParseError{Key: keyUnknownArgument, Cursor: span.absolute(skipSpace(span.text, n))}
	}
	return nil
}

// keyLiteralIncorrect is the error the vanilla grammar's own readers name
// the literal they were reading towards with.
const keyLiteralIncorrect = "argument.literal.incorrect"

// keyMessageTooLong is the refusal a message argument's text over its
// character cap gets.
// vanilla: MessageArgument.Message.parseText
const keyMessageTooLong = "argument.message.too_long"

// maxMessageLength is the longest text a message argument accepts, in
// characters.
const maxMessageLength = 256

// The errors the walk's own argument readers throw.
// vanilla: TimeArgument, IdentifierArgument, BoolArgumentType, StringReader,
// IntegerArgumentType, ItemParser, BlockStateParser
const (
	keyTimeInvalidUnit    = "argument.time.invalid_unit"
	keyTimeTooLow         = "argument.time.tick_count_too_low"
	keyIDInvalid          = "argument.id.invalid"
	keyBoolExpected       = "parsing.bool.expected"
	keyBoolInvalid        = "parsing.bool.invalid"
	keyQuoteEscape        = "parsing.quote.escape"
	keyQuoteExpectedEnd   = "parsing.quote.expected.end"
	keyIntegerLow         = "argument.integer.low"
	keyIntegerBig         = "argument.integer.big"
	keyFloatLow           = "argument.float.low"
	keyFloatBig           = "argument.float.big"
	keyDoubleLow          = "argument.double.low"
	keyDoubleBig          = "argument.double.big"
	keyItemIDInvalid      = "argument.item.id.invalid"
	keyBlockIDInvalid     = "argument.block.id.invalid"
	keyBlockTagDisallowed = "argument.block.tag.disallowed"
	keyBlockTagUnknown    = "arguments.block.tag.unknown"
	// The property overrides a block state argument carries, which the
	// command's own reader refuses.
	// vanilla: BlockStateParser.readProperties
	keyBlockPropertyUnknown   = "argument.block.property.unknown"
	keyBlockPropertyInvalid   = "argument.block.property.invalid"
	keyBlockPropertyDuplicate = "argument.block.property.duplicate"
	keyBlockPropertyNoValue   = "argument.block.property.novalue"
	keyBlockPropertyUnclosed  = "argument.block.property.unclosed"
	keyExpectedSymbol         = "parsing.expected"
)

// The dispatcher's own refusals, which the messages a load error or an
// instantiated macro spells read the way a client's language table does.
// vanilla: CommandDispatcher
const (
	keyUnknownCommand  = "command.unknown.command"
	keyUnknownArgument = "command.unknown.argument"
)

// messageOf spells a translation key the way the client's language table
// renders it, which is how vanilla logs the exceptions the dispatcher and
// its arguments throw.
func messageOf(key string, with ...string) string {
	args := make([]protocol.Text, len(with))
	for i, w := range with {
		args[i] = protocol.Text{Text: w}
	}
	return protocol.Text{Translate: key, With: args}.Plain()
}

// joinedSelector is a selector rejoined from its words into the text
// vanilla's reader would read, with each word's offset kept to map a cursor
// back to the input.
type joinedSelector struct {
	text   string
	starts []int
	tokens []Token
}

// blockWordStart is where an argument's first word stands in the input (an
// execute's tail included), for a command's own parse error; -1 when no
// token holds it.
func blockWordStart(input, firstWord string) int {
	for _, token := range Tokenize(input) {
		if token.Text == firstWord {
			return token.Start
		}
	}
	return -1
}

// joinSelector joins the words of a selector into one string.
func joinSelector(tokens []Token) joinedSelector {
	var span joinedSelector
	span.tokens = tokens
	var b strings.Builder
	for i, token := range tokens {
		if i > 0 {
			b.WriteString(cmp.Or(token.Gap, " "))
		}
		span.starts = append(span.starts, b.Len())
		b.WriteString(token.Text)
	}
	span.text = b.String()
	return span
}

// absolute maps a cursor in the joined text back to the input it was built
// from, landing on the word the cursor sat in.
func (s joinedSelector) absolute(offset int) int {
	for i, v := range slices.Backward(s.starts) {
		if offset >= v {
			local := min(offset-v, len(s.tokens[i].Text))
			return s.tokens[i].Start + local
		}
	}
	return s.tokens[0].Start
}

// ArgumentArity reports how many words an argument consumes: 1 for most, 3
// for a position, 2 for a rotation, the rest for greedy parsers. Entity
// arguments, item stacks, item predicates and blocks take as many words as
// their brackets, quotes or block entity tag span.
func ArgumentArity(node *Node, tokens []Token) int {
	switch node.Parser {
	case ParserVec3, ParserBlockPos:
		return 3
	case ParserRotation, ParserVec2, ParserColumnPos:
		return 2
	case ParserItemStack:
		return itemStackSpanCount(tokens)
	case ParserItemPred:
		// A predicate's bracketed tests may be parted from its item by
		// whitespace, so its span is read over the words rather than off
		// the first word alone.
		return itemPredicateSpanCount(tokens)
	case ParserBool:
		// A quoted bool's read runs to its closing quote, which a later
		// word may carry. vanilla: StringReader.readStringUntil
		return itemStackSpanCount(tokens)
	case ParserBlockState, ParserBlockPredicate:
		return blockStateSpanCount(tokens)
	case ParserParticle:
		// Options in braces may hold spaces. vanilla: ParticleArgument
		return blockStateSpanCount(tokens)
	case ParserComponent:
		return componentSpanCount(tokens)
	case ParserNbtPath:
		// A path's quoted keys and the compound matchers its elements
		// carry span the words the spaces inside them split.
		return pathSpan(tokens, tokenText)
	case ParserLootModifier, ParserSlotSource, ParserLootPredicate, ParserLootTable, ParserDialog, ParserFeature, ParserContextIntProvider, ParserContextFloatProvider:
		// An inline modifier or table's compounds and lists span the words the
		// spaces inside them split.
		return spanCount(tokens, tokenText, true, false)
	case ParserNbtCompound, ParserNbtTag:
		// One compound or tag reads the words its braces, brackets and
		// quotes span, and stops where the value closes; anything behind
		// the word that closes it is the separator rule's to refuse.
		// vanilla: CompoundTagArgument.parse, NbtTagArgument.parse
		return spanCount(tokens, tokenText, true, false)
	}
	if node.EntityArg != nil {
		return selectorSpanCount(tokens)
	}
	if node.Selector != nil {
		return selectorSpanCount(tokens)
	}
	if node.Quotable {
		// A quoted phrase runs to the quote that closes it, which a
		// later word may carry. vanilla: StringReader.readStringUntil
		return itemStackSpanCount(tokens)
	}
	if node.Greedy {
		return len(tokens)
	}
	return 1
}

// itemStackSpanCount counts an item stack's or predicate's words: one,
// unless a bracket or quote it opens closes in a later word.
func itemStackSpanCount(tokens []Token) int {
	return spanCount(tokens, tokenText, false, false)
}

// tokenText and plainText read a word off a walk token or a handler's plain
// word, in place, so a span read at each step costs only its own words.
func tokenText(t Token) string  { return t.Text }
func plainText(w string) string { return w }

// itemPredicateSpanCount counts an item predicate's words: the first alone
// if it opens no bracket, else up to the closing one, whitespace between a
// name and its bracket included.
func itemPredicateSpanCount(tokens []Token) int {
	return spanCount(tokens, tokenText, false, true)
}

// valueSpanCount counts how many words a plain value spans, an item stack, a
// block state or a quoted string alike: what a command's handler counts its
// own argument from.
func valueSpanCount(words []string) int {
	return spanCount(words, plainText, false, false)
}

// predicateSpanCount counts how many words an item predicate spans: its first
// word alone when it opens no bracket, and otherwise the words up to the one
// that closes the list a word behind the item may open.
func predicateSpanCount(words []string) int {
	return spanCount(words, plainText, false, true)
}

// blockStateSpanCount counts how many words a block argument spans, whose
// block entity tag braces count the way an item's component brackets do.
func blockStateSpanCount(tokens []Token) int {
	return spanCount(tokens, tokenText, true, false)
}

// blockSpanCount is blockStateSpanCount over plain words, which is what a
// command's handler counts its own argument from.
func blockSpanCount(words []string) int {
	return spanCount(words, plainText, true, false)
}

// spanCount is the bracket readers' word span: one, unless a bracket or
// quote opened closes in a later word. braces counts braces too (a block's
// entity tag; item component lists open none). leading lets a leading word
// stand apart from its bracket (a predicate's item). A list never closed
// claims the rest, so its parser reports the unterminated bracket rather
// than an unknown argument.
func spanCount[W any](words []W, textOf func(W) string, braces, leading bool) int {
	if len(words) == 0 {
		return 1
	}
	depth, quote, opened := 0, byte(0), false
	for count, word := range words {
		text := textOf(word)
		for i := 0; i < len(text); i++ {
			c := text[i]
			switch {
			case quote != 0:
				if c == '\\' {
					i++
					continue
				}
				if c == quote {
					quote = 0
				}
			case c == '"':
				quote = '"'
				opened = true
			case c == '\'':
				quote = '\''
				opened = true
			case c == '[':
				depth++
				opened = true
			case c == ']':
				if depth > 0 {
					depth--
				}
			case braces && c == '{':
				depth++
				opened = true
			case braces && c == '}':
				if depth > 0 {
					depth--
				}
			}
		}
		if opened && depth == 0 && quote == 0 {
			// The bracket, brace or quote this value opened is closed
			// again, which is where the value ends.
			return count + 1
		}
		if !opened && (!leading || count > 0) {
			// The value opened nothing, so it was never more than the
			// words read so far, and whatever follows is the next
			// argument's.
			return 1
		}
	}
	if opened {
		return len(words)
	}
	return 1
}

// selectorSpanCount counts a selector's words: one, unless a bracket or quote
// it opens closes later. An unclosed one claims the rest, so its parser
// reports the unterminated bracket.
func selectorSpanCount(tokens []Token) int {
	count, _ := scanSelector(tokens, tokenText)
	return count
}

// selectorScan walks a selector's words as EntitySelectorParser reads its
// brackets and quotes: count words taken, open when what it opened never
// closed among them.
func selectorScan(words []string) (count int, open bool) {
	return scanSelector(words, plainText)
}

// scanSelector is selectorScan over words of either shape, read in place.
func scanSelector[W any](words []W, textOf func(W) string) (count int, open bool) {
	if len(words) == 0 {
		return 1, false
	}
	first := textOf(words[0])
	if first == "" || first[0] != '@' {
		return 1, false
	}
	depth, quote, opened := 0, byte(0), false
	for count, w := range words {
		word := textOf(w)
		for i := 0; i < len(word); i++ {
			c := word[i]
			switch {
			case quote != 0:
				if c == '\\' {
					i++
					continue
				}
				if c == quote {
					quote = 0
				}
			case c == '"':
				quote = '"'
				opened = true
			case c == '\'':
				quote = '\''
				opened = true
			case c == '[':
				depth++
				opened = true
			case c == ']':
				if depth > 0 {
					depth--
				}
			}
		}
		if opened && depth == 0 && quote == 0 {
			// The bracket or quote this selector opened is closed
			// again, which is where the selector ends.
			return count + 1, false
		}
		if !opened {
			// The first word opened nothing, so the selector was never
			// more than that word, and whatever follows it is another
			// argument's.
			return 1, false
		}
	}
	return len(words), true
}

// wordsIn joins words[at:at+span], which are the tail of input's words,
// keeping the spacing inside a quoted value.
func wordsIn(input string, words []string, at, span int) string {
	if span < 2 {
		return strings.Join(words[at:at+span], " ")
	}
	return wordsInTokens(Tokenize(input), words, at, span)
}

// wordsInTokens joins words[at:at+span], which are the tail of tokens, by the
// whitespace the input had between them. Words that do not line up with the
// tokens are joined with single spaces.
func wordsInTokens(tokens []Token, words []string, at, span int) string {
	joined := strings.Join(words[at:at+span], " ")
	if span < 2 {
		return joined
	}
	first := len(tokens) - len(words) + at
	last := first + span - 1
	if first < 0 || last >= len(tokens) || tokens[first].Text != words[at] || tokens[last].Text != words[at+span-1] {
		return joined
	}
	var b strings.Builder
	b.WriteString(tokens[first].Text)
	for _, t := range tokens[first+1 : last+1] {
		b.WriteString(cmp.Or(t.Gap, " "))
		b.WriteString(t.Text)
	}
	return b.String()
}

// selectorWordIn rejoins the selector starting at words[at] (a tail of
// input's words) as a selector reader reads it, quote spacing kept, and
// reports its word span.
func selectorWordIn(input string, words []string, at int) (word string, span int) {
	if at >= len(words) {
		return "", 1
	}
	span, _ = selectorScan(words[at:])
	return wordsIn(input, words, at, span), span
}

// CanonicalName maps an alias onto the command it stands for; the tree itself
// walks aliases through their redirects, so this is only the name the
// dispatch is keyed on.
func CanonicalName(word string) string {
	switch word {
	case "tp":
		return "teleport"
	case "tell", "w":
		return "msg"
	case "xp":
		return "experience"
	case "tm":
		return "teammsg"
	}
	return word
}

// ParseTimeArg parses a minecraft:time argument to its tick count, a Java
// float product saturated at the int range.
// vanilla: TimeArgument.parse
func ParseTimeArg(s string) (int64, bool) {
	var numPart, unitPart string
	for i, r := range s {
		if r >= '0' && r <= '9' || r == '.' || r == '-' {
			continue
		}
		numPart, unitPart = s[:i], s[i:]
		break
	}
	// No unit was found, so the whole word is the number.
	if numPart == "" && unitPart == "" {
		numPart = s
	}

	// A number past the float range parses as an infinity in Java, which
	// saturates; the range error hands the same infinity over here.
	f, err := strconv.ParseFloat(numPart, 32)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}

	factor, ok := timeUnits[unitPart]
	if !ok {
		return 0, false
	}

	// The product is carried as the Java float the multiplication runs in.
	product := float32(f) * float32(factor)
	ticks := math.Floor(float64(product) + 0.5)
	switch {
	case ticks >= float64(math.MaxInt32):
		return math.MaxInt32, true
	case ticks <= float64(math.MinInt32):
		return math.MinInt32, true
	}
	return int64(ticks), true
}

// checkTimeMin returns the check that refuses a minecraft:time argument
// whose tick count falls below floor, cursor at the word's end.
// vanilla: TimeArgument.parse
func checkTimeMin(floor int32) func(string) *ParseError {
	return func(word string) *ParseError {
		ticks, ok := ParseTimeArg(word)
		if !ok || ticks >= int64(floor) {
			return nil
		}
		return &ParseError{
			Key:    keyTimeTooLow,
			With:   []string{strconv.FormatInt(int64(floor), 10), strconv.FormatInt(ticks, 10)},
			Cursor: len(word),
		}
	}
}

// timeUnits maps the suffixes a minecraft:time argument can carry to their
// tick multipliers, matching the vanilla TimeArgument.UNITS table.
var timeUnits = map[string]float64{
	"d": 24000,
	"s": 20,
	"t": 1,
	"":  1,
}
