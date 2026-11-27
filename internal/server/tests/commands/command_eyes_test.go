package commands

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// readDouble runs a `data get entity` query and parses the number it prints.
func readDouble(t *testing.T, c *servertest.Client, cmd string) float64 {
	t.Helper()
	feedback, ok := protocol.ReadText(protocol.NewReader(runChat(t, c, cmd)))
	if !ok || len(feedback.With) != 2 {
		t.Fatalf("%s: feedback %+v", cmd, feedback)
	}
	text := strings.TrimRight(feedback.With[1].Plain(), "df")
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		t.Fatalf("%s: %q is no number", cmd, text)
	}
	return v
}

// TestExecuteAnchoredEyesUsesTheEntitysEyeHeight checks that a non-player
// entity's eyes sit at its type's eye height, for the anchor and for facing.
// vanilla: Entity.getEyePosition, EntityType.Builder eyeHeight
func TestExecuteAnchoredEyesUsesTheEntitysEyeHeight(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")
	for i, tc := range []struct {
		summon string
		eye    float64
	}{
		{"pig", 0.765},
		{"cow", 1.3},
		{"wolf", 0.34375},
		{"armor_stand", 1.7775},
		{"item", 0.2125},
	} {
		data := "Rotation:[0f,0f],Tags:[\"t" + fmt.Sprint(i) + "\"]"
		switch tc.summon {
		case "wolf":
			data += ",Age:-24000"
		case "item":
			data += `,Item:{id:"minecraft:stone",count:1}`
		}
		// Each entity stands at y=64 on its own x so they stay apart.
		runChat(t, c, fmt.Sprintf("summon minecraft:%s %d.5 64 0.5 {%s}", tc.summon, 10*i, data))
		runChat(t, c, fmt.Sprintf(`execute as @e[tag=t%d,limit=1] at @s anchored eyes positioned ^ ^ ^ run summon marker ~ ~ ~ {Tags:["x%d"]}`, i, i))
		got := readDouble(t, c, fmt.Sprintf("data get entity @e[tag=x%d,limit=1] Pos[1]", i)) - 64
		if math.Abs(got-tc.eye) > 1e-6 {
			t.Errorf("%s: eyes at %v above the feet, want %v", tc.summon, got, tc.eye)
		}
	}

	// A pig at (0.5, 64, 0.5) seen from (3.5, 69, 4.5): the aim is at
	// the eyes, not the feet.
	runChat(t, c, `summon marker 0 0 0 {Tags:["m"]}`)
	runChat(t, c, `execute positioned 3.5 69 4.5 facing entity @e[tag=t0,limit=1] eyes run tp @e[tag=m,limit=1] ~ ~ ~ ~ ~`)
	if got := readDouble(t, c, "data get entity @e[tag=m,limit=1] Rotation[1]"); math.Abs(got-40.26437) > 1e-3 {
		t.Errorf("facing the pig's eyes pitched %v, want 40.26437", got)
	}
	c.Disconnect()
}
