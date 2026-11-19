package protocol

import (
	"strconv"
	"strings"
)

// ansiColors are the SGR colour codes of the named text colours. The game's
// sixteen colours are the classic sixteen-colour palette, the dark ones the
// normal codes and the light ones the bright codes; gold, 0xFFAA00, is
// closest to yellow.
var ansiColors = map[string]string{
	"black":        "30",
	"dark_blue":    "34",
	"dark_green":   "32",
	"dark_aqua":    "36",
	"dark_red":     "31",
	"dark_purple":  "35",
	"gold":         "33",
	"gray":         "37",
	"dark_gray":    "90",
	"blue":         "94",
	"green":        "92",
	"aqua":         "96",
	"red":          "91",
	"light_purple": "95",
	"yellow":       "93",
	"white":        "97",
}

// sgr is the escape code that draws text in st, starting from a reset so
// nothing of the previous style is left on.
func sgr(st style) string {
	codes := []string{"0"}
	if c, ok := ansiColors[st.color]; ok {
		codes = append(codes, c)
	} else if rgb, ok := hexColor(st.color); ok {
		codes = append(codes, "38;2;"+strconv.Itoa(rgb>>16)+";"+strconv.Itoa(rgb>>8&0xFF)+";"+strconv.Itoa(rgb&0xFF))
	}
	if st.italic {
		codes = append(codes, "3")
	}
	if st.underline {
		codes = append(codes, "4")
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

// hexColor reads a "#RRGGBB" colour.
func hexColor(s string) (int, bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, false
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	return int(v), err == nil
}
