package management

import (
	"encoding/json"
	"fmt"
	"net"
	"slices"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// banSource is the source of a ban a request leaves unnamed.
// vanilla: BanlistService.BAN_SOURCE, IpBanlistService.BAN_SOURCE
const banSource = "Management server"

// userBanOut is a player ban a response or notification carries; reason
// and expires are left out when the entry has none. vanilla: UserBanDto
type userBanOut struct {
	Player  playerOut `json:"player"`
	Reason  string    `json:"reason,omitempty"`
	Source  string    `json:"source"`
	Expires string    `json:"expires,omitempty"`
}

// ipBanOut is userBanOut for an address. vanilla: IpBanDto
type ipBanOut struct {
	IP      string `json:"ip"`
	Reason  string `json:"reason,omitempty"`
	Source  string `json:"source"`
	Expires string `json:"expires,omitempty"`
}

// instantText is t as ExtraCodecs.INSTANT_ISO8601 writes it, empty for zero.
func instantText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func userBanOutOf(b UserBan) userBanOut {
	return userBanOut{playerOut{b.Profile.UUID, b.Profile.Name}, b.Reason, b.Source, instantText(b.Expires)}
}

func ipBanOutOf(b IPBan) ipBanOut {
	return ipBanOut{b.IP, b.Reason, b.Source, instantText(b.Expires)}
}

// banFields are the fields the ban DTOs share; Source is banSource when the
// request leaves it out.
type banFields struct {
	Reason  string
	Source  string
	Expires time.Time
}

// decodeBanFields decodes reason, source and expires, in reverse field order
// as RecordCodecBuilder reports the first error.
func decodeBanFields(obj map[string]json.RawMessage) (banFields, error) {
	f := banFields{Source: banSource}
	if v, ok := field(obj, "expires"); ok {
		var str string
		if json.Unmarshal(v, &str) != nil {
			return f, codecError("Not a string: " + compactJSON(v))
		}
		t, err := time.Parse(time.RFC3339, str)
		if err != nil {
			// vanilla: DateTimeParseException's message
			return f, codecError(fmt.Sprintf("Text '%s' could not be parsed", str))
		}
		f.Expires = t
	}
	for _, k := range []string{"source", "reason"} {
		v, ok := field(obj, k)
		if !ok {
			continue
		}
		var str string
		if json.Unmarshal(v, &str) != nil {
			return f, codecError("Not a string: " + compactJSON(v))
		}
		if k == "source" {
			f.Source = str
		} else {
			f.Reason = str
		}
	}
	return f, nil
}

// required returns obj's value for k, or the codec's missing-key error.
func required(obj map[string]json.RawMessage, raw json.RawMessage, k string) (json.RawMessage, error) {
	v, ok := field(obj, k)
	if !ok {
		return nil, codecError(fmt.Sprintf("No key %s in MapLike[%s]", k, compactJSON(raw)))
	}
	return v, nil
}

// userBanIn is a request's player ban. vanilla: UserBanDto.CODEC
type userBanIn struct {
	Player playerIn
	banFields
}

func decodeUserBan(raw json.RawMessage) (userBanIn, error) {
	var b userBanIn
	obj, err := jsonObject(raw)
	if err != nil {
		return b, err
	}
	if b.banFields, err = decodeBanFields(obj); err != nil {
		return b, err
	}
	v, err := required(obj, raw, "player")
	if err != nil {
		return b, err
	}
	b.Player, err = decodePlayer(v)
	return b, err
}

// ipBanIn is a request's address ban: ip_bans/add's may name a player
// instead of an address. vanilla: IncomingIpBanDto.CODEC, IpBanDto.CODEC
type ipBanIn struct {
	Player *playerIn
	IP     string
	banFields
}

// decodeIPBan decodes an address ban; withIP makes ip required, as
// IpBanDto.CODEC does, and leaves player out.
func decodeIPBan(withIP bool) func(json.RawMessage) (ipBanIn, error) {
	return func(raw json.RawMessage) (ipBanIn, error) {
		var b ipBanIn
		obj, err := jsonObject(raw)
		if err != nil {
			return b, err
		}
		if b.banFields, err = decodeBanFields(obj); err != nil {
			return b, err
		}
		if withIP {
			v, err := required(obj, raw, "ip")
			if err != nil {
				return b, err
			}
			b.IP, err = decodeString(v)
			return b, err
		}
		if v, ok := field(obj, "ip"); ok {
			if b.IP, err = decodeString(v); err != nil {
				return b, err
			}
		}
		if v, ok := field(obj, "player"); ok {
			p, err := decodePlayer(v)
			if err != nil {
				return b, err
			}
			b.Player = &p
		}
		return b, nil
	}
}

// validIP reports whether ip is an address. vanilla: InetAddresses.isInetAddress
func validIP(ip string) bool { return net.ParseIP(ip) != nil }

// banMethods are the player and address ban services' methods.
// vanilla: IncomingRpcMethods.registerBanlistService,
// registerIpBanlistService, BanlistService, IpBanlistService
func (s *Server) banMethods(m map[string]method) {
	m["minecraft:bans"] = method{run: func(*conn, json.RawMessage) (any, error) {
		return s.bans(), nil
	}}
	m["minecraft:bans/add"] = method{run: s.bansAdd}
	m["minecraft:bans/remove"] = method{run: s.bansRemove}
	m["minecraft:bans/set"] = method{run: s.bansSet}
	m["minecraft:bans/clear"] = method{run: func(*conn, json.RawMessage) (any, error) {
		s.host.BansClear()
		return s.bans(), nil
	}}
	m["minecraft:ip_bans"] = method{run: func(*conn, json.RawMessage) (any, error) {
		return s.ipBans(), nil
	}}
	m["minecraft:ip_bans/add"] = method{run: s.ipBansAdd}
	m["minecraft:ip_bans/remove"] = method{run: s.ipBansRemove}
	m["minecraft:ip_bans/set"] = method{run: s.ipBansSet}
	m["minecraft:ip_bans/clear"] = method{run: func(c *conn, _ json.RawMessage) (any, error) {
		c.logf("Clear ip ban list")
		s.host.IPBansClear()
		return s.ipBans(), nil
	}}
}

// bans is the player ban list in file order.
func (s *Server) bans() []userBanOut {
	list := s.host.Bans()
	out := make([]userBanOut, len(list))
	for i, b := range list {
		out[i] = userBanOutOf(b)
	}
	return out
}

// ipBans is the address ban list in file order.
func (s *Server) ipBans() []ipBanOut {
	list := s.host.IPBans()
	out := make([]ipBanOut, len(list))
	for i, b := range list {
		out[i] = ipBanOutOf(b)
	}
	return out
}

// resolveBans decodes a [user_ban] param and resolves each player, skipping
// those that name nobody.
func (s *Server) resolveBans(raw json.RawMessage) ([]UserBan, error) {
	in, err := decodeList(raw, decodeUserBan)
	if err != nil {
		return nil, errInvalidParams(err.Error())
	}
	out := make([]UserBan, 0, len(in))
	for _, b := range in {
		if profile, ok := s.lookup(b.Player); ok {
			out = append(out, UserBan{profile, b.Reason, b.Source, b.Expires})
		}
	}
	return out, nil
}

// ban writes b and disconnects the player if online.
// vanilla: MinecraftBanListServiceImpl.addUserBan, BanlistService.add
func (s *Server) ban(c *conn, b UserBan) {
	reason := b.Reason
	if reason == "" {
		reason = "Banned by an operator."
	}
	c.logf("Add player '%s' to banlist. Reason: '%s'", b.Profile.Name, reason)
	s.host.BanPut(b)
	s.host.Kick(b.Profile.UUID, protocol.Text{Translate: "multiplayer.disconnect.banned"})
}

func (s *Server) bansAdd(c *conn, p json.RawMessage) (any, error) {
	bans, err := s.resolveBans(p)
	if err != nil {
		return nil, err
	}
	for _, b := range bans {
		s.ban(c, b)
	}
	return s.bans(), nil
}

func (s *Server) bansRemove(c *conn, p json.RawMessage) (any, error) {
	players, err := s.resolvePlayers(p)
	if err != nil {
		return nil, err
	}
	for _, pl := range players {
		c.logf("Remove player '%s' from banlist", pl.Name)
		s.host.BanRemove(pl)
	}
	return s.bans(), nil
}

// bansSet lifts the bans the final list lacks, then writes the new ones;
// entries compare by player, reason, source and expiry.
func (s *Server) bansSet(c *conn, p json.RawMessage) (any, error) {
	final, err := s.resolveBans(p)
	if err != nil {
		return nil, err
	}
	current := s.host.Bans()
	for _, b := range current {
		if !containsBan(final, b) {
			c.logf("Remove player '%s' from banlist", b.Profile.Name)
			s.host.BanRemove(b.Profile)
		}
	}
	for i, b := range final {
		if !containsBan(current, b) && !containsBan(final[:i], b) {
			s.ban(c, b)
		}
	}
	return s.bans(), nil
}

func containsBan(list []UserBan, b UserBan) bool {
	return slices.ContainsFunc(list, func(o UserBan) bool {
		return o.Profile.UUID == b.Profile.UUID && o.Profile.Name == b.Profile.Name && o.Reason == b.Reason && o.Source == b.Source && o.Expires.Equal(b.Expires)
	})
}

// banIP writes b and returns the online players at its address.
// vanilla: MinecraftBanListServiceImpl.addIpBan, IpBanlistService.banIp
func (s *Server) banIP(c *conn, b IPBan) []OnlinePlayer {
	c.logf("Add ip '%s' to ban list", b.IP)
	s.host.IPBanPut(b)
	var at []OnlinePlayer
	for _, o := range s.host.OnlinePlayers() {
		if o.IP == b.IP {
			at = append(at, o)
		}
	}
	return at
}

func (s *Server) kickIPBanned(players []OnlinePlayer) {
	for _, o := range players {
		s.host.Kick(o.Profile.UUID, protocol.Text{Translate: "multiplayer.disconnect.ip_banned"})
	}
}

// ipBansAdd bans each valid address, else the address of the online player
// the entry names, else nothing. vanilla: IpBanlistService.add
func (s *Server) ipBansAdd(c *conn, p json.RawMessage) (any, error) {
	in, err := decodeList(p, decodeIPBan(false))
	if err != nil {
		return nil, errInvalidParams(err.Error())
	}
	var kick []OnlinePlayer
	for _, b := range in {
		ip := b.IP
		if !validIP(ip) {
			o, ok := OnlinePlayer{}, false
			if b.Player != nil {
				o, ok = s.find(*b.Player)
			}
			if !ok {
				continue
			}
			ip = o.IP
		}
		kick = append(kick, s.banIP(c, IPBan{ip, b.Reason, b.Source, b.Expires})...)
	}
	s.kickIPBanned(kick)
	return s.ipBans(), nil
}

func (s *Server) ipBansRemove(c *conn, p json.RawMessage) (any, error) {
	ips, err := decodeList(p, decodeString)
	if err != nil {
		return nil, errInvalidParams(err.Error())
	}
	for _, ip := range ips {
		c.logf("Remove ip '%s' from ban list", ip)
		s.host.IPBanRemove(ip)
	}
	return s.ipBans(), nil
}

// ipBansSet drops invalid addresses, lifts the bans the final list lacks,
// writes the new ones and then disconnects the players at their addresses;
// entries compare by address, reason, source and expiry.
func (s *Server) ipBansSet(c *conn, p json.RawMessage) (any, error) {
	in, err := decodeList(p, decodeIPBan(true))
	if err != nil {
		return nil, errInvalidParams(err.Error())
	}
	final := make([]IPBan, 0, len(in))
	for _, b := range in {
		if validIP(b.IP) {
			final = append(final, IPBan{b.IP, b.Reason, b.Source, b.Expires})
		}
	}
	current := s.host.IPBans()
	for _, b := range current {
		if !containsIPBan(final, b) {
			c.logf("Remove ip '%s' from ban list", b.IP)
			s.host.IPBanRemove(b.IP)
		}
	}
	var kick []OnlinePlayer
	for i, b := range final {
		if !containsIPBan(current, b) && !containsIPBan(final[:i], b) {
			kick = append(kick, s.banIP(c, b)...)
		}
	}
	s.kickIPBanned(kick)
	return s.ipBans(), nil
}

func containsIPBan(list []IPBan, b IPBan) bool {
	return slices.ContainsFunc(list, func(o IPBan) bool {
		return o.IP == b.IP && o.Reason == b.Reason && o.Source == b.Source && o.Expires.Equal(b.Expires)
	})
}

// kickIn is players/kick's entry. vanilla: PlayerService.KickDto
type kickIn struct {
	Player  playerIn
	Message *messageIn
}

func decodeKick(raw json.RawMessage) (kickIn, error) {
	var k kickIn
	obj, err := jsonObject(raw)
	if err != nil {
		return k, err
	}
	if v, ok := field(obj, "message"); ok {
		m, err := decodeMessage(v)
		if err != nil {
			return k, err
		}
		k.Message = &m
	}
	v, err := required(obj, raw, "player")
	if err != nil {
		return k, err
	}
	k.Player, err = decodePlayer(v)
	return k, err
}

// kick is players/kick: it returns the entries of the players it found
// online as they were sent. vanilla: PlayerService.kick
func (s *Server) kick(c *conn, p json.RawMessage) (any, error) {
	in, err := decodeList(p, decodeKick)
	if err != nil {
		return nil, errInvalidParams(err.Error())
	}
	kicked := []playerIn{}
	for _, k := range in {
		o, ok := s.find(k.Player)
		if !ok {
			continue
		}
		c.logf("Remove player '%s'", o.Profile.Name)
		text := protocol.Text{Translate: "multiplayer.disconnect.kicked"}
		if k.Message != nil {
			if t, ok := k.Message.text(); ok {
				text = t
			}
		}
		s.host.Kick(o.Profile.UUID, text)
		kicked = append(kicked, k.Player)
	}
	return kicked, nil
}
