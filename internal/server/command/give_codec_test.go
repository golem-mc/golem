package command

import "testing"

// TestComponentValueTakesWhatTheCodecTakes checks that a value vanilla's
// codec accepts is not reported: unknown keys are skipped and a number of any
// type reads as an int.
// vanilla: ItemParser.readComponent, Codec.INT
func TestComponentValueTakesWhatTheCodecTakes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ key, value string }{
		{"minecraft:potion_contents", `{potion:"minecraft:water",x:1b}`},
		{"minecraft:potion_contents", `{custom_color:5b}`},
		{"minecraft:fireworks", `{flight_duration:1b,bogus:1}`},
		{"minecraft:fireworks", `{explosions:[{shape:"small_ball",bogus:1}]}`},
	} {
		if got := componentValueMalformed(tc.key, tc.value); got != "" {
			t.Errorf("%s=%s refused with %q", tc.key, tc.value, got)
		}
	}
}
