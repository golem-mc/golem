package server

import (
	"fmt"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/world"
)

// gameRuleEntryLimit bounds the game rule packets' prefixed array. The table
// has fifty-nine rules and a client sends only changes, so the bound just
// stops a lying length from allocating.
const gameRuleEntryLimit = 256

// gameRuleEntry is one pair of those packets: the namespaced rule name and
// its value as text.
type gameRuleEntry struct {
	name  string
	value string
}

// readGameRuleEntries reads the game rule packets' prefixed array. A count
// out of bounds or a truncated field is an error, which the caller turns
// into the decode disconnect.
func readGameRuleEntries(r *protocol.Reader) ([]gameRuleEntry, error) {
	count := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return nil, err
	}
	if count < 0 || count > gameRuleEntryLimit {
		return nil, fmt.Errorf("game rule entries %d out of range (max %d)", count, gameRuleEntryLimit)
	}
	entries := make([]gameRuleEntry, 0, count)
	for range count {
		entries = append(entries, gameRuleEntry{name: r.ReadString(32767), value: r.ReadString(32767)})
	}
	if err := r.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

// sendGameRuleValues answers the Edit Game Rules screen's Client Command: a
// gamemaster or above gets every offered rule's value, others a warning.
// The feature-gated rule is absent, matching the /gamerule tree.
// vanilla: ServerGamePacketListenerImpl.handleClientCommand
func (c *client) sendGameRuleValues() {
	if c.PermLevel() < command.PermLevelGamemasters {
		c.log.Warn("player tried to request game rule values without permission",
			"player", c.player.Name)
		return
	}
	var entries []gameRuleEntry
	for _, rule := range world.GameRuleTable {
		if rule.FeatureGated {
			continue
		}
		entries = append(entries, gameRuleEntry{
			name:  "minecraft:" + rule.ID,
			value: command.GameRuleValueText(c, rule.ID, rule.Kind).Text,
		})
	}
	var b protocol.Buffer
	b.PutVarInt(int32(len(entries)))
	for _, e := range entries {
		b.PutString(e.name)
		b.PutString(e.value)
	}
	c.send(protocol.IDPlayGameRuleValues, b.Bytes())
}

// handleSetGameRule applies each Set Game Rule entry as /gamerule does, for
// a gamemaster or above. An unknown rule or unparsable value changes
// nothing. vanilla: ServerGamePacketListenerImpl.handleSetGameRule
func (c *client) handleSetGameRule(r *protocol.Reader) error {
	entries, err := readGameRuleEntries(r)
	if err != nil {
		return c.decodeFailure("set_game_rule", err)
	}
	if c.PermLevel() < command.PermLevelGamemasters {
		c.log.Warn("player tried to set game rules without permission",
			"player", c.player.Name)
		return nil
	}
	for _, e := range entries {
		// The packet uses the namespaced name; the table keys rules by plain name.
		id := strings.TrimPrefix(e.name, "minecraft:")
		rule, ok := world.RuleByID(id)
		if !ok {
			c.log.Warn(fmt.Sprintf("Received request to set unknown game rule: %s", e.name))
			continue
		}
		if !command.SetGamerule(c, id, e.value) {
			continue
		}
		c.srv.broadcastGameRuleChangeToOperators(id, command.GameRuleValueText(c, id, rule.Kind))
	}
	return nil
}

// broadcastGameRuleChangeToOperators tells every online operator, sender
// included, that a rule was set, in place of the admin echo vanilla's
// packet path does not send.
// vanilla: ServerGamePacketListenerImpl.broadcastGameRuleChangeToOperators
func (s *Server) broadcastGameRuleChangeToOperators(id string, value protocol.Text) {
	message := protocol.Text{
		Translate: "commands.gamerule.set",
		With:      []protocol.Text{{Text: id}, value},
	}
	for _, op := range s.playerClients() {
		// Read the op list, not the cached level, which only the list's lock keeps
		// current across /op and /deop.
		if s.ops.Level(op.player.UUID) == 0 {
			continue
		}
		op.Notify(message)
	}
}
