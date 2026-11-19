package protocol

import (
	"encoding/json"
	"fmt"
)

// StatusResponse is the JSON payload describing the server in the server list.
type StatusResponse struct {
	Version     StatusVersion     `json:"version"`
	Players     StatusPlayers     `json:"players"`
	Description StatusDescription `json:"description"`
	Favicon     string            `json:"favicon,omitempty"`
	// EnforcesSecureChat is always written; the client reads a missing key as false.
	EnforcesSecureChat bool `json:"enforcesSecureChat"`
	// PreventsChatReports is the No Chat Reports extension, omitted when disabled.
	PreventsChatReports bool `json:"preventsChatReports,omitempty"`
}

type StatusVersion struct {
	Name     string `json:"name"`
	Protocol int    `json:"protocol"`
}

type StatusPlayers struct {
	Max    int `json:"max"`
	Online int `json:"online"`
	// Sample is the players the client lists when the count is hovered.
	Sample []StatusPlayer `json:"sample,omitempty"`
}

// StatusPlayer is one entry of the status sample.
type StatusPlayer struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

type StatusDescription struct {
	Text string `json:"text"`
	// Component overrides Text with an encoded JSON component.
	Component json.RawMessage `json:"-"`
}

// MarshalJSON retains the full component when supplied by the server adapter.
func (s StatusDescription) MarshalJSON() ([]byte, error) {
	if s.Component != nil {
		return s.Component, nil
	}
	return json.Marshal(struct {
		Text string `json:"text"`
	}{s.Text})
}

// Encode returns the payload of a Status Response packet.
func (s StatusResponse) Encode() ([]byte, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("marshal status response: %w", err)
	}
	var b Buffer
	b.PutString(string(data))
	return b.Bytes(), nil
}
