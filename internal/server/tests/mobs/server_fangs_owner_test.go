package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSummonedFangsKeepAPlayerOwner: fangs summoned with a live player's
// Owner spare that player and their teammates and hurt anyone else.
// vanilla: EvokerFangs.dealDamageTo
func TestSummonedFangsKeepAPlayerOwner(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Owner"))
	srv.SetSpawning(false)
	owner := connect().Join("Owner")
	connect().Join("Mate")
	connect().Join("Foe")
	servertest.HoldTicks(t, srv)
	srv.RunLine("team add t")
	srv.RunLine("team join t Owner")
	srv.RunLine("team join t Mate")
	srv.RunLine("tp Owner 0 80 0")
	srv.RunLine("tp Mate 8 80 0")
	srv.RunLine("tp Foe 16 80 0")
	servertest.RunTicks(t, srv, 40)

	id := protocol.OfflineUUID("Owner")
	ints := fmt.Sprintf("[I;%d,%d,%d,%d]",
		int32(uint32(id[0])<<24|uint32(id[1])<<16|uint32(id[2])<<8|uint32(id[3])),
		int32(uint32(id[4])<<24|uint32(id[5])<<16|uint32(id[6])<<8|uint32(id[7])),
		int32(uint32(id[8])<<24|uint32(id[9])<<16|uint32(id[10])<<8|uint32(id[11])),
		int32(uint32(id[12])<<24|uint32(id[13])<<16|uint32(id[14])<<8|uint32(id[15])))
	for _, name := range []string{"Owner", "Mate", "Foe"} {
		p := srv.PlayerPosition(name)
		before := srv.PlayerHealth(name)
		owner.ChatCommand(fmt.Sprintf("summon minecraft:evoker_fangs %f %f %f {Warmup:0,Owner:%s}", p.X, p.Y, p.Z, ints))
		servertest.RunTicks(t, srv, 30)
		hurt := srv.PlayerHealth(name) < before
		if want := name == "Foe"; hurt != want {
			t.Errorf("%s hurt = %v, want %v (health %v -> %v)", name, hurt, want, before, srv.PlayerHealth(name))
		}
	}
}
