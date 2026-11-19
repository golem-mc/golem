package commands

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: StorageDataAccessor.getData, DataCommands.manipulateData,
// ExecuteCommand.storeData, FunctionCommand.getArgumentTag
func TestStoragePathCommandsIgnoreUnrelatedData(t *testing.T) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	t.Cleanup(func() { slog.SetDefault(logger) })
	srv, _ := startFunctionServerWith(t, t.TempDir(), map[string]string{
		"t/function/m.mcfunction": "$return $(v)\n",
	}, func(cfg *config.Config) {
		cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	})
	servertest.HoldTicks(t, srv)
	srv.RunRconLine(`data merge storage t:s {v:1,args:{v:1},text:"abc",src:{v:1},list:[1]}`)
	cases := [][]string{
		{`data get storage t:s v`},
		{`data get storage t:s v 2`},
		{`execute if data storage t:s v`},
		{`execute store result storage t:s v int 1 run data get storage t:s args.v`},
		{`data modify storage t:s w set value 1`},
		{`data modify storage t:s w set from storage t:s src.v`},
		{`data modify storage t:other w set from storage t:s src.v`},
		{`data modify storage t:s w set string storage t:s text 1`},
		{`data modify storage t:s dst merge from storage t:s src`},
		{`data modify storage t:s dst set value 1`, `data remove storage t:s dst`},
		{`data modify storage t:s dst set from storage t:s list`, `data modify storage t:s dst append from storage t:s list[]`},
		{`data modify storage t:s dst set from storage t:s list`, `data modify storage t:s dst prepend from storage t:s list[]`},
		{`data modify storage t:s dst set from storage t:s list`, `data modify storage t:s dst insert 0 from storage t:s list[]`},
		{`function t:m with storage t:s args`},
	}
	measure := func(lines []string) float64 {
		return testing.AllocsPerRun(5, func() {
			for _, line := range lines {
				srv.RunRconLine(line)
			}
		})
	}
	small := make([]float64, len(cases))
	for i, lines := range cases {
		small[i] = measure(lines)
	}
	var big strings.Builder
	big.WriteString(`data modify storage t:s big set value [`)
	for i := range 1000 {
		if i > 0 {
			big.WriteByte(',')
		}
		fmt.Fprintf(&big, `{id:"entry_%d",score:%d,pos:[1.0d,2.0d,3.0d],tags:["a","b","c"]}`, i, i)
	}
	big.WriteByte(']')
	if result := srv.RunRconLine(big.String()); len(result) != 1 || result[0].Translate != "commands.data.storage.modified" {
		t.Fatalf("populate storage: %+v", result)
	}
	for i, lines := range cases {
		t.Run(lines[len(lines)-1], func(t *testing.T) {
			large := measure(lines)
			// A path operation must not allocate copies of unrelated tags.
			if large > small[i]+100 {
				t.Errorf("allocations with 1000 unrelated entries = %.0f, without = %.0f", large, small[i])
			}
		})
	}
}
