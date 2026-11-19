package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

// The data pack and the reports hold JSON of many shapes, and the generators
// that read them are strict about the shape they expect: they stop with a
// message rather than guess, so a newer data pack cannot slip a field past
// them. fail raises that stop from any depth; run reports it as an error.

// genError is a generator's own refusal, as opposed to a bug.
type genError string

func fail(format string, args ...any) {
	panic(genError(fmt.Sprintf(format, args...)))
}

// obj is a decoded JSON object.
type obj map[string]any

// decode parses JSON into an obj. A number written without a fraction or
// exponent is an int64 and any other a float64, so a table can tell 1 from 1.0
// where the difference reaches the output; asNum reads either.
func decode(data []byte, what string) obj {
	m, ok := decodeValue(data, what).(map[string]any)
	if !ok {
		fail("%s: not a JSON object", what)
	}
	return obj(m)
}

// decodeValue also accepts the inline sequences used by item modifiers.
func decodeValue(data []byte, what string) any {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		fail("%s: %v", what, err)
	}
	return normalize(v)
}

func normalize(v any) any {
	switch x := v.(type) {
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return i
		}
		f, err := x.Float64()
		if err != nil {
			fail("bad number %s", x)
		}
		return f
	case map[string]any:
		for k, e := range x {
			x[k] = normalize(e)
		}
		return x
	case []any:
		for i, e := range x {
			x[i] = normalize(e)
		}
		return x
	}
	return v
}

// asNum reads a JSON number of either kind.
func asNum(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int64:
		return float64(x), true
	}
	return 0, false
}

func (o obj) has(key string) bool {
	_, ok := o[key]
	return ok
}

// str is a required string.
func (o obj) str(key string) string {
	v, ok := o[key].(string)
	if !ok {
		fail("%q is missing or not a string: %v", key, o[key])
	}
	return v
}

func (o obj) strOr(key, def string) string {
	if !o.has(key) {
		return def
	}
	return o.str(key)
}

// num is a required number.
func (o obj) num(key string) float64 {
	v, ok := asNum(o[key])
	if !ok {
		fail("%q is missing or not a number: %v", key, o[key])
	}
	return v
}

func (o obj) numOr(key string, def float64) float64 {
	if !o.has(key) {
		return def
	}
	return o.num(key)
}

// intOr is an integer, truncated toward zero.
func (o obj) intOr(key string, def int) int {
	return int(o.numOr(key, float64(def)))
}

func (o obj) boolOr(key string, def bool) bool {
	if !o.has(key) {
		return def
	}
	v, ok := o[key].(bool)
	if !ok {
		fail("%q is not a boolean: %v", key, o[key])
	}
	return v
}

// obj is a nested object; ok is false when the key is absent.
func (o obj) obj(key string) (obj, bool) {
	v, present := o[key]
	if !present {
		return nil, false
	}
	m, ok := v.(map[string]any)
	if !ok {
		fail("%q is not an object: %v", key, v)
	}
	return obj(m), true
}

// list is a required array.
func (o obj) list(key string) []any {
	v, ok := o[key].([]any)
	if !ok {
		fail("%q is missing or not an array: %v", key, o[key])
	}
	return v
}

func (o obj) listOr(key string) []any {
	if !o.has(key) {
		return nil
	}
	return o.list(key)
}

// asObj reads an array element as an object.
func asObj(v any, what string) obj {
	m, ok := v.(map[string]any)
	if !ok {
		fail("%s is not an object: %v", what, v)
	}
	return obj(m)
}

// asStr reads an array element as a string.
func asStr(v any, what string) string {
	s, ok := v.(string)
	if !ok {
		fail("%s is not a string: %v", what, v)
	}
	return s
}

// readObj reads a JSON file from disk.
func readObj(path string) obj {
	b, err := os.ReadFile(path)
	if err != nil {
		fail("%v", err)
	}
	return decode(b, path)
}

// qualified gives a bare resource name its minecraft: namespace.
func qualified(name string) string {
	for _, c := range name {
		if c == ':' {
			return name
		}
	}
	return "minecraft:" + name
}

// protocolConst reads an integer constant out of internal/protocol/ids.go as source,
// for the generators that check a registry ID against the number the server
// is built with. Reading the file rather than importing protocol keeps the
// generator buildable when that package is not.
func protocolConst(e *env, name string) (int, error) {
	file := filepath.Join(e.root, "internal", "protocol", "ids.go")
	src, err := os.ReadFile(file)
	if err != nil {
		return 0, err
	}
	m := regexp.MustCompile(`(?m)^\s*(?:const\s+)?` + regexp.QuoteMeta(name) + `\s*=\s*(\d+)\b`).FindSubmatch(src)
	if m == nil {
		return 0, fmt.Errorf("no constant %s in %s", name, file)
	}
	return strconv.Atoi(string(m[1]))
}

// dpRead reads one file of the data pack.
func dpRead(dp *vanilla.DataPack, name string) ([]byte, error) {
	return fs.ReadFile(dp, name)
}

// readDir lists the names of the files in a directory on disk.
func readDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%w; run `mise run reference:reports`", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	return names, nil
}
