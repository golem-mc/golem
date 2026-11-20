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
	if w, ok := s.Source.(command.FeedbackWanter); !ok || w.WantsFeedback() {
		return true
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
