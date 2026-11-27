package commands

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestNonMobBodiesKeepTeleportAndRotateAngles: /tp, /rotate and
// `rotated as` read and write both angles of every body, whether or not its
// packets carry them.
// vanilla: Entity.teleportTo, Entity.forceSetRotation, Entity.getRotationVector
func TestNonMobBodiesKeepTeleportAndRotateAngles(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Op", servertest.OpsListing(t, "Op"))
	servertest.HoldTicks(t, srv)
	_ = c
	passes := func(line string) bool {
		return strings.Contains(fmt.Sprint(srv.RunLine("execute if entity "+line)), "conditional.pass")
	}
	for _, tc := range []struct{ kind, extra string }{
		{"armor_stand", ""},
		{"oak_boat", ""},
		{"tnt", ",fuse:200"},
		{"falling_block", `,BlockState:{Name:"minecraft:sand"}`},
		{"end_crystal", ""},
		{"experience_orb", ""},
		{"area_effect_cloud", ",Duration:-1"},
		{"item", `,Item:{id:"minecraft:stick",count:1}`},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			srv.RunLine(`summon minecraft:marker 5.5 64 5.5 {Tags:["x"]}`)
			srv.RunLine(`summon minecraft:` + tc.kind + ` 5.5 64 5.5 {Tags:["t"],NoGravity:1b,Rotation:[0f,10f]` + tc.extra + `}`)
			defer srv.RunLine("kill @e[tag=t]")
			defer srv.RunLine("kill @e[tag=x]")
			one := "@e[tag=t,limit=1]"
			srv.RunLine("tp " + one + " 8.5 64 8.5 70 50")
			if !passes(one + " if entity @e[tag=t,nbt={Rotation:[70.0f,50.0f]}]") {
				t.Error("tp did not set the angles")
			}
			srv.RunLine("rotate " + one + " 20 -30")
			if !passes("@e[tag=t,nbt={Rotation:[20.0f,-30.0f]}]") {
				t.Error("rotate did not set the angles")
			}
			srv.RunLine("execute as @e[tag=t] rotated as @s run tp @e[tag=x,limit=1] 5.5 64 5.5 ~ ~")
			if !passes("@e[tag=x,nbt={Rotation:[20.0f,-30.0f]}]") {
				t.Error("rotated as did not read the angles")
			}
		})
	}
}
