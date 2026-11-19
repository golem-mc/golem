package admin

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"path/filepath"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/google/uuid"
)

// usercacheRecord is one line of usercache.json, in the shape the vanilla
// server writes.
type usercacheRecord struct {
	UUID      string `json:"uuid"`
	Name      string `json:"name"`
	ExpiresOn string `json:"expiresOn"`
}

// usercacheStamp is the stamp vanilla's own adds write: a month out, in the
// layout every server list file shares.
func usercacheStamp() string {
	return time.Now().AddDate(0, 1, 0).Format("2006-01-02 15:04:05 -0700")
}

// usercacheAt points the server's user cache at a file of the test's own,
// seeded with the entries given, so that a test does not write usercache.json
// into the working directory the package's tests share. The file is first read
// when a join or a name lookup reaches for the cache.
func usercacheAt(t *testing.T, records ...usercacheRecord) func(*server.Server) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "usercache.json")
	servertest.WriteJSONList(t, path, records)
	return func(s *server.Server) { s.SetUsercacheFile(path) }
}

// TestUsercacheKeepsJoinedPlayers joins two players and reads usercache.json
// back: both are kept, the one who joined last first, and each entry carries
// their UUID, their name and a stamp a month out.
func TestUsercacheKeepsJoinedPlayers(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, _, _, _ := servertest.AdminPair(t, cfg, usercacheAt(t))

	records := servertest.ReadListFile[usercacheRecord](t, srv.UsercacheFile())
	if len(records) != 2 {
		t.Fatalf("usercache.json holds %d entries, want Alice and Bob", len(records))
	}
	if records[0].Name != "Bob" || records[0].UUID != protocol.OfflineUUID("Bob").String() {
		t.Errorf("front usercache entry = %+v, want Bob, who joined last, under his UUID", records[0])
	}
	if records[1].Name != "Alice" || records[1].UUID != protocol.OfflineUUID("Alice").String() {
		t.Errorf("second usercache entry = %+v, want Alice", records[1])
	}
	// A month is 28 to 31 days; AddDate would overflow Oct 31 into Dec 1,
	// while the cache clamps to the end of the next month.
	now := time.Now()
	earliest, latest := now.AddDate(0, 0, 28), now.AddDate(0, 0, 31)
	for _, record := range records {
		expires, err := time.Parse("2006-01-02 15:04:05 -0700", record.ExpiresOn)
		if err != nil {
			t.Errorf("usercache entry %s expiresOn %q does not read", record.Name, record.ExpiresOn)
			continue
		}
		if expires.Before(earliest.Add(-time.Hour)) || expires.After(latest.Add(time.Hour)) {
			t.Errorf("usercache entry %s expiresOn %q, want a month out", record.Name, record.ExpiresOn)
		}
	}
}

// TestUsercacheRejoinsToFront rejoins a player and reads the cache again: the
// rejoin refreshes their entry and stands it back at the front, the most
// recently joined first.
func TestUsercacheRejoinsToFront(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, usercacheAt(t))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	bobID := alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	bob.Disconnect()
	alice.ExpectPlayerRemoved(bobID, "Bob")
	alice.ExpectAnnouncement("multiplayer.player.left", "Bob")

	connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	records := servertest.ReadListFile[usercacheRecord](t, srv.UsercacheFile())
	if len(records) != 2 {
		t.Fatalf("usercache.json holds %d entries, want Alice and Bob", len(records))
	}
	if records[0].Name != "Bob" || records[1].Name != "Alice" {
		t.Errorf("usercache.json = %+v, want the rejoined Bob first", records)
	}
}

// TestUsercacheResolvesPlayersWhoLeft runs the profile-argument commands on
// Bob, who has joined and left, under a casing his name was not joined with:
// the pair the cache kept answers, not an offline-UUID of the typed word —
// which is the identity no command could find once Bob has left. In online
// mode the offline fallback is not there at all, and the cache is the only
// source.
func TestUsercacheResolvesPlayersWhoLeft(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"), usercacheAt(t),
		servertest.WriteAdminFiles(t, nil, nil, nil))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	bobID := alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	bob.Disconnect()
	alice.ExpectPlayerRemoved(bobID, "Bob")
	alice.ExpectAnnouncement("multiplayer.player.left", "Bob")

	// Op first: the operator list is not one a player argument reads, so the
	// resolution reaches the cache on its own.
	alice.ChatCommand("op bob")
	alice.ExpectCommandFeedbackText("commands.op.success", protocol.Text{Text: "Bob"})
	ops := servertest.ReadListFile[servertest.OpRecord](t, srv.OpsFile())
	if len(ops) != 2 || ops[1].UUID != protocol.OfflineUUID("Bob").String() || ops[1].Name != "Bob" {
		t.Errorf("ops.json = %+v, want Bob op'd under the UUID he joined with", ops)
	}

	alice.ChatCommand("ban bob Left")
	alice.ExpectCommandFeedbackText("commands.ban.success",
		protocol.Text{Text: "Bob"}, protocol.Text{Text: "Left"})
	records := servertest.ReadListFile[servertest.BanRecord](t, srv.BannedPlayersFile())
	if len(records) != 1 || records[0].UUID != protocol.OfflineUUID("Bob").String() ||
		records[0].Name != "Bob" || records[0].Reason != "Left" {
		t.Errorf("banned-players.json = %+v, want Bob banned under the UUID he joined with", records)
	}

	alice.ChatCommand("whitelist add Bob")
	alice.ExpectCommandFeedbackText("commands.whitelist.add.success", protocol.Text{Text: "Bob"})
	listed := servertest.ReadListFile[servertest.WhitelistRecord](t, srv.WhitelistFile())
	if len(listed) != 1 || listed[0].UUID != protocol.OfflineUUID("Bob").String() || listed[0].Name != "Bob" {
		t.Errorf("whitelist.json = %+v, want Bob listed under the UUID he joined with", listed)
	}

	alice.Disconnect()
}

// TestUsercacheResolvesAStoredUUID plants an entry in usercache.json — the
// standing-in for the entry an authenticated join would leave behind, since
// no client in this environment can log in online — and checks the commands
// resolve the name to the UUID the file holds rather than one derived from
// the typed word.
func TestUsercacheResolvesAStoredUUID(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	steve := uuid.Must(uuid.NewRandom())
	seed := usercacheRecord{UUID: steve.String(), Name: "Steve", ExpiresOn: usercacheStamp()}
	srv, _, alice := servertest.AdminSolo(t, cfg, usercacheAt(t, seed), servertest.WriteAdminFiles(t, nil, nil, nil))

	alice.ChatCommand("op Steve")
	alice.ExpectCommandFeedbackText("commands.op.success", protocol.Text{Text: "Steve"})
	ops := servertest.ReadListFile[servertest.OpRecord](t, srv.OpsFile())
	if len(ops) != 2 || ops[1].UUID != steve.String() || ops[1].Name != "Steve" {
		t.Errorf("ops.json = %+v, want Steve listed under the UUID the cache holds", ops)
	}

	alice.ChatCommand("whitelist add Steve")
	alice.ExpectCommandFeedbackText("commands.whitelist.add.success", protocol.Text{Text: "Steve"})
	listed := servertest.ReadListFile[servertest.WhitelistRecord](t, srv.WhitelistFile())
	if len(listed) != 1 || listed[0].UUID != steve.String() || listed[0].Name != "Steve" {
		t.Errorf("whitelist.json = %+v, want Steve listed under the UUID the cache holds", listed)
	}

	alice.ChatCommand("ban Steve Bad")
	alice.ExpectCommandFeedbackText("commands.ban.success",
		protocol.Text{Text: "Steve"}, protocol.Text{Text: "Bad"})
	records := servertest.ReadListFile[servertest.BanRecord](t, srv.BannedPlayersFile())
	if len(records) != 1 || records[0].UUID != steve.String() || records[0].Name != "Steve" ||
		records[0].Reason != "Bad" {
		t.Errorf("banned-players.json = %+v, want Steve banned under the UUID the cache holds", records)
	}

	alice.Disconnect()
}

// TestUsercacheExpiredEntryLetsTheResolutionPass checks the stamp is read: a
// name whose cache entry has lapsed resolves without it, since the offline
// fallback is what an expired entry leaves behind.
func TestUsercacheExpiredEntryLetsTheResolutionPass(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	seed := usercacheRecord{
		UUID:      uuid.Must(uuid.NewRandom()).String(),
		Name:      "Steve",
		ExpiresOn: "2020-01-01 00:00:00 +0000",
	}
	srv, _, alice := servertest.AdminSolo(t, cfg, usercacheAt(t, seed), servertest.WriteAdminFiles(t, nil, nil, nil))

	alice.ChatCommand("op Steve")
	alice.ExpectCommandFeedbackText("commands.op.success", protocol.Text{Text: "Steve"})
	ops := servertest.ReadListFile[servertest.OpRecord](t, srv.OpsFile())
	if len(ops) != 2 || ops[1].UUID != protocol.OfflineUUID("Steve").String() {
		t.Errorf("ops.json = %+v, want Steve under the offline UUID of the expired entry's name", ops)
	}

	alice.Disconnect()
}

// TestUsercacheDropsUnreadableEntries seeds the file with an entry whose UUID
// does not parse and one whose stamp does not, then joins a player: the next
// write drops both, as vanilla's load would, rather than carrying them
// forever and counting them against the MRU limit.
func TestUsercacheDropsUnreadableEntries(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	badUUID := usercacheRecord{UUID: "not-a-uuid", Name: "Ghost", ExpiresOn: usercacheStamp()}
	badStamp := usercacheRecord{UUID: uuid.Must(uuid.NewRandom()).String(), Name: "Phantom", ExpiresOn: "not a date"}
	srv, _ := servertest.Join(t, cfg, "Alice", usercacheAt(t, badUUID, badStamp))

	records := servertest.ReadListFile[usercacheRecord](t, srv.UsercacheFile())
	if len(records) != 1 || records[0].Name != "Alice" {
		t.Errorf("usercache.json = %+v, want only Alice, the unreadable entries dropped", records)
	}
}

// TestUsercacheSurvivesRestart runs the second half of the cache's contract:
// a restarted server reads usercache.json, and the runs' own joins and an
// entry planted before the restart both still resolve through it.
func TestUsercacheSurvivesRestart(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// A UUID no name derives offline, standing in for an authenticated join.
	steve := uuid.Must(uuid.NewRandom())
	dir := t.TempDir()
	usercachePath := filepath.Join(dir, "usercache.json")
	bansPath := filepath.Join(dir, "banned-players.json")
	whitelistPath := filepath.Join(dir, "whitelist.json")
	opsPath := filepath.Join(dir, "ops.json")
	seed := []usercacheRecord{{UUID: steve.String(), Name: "Steve", ExpiresOn: usercacheStamp()}}
	servertest.WriteJSONList(t, usercachePath, seed)
	servertest.WriteJSONList(t, bansPath, []servertest.BanRecord{})
	servertest.WriteJSONList(t, whitelistPath, []servertest.WhitelistRecord{})
	servertest.WriteJSONList(t, opsPath, []servertest.OpRecord{})
	point := func(s *server.Server) {
		s.SetUsercacheFile(usercachePath)
		s.SetBannedPlayersFile(bansPath)
		s.SetWhitelistFile(whitelistPath)
		s.SetOpsFile(opsPath)
	}

	srv, _, alice := servertest.AdminSolo(t, cfg, point)
	alice.Disconnect()
	srv.Shutdown()

	restarted := *cfg
	srv2, _ := servertest.Start(t, &restarted, point)

	if reply := srv2.RunLine("ban alice"); len(reply) != 1 || reply[0].Translate != "commands.ban.success" {
		t.Fatalf("ban alice after the restart answered %v, want commands.ban.success", reply)
	}
	records := servertest.ReadListFile[servertest.BanRecord](t, srv2.BannedPlayersFile())
	if len(records) != 1 || records[0].UUID != protocol.OfflineUUID("Alice").String() ||
		records[0].Name != "Alice" {
		t.Errorf("banned-players.json = %+v, want Alice under the UUID her join cached", records)
	}

	if reply := srv2.RunLine("op Steve"); len(reply) != 1 || reply[0].Translate != "commands.op.success" {
		t.Fatalf("op Steve after the restart answered %v, want commands.op.success", reply)
	}
	ops := servertest.ReadListFile[servertest.OpRecord](t, srv2.OpsFile())
	if len(ops) != 1 || ops[0].UUID != steve.String() || ops[0].Name != "Steve" {
		t.Errorf("ops.json = %+v, want Steve under the UUID the cache holds", ops)
	}
	// The restart reads the cache without rewriting it: Steve's own stamp survives.
	entries := servertest.ReadListFile[usercacheRecord](t, srv2.UsercacheFile())
	if len(entries) != 2 || entries[0].Name != "Alice" || entries[1].Name != "Steve" ||
		entries[1].ExpiresOn != seed[0].ExpiresOn {
		t.Errorf("usercache.json after the restart = %+v, want Alice's join and Steve's seed", entries)
	}
}

// TestUsercacheRenamedJoin joins a player whose cached name differs from
// the one they arrive with: the others read the renamed announcement naming
// the old one, and once the cache holds the new name, a rejoin is plain.
func TestUsercacheRenamedJoin(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, connect, alice := servertest.AdminSolo(t, cfg, usercacheAt(t, usercacheRecord{
		UUID: protocol.OfflineUUID("Bob").String(), Name: "Robert", ExpiresOn: usercacheStamp(),
	}))

	bob := connect().Join("Bob")
	var want protocol.Buffer
	want.PutText(protocol.Text{
		Translate: "multiplayer.player.joined.renamed",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Bob"), {Text: "Robert"}},
		Color:     protocol.ColorYellow,
	})
	want.PutBool(false)
	alice.ExpectPlayerAdded("Bob")
	if got := alice.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, want.Bytes()) {
		t.Errorf("rename announcement = % X, want % X", got, want.Bytes())
	}

	bob.Disconnect()
	for id := range alice.Packets(servertest.ExpectTimeout) {
		if id == protocol.IDPlaySystemChat {
			break
		}
	}
	connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")
}
