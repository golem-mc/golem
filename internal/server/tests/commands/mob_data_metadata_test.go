package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: Entity.setNoGravity, SynchedEntityData.packDirty
func TestCommandDataMobNoGravityIsSentOnce(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = 1, 1
	srv, c := servertest.Join(t, cfg, "Watcher", servertest.OpsListing(t, "Watcher"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:pig ~2 ~2 ~ {NoAI:1b,NoGravity:1b}`)
	spawn := c.AwaitSpawn(protocol.EntityTypePig)
	if e := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexNoGravity); e == nil || !e.FlagVal {
		t.Fatalf("spawn NoGravity = %+v, want true", e)
	}

	for _, step := range []struct {
		name, tag string
		want      bool
		count     int
	}{
		{"clear", "0b", false, 1},
		{"already clear", "0b", false, 0},
		{"set", "1b", true, 1},
		{"already set", "1b", true, 0},
	} {
		t.Run(step.name, func(t *testing.T) {
			c.ChatCommand(`data merge entity @e[type=minecraft:pig,limit=1] {NoGravity:` + step.tag + `}`)
			count := 0
			read := func(id int32, data []byte) bool {
				if id != protocol.IDPlayEntityMetadata {
					return false
				}
				r := protocol.NewReader(data)
				if r.ReadVarInt() != spawn.EntityID {
					return false
				}
				entries, ok := servertest.ReadMetadata(r)
				if !ok || r.Err() != nil {
					t.Fatalf("invalid metadata: %x", data)
				}
				if e := servertest.MetaOf(entries, protocol.MetadataIndexNoGravity); e != nil {
					count++
					if e.Typ != protocol.MetadataBoolean || e.FlagVal != step.want {
						t.Errorf("NoGravity = %+v, want boolean %t", e, step.want)
					}
				}
				return false
			}
			c.ReadTicks(srv, 0, read)
			if count != step.count {
				t.Errorf("command sent %d NoGravity updates, want %d", count, step.count)
			}
			count = 0
			c.ReadTicks(srv, 1, read)
			if count != 0 {
				t.Errorf("following tick repeated %d NoGravity updates", count)
			}
		})
	}
}
