package nbt

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"strconv"
	"strings"
	"sync"
)

// unicodeNamesData is the table tools/java/UnicodeNames.java writes
// (mise run gen:unicodenames).
//
//go:embed unicodenames.txt.gz
var unicodeNamesData []byte

type codePointRange struct{ first, last rune }

var (
	unicodeNamesOnce sync.Once
	unicodeNames     map[string]rune
	unicodeBlocks    map[string][]codePointRange
)

func loadUnicodeNames() {
	unicodeNames = make(map[string]rune, 41000)
	unicodeBlocks = map[string][]codePointRange{}
	zr, err := gzip.NewReader(bytes.NewReader(unicodeNamesData))
	if err != nil {
		panic(err)
	}
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		kind, rest, _ := strings.Cut(sc.Text(), " ")
		switch kind {
		case "N":
			hex, name, _ := strings.Cut(rest, " ")
			cp, _ := strconv.ParseUint(hex, 16, 32)
			unicodeNames[name] = rune(cp)
		case "B":
			f := strings.SplitN(rest, " ", 3)
			first, _ := strconv.ParseUint(f[0], 16, 32)
			last, _ := strconv.ParseUint(f[1], 16, 32)
			unicodeBlocks[f[2]] = append(unicodeBlocks[f[2]], codePointRange{rune(first), rune(last)})
		}
	}
	if err := sc.Err(); err != nil {
		panic(err)
	}
}

// codePointOf resolves a \N{...} name the way Character.codePointOf does:
// ASCII case-insensitive, surrounding spaces ignored, and a code point with
// no name of its own answered by its "BLOCK NAME HEX" form.
// vanilla: Character.codePointOf
func codePointOf(name string) (rune, bool) {
	unicodeNamesOnce.Do(loadUnicodeNames)
	name = strings.ToUpper(strings.Trim(name, " "))
	if cp, ok := unicodeNames[name]; ok {
		return cp, true
	}
	i := strings.LastIndexByte(name, ' ')
	if i < 0 {
		return 0, false
	}
	hex := name[i+1:]
	cp, err := strconv.ParseUint(hex, 16, 32)
	if err != nil || strconv.FormatUint(cp, 16) != strings.ToLower(hex) {
		return 0, false
	}
	for _, r := range unicodeBlocks[name[:i]] {
		if rune(cp) >= r.first && rune(cp) <= r.last {
			return rune(cp), true
		}
	}
	return 0, false
}
