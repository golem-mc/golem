package commands

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSortedLimitedSelectorKeepsNearestAndFurthest: sort=nearest|furthest with
// a limit picks the closest or furthest bodies that pass the filters, ties go
// to the lower entity ID.
// vanilla: EntitySelector.sortAndLimit
func TestSortedLimitedSelectorKeepsNearestAndFurthest(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Op", servertest.OpsListing(t, "Op"))
	servertest.HoldTicks(t, srv)
	// name, x, passes the nbt filter
	for _, m := range []struct {
		name string
		x    int
		ok   bool
	}{
		{"a", 6, true}, {"b", 2, false}, {"c", 9, true}, {"d", 4, true},
		{"e", 9, true}, {"f", 1, false}, {"g", 4, true}, {"h", 20, false},
	} {
		srv.RunLine(fmt.Sprintf(`summon minecraft:marker %d.5 64 0.5 {Tags:["m","%s"],data:{ok:%db}}`, m.x, m.name, b2i(m.ok)))
	}
	pick := func(sel string) string {
		srv.RunLine("tag @e[tag=hit] remove hit")
		srv.RunLine("execute positioned 0.5 64 0.5 run tag " + sel + " add hit")
		var got []string
		for _, n := range "abcdefgh" {
			if strings.Contains(fmt.Sprint(srv.RunLine("execute if entity @e[tag=hit,tag="+string(n)+"]")), "conditional.pass") {
				got = append(got, string(n))
			}
		}
		slices.Sort(got)
		return strings.Join(got, "")
	}
	const f = "@e[tag=m,nbt={data:{ok:1b}},"
	for _, tc := range []struct{ sel, want string }{
		{f + "sort=nearest,limit=1]", "d"},
		{f + "sort=nearest,limit=2]", "dg"},
		{f + "sort=nearest,limit=3]", "adg"},
		{f + "sort=furthest,limit=1]", "c"},
		{f + "sort=furthest,limit=2]", "ce"},
		{f + "sort=furthest,limit=3]", "ace"},
		{"@e[tag=m,sort=nearest,limit=2]", "bf"},
		{"@e[tag=m,sort=furthest,limit=1]", "h"},
	} {
		if got := pick(tc.sel); got != tc.want {
			t.Errorf("%s picked %q, want %q", tc.sel, got, tc.want)
		}
	}
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestSortedLimitedSelectorVisitsInSortedOrder: the picks come back nearest or
// furthest first, not in entity ID order.
// vanilla: EntitySelector.sortAndLimit
func TestSortedLimitedSelectorVisitsInSortedOrder(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Op", servertest.OpsListing(t, "Op"))
	servertest.HoldTicks(t, srv)
	srv.RunLine("scoreboard objectives add visit dummy")
	// Spawn order is the reverse of the nearest-first order.
	for i, x := range []int{9, 7, 5, 3} {
		srv.RunLine(fmt.Sprintf(`summon minecraft:marker %d.5 64 0.5 {Tags:["m","v%d"]}`, x, i))
	}
	for _, tc := range []struct {
		sel  string
		want []string // tags in visiting order
	}{
		{"@e[tag=m,sort=nearest,limit=3]", []string{"v3", "v2", "v1"}},
		{"@e[tag=m,sort=furthest,limit=3]", []string{"v0", "v1", "v2"}},
	} {
		srv.RunLine("scoreboard players reset * visit")
		srv.RunLine("scoreboard players set #n visit 0")
		// The counter's new value is stored on each body as it is visited.
		srv.RunLine("execute positioned 0.5 64 0.5 as " + tc.sel + " store result score @s visit run scoreboard players add #n visit 1")
		for i, tag := range tc.want {
			line := fmt.Sprintf("execute if entity @e[tag=%s,scores={visit=%d}]", tag, i+1)
			if !strings.Contains(fmt.Sprint(srv.RunLine(line)), "conditional.pass") {
				t.Errorf("%s: %s was not visited number %d", tc.sel, tag, i+1)
			}
		}
	}
}
