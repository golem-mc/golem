package main

import "strings"

// The generators here read the Java source CFR makes of the client's and the
// server's classes. The patterns match its output for the exact version; one
// that stops matching shows up as an empty or short table, and the generation
// refuses those rather than writing them.

// dyeColours are the 16 Minecraft dye colours, used as prefixes in dyed block
// names.
var dyeColours = []string{
	"black_", "blue_", "brown_", "cyan_", "gray_", "green_",
	"light_blue_", "light_gray_", "lime_", "magenta_", "orange_",
	"pink_", "purple_", "red_", "white_", "yellow_",
}

// weatheringPrefixes are the weathering stages of copper blocks.
var weatheringPrefixes = []string{"exposed_", "weathered_", "oxidized_"}

// copperPrefixes are the prefixes a copper family's blocks carry: none, each
// weathering stage, waxed, and waxed at each stage.
func copperPrefixes() []string {
	out := append([]string{""}, weatheringPrefixes...)
	out = append(out, "waxed_")
	for _, p := range weatheringPrefixes {
		out = append(out, "waxed_"+p)
	}
	return out
}

// matchParen returns the index of the close paren matching the open at openIdx,
// or -1.
func matchParen(s string, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// extractStatement returns the text from start up to the semicolon at
// paren/brace depth 0. A multi-line lambda's body contains ';\n' sequences
// that a non-greedy pattern would stop at.
func extractStatement(text string, start int) string {
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '(', '{':
			depth++
		case ')', '}':
			depth--
		case ';':
			if depth == 0 {
				return text[start:i]
			}
		}
	}
	return text[start:]
}

// statementAfter returns the text of a declaration from the match at start to
// the next "public static final", or 500 bytes past the match when there is
// none.
func statementAfter(text string, start, matchEnd int) string {
	end := strings.Index(text[start+1:], "public static final")
	if end == -1 {
		return text[start:min(len(text), matchEnd+500)]
	}
	return text[start : start+1+end]
}
