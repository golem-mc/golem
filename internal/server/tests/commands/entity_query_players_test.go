package commands

import (
	"crypto/md5"
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/google/uuid"
)

// TestEntityQueryPlayersByTypeAndUUID checks that a selector's type or UUID
// reaches players only when it can match them, and that results keep join
// order for players, then the marker.
// vanilla: EntitySelector.findEntities
func TestEntityQueryPlayersByTypeAndUUID(t *testing.T) {
	t.Parallel()
	srv, _, _, _ := servertest.StartExecuteWorldForTwo(t, "Alpha", "Bravo")
	servertest.HoldTicks(t, srv)
	srv.RunLine("scoreboard objectives add ord dummy")
	srv.RunLine("scoreboard objectives add n dummy")
	srv.RunLine(`summon minecraft:marker 0.5 64 0.5 {UUID:[I;1,2,3,4]}`)
	names := []struct{ label, who string }{{"A", "@a[name=Alpha,"}, {"B", "@a[name=Bravo,"}, {"M", "@e[type=marker,"}}
	// ord is assigned by running one command per selector result: the counter
	// is bumped and copied in the same execute-as pass via a chained run.
	got := func(sel string) string {
		srv.RunLine("scoreboard players set #n n 0")
		srv.RunLine("scoreboard players reset * ord")
		srv.RunLine("execute as " + sel + " store result score @s ord run scoreboard players add #n n 1")
		var out []string
		for i := 1; i <= 3; i++ {
			for _, n := range names {
				if strings.Contains(fmt.Sprint(srv.RunLine(fmt.Sprintf("execute if entity %sscores={ord=%d}]", n.who, i))), "conditional.pass") {
					out = append(out, n.label)
				}
			}
		}
		return strings.Join(out, "")
	}
	// Java's UUID.nameUUIDFromBytes: MD5 of the name alone, version 3.
	sum := md5.Sum([]byte("OfflinePlayer:Alpha"))
	sum[6] = sum[6]&0x0f | 0x30
	sum[8] = sum[8]&0x3f | 0x80
	alpha := uuid.UUID(sum)
	for _, tc := range []struct{ sel, want string }{
		{"@e[type=player]", "AB"},
		{"@e[type=marker]", "M"},
		{"@e", "ABM"},
		{"@e[type=!player]", "M"},
		{"@e[type=player,limit=1]", "A"},
		{"@e[type=marker,limit=1]", "M"},
		{"@e[x=0,y=64,z=0,dx=1,dy=1,dz=1]", "AB"},
		{"@e[type=player,name=Bravo]", "B"},
		{"00000001-0000-0002-0000-000300000004", "M"},
		{alpha.String(), "A"},
		{"00000000-0000-0000-0000-00000000dead", ""},
	} {
		if g := got(tc.sel); g != tc.want {
			t.Errorf("%s yielded %q, want %q", tc.sel, g, tc.want)
		}
	}
}
