package management

import (
	"encoding/json"
	"fmt"
	"slices"
)

// operatorIn is a request's operator: level and bypass may be missing.
// vanilla: OperatorService.OperatorDto
type operatorIn struct {
	Player playerIn
	Level  *int
	Bypass *bool
}

// operatorOut is an operator a response or notification carries, always
// with all three fields.
type operatorOut struct {
	Player playerOut `json:"player"`
	Level  int       `json:"permissionLevel"`
	Bypass bool      `json:"bypassesPlayerLimit"`
}

func operatorOutOf(op Operator) operatorOut {
	return operatorOut{playerOut{op.Profile.UUID, op.Profile.Name}, op.Level, op.BypassesPlayerLimit}
}

// decodeOperator decodes OperatorDto.CODEC. The level is clamped to 0-4.
// vanilla: PermissionLevel.INT_CODEC (ByIdMap, CLAMP)
func decodeOperator(raw json.RawMessage) (operatorIn, error) {
	var op operatorIn
	obj, err := jsonObject(raw)
	if err != nil {
		return op, err
	}
	if v, ok := field(obj, "bypassesPlayerLimit"); ok {
		var b bool
		if json.Unmarshal(v, &b) != nil {
			return op, codecError("Not a boolean: " + compactJSON(v))
		}
		op.Bypass = &b
	}
	if v, ok := field(obj, "permissionLevel"); ok {
		var f float64
		if json.Unmarshal(v, &f) != nil {
			return op, codecError("Not a number: " + compactJSON(v))
		}
		level := min(max(int(f), 0), 4)
		op.Level = &level
	}
	v, ok := field(obj, "player")
	if !ok {
		return op, codecError(fmt.Sprintf("No key player in MapLike[%s]", compactJSON(raw)))
	}
	op.Player, err = decodePlayer(v)
	return op, err
}

// operatorMethods are the operator service's methods.
// vanilla: IncomingRpcMethods.registerOperatorService, OperatorService
func (s *Server) operatorMethods(m map[string]method) {
	m["minecraft:operators"] = method{run: func(*conn, json.RawMessage) (any, error) {
		return s.operators(), nil
	}}
	m["minecraft:operators/add"] = method{run: s.operatorsAdd}
	m["minecraft:operators/remove"] = method{run: s.operatorsRemove}
	m["minecraft:operators/set"] = method{run: s.operatorsSet}
	m["minecraft:operators/clear"] = method{run: func(c *conn, _ json.RawMessage) (any, error) {
		c.logf("Clear operator list")
		s.host.OpsClear()
		return s.operators(), nil
	}}
}

// operators is the operator list in file order.
func (s *Server) operators() []operatorOut {
	ops := s.host.Operators()
	out := make([]operatorOut, len(ops))
	for i, op := range ops {
		out[i] = operatorOutOf(op)
	}
	return out
}

// resolveOperators decodes an [operator] param and resolves each player,
// filling a missing level with op-permission-level and a missing bypass flag
// with false, or with the player's current one when keepBypass is set (add
// keeps it; set deops first, so it is lost). vanilla: OperatorService,
// PlayerList.op
func (s *Server) resolveOperators(raw json.RawMessage, keepBypass bool) ([]Operator, error) {
	in, err := decodeList(raw, decodeOperator)
	if err != nil {
		return nil, errInvalidParams(err.Error())
	}
	current := s.host.Operators()
	out := make([]Operator, 0, len(in))
	for _, o := range in {
		profile, ok := s.lookup(o.Player)
		if !ok {
			continue
		}
		op := Operator{Profile: profile, Level: s.host.DefaultOpLevel()}
		if o.Level != nil {
			op.Level = *o.Level
		}
		if o.Bypass != nil {
			op.BypassesPlayerLimit = *o.Bypass
		} else if i := slices.IndexFunc(current, func(c Operator) bool { return c.Profile.UUID == profile.UUID }); keepBypass && i >= 0 {
			op.BypassesPlayerLimit = current[i].BypassesPlayerLimit
		}
		out = append(out, op)
	}
	return out, nil
}

func (s *Server) operatorsAdd(c *conn, p json.RawMessage) (any, error) {
	ops, err := s.resolveOperators(p, true)
	if err != nil {
		return nil, err
	}
	for _, op := range ops {
		c.logf("Op '%s'", op.Profile.Name)
		s.host.OpPut(op)
	}
	return s.operators(), nil
}

func (s *Server) operatorsRemove(c *conn, p json.RawMessage) (any, error) {
	players, err := s.resolvePlayers(p)
	if err != nil {
		return nil, err
	}
	for _, pl := range players {
		c.logf("Deop '%s'", pl.Name)
		s.host.Deop(pl)
	}
	return s.operators(), nil
}

// operatorsSet deops the entries the final list lacks, then ops the new
// ones; entries compare by id, name, level and bypass flag.
func (s *Server) operatorsSet(c *conn, p json.RawMessage) (any, error) {
	final, err := s.resolveOperators(p, false)
	if err != nil {
		return nil, err
	}
	current := s.host.Operators()
	for _, op := range current {
		if !containsOperator(final, op) {
			c.logf("Deop '%s'", op.Profile.Name)
			s.host.Deop(op.Profile)
		}
	}
	for i, op := range final {
		if !containsOperator(current, op) && !containsOperator(final[:i], op) {
			c.logf("Op '%s'", op.Profile.Name)
			s.host.OpPut(op)
		}
	}
	return s.operators(), nil
}

func containsOperator(list []Operator, op Operator) bool {
	return slices.ContainsFunc(list, func(o Operator) bool {
		return o.Profile.UUID == op.Profile.UUID && o.Profile.Name == op.Profile.Name &&
			o.Level == op.Level && o.BypassesPlayerLimit == op.BypassesPlayerLimit
	})
}
