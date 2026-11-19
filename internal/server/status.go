package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"syscall"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

// status answers a server list query: the server description, then the
// latency ping.
func (c *client) status() error {
	requested := false
	for {
		packetID, data, err := c.conn.ReadPacket()
		if err != nil {
			// Clients that only refresh the server list hang up after the response.
			if isDisconnect(err) {
				return nil
			}
			return fmt.Errorf("read status packet: %w", err)
		}
		switch packetID {
		case protocol.IDStatusRequest:
			// vanilla: ServerStatusPacketListenerImpl.handleStatusRequest
			if requested {
				return nil
			}
			requested = true
			if err := c.writeStatusResponse(); err != nil {
				return err
			}
		case protocol.IDStatusPing:
			// vanilla: ServerStatusPacketListenerImpl.handlePingRequest answers without
			// a prior Status Request, then disconnects. The body is exactly one long;
			// a short or overlong one fails decoding and closes unanswered.
			r := protocol.NewReader(data)
			payload := r.ReadI64()
			if r.Err() != nil || r.Remaining() != 0 {
				return fmt.Errorf("ping request of %d bytes, want 8", len(data))
			}
			var b protocol.Buffer
			b.PutI64(payload)
			return c.writePacket(protocol.IDStatusPong, "pong response", b.Bytes())
		default:
			return fmt.Errorf("unexpected status packet 0x%02X", packetID)
		}
	}
}

// legacyPing answers a pre-1.7 server list ping, leaving the connection to
// be closed. Anything starting 0xFE that is no legacy ping is dropped.
func (c *client) legacyPing() error {
	buf, err := c.conn.PeekBuffered()
	if err != nil {
		return fmt.Errorf("peek legacy ping: %w", err)
	}
	kind, version := protocol.ParseLegacyPing(buf)
	if kind == protocol.LegacyPingNone {
		c.log.Debug("legacy ping malformed")
		return nil
	}
	c.log.Debug("legacy ping", "kind", kind)
	event, err := c.serverListPing(version)
	if err != nil {
		return err
	}
	motd, online, maxPlayers := c.srv.motd.Load().Plain(), c.srv.PlayerCount(), int(c.srv.maxPlayers.Load())
	if event != nil {
		motd, online, maxPlayers = pluginProtocolText(event.MOTD).Plain(), event.Online, event.Max
	}
	return c.conn.WriteRaw(protocol.LegacyPingReply(kind, motd, online, maxPlayers))
}

// serverListPing shares field edits and cancellation with the legacy fallback.
// A nil event means no listener exists; callers retain the default response path.
func (c *client) serverListPing(version int) (*plugin.ServerListPingEvent, error) {
	if c.srv.plugins == nil || !c.srv.plugins.Has(pluginruntime.ServerListPing) {
		return nil, nil
	}
	text, _ := pluginText(*c.srv.motd.Load())
	event := &plugin.ServerListPingEvent{Address: c.conn.RemoteAddr(), Protocol: version, MOTD: text, Online: c.srv.PlayerCount(), Max: int(c.srv.maxPlayers.Load()), HidePlayers: c.srv.hideOnlinePlayers.Load()}
	for _, p := range c.srv.statusSample() {
		event.Sample = append(event.Sample, plugin.Profile{ID: plugin.UUID(uuid.MustParse(p.ID)), Name: p.Name})
	}
	c.srv.plugins.Fire(event)
	if event.Cancelled() {
		return nil, fmt.Errorf("server list ping cancelled")
	}
	return event, nil
}

func (c *client) writeStatusResponse() error {
	event, err := c.serverListPing(c.statusProtocol)
	if err != nil {
		return err
	}
	text, _ := pluginText(*c.srv.motd.Load())
	if event != nil {
		text = event.MOTD
	}
	motd, err := json.Marshal(pluginTextJSON(text))
	if err != nil {
		return err
	}
	resp := protocol.StatusResponse{
		Version: protocol.StatusVersion{
			Name:     protocol.GameVersion,
			Protocol: protocol.Version,
		},
		Players: protocol.StatusPlayers{
			Max:    int(c.srv.maxPlayers.Load()),
			Online: c.srv.PlayerCount(),
		},
		Description: protocol.StatusDescription{
			Component: motd,
		},
		Favicon:             c.srv.favicon,
		EnforcesSecureChat:  c.srv.Config.AdvertisesSecureChat(),
		PreventsChatReports: c.srv.Config.PreventChatReports,
	}
	if event == nil {
		resp.Players.Sample = c.srv.statusSample()
	} else {
		resp.Players.Max, resp.Players.Online = event.Max, event.Online
		if !event.HidePlayers {
			for _, p := range event.Sample {
				resp.Players.Sample = append(resp.Players.Sample, protocol.StatusPlayer{ID: uuid.UUID(p.ID).String(), Name: p.Name})
			}
		}
		if event.Favicon != nil {
			resp.Favicon = "data:image/png;base64," + base64.StdEncoding.EncodeToString(event.Favicon)
		}
	}
	payload, err := resp.Encode()
	if err != nil {
		return err
	}
	return c.writePacket(protocol.IDStatusResponse, "status response", payload)
}

// isDisconnect reports whether an error is just the peer having hung up.
func isDisconnect(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE)
}

// statusSample picks up to 12 online players in random order for the
// server list; one disallowing listing shows as the nil-UUID "Anonymous
// Player". Empty with hide-online-players.
// vanilla: MinecraftServer.buildPlayerStatus
func (s *Server) statusSample() []protocol.StatusPlayer {
	if s.hideOnlinePlayers.Load() {
		return nil
	}
	clients := s.playerClients()
	n := min(len(clients), statusSampleSize)
	offset := rand.IntN(len(clients) - n + 1)
	sample := make([]protocol.StatusPlayer, 0, n)
	for _, c := range clients[offset : offset+n] {
		if c.allowsListing.Load() {
			sample = append(sample, protocol.StatusPlayer{Name: c.player.Name, ID: c.player.UUID.String()})
		} else {
			sample = append(sample, protocol.StatusPlayer{Name: "Anonymous Player", ID: uuid.Nil.String()})
		}
	}
	rand.Shuffle(len(sample), func(i, j int) { sample[i], sample[j] = sample[j], sample[i] })
	return sample
}

// statusSampleSize is the server list's player sample cap.
// vanilla: MinecraftServer.buildPlayerStatus
const statusSampleSize = 12
