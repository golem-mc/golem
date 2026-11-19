package management

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
)

// playerIn is a player a request names: either field may be missing.
// vanilla: PlayerDto
type playerIn struct {
	ID   *uuid.UUID `json:"id,omitempty"`
	Name *string    `json:"name,omitempty"`
}

// playerOut is a player a response or notification carries.
type playerOut struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// messageIn is a request's message. vanilla: Message
type messageIn struct {
	Literal            *string  `json:"literal"`
	Translatable       *string  `json:"translatable"`
	TranslatableParams []string `json:"translatableParams"`
}

// text returns the message as a component, false when it has none.
// vanilla: Message.asComponent
func (m messageIn) text() (protocol.Text, bool) {
	switch {
	case m.Translatable != nil:
		with := make([]protocol.Text, len(m.TranslatableParams))
		for i, p := range m.TranslatableParams {
			with[i] = protocol.Text{Text: p}
		}
		return protocol.Text{Translate: *m.Translatable, With: with}, true
	case m.Literal != nil:
		return protocol.Text{Text: *m.Literal}, true
	}
	return protocol.Text{}, false
}

// systemMessage is server/system_message's param.
// vanilla: ServerStateService.SystemMessage
type systemMessage struct {
	Message          messageIn
	Overlay          bool
	ReceivingPlayers []playerIn
	HasReceiving     bool
}

// decodeSystemMessage decodes the param like SystemMessage.CODEC: message and
// overlay are required, a malformed receivingPlayers counts as absent.
// The error is the codec's text, for the Invalid params data; RecordCodecBuilder
// joins field errors in reverse field order, so overlay is decoded first.
func decodeSystemMessage(p json.RawMessage) (systemMessage, error) {
	var m systemMessage
	obj, err := jsonObject(p)
	if err != nil {
		return m, err
	}
	var errs []string
	missing := func(k string) {
		errs = append(errs, fmt.Sprintf("No key %s in MapLike[%s]", k, compactJSON(p)))
	}
	if raw, ok := field(obj, "overlay"); !ok {
		missing("overlay")
	} else if err := json.Unmarshal(raw, &m.Overlay); err != nil {
		errs = append(errs, "Not a boolean: "+compactJSON(raw))
	}
	if raw, ok := field(obj, "message"); !ok {
		missing("message")
	} else if msg, err := decodeMessage(raw); err != nil {
		errs = append(errs, err.Error())
	} else {
		m.Message = msg
	}
	if raw, ok := field(obj, "receivingPlayers"); ok {
		if list, err := decodeList(raw, decodePlayer); err == nil {
			m.ReceivingPlayers, m.HasReceiving = list, true
		}
	}
	if len(errs) > 0 {
		return m, errors.New(strings.Join(errs, "; "))
	}
	return m, nil
}

// decodeMessage decodes Message.CODEC: translatableParams is lenient.
func decodeMessage(raw json.RawMessage) (messageIn, error) {
	var m messageIn
	obj, err := jsonObject(raw)
	if err != nil {
		return m, err
	}
	// Reverse field order, as RecordCodecBuilder joins the errors.
	var errs []string
	optString := func(k string) *string {
		v, ok := field(obj, k)
		if !ok {
			return nil
		}
		var str string
		if json.Unmarshal(v, &str) != nil {
			errs = append(errs, "Not a string: "+compactJSON(v))
			return nil
		}
		return &str
	}
	m.Translatable = optString("translatable")
	m.Literal = optString("literal")
	if v, ok := field(obj, "translatableParams"); ok {
		if params, err := decodeList(v, decodeString); err == nil {
			m.TranslatableParams = params
		}
	}
	if len(errs) > 0 {
		return m, codecError(strings.Join(errs, "; "))
	}
	return m, nil
}

// decodeString decodes a JSON string; null is not one, though encoding/json
// would leave the string empty. vanilla: JsonOps.getStringValue
func decodeString(raw json.RawMessage) (string, error) {
	var str string
	if string(raw) == "null" || json.Unmarshal(raw, &str) != nil {
		return "", codecError("Not a string: " + compactJSON(raw))
	}
	return str, nil
}

// jsonObject decodes raw as an object. vanilla: JsonOps.getMap
func jsonObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return nil, codecError("Not a JSON object: " + compactJSON(raw))
	}
	return obj, nil
}

// field returns obj's value for k; a JSON null counts as missing.
// vanilla: JsonOps MapLike.get
func field(obj map[string]json.RawMessage, k string) (json.RawMessage, bool) {
	v, ok := obj[k]
	if !ok || string(v) == "null" {
		return nil, false
	}
	return v, true
}

// codecError is a DFU decode failure's text, sent as the Invalid params data.
type codecError string

func (e codecError) Error() string { return string(e) }

func compactJSON(raw json.RawMessage) string {
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return string(raw)
	}
	return b.String()
}

type versionOut struct {
	Name     string `json:"name"`
	Protocol int    `json:"protocol"`
}

// serverState is what server/status and its notification carry.
// vanilla: ServerStateService.ServerState
type serverState struct {
	Started bool        `json:"started"`
	Players []playerOut `json:"players,omitempty"`
	Version versionOut  `json:"version"`
}

func (s *Server) state() serverState {
	return serverState{Started: true, Players: s.players(),
		Version: versionOut{protocol.GameVersion, protocol.Version}}
}

// players lists the online players. vanilla: PlayerService.get
func (s *Server) players() []playerOut {
	online := s.host.OnlinePlayers()
	out := make([]playerOut, len(online))
	for i, p := range online {
		out[i] = playerOut{p.Profile.UUID, p.Profile.Name}
	}
	return out
}

// find returns the online player p names: by id when it has one, else by
// name ignoring case. vanilla: PlayerList.getPlayer, getPlayerByName
func (s *Server) find(p playerIn) (OnlinePlayer, bool) {
	for _, o := range s.host.OnlinePlayers() {
		switch {
		case p.ID != nil:
			if o.Profile.UUID == *p.ID {
				return o, true
			}
		case p.Name != nil:
			if strings.EqualFold(o.Profile.Name, *p.Name) {
				return o, true
			}
		}
	}
	return OnlinePlayer{}, false
}
