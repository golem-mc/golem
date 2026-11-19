package server

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/plugin"
	"go.yaml.in/yaml/v4"
)

// Bukkit's /help, which replaces vanilla's whenever a plugin is loaded.
// Strings, colours and paging are CraftBukkit's: HelpCommand,
// SimpleHelpMap, IndexHelpTopic, GenericCommandHelpTopic, ChatPaginator.

const (
	helpFile = "help.yml"
	// helpPageWidth and helpPageHeight are what a player is paged by:
	// ChatPaginator.GUARANTEED_NO_WRAP_CHAT_PAGE_WIDTH and
	// CLOSED_CHAT_PAGE_HEIGHT - 1.
	helpPageWidth  = 55
	helpPageHeight = 9
	// helpUnbounded is ChatPaginator.UNBOUNDED_PAGE_WIDTH / _HEIGHT, the
	// console's.
	helpUnbounded = 1<<31 - 1
	// builtinHelpGroup is the index topic of the server's own commands,
	// which CraftBukkit splits into "Minecraft" and "Bukkit".
	builtinHelpGroup = "Golem"
)

const defaultHelpYAML = `# This is the help configuration file.
#
# By default you do not need to modify this file. Help topics for all plugin
# commands are provided by the plugins themselves. You only need to modify
# this file to add help pages to your server or to override the help pages of
# existing plugins.

# Topics that are not commands. The name is what /help takes:
#
# general-topics:
#     Rules:
#         shortText: Our server rules
#         fullText: Be nice to each other.
#         permission: ""
general-topics: {}

# Topics listing other topics. commands names the topics (such as /warp, or
# a plugin's name) in the list:
#
# index-topics:
#     Staff:
#         shortText: Commands for staff
#         preamble: Staff commands
#         permission: ""
#         commands:
#             - /ban
#             - /kick
index-topics: {}

# Changes to topics that exist. <text> in an amendment stands for the text
# it replaces; a permission replaces the one that shows the topic:
#
# amended-topics:
#     /seed:
#         shortText: <text> (op only)
#         fullText: <text>
#         permission: minecraft.command.seed
amended-topics: {}

# Plugins whose commands /help does not list.
ignore-plugins: []

# Whether /help lists every command, or only the topics that are not commands.
command-topics-in-master-index: true
`

type helpConfig struct {
	General map[string]struct {
		ShortText  string `yaml:"shortText"`
		FullText   string `yaml:"fullText"`
		Permission string `yaml:"permission"`
	} `yaml:"general-topics"`
	Index map[string]struct {
		ShortText  string   `yaml:"shortText"`
		Preamble   string   `yaml:"preamble"`
		Permission string   `yaml:"permission"`
		Commands   []string `yaml:"commands"`
	} `yaml:"index-topics"`
	Amended map[string]struct {
		ShortText  *string `yaml:"shortText"`
		FullText   *string `yaml:"fullText"`
		Permission *string `yaml:"permission"`
	} `yaml:"amended-topics"`
	IgnorePlugins              []string `yaml:"ignore-plugins"`
	CommandTopicsInMasterIndex *bool    `yaml:"command-topics-in-master-index"`
}

type pluginHelp struct {
	mu    sync.Mutex
	cfg   helpConfig
	added []plugin.HelpTopic
}

// loadHelpConfig writes help.yml when the server has none, and reads it.
// A file that does not parse is reported and replaced by the defaults in
// memory.
func (h *pluginHelp) load() {
	data, err := os.ReadFile(helpFile)
	if os.IsNotExist(err) {
		if err := os.WriteFile(helpFile, []byte(defaultHelpYAML), 0o644); err != nil {
			slog.Warn("write help.yml", "error", err)
		}
		data = []byte(defaultHelpYAML)
	} else if err != nil {
		slog.Warn("read help.yml", "error", err)
		return
	}
	var cfg helpConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		slog.Warn("parse help.yml", "error", err)
		return
	}
	h.cfg = cfg
}

// helpTopic is the one implementation of plugin.HelpTopic the server builds.
type helpTopic struct {
	name, short string
	full        func(plugin.CommandSender) string
	see         func(plugin.CommandSender) bool
}

func (t *helpTopic) Name() string                           { return t.name }
func (t *helpTopic) ShortText() string                      { return t.short }
func (t *helpTopic) FullText(s plugin.CommandSender) string { return t.full(s) }
func (t *helpTopic) CanSee(s plugin.CommandSender) bool     { return t.see(s) }

func seeAll(plugin.CommandSender) bool { return true }

// isHelpConsole tells the server console from every other sender; a command
// block is paged like a player.
func isHelpConsole(s plugin.CommandSender) bool {
	switch s := s.(type) {
	case pluginConsole:
		return true
	case pluginSourceSender:
		_, ok := command.Origin(s.src).(*consoleSource)
		return ok
	}
	return false
}

// seeWith shows a topic to the console, and to others holding
// perm when one is named.
func seeWith(perm string) func(plugin.CommandSender) bool {
	return func(s plugin.CommandSender) bool {
		return isHelpConsole(s) || perm == "" || s.HasPermission(perm)
	}
}

// helpNameLess orders topic names: command topics after the others, each
// group by name ignoring case. vanilla-bukkit: HelpTopicComparator
func helpNameLess(a, b string) int {
	if sa, sb := strings.HasPrefix(a, "/"), strings.HasPrefix(b, "/"); sa != sb {
		if sa {
			return 1
		}
		return -1
	}
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

func sortHelpTopics(topics []plugin.HelpTopic) {
	slices.SortStableFunc(topics, func(a, b plugin.HelpTopic) int { return helpNameLess(a.Name(), b.Name()) })
}

// indexHelpTopic lists topics, one line each. vanilla-bukkit: IndexHelpTopic
func indexHelpTopic(name, short, preamble string, perm string, topics func() []plugin.HelpTopic) *helpTopic {
	return &helpTopic{
		name: name, short: short,
		see: func(s plugin.CommandSender) bool { return perm == "" || s.HasPermission(perm) },
		full: func(sender plugin.CommandSender) string {
			var sb strings.Builder
			if preamble != "" {
				sb.WriteString("§7" + preamble + "\n")
			}
			_, player := sender.(plugin.Player)
			for _, t := range topics() {
				if !t.CanSee(sender) {
					continue
				}
				line := strings.ReplaceAll("§6"+t.Name()+": §f"+t.ShortText(), "\n", ". ")
				if r := []rune(line); player && len(r) > helpPageWidth {
					line = string(r[:helpPageWidth-3]) + "..."
				}
				sb.WriteString(line + "\n")
			}
			return sb.String()
		},
	}
}

// helpCommand is what a command's topic is built from.
type helpCommand struct {
	label, description, usage string
	aliases                   []string
	plugin                    string
	// see decides who is shown the topic; the console always is.
	see func(plugin.CommandSender) bool
}

// commandTopic is a command's own page. vanilla-bukkit: GenericCommandHelpTopic
func commandTopic(c helpCommand) *helpTopic {
	name := "/" + c.label
	short := c.description
	if i := strings.Index(short, "\n"); i > 1 {
		short = short[:i-1] // sic: CraftBukkit cuts one more than the line
	}
	usage := strings.ReplaceAll(strings.ReplaceAll(c.usage, "<command>", c.label), "\n", "\n§f")
	full := "§6Description: §f" + c.description + "\n§6Usage: §f" + usage
	if len(c.aliases) > 0 {
		full += "\n§6Aliases: §f" + strings.Join(c.aliases, ", ")
	}
	return &helpTopic{name: name, short: short, full: func(plugin.CommandSender) string { return full }, see: c.see}
}

// helpSnapshot is the table at one moment: the commands change, so every
// /help builds it again.
type helpSnapshot struct {
	topics map[string]plugin.HelpTopic // by lower-cased name
	index  plugin.HelpTopic
}

func (s *helpSnapshot) topic(name string) plugin.HelpTopic {
	if name == "" {
		return s.index
	}
	if t, ok := s.topics[strings.ToLower(name)]; ok {
		return t
	}
	return nil
}

func (s *helpSnapshot) sorted() []plugin.HelpTopic {
	out := make([]plugin.HelpTopic, 0, len(s.topics))
	for _, t := range s.topics {
		out = append(out, t)
	}
	sortHelpTopics(out)
	return out
}

func applyAmendment(base string, amendment *string) string {
	if amendment == nil {
		return base
	}
	return strings.ReplaceAll(colorAmpersands(*amendment), "<text>", base)
}

// colorAmpersands is ChatColor.translateAlternateColorCodes('&', ...).
func colorAmpersands(s string) string {
	b := []byte(s)
	for i := 0; i+1 < len(b); i++ {
		if b[i] == '&' && strings.IndexByte("0123456789AaBbCcDdEeFfKkLlMmNnOoRrXx", b[i+1]) >= 0 {
			b[i] = 0xC2
			b = slices.Insert(b, i+1, 0xA7)
			i++
		}
	}
	return string(b)
}

func (s *Server) helpSnapshot() *helpSnapshot {
	h := &s.pluginHelp
	h.mu.Lock()
	cfg, added := h.cfg, slices.Clone(h.added)
	h.mu.Unlock()
	ignored := cfg.IgnorePlugins

	snap := &helpSnapshot{topics: make(map[string]plugin.HelpTopic)}
	put := func(t plugin.HelpTopic) { snap.topics[strings.ToLower(t.Name())] = t }
	alias := make(map[string]bool)
	byPlugin := make(map[string][]plugin.HelpTopic)

	s.pluginCommands.mu.Lock()
	var plugins []plugin.CommandInfo
	taken := make(map[string]bool)
	for _, c := range s.pluginCommands.roots {
		plugins = append(plugins, c.info)
		for _, n := range c.roots {
			taken[n.Name] = true
		}
	}
	s.pluginCommands.mu.Unlock()

	// A topic of a command in a plugin's name for the sender who cannot see
	// the command's own is hidden with it.
	var aliasTopic = func(name, target string) {
		t := &helpTopic{name: "/" + name, short: "§eAlias for §f/" + target}
		t.full = func(plugin.CommandSender) string { return t.short }
		t.see = func(sender plugin.CommandSender) bool {
			if to := snap.topics["/"+strings.ToLower(target)]; to != nil {
				return to.CanSee(sender)
			}
			return false
		}
		put(t)
		alias[strings.ToLower(t.name)] = true
	}

	for _, n := range command.ForLevel(command.PermLevelOwners).Children {
		if taken[n.Name] {
			continue
		}
		name := n.Name
		t := commandTopic(helpCommand{label: name, description: "A built-in command.", usage: "/" + name + " <args>", see: func(sender plugin.CommandSender) bool {
			return isHelpConsole(sender) || command.FindLiteralChild(command.ForLevel(sender.OpLevel()), name) != nil
		}})
		put(t)
		byPlugin[builtinHelpGroup] = append(byPlugin[builtinHelpGroup], t)
	}
	if !taken["help"] {
		aliases := []string(nil)
		if !taken["?"] {
			aliases = []string{"?"}
		}
		t := commandTopic(helpCommand{label: "help", description: "Shows the help menu", usage: "/help <pageNumber>\n/help <topic>\n/help <topic> <pageNumber>", aliases: aliases, see: seeAll})
		put(t)
		byPlugin[builtinHelpGroup] = append(byPlugin[builtinHelpGroup], t)
		for _, a := range aliases {
			aliasTopic(a, "help")
		}
	}
	for _, info := range plugins {
		if slices.Contains(ignored, info.Plugin) {
			continue
		}
		var aliases []string
		for _, a := range info.Aliases {
			if a != info.Name {
				aliases = append(aliases, a)
			}
		}
		usage := info.Usage
		if usage == "" {
			usage = "/<command>"
		}
		t := commandTopic(helpCommand{label: info.Name, description: info.Description, usage: usage, aliases: aliases, plugin: info.Plugin, see: seeWith(info.Permission)})
		put(t)
		byPlugin[info.Plugin] = append(byPlugin[info.Plugin], t)
		for _, a := range aliases {
			aliasTopic(a, info.Name)
		}
	}

	var aliasTopics []plugin.HelpTopic
	for _, t := range snap.topics {
		if alias[strings.ToLower(t.Name())] {
			aliasTopics = append(aliasTopics, t)
		}
	}
	sortHelpTopics(aliasTopics)
	if len(aliasTopics) > 0 {
		put(indexHelpTopic("Aliases", "Lists command aliases", "", "", func() []plugin.HelpTopic { return aliasTopics }))
	}
	for name, topics := range byPlugin {
		sortHelpTopics(topics)
		put(indexHelpTopic(name, "All commands for "+name, "Below is a list of all "+name+" commands:", "", func() []plugin.HelpTopic { return topics }))
	}

	for name, g := range cfg.General {
		put(plugin.NewHelpTopic(name, colorAmpersands(g.ShortText), colorAmpersands(g.FullText), g.Permission))
	}
	for name, x := range cfg.Index {
		put(indexHelpTopic(name, colorAmpersands(x.ShortText), colorAmpersands(x.Preamble), x.Permission, func() []plugin.HelpTopic {
			var out []plugin.HelpTopic
			for _, c := range x.Commands {
				if t := snap.topic(c); t != nil {
					out = append(out, t)
				}
			}
			sortHelpTopics(out)
			return out
		}))
	}
	for _, t := range added {
		put(t)
	}

	for name, a := range cfg.Amended {
		t := snap.topics[strings.ToLower(name)]
		if t == nil {
			continue
		}
		old := t
		amended := &helpTopic{name: t.Name(), short: applyAmendment(t.ShortText(), a.ShortText), see: old.CanSee}
		amended.full = func(sender plugin.CommandSender) string { return applyAmendment(old.FullText(sender), a.FullText) }
		if a.Permission != nil {
			amended.see = func(sender plugin.CommandSender) bool { return sender.HasPermission(*a.Permission) }
		}
		put(amended)
	}

	masterCommands := cfg.CommandTopicsInMasterIndex == nil || *cfg.CommandTopicsInMasterIndex
	snap.index = indexHelpTopic("Index", "", "Use /help [n] to get page n of help.", "", func() []plugin.HelpTopic {
		var out []plugin.HelpTopic
		for _, t := range snap.sorted() {
			if alias[strings.ToLower(t.Name())] || (!masterCommands && isCommandTopic(t)) {
				continue
			}
			out = append(out, t)
		}
		return out
	})
	return snap
}

type pluginHelpMap struct{ srv *Server }

func (h *pluginHost) Help() plugin.HelpMap { return pluginHelpMap{h.srv} }

func (m pluginHelpMap) Topic(name string) (plugin.HelpTopic, bool) {
	t := m.srv.helpSnapshot().topic(name)
	return t, t != nil
}
func (m pluginHelpMap) Topics() []plugin.HelpTopic { return m.srv.helpSnapshot().sorted() }
func (m pluginHelpMap) Add(t plugin.HelpTopic) {
	h := &m.srv.pluginHelp
	h.mu.Lock()
	defer h.mu.Unlock()
	h.added = slices.DeleteFunc(h.added, func(o plugin.HelpTopic) bool { return strings.EqualFold(o.Name(), t.Name()) })
	h.added = append(h.added, t)
}

// bukkitHelpRoots are the roots /help runs from: golem:help, and help and ?
// unless a plugin took them.
func (s *Server) bukkitHelpRoots(taken map[string]bool) []*command.Node {
	args := &command.Node{Type: command.NodeArgument, Name: "args", Executable: true, Parser: command.ParserString,
		Properties: func(b *protocol.Buffer) { b.PutVarInt(2) }, Greedy: true, Handler: s.runBukkitHelp}
	root := &command.Node{Type: command.NodeLiteral, Plugin: true, Name: "golem:help", Executable: true, Handler: s.runBukkitHelp, Children: []*command.Node{args}}
	roots := []*command.Node{root}
	for _, name := range []string{"help", "?"} {
		if !taken[name] {
			roots = append(roots, &command.Node{Type: command.NodeLiteral, Plugin: true, Name: name, Redirect: root, Executable: true, Handler: s.runBukkitHelp})
		}
	}
	return roots
}

// runBukkitHelp is golem:help. vanilla-bukkit: HelpCommand.execute
func (s *Server) runBukkitHelp(src command.Source, input string, _ map[string]command.ArgumentValue) (int32, error) {
	sender := s.pluginCommandSender(src)
	args := strings.Fields(input)
	if len(args) > 0 {
		args = args[1:]
	}
	topicName, page := "", 1
	if n := len(args); n > 0 {
		if last := args[n-1]; isDigits(last) {
			topicName = strings.Join(args[:n-1], " ")
			if v, err := strconv.Atoi(last); err == nil && v > 0 {
				page = v
			}
		} else {
			topicName = strings.Join(args, " ")
		}
	}
	width, height := helpPageWidth, helpPageHeight
	if isHelpConsole(sender) {
		width, height = helpUnbounded, helpUnbounded
	}
	send := func(line string) { _ = src.Message(legacyText(line)) }

	snap := s.helpSnapshot()
	topic := snap.topic(topicName)
	if topic == nil {
		topic = snap.topic("/" + topicName)
	}
	if topic == nil {
		topic = helpMatches(snap, topicName)
	}
	if topic == nil || !topic.CanSee(sender) {
		send("§cNo help for " + topicName)
		return 1, nil
	}
	lines, number, total := paginate(topic.FullText(sender), page, width, height)
	header := "§e--------- §fHelp: " + topic.Name() + " "
	if total > 1 {
		header += fmt.Sprintf("(%d/%d) ", number, total)
	}
	header += "§e"
	for i := utf8.RuneCountInString(header); i < helpPageWidth; i++ {
		header += "-"
	}
	send(header)
	for _, line := range lines {
		send(line)
	}
	return 1, nil
}

func isDigits(s string) bool {
	return s != "" && !strings.ContainsFunc(s, func(r rune) bool { return r < '0' || r > '9' })
}

// helpMatches is the Search topic for a name no topic has: the topics that
// start like it, within a few edits. vanilla-bukkit: HelpCommand.findPossibleMatches
func helpMatches(snap *helpSnapshot, search string) plugin.HelpTopic {
	maxDistance := len(search)/5 + 3
	search = strings.TrimPrefix(search, "/")
	if search == "" {
		return nil
	}
	var found []plugin.HelpTopic
	for _, t := range snap.sorted() {
		name := strings.TrimPrefix(t.Name(), "/")
		if len(name) < len(search) || !strings.EqualFold(name[:1], search[:1]) {
			continue
		}
		if editDistance(search, name[:len(search)]) < maxDistance {
			found = append(found, t)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return indexHelpTopic("Search", "", "Search for: "+search, "", func() []plugin.HelpTopic { return found })
}

// editDistance is the Damerau-Levenshtein distance with adjacent swaps.
func editDistance(a, b string) int {
	d := make([][]int, len(a)+1)
	for i := range d {
		d[i] = make([]int, len(b)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && a[i-1] == b[j-2] && a[i-2] == b[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+cost)
			}
		}
	}
	return d[len(a)][len(b)]
}

// paginate cuts text into pages of height lines at most width wide and
// answers the page asked for, clamped, with the page count.
// vanilla-bukkit: ChatPaginator.paginate
func paginate(text string, page, width, height int) (lines []string, number, total int) {
	all := wordWrap(text, width)
	total = len(all) / height
	if len(all)%height != 0 {
		total++
	}
	number = min(page, total)
	from := (number - 1) * height
	return all[from:min(from+height, len(all))], number, total
}

// wordWrap breaks text into lines of width characters, not counting colour
// codes, and carries the colour of a line to the next.
// vanilla-bukkit: ChatPaginator.wordWrap
func wordWrap(text string, width int) []string {
	if len(text) <= width && !strings.Contains(text, "\n") {
		return []string{text}
	}
	raw := []rune(text + " ")
	var word, line []rune
	var lines []string
	colorChars := 0
	// split is String.split("(?<=\\G.{width})"): pieces of width characters.
	split := func(w []rune) []string {
		var out []string
		for len(w) > width {
			out = append(out, string(w[:width]))
			w = w[width:]
		}
		return append(out, string(w))
	}
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c == '§' {
			word = append(word, '§')
			if i+1 < len(raw) {
				word = append(word, raw[i+1])
			}
			colorChars += 2
			i++
			continue
		}
		if c != ' ' && c != '\n' {
			word = append(word, c)
			continue
		}
		switch {
		case len(line) == 0 && len(word) > width:
			lines = append(lines, split(word)...)
		case len(line)+1+len(word)-colorChars == width:
			if len(line) > 0 {
				line = append(line, ' ')
			}
			lines = append(lines, string(append(line, word...)))
			line, colorChars = nil, 0
		case len(line)+1+len(word)-colorChars > width:
			for _, part := range split(word) {
				lines = append(lines, string(line))
				line = []rune(part)
			}
			colorChars = 0
		default:
			if len(line) > 0 {
				line = append(line, ' ')
			}
			line = append(line, word...)
		}
		word = nil
		if c == '\n' {
			lines = append(lines, string(line))
			line = nil
		}
	}
	if len(line) > 0 {
		lines = append(lines, string(line))
	}
	if len(lines) == 0 {
		return []string{"§f"}
	}
	if !strings.HasPrefix(lines[0], "§") {
		lines[0] = "§f" + lines[0]
	}
	for i := 1; i < len(lines); i++ {
		prev := lines[i-1]
		if at := strings.LastIndex(prev, "§"); at >= 0 && !strings.HasPrefix(lines[i], "§") {
			if r := []rune(prev[at+len("§"):]); len(r) > 0 {
				lines[i] = "§" + string(r[0]) + lines[i]
			}
		}
	}
	return lines
}

var legacyColorNames = map[rune]string{
	'0': "black", '1': "dark_blue", '2': "dark_green", '3': "dark_aqua", '4': "dark_red", '5': "dark_purple",
	'6': "gold", '7': "gray", '8': "dark_gray", '9': "blue", 'a': "green", 'b': "aqua", 'c': "red",
	'd': "light_purple", 'e': "yellow", 'f': "white",
}

// legacyText is a § coloured string as the components a client draws; a
// colour code resets the decorations, as in the legacy format.
func legacyText(s string) protocol.Text {
	if !strings.Contains(s, "§") {
		return protocol.Text{Text: s}
	}
	var parts []protocol.Text
	cur := protocol.Text{}
	var text []rune
	flush := func() {
		if len(text) > 0 {
			seg := cur
			seg.Text = string(text)
			parts = append(parts, seg)
			text = nil
		}
	}
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '§' || i+1 >= len(runes) {
			text = append(text, runes[i])
			continue
		}
		i++
		code := runes[i] | 0x20 // lower case
		if name, ok := legacyColorNames[code]; ok {
			flush()
			cur = protocol.Text{Color: name}
		} else if code == 'r' {
			flush()
			cur = protocol.Text{}
		} else if code == 'l' {
			flush()
			cur.Bold = true
		} else if code == 'o' {
			flush()
			cur.Italic = true
		} else if code == 'n' {
			flush()
			cur.Underline = true
		}
	}
	flush()
	return protocol.Text{Extra: parts}
}

// isCommandTopic reports whether t is a command's topic (named "/label").
func isCommandTopic(t plugin.HelpTopic) bool {
	n := t.Name()
	return n != "" && n[0] == '/'
}
