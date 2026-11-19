package command

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
)

// messageSelector is one selector a message argument carried, the slice of
// the message it covers, and the entities it resolved to.
type messageSelector struct {
	start, end int
	entities   []Entity
}

// resolveChatMessage replaces each selector in a message argument with the
// display names it names, the rest as typed, before a /say, /me or /msg goes
// out. Without the selector permission the raw text is kept; an @ starting
// no selector stays.
// vanilla: MessageArgument.Message.toComponent
func resolveChatMessage(src Source, message string) protocol.Text {
	if src.PermLevel() < PermLevelGamemasters {
		return protocol.Text{Text: message}
	}
	selectors := messageSelectors(src, message)
	if len(selectors) == 0 {
		return protocol.Text{Text: message}
	}
	out := protocol.Text{Text: message[:selectors[0].start]}
	readTo := selectors[0].start
	for _, sel := range selectors {
		if readTo < sel.start {
			out.Extra = append(out.Extra, protocol.Text{Text: message[readTo:sel.start]})
		}
		out.Extra = append(out.Extra, formatDisplayNames(sel.entities))
		readTo = sel.end
	}
	if readTo < len(message) {
		out.Extra = append(out.Extra, protocol.Text{Text: message[readTo:]})
	}
	return out
}

// messageSelectors reads the selectors a message argument holds: each @ that
// EntitySelectorParser accepts, with an @ it refuses as an unknown or missing
// selector letter stepped over and left in the text.
// vanilla: MessageArgument.Message.parseText
func messageSelectors(src Source, message string) []messageSelector {
	var selectors []messageSelector
	for at := 0; at < len(message); {
		if message[at] != '@' {
			at++
			continue
		}
		sel, n, err := parseSelectorWord(message[at:], at, true)
		if err != nil {
			// The walk has already failed the command on any refusal but
			// an unknown or missing selector letter.
			at++
			continue
		}
		selectors = append(selectors, messageSelector{
			start:    at,
			end:      at + n,
			entities: sel.findEntities(src),
		})
		at += n
	}
	return selectors
}

// WithChatArguments scopes immutable verified messages to one invocation.
func WithChatArguments(src Source, arguments chatsecurity.Arguments) Source {
	return argumentSource{Source: src, arguments: arguments}
}

type argumentSource struct {
	Source
	arguments chatsecurity.Arguments
}

func (s argumentSource) ChatArgument(name string) *chatsecurity.Message {
	message, ok := s.arguments.Message(name)
	if !ok {
		return nil
	}
	return &message
}

func (s argumentSource) InContext(ctx Context) (Source, bool) {
	src, ok := s.Source.InContext(ctx)
	if !ok {
		return nil, false
	}
	// Function bodies execute their own commands without the caller's arguments.
	if ctx.Silent {
		return src, true
	}
	return WithChatArguments(src, s.arguments), true
}

func (s argumentSource) WantsFeedback() bool { return wantsFeedback(s.Source) }

func (s argumentSource) Result(success bool, result int32) {
	reportResult(s.Source, success, result)
}
