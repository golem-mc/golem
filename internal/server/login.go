package server

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

// outdatedClientProtocol is the protocol below which the version mismatch
// uses the outdated_client key instead of incompatible.
// vanilla: ServerHandshakePacketListenerImpl.beginLogin
const outdatedClientProtocol = 754

// login runs the login phase: identify, authenticate in online mode,
// enable compression unless disabled, and hand over to configuration.
func (c *client) login(handshake protocol.Handshake) error {
	timeout := loginTimeout
	if c.srv.loginTimeout > 0 {
		timeout = c.srv.loginTimeout
	}
	if err := c.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	err := c.runLogin(handshake)
	if errors.Is(err, os.ErrDeadlineExceeded) {
		// vanilla: ServerLoginPacketListenerImpl.tick
		if werr := c.rejectLoginComponent(protocol.Text{Translate: "multiplayer.disconnect.slow_login"}); werr != nil {
			return fmt.Errorf("%w (%w)", err, werr)
		}
	}
	return err
}

// runLogin is login without the deadline handling.
func (c *client) runLogin(handshake protocol.Handshake) error {

	if handshake.ProtocolVersion != protocol.Version {
		// Below 1.16.5 (protocol 754) is outdated, newer incompatible; both name
		// this version. vanilla: ServerHandshakePacketListenerImpl.beginLogin
		key := "multiplayer.disconnect.incompatible"
		if handshake.ProtocolVersion < outdatedClientProtocol {
			key = "multiplayer.disconnect.outdated_client"
		}
		return c.rejectLoginComponent(protocol.Text{
			Translate: key,
			With:      []protocol.Text{{Text: protocol.GameVersion}},
		})
	}

	packetID, data, err := c.conn.ReadPacket()
	if err != nil {
		return fmt.Errorf("read login start: %w", err)
	}
	if packetID != protocol.IDLoginStart {
		if err := c.rejectLoginQuery(packetID); err != nil {
			return err
		}
		return fmt.Errorf("expected login start, got packet 0x%02X", packetID)
	}

	r := protocol.NewReader(data)
	name := r.ReadString(16)
	r.ReadUUID() // the client's idea of its own UUID, which we do not trust
	if err := r.Err(); err != nil {
		return fmt.Errorf("login start: %w", err)
	}
	if !validUsername(name) {
		return c.rejectLogin(fmt.Errorf("invalid username %q", name), "Invalid username.")
	}

	c.log = c.log.With("player", name)

	if c.srv.Config.OnlineMode {
		profile, err := c.authenticate(name)
		if err != nil {
			// rejectLoginQuery has already written the Disconnect.
			if errors.Is(err, errLoginQueryAnswered) {
				return err
			}
			switch {
			case errors.Is(err, errSessionUnverified):
				return c.rejectLoginKeyed(err, "multiplayer.disconnect.unverified_username")
			case errors.Is(err, errSessionUnavailable):
				return c.rejectLoginKeyed(err, "multiplayer.disconnect.authservers_down")
			}
			return c.rejectLogin(err, "Failed to verify your account with Mojang.")
		}
		c.profile = profile
		c.srv.mgmt.LoginActivity()
	} else {
		// Without authentication the UUID is vanilla's offline UUID.
		c.profile = player.Profile{UUID: protocol.OfflineUUID(name), Name: name}
	}

	if c.srv.plugins != nil && c.srv.plugins.Has(pluginruntime.PlayerPreLogin) {
		e := &plugin.PlayerPreLoginEvent{Profile: pluginProfile(c.profile), Address: c.conn.RemoteAddr()}
		c.srv.plugins.Fire(e)
		if e.Deny {
			return c.rejectLoginComponent(pluginProtocolText(e.Reason))
		}
	}
	if c.srv.plugins != nil {
		c.pluginConnecting.Store(true)
	}

	// Banned players, unlisted ones with the white-list on, banned addresses,
	// and arrivals at a full server are refused before login is confirmed.
	// vanilla: PlayerList.canPlayerLogin
	reason, refused := c.srv.loginRefusal(c.profile, c.remoteIP())
	hostname := fmt.Sprintf("%s:%d", handshake.ServerAddress, handshake.ServerPort)
	if reason, refused = c.pluginLogin(hostname, reason, refused); refused {
		return c.rejectLoginComponent(reason)
	}

	if err := c.enableCompression(); err != nil {
		return err
	}
	if err := c.sendLoginSuccess(); err != nil {
		return err
	}

	packetID, _, err = c.conn.ReadPacket()
	if err != nil {
		return fmt.Errorf("read login acknowledged: %w", err)
	}
	if packetID != protocol.IDLoginAcknowledged {
		if err := c.rejectLoginQuery(packetID); err != nil {
			return err
		}
		return fmt.Errorf("expected login acknowledged, got packet 0x%02X", packetID)
	}

	c.log.Debug("player logged in", "uuid", c.profile.UUID)
	return nil
}

// loginAndPlay runs login, configuration and play, for both the Login and
// an accepted Transfer intent.
func (c *client) loginAndPlay(handshake protocol.Handshake) error {
	defer func() { c.pluginConnecting.Store(false) }()
	if err := c.login(handshake); err != nil {
		return err
	}
	if err := c.configure(); err != nil {
		return err
	}
	return c.play()
}

// validUsername reports whether a name is at most 16 printable ASCII
// characters (33 to 126). Unlike vanilla's check, an empty name is refused.
// vanilla: StringUtil.isValidPlayerName
func validUsername(name string) bool {
	if len(name) < 1 || len(name) > 16 {
		return false
	}
	for i := 0; i < len(name); i++ {
		if name[i] <= 32 || name[i] >= 127 {
			return false
		}
	}
	return true
}

// rejectLogin tells the client why it cannot join and returns the reason.
func (c *client) rejectLogin(reason error, message string) error {
	var b protocol.Buffer
	b.PutJSONTextComponent(message)
	if err := c.conn.WritePacket(protocol.IDLoginDisconnect, b.Bytes()); err != nil {
		return fmt.Errorf("%w (and writing the disconnect failed: %w)", reason, err)
	}
	return reason
}

// rejectLoginKeyed is rejectLogin with a translatable reason, returning
// the causing error.
func (c *client) rejectLoginKeyed(reason error, key string) error {
	var b protocol.Buffer
	b.PutString(protocol.JSONText(protocol.Text{Translate: key}))
	if err := c.conn.WritePacket(protocol.IDLoginDisconnect, b.Bytes()); err != nil {
		return fmt.Errorf("%w (and writing the disconnect failed: %w)", reason, err)
	}
	return reason
}

// errLoginQueryAnswered marks a Disconnect rejectLoginQuery already wrote,
// so the caller does not reject twice.
var errLoginQueryAnswered = errors.New("unexpected login query answered")

// rejectLoginQuery disconnects a client answering a cookie or login plugin
// request never sent, reporting whether it was one.
func (c *client) rejectLoginQuery(packetID int32) error {
	switch packetID {
	case protocol.IDLoginPluginResponse, protocol.IDLoginCookieResponse:
		return fmt.Errorf("%w: %w", errLoginQueryAnswered, c.rejectLoginComponent(unexpectedQueryResponse()))
	}
	return nil
}

// rejectLoginComponent is rejectLogin with a translated reason, sent as the
// component's JSON in the login Disconnect.
func (c *client) rejectLoginComponent(reason protocol.Text) error {
	var b protocol.Buffer
	if reason.Raw != nil {
		text, _ := pluginText(reason)
		data, err := json.Marshal(pluginTextJSON(text))
		if err != nil {
			return fmt.Errorf("encode login disconnect: %w", err)
		}
		b.PutString(string(data))
	} else {
		b.PutString(protocol.JSONText(reason))
	}
	if err := c.conn.WritePacket(protocol.IDLoginDisconnect, b.Bytes()); err != nil {
		return fmt.Errorf("write login disconnect: %w", err)
	}
	return fmt.Errorf("login refused: %s", reason.Plain())
}

// remoteIP is the client's address without port, the IP ban list's key.
func (c *client) remoteIP() string {
	addr := c.conn.RemoteAddr().String()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// loginRefusal is the reason the player is refused at login, checked as:
// player ban, white-list, address ban, full server. False means may join.
// vanilla: PlayerList.canPlayerLogin
func (s *Server) loginRefusal(profile player.Profile, ip string) (protocol.Text, bool) {
	if reason, refused := s.listRefusal(profile, ip, 0); refused {
		return reason, true
	}
	if s.PlayerCount() >= int(s.maxPlayers.Load()) && !s.ops.bypassesPlayerLimit(profile.UUID) {
		return protocol.Text{Translate: "multiplayer.disconnect.server_full"}, true
	}
	return protocol.Text{}, false
}

// listRefusal is loginRefusal without the capacity check: bans and
// white-list, which need no server lock.
// bypass is a mask of plugin.LoginResult categories allowed by the login listener.
func (s *Server) listRefusal(profile player.Profile, ip string, bypass uint8) (protocol.Text, bool) {
	if entry, banned := s.bans.bannedProfile(profile); banned && bypass&(1<<plugin.LoginBanned) == 0 {
		return banRefusal("multiplayer.disconnect.banned.reason", "multiplayer.disconnect.banned.expiration", entry.Expires, entry.Reason), true
	}
	if !s.whitelistAllows(profile) && bypass&(1<<plugin.LoginWhitelist) == 0 {
		return protocol.Text{Translate: "multiplayer.disconnect.not_whitelisted"}, true
	}
	if entry, banned := s.bans.bannedIP(ip); banned && bypass&(1<<plugin.LoginBanned) == 0 {
		return banRefusal("multiplayer.disconnect.banned_ip.reason", "multiplayer.disconnect.banned_ip.expiration", entry.Expires, entry.Reason), true
	}
	return protocol.Text{}, false
}

// banRefusal is a live ban's login refusal: its reason, and expiry date for
// a dated ban. vanilla: PlayerList.canPlayerLogin
func banRefusal(reasonKey, expiryKey, expires, reason string) protocol.Text {
	refusal := protocol.Text{Translate: reasonKey, With: []protocol.Text{banReasonMessage(reason)}}
	if date, ok := banExpiry(expires); ok {
		refusal.Extra = append(refusal.Extra, protocol.Text{
			Translate: expiryKey,
			With:      []protocol.Text{{Text: date}},
		})
	}
	return refusal
}

// whitelistAllows is PlayerList.isWhiteListed: anyone with the white-list
// off, else operators and listed players.
func (s *Server) whitelistAllows(profile player.Profile) bool {
	if !s.whitelist.isEnabled() {
		return true
	}
	return s.ops.listed(profile.UUID) || s.whitelist.contains(profile.UUID)
}

// enableCompression sends the network-compression-threshold (negative
// disables) and switches to the compressed format; disabled sends nothing
// and keeps the plain format.
// vanilla: ServerLoginPacketListenerImpl.verifyLoginAndFinishConnectionSetup
func (c *client) enableCompression() error {
	if c.srv.Config.NetworkCompressionThreshold < 0 {
		return nil
	}
	if err := c.write(protocol.IDSetCompression, "set compression", func(b *protocol.Buffer) {
		b.PutVarInt(int32(c.srv.Config.NetworkCompressionThreshold))
	}); err != nil {
		return err
	}
	c.conn.EnableCompression(c.srv.Config.NetworkCompressionThreshold)
	return nil
}

// sendLoginSuccess confirms the login and moves the client to configuration.
func (c *client) sendLoginSuccess() error {
	sessionID, err := uuid.NewRandom()
	if err != nil {
		return fmt.Errorf("generate session id: %w", err)
	}
	return c.write(protocol.IDLoginSuccess, "login success", func(b *protocol.Buffer) {
		b.PutUUID(c.profile.UUID)
		b.PutString(c.profile.Name)
		putProperties(b, c.profile.Properties)
		b.PutUUID(sessionID)
	})
}

// authenticate runs the encrypted handshake and asks Mojang's session
// server to verify the player, returning its authoritative profile.
func (c *client) authenticate(name string) (player.Profile, error) {
	verifyToken := make([]byte, 4)
	if _, err := rand.Read(verifyToken); err != nil {
		return player.Profile{}, fmt.Errorf("generate verify token: %w", err)
	}

	if err := c.write(protocol.IDEncryptionRequest, "encryption request", func(b *protocol.Buffer) {
		b.PutString("") // server ID, empty since 1.7
		b.PutBytes(c.srv.publicKey)
		b.PutBytes(verifyToken)
		b.PutBool(true) // the client should authenticate with Mojang
	}); err != nil {
		return player.Profile{}, err
	}

	packetID, data, err := c.conn.ReadPacket()
	if err != nil {
		return player.Profile{}, fmt.Errorf("read encryption response: %w", err)
	}
	if packetID != protocol.IDEncryptionResponse {
		if err := c.rejectLoginQuery(packetID); err != nil {
			return player.Profile{}, err
		}
		return player.Profile{}, fmt.Errorf("expected encryption response, got packet 0x%02X", packetID)
	}

	r := protocol.NewReader(data)
	encryptedSecret := r.ReadBytes(256)
	encryptedToken := r.ReadBytes(256)
	if err := r.Err(); err != nil {
		return player.Profile{}, fmt.Errorf("encryption response: %w", err)
	}

	// The client encrypts with RSA/ECB/PKCS1Padding, so the protocol requires
	// PKCS #1 v1.5 here; OAEP is not an option.
	secret, err := rsa.DecryptPKCS1v15(nil, c.srv.privateKey, encryptedSecret) //nolint:staticcheck // SA1019: fixed by the protocol
	if err != nil {
		return player.Profile{}, fmt.Errorf("decrypt shared secret: %w", err)
	}
	token, err := rsa.DecryptPKCS1v15(nil, c.srv.privateKey, encryptedToken) //nolint:staticcheck // SA1019: fixed by the protocol
	if err != nil {
		return player.Profile{}, fmt.Errorf("decrypt verify token: %w", err)
	}
	if string(token) != string(verifyToken) {
		return player.Profile{}, fmt.Errorf("verify token does not match")
	}

	// From here on everything is encrypted with the shared secret, including
	// the reply to the session check below.
	if err := c.conn.EnableEncryption(secret); err != nil {
		return player.Profile{}, err
	}

	return c.hasJoined(name, serverHash(secret, c.srv.publicKey))
}

// serverHash is the login attempt's digest for the session server.
// Minecraft prints the SHA-1 as a signed number, not unsigned hex.
func serverHash(secret, publicKey []byte) string {
	h := sha1.New()
	h.Write(nil) // the server ID, which is empty
	h.Write(secret)
	h.Write(publicKey)

	sum := h.Sum(nil)
	n := new(big.Int).SetBytes(sum)
	if sum[0]&0x80 != 0 {
		// Interpret the digest as a two's complement negative number.
		n.Sub(n, new(big.Int).Lsh(big.NewInt(1), uint(len(sum)*8)))
	}
	return n.Text(16)
}

// sessionServer is Mojang's endpoint for confirming that a player has joined.
const defaultSessionServer = "https://sessionserver.mojang.com/session/minecraft/hasJoined"

func (s *Server) sessionServerURL() string {
	if s.sessionServer != "" {
		return s.sessionServer
	}
	return defaultSessionServer
}

// errSessionUnverified is a session server not knowing the player's join;
// errSessionUnavailable one that could not be asked or failed.
// vanilla: ServerLoginPacketListenerImpl.handleKey
var (
	errSessionUnverified  = errors.New("session server did not verify the player")
	errSessionUnavailable = errors.New("session server unavailable")
)

// hasJoined asks the session server for the player's profile, which only
// succeeds if the client authenticated with the same hash.
func (c *client) hasJoined(name, hash string) (player.Profile, error) {
	query := url.Values{"username": {name}, "serverId": {hash}}
	if c.srv.Config.PreventProxyConnections {
		// vanilla: ServerLoginPacketListenerImpl.getAddress
		query.Set("ip", c.remoteIP())
	}
	req, err := http.NewRequest(http.MethodGet, c.srv.sessionServerURL()+"?"+query.Encode(), nil)
	if err != nil {
		return player.Profile{}, fmt.Errorf("session server request: %w", err)
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return player.Profile{}, fmt.Errorf("contact session server: %w: %w", errSessionUnavailable, err)
	}
	defer resp.Body.Close()

	// 204 means the player never authenticated for this hash: an
	// unauthenticated client.
	if resp.StatusCode == http.StatusNoContent {
		return player.Profile{}, fmt.Errorf("%s is not authenticated with Mojang: %w", name, errSessionUnverified)
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= 500 {
			return player.Profile{}, fmt.Errorf("session server returned %s: %w", resp.Status, errSessionUnavailable)
		}
		return player.Profile{}, fmt.Errorf("session server returned %s", resp.Status)
	}

	var body struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Properties []struct {
			Name      string `json:"name"`
			Value     string `json:"value"`
			Signature string `json:"signature"`
		} `json:"properties"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return player.Profile{}, fmt.Errorf("decode session server response: %w", err)
	}

	id, err := uuid.Parse(body.ID)
	if err != nil {
		return player.Profile{}, fmt.Errorf("session server profile: %w", err)
	}
	if !strings.EqualFold(body.Name, name) {
		return player.Profile{}, fmt.Errorf("session server returned profile for %q, not %q", body.Name, name)
	}

	profile := player.Profile{UUID: id, Name: body.Name}
	for _, p := range body.Properties {
		profile.Properties = append(profile.Properties, player.Property{
			Name:      p.Name,
			Value:     p.Value,
			Signature: p.Signature,
		})
	}
	return profile, nil
}
