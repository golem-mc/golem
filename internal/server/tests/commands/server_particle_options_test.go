package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestParticleOptionErrors checks that /particle refuses missing or out-of-range
// options with vanilla's DFU wording and before parsing coordinates.
func TestParticleOptionErrors(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.SpawningTestConfig(), "Testificate", servertest.OpsListing(t, "Testificate"))

	for _, tc := range []struct {
		name    string
		command string
		want    string
	}{
		{
			"dust missing scale",
			"particle dust{color:[1.0,0.0,0.0]} ~ ~1 ~",
			"No key scale in MapLike[{color:[1.0d,0.0d,0.0d]}]",
		},
		{
			"dust missing all",
			"particle dust ~ ~1 ~",
			"No key scale in MapLike[{}]; No key color in MapLike[{}]",
		},
		{
			"entity_effect missing color",
			"particle entity_effect ~ ~1 ~",
			"No key color in MapLike[{}]",
		},
		{
			"dust scale above max",
			"particle dust{color:[1.0,0.0,0.0],scale:5.0} ~ ~1 ~",
			"Value must be within range [0.01;4.0]: 5.0",
		},
		{
			"dust scale below min",
			"particle dust{color:[1.0,0.0,0.0],scale:0.0} ~ ~1 ~",
			"Value must be within range [0.01;4.0]: 0.0",
		},
		{
			"block missing options before bad coords",
			"particle block stone ~ ~1 ~",
			"No key block_state in MapLike[{}]",
		},
		{
			"dust_color_transition missing all",
			"particle dust_color_transition ~ ~1 ~",
			"No key scale in MapLike[{}]; No key to_color in MapLike[{}]; No key from_color in MapLike[{}]",
		},
		{
			"tinted_leaves missing color",
			"particle tinted_leaves ~ ~1 ~",
			"No key color in MapLike[{}]",
		},
		{
			"flash missing color",
			"particle flash ~ ~1 ~",
			"No key color in MapLike[{}]",
		},
		{
			"item missing item",
			"particle item ~ ~1 ~",
			"No key item in MapLike[{}]",
		},
		{
			"sculk_charge missing roll",
			"particle sculk_charge ~ ~1 ~",
			"No key roll in MapLike[{}]",
		},
		{
			"shriek missing delay",
			"particle shriek ~ ~1 ~",
			"No key delay in MapLike[{}]",
		},
		{
			"trail missing all",
			"particle trail ~ ~1 ~",
			"No key duration in MapLike[{}]; No key color in MapLike[{}]; No key target in MapLike[{}]",
		},
		{
			"trail non-positive duration",
			"particle trail{target:[1,2,3],color:1,duration:0} ~ ~1 ~",
			"Value must be positive: 0",
		},
		{
			"vibration missing all",
			"particle vibration ~ ~1 ~",
			"No key arrival_in_ticks in MapLike[{}]; No key destination in MapLike[{}]",
		},
		{
			"vibration missing type",
			"particle vibration{destination:{},arrival_in_ticks:1} ~ ~1 ~",
			"No key type in MapLike[{}]",
		},
		{
			"vibration missing pos",
			`particle vibration{destination:{type:"block"},arrival_in_ticks:1} ~ ~1 ~`,
			`No key pos in MapLike[{type:"block"}]`,
		},
		{
			"vibration entity source disallowed",
			`particle vibration{destination:{type:"entity"},arrival_in_ticks:1} ~ ~1 ~`,
			"Entity position sources are not allowed",
		},
		{
			"geyser missing water_blocks",
			"particle geyser ~ ~1 ~",
			"No key water_blocks in MapLike[{}]",
		},
		{
			"geyser non-positive water_blocks",
			"particle geyser{water_blocks:0} ~ ~1 ~",
			"Value must be positive: 0",
		},
		{
			"geyser_base missing all",
			"particle geyser_base ~ ~1 ~",
			"No key burst_impulse_base in MapLike[{}]; No key water_blocks in MapLike[{}]",
		},
	} {
		c.Run(t, tc.name, func(t *testing.T) {
			c.ChatCommand(tc.command)
			c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("particle.invalidOptions", tc.want))
			expectNoSystemChat(t, c)
		})
	}
	c.Disconnect()
}
