package server

import (
	"github.com/golem-mc/golem/internal/server/command"
)

// WantsFeedback is false when both the output line and the operator echo go
// nowhere, by the rules below.
func (s *execSource) WantsFeedback() bool {
	if s.silent {
		return false
	}
	// Under execute the source keeps its own CommandSource, so its answer is
	// final (vanilla: CommandSourceStack.sendSuccess).
	if w, ok := s.Source.(command.FeedbackWanter); ok {
		return w.WantsFeedback()
	}
	send, _, logAdmin := s.rules().FeedbackRules()
	return send || logAdmin
}

// WantsFeedback reports whether the block keeps output or echoes to
// operators.
func (src *commandBlockSource) WantsFeedback() bool {
	if !src.trackOutput {
		return false
	}
	send, output, _ := src.rules().FeedbackRules()
	return send || output
}
