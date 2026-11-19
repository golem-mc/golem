package sounds

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func TestAnOmenEffectPlaysItsSoundOnlyWhenNewlyAdded(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())

	watcher := connect()
	watcher.JoinAs("Watcher")
	actor := connect()
	actor.JoinAs("Actor")
	watcher.ExpectPlayerAdded("Actor")

	for i, name := range []string{"bad_omen", "trial_omen", "raid_omen"} {
		want, ok := registry.SoundEventID("minecraft:event.mob_effect." + name)
		if !ok {
			t.Fatalf("no sound event for %s", name)
		}
		srv.GiveEffect("Actor", "minecraft:"+name, 1200, 0, false)
		// The refresh is silent; the sentinel's update ends the read.
		srv.GiveEffect("Actor", "minecraft:"+name, 1200, 0, false)
		srv.GiveEffect("Actor", "minecraft:"+name, 1200, 1, false)
		srv.GiveEffect("Actor", "minecraft:speed", 1200, int32(i), false)

		sounds := 0
		for id, data := range actor.Packets(servertest.ExpectTimeout) {
			if id == protocol.IDPlaySoundEffect {
				sounds++
				if got := protocol.NewReader(data).ReadVarInt() - 1; got != want {
					t.Errorf("%s sound = %d, want %d", name, got, want)
				}
			}
			if id == protocol.IDPlayUpdateMobEffect {
				r := protocol.NewReader(data)
				r.ReadVarInt()
				if r.ReadVarInt() == registry.MustMobEffect("minecraft:speed") {
					break
				}
			}
		}
		if sounds != 1 {
			t.Errorf("%s: the player heard %d sounds, want 1", name, sounds)
		}
		if got := protocol.NewReader(watcher.AwaitPlay(protocol.IDPlaySoundEffect)).ReadVarInt() - 1; got != want {
			t.Errorf("%s: the observer heard sound %d, want %d", name, got, want)
		}
	}
}
