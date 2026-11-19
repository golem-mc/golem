package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

// The Java-driven generators boot the vanilla server's registries with one of
// the programs under tools/java and read what it prints. The server's logger
// prefixes every line (see vanilla.Ref.RunJava), so the patterns below anchor
// at the end of the line.

// java runs the tools/java program name and returns its standard output.
func (e *env) java(name string, o vanilla.JavaOptions) (string, error) {
	return e.ref.RunJava(filepath.Join(e.root, "tools", "java", name+".java"), o)
}

// asked runs a helper and returns the submatches of every line re matches. re
// is compiled (?m), so $ is the end of a line.
func (e *env) asked(helper string, re *regexp.Regexp) ([][]string, error) {
	out, err := e.java(helper, vanilla.JavaOptions{})
	if err != nil {
		return nil, err
	}
	return re.FindAllStringSubmatch(out, -1), nil
}

// stdoutLine is the text a helper printed, without the logger's prefix.
func stdoutLine(line string) string {
	const marker = "[STDOUT]: "
	if i := strings.LastIndex(line, marker); i >= 0 {
		return line[i+len(marker):]
	}
	return line
}

// needAtLeast stops a generation whose dump is too short to be the real thing,
// which is what a wrong classpath looks like.
func needAtLeast(n, want int, what string) error {
	if n < want {
		return fmt.Errorf("only %d %s in the dump; is the classpath right?", n, what)
	}
	return nil
}

// stateKey identifies a block state by its block and its properties in sorted
// "k=v,k=v" form, the key the block report's states and the helpers' dumps
// share.
type stateKey struct{ name, props string }

// stateIDs maps every state of the block report to its ID.
func stateIDs(blocks vanilla.Blocks) map[stateKey]int {
	ids := map[stateKey]int{}
	for name, block := range blocks.All() {
		for _, s := range block.States {
			ids[stateKey{name, encodeProps(s.Properties)}] = s.ID
		}
	}
	return ids
}

// stateLine is a dumped state's identity: "Block{minecraft:stone}[a=b,c=d]".
var stateName = `STATE Block\{([^}]+)\}(?:\[([^\]]*)\])?`

// keyOf builds the stateKey of a dumped state from its name and the
// unsorted properties text.
func keyOf(name, props string) stateKey {
	return stateKey{name, encodeProps(parseProps(props))}
}

func parseProps(props string) map[string]string {
	out := map[string]string{}
	for _, pair := range splitComma(props) {
		for i := 0; i < len(pair); i++ {
			if pair[i] == '=' {
				out[pair[:i]] = pair[i+1:]
				break
			}
		}
	}
	return out
}

func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	var parts []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	return parts
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		fail("%q is not an integer", s)
	}
	return n
}

func atof(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		fail("%q is not a number", s)
	}
	return f
}

// valueRun is one stretch of consecutive state IDs sharing a value.
type valueRun[V comparable] struct {
	from  int
	value V
}

// runsOf run-length encodes values by ascending state ID: a run starts at its
// first state ID and lasts until the next run's.
func runsOf[V comparable](byID map[int]V) []valueRun[V] {
	ids := make([]int, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	var runs []valueRun[V]
	for _, id := range ids {
		if len(runs) == 0 || runs[len(runs)-1].value != byID[id] {
			runs = append(runs, valueRun[V]{id, byID[id]})
		}
	}
	return runs
}
