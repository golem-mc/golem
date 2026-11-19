package management

import (
	"encoding/json"
)

// methodTable builds the methods this server answers.
// vanilla: IncomingRpcMethods.bootstrap
func (s *Server) methodTable() map[string]method {
	m := map[string]method{
		"minecraft:rpc.discover": {run: func(*conn, json.RawMessage) (any, error) {
			return json.RawMessage(discoverJSON), nil
		}},
		"minecraft:players": {run: func(*conn, json.RawMessage) (any, error) {
			return s.players(), nil
		}},
		"minecraft:players/kick": {run: s.kick},
		"minecraft:server/status": {run: func(*conn, json.RawMessage) (any, error) {
			return s.state(), nil
		}},
		"minecraft:server/save": {run: s.save},
		"minecraft:server/stop": {run: func(c *conn, _ json.RawMessage) (any, error) {
			c.logf("Halt server. WaitForShutdown: false")
			s.host.RequestStop()
			return true, nil
		}},
		"minecraft:server/system_message": {run: s.systemMessage},
	}
	s.allowlistMethods(m)
	s.operatorMethods(m)
	s.banMethods(m)
	s.gameRuleMethods(m)
	s.settingMethods(m)
	return m
}

// served is the method table with each param name from the schema.
func (s *Server) served() map[string]method {
	m := s.methodTable()
	for name, e := range m {
		e.param = methodParams[name]
		m[name] = e
	}
	return m
}

// save is server/save. vanilla: ServerStateService.save
func (s *Server) save(c *conn, p json.RawMessage) (any, error) {
	var flush bool
	if err := json.Unmarshal(p, &flush); err != nil {
		return nil, errInvalidParams("Not a boolean: " + compactJSON(p))
	}
	c.logf("Save everything. SuppressLogs: true, flush: %t, force: true", flush)
	return s.host.SaveAll(flush), nil
}

// systemMessage is server/system_message.
// vanilla: ServerStateService.systemMessage
func (s *Server) systemMessage(c *conn, p json.RawMessage) (any, error) {
	m, err := decodeSystemMessage(p)
	if err != nil {
		return nil, errInvalidParams(err.Error())
	}
	text, ok := m.Message.text()
	if !ok {
		return false, nil
	}
	overlay := m.Overlay
	if !m.HasReceiving {
		c.logf("Broadcast system message (overlay: %t): '%s'", overlay, text.Plain())
		s.host.TellAll(text, overlay)
		return true, nil
	}
	if len(m.ReceivingPlayers) == 0 {
		return false, nil
	}
	var found []OnlinePlayer
	for _, want := range m.ReceivingPlayers {
		if o, ok := s.find(want); ok {
			found = append(found, o)
		}
	}
	for _, o := range found {
		s.host.Tell(o.Profile.UUID, text, overlay)
	}
	return true, nil
}
