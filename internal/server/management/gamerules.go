package management

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// GameRule is one game rule as the management API shows it.
type GameRule struct {
	ID       string // without the minecraft: namespace
	Integer  bool
	Bool     bool
	Int      int32
	Min, Max int32 // an integer rule's bounds
}

// typedRuleOut is GameRuleUpdate.TYPED_CODEC's form.
type typedRuleOut struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

func typedRule(r GameRule) typedRuleOut {
	if r.Integer {
		return typedRuleOut{"minecraft:" + r.ID, "integer", r.Int}
	}
	return typedRuleOut{"minecraft:" + r.ID, "boolean", r.Bool}
}

// value is the rule's current value as the log line prints it.
func (r GameRule) value() string {
	if r.Integer {
		return strconv.Itoa(int(r.Int))
	}
	return strconv.FormatBool(r.Bool)
}

// gameRuleMethods are the game rule service's methods.
// vanilla: IncomingRpcMethods.bootstrap, GameRulesService
func (s *Server) gameRuleMethods(m map[string]method) {
	m["minecraft:gamerules"] = method{run: func(*conn, json.RawMessage) (any, error) {
		rules := s.host.GameRules()
		out := make([]typedRuleOut, len(rules))
		for i, r := range rules {
			out[i] = typedRule(r)
		}
		return out, nil
	}}
	m["minecraft:gamerules/update"] = method{run: s.gameRulesUpdate}
}

// gameRulesUpdate is gamerules/update.
// vanilla: MinecraftGameRuleServiceImpl.updateGameRule
func (s *Server) gameRulesUpdate(c *conn, p json.RawMessage) (any, error) {
	r, err := s.decodeGameRule(p)
	if err != nil {
		return nil, errInvalidParams(err.Error())
	}
	c.logf("Game rule '%s' updated from '%s' to '%s'", r.ID, s.gameRule(r.ID).value(), r.value())
	s.host.SetGameRule(r)
	return typedRule(r), nil
}

// gameRule is the offered rule id; zero when there is none.
func (s *Server) gameRule(id string) GameRule {
	for _, r := range s.host.GameRules() {
		if r.ID == id {
			return r
		}
	}
	return GameRule{}
}

// decodeGameRule decodes GameRuleUpdate.CODEC into the rule with its new
// value; a type field is ignored.
func (s *Server) decodeGameRule(raw json.RawMessage) (GameRule, error) {
	obj, err := jsonObject(raw)
	if err != nil {
		return GameRule{}, err
	}
	k, ok := field(obj, "key")
	if !ok {
		return GameRule{}, codecError(fmt.Sprintf("No key key in MapLike[%s]", compactJSON(raw)))
	}
	str, err := decodeString(k)
	if err != nil {
		return GameRule{}, err
	}
	key, ok := qualify(str)
	if !ok {
		// vanilla: Identifier.read
		return GameRule{}, codecError("Not a valid resource location: " + str)
	}
	var r GameRule
	found := false
	for _, g := range s.host.GameRules() {
		if "minecraft:"+g.ID == key {
			r, found = g, true
			break
		}
	}
	if !found {
		return GameRule{}, codecError("Unknown registry key in ResourceKey[minecraft:root / minecraft:game_rule]: " + key)
	}
	v, ok := field(obj, "value")
	if !ok {
		return GameRule{}, codecError(fmt.Sprintf("No key value in MapLike[%s]", compactJSON(raw)))
	}
	if !r.Integer {
		if json.Unmarshal(v, &r.Bool) != nil {
			return GameRule{}, codecError("Not a boolean: " + compactJSON(v))
		}
		return r, nil
	}
	var f float64
	if json.Unmarshal(v, &f) != nil {
		return GameRule{}, codecError("Not a number: " + compactJSON(v))
	}
	// Number.intValue: an integer wraps to 32 bits, a fraction truncates.
	n := int64(f)
	if i, err := strconv.ParseInt(string(v), 10, 64); err == nil {
		n = i
	}
	r.Int = int32(n)
	if r.Int < r.Min || r.Int > r.Max {
		return GameRule{}, codecError(fmt.Sprintf("Value %d outside of range [%d:%d]", r.Int, r.Min, r.Max))
	}
	return r, nil
}

// GameRuleChanged tells the clients a game rule was set.
// vanilla: JsonRpcNotificationService.onGameRuleChanged
func (s *Server) GameRuleChanged(r GameRule) {
	if s != nil {
		s.notify("gamerules/updated", typedRule(r))
	}
}
