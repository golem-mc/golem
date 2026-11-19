package combat

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAnArrowsDataWriteIsToldOnce merges a critical flag and a piercing
// level onto a held-up arrow: its watcher is told the turned fields in one
// metadata packet, and the ticks after the write tell them no second time.
// vanilla: EntityDataAccessor.setData, SynchedEntityData.packDirty,
// ServerEntity.sendDirtyEntityData
func TestAnArrowsDataWriteIsToldOnce(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BowTestConfig(), servertest.OpsListing(t, "Fletcher"))
	c := connect().Join("Fletcher")
	c.ChatCommand("gamemode creative")
	c.Settle()
	servertest.HoldTicks(t, srv)
	r := servertest.NewMetadataRecorder(t, srv, c)

	packets := r.Run(`summon minecraft:arrow 0.5 70 6.5 {NoGravity:1b,Tags:["held"]}`)
	id := servertest.SpawnedID(packets, protocol.EntityTypeArrow)
	if id == 0 {
		t.Fatal("summoning the arrow sent no spawn")
	}
	r.Read(5)

	c.ChatCommand(`data merge entity @e[tag=held,limit=1] {crit:1b,PierceLevel:3b}`)
	r.Record("write", id, r.Read(10))
	got := r.Got["write"]
	if len(got) != 1 {
		t.Fatalf("the write was told in %d metadata packets (%s), want one", len(got), strings.Join(got, "|"))
	}
	// The arrow flags byte with the critical bit, then the piercing level.
	if want := "080001090003ff"; got[0] != want {
		t.Errorf("the write was told as %s, want %s", got[0], want)
	}
}
