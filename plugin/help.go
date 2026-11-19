package plugin

// HelpMap is the table of topics /help answers: one per command, one per
// plugin, the index and what help.yml adds. Bukkit: HelpMap
type HelpMap interface {
	// Topic answers the topic of this name, ignoring case: "" is the index,
	// "/warp" a command's, and a plugin's own name its commands'.
	Topic(name string) (HelpTopic, bool)
	// Topics lists every topic except the index, the ones whose names do not
	// start with a slash first, each group by name. Bukkit: getHelpTopics
	Topics() []HelpTopic
	// Add adds a topic, replacing the one of its name. Bukkit: addTopic
	Add(t HelpTopic)
}

// HelpTopic is one page of /help. Bukkit: HelpTopic
type HelpTopic interface {
	Name() string
	// ShortText is the line the index shows.
	ShortText() string
	// FullText is what /help <name> pages through, with § colour codes.
	FullText(sender CommandSender) string
	// CanSee tells whether sender is shown the topic.
	CanSee(sender CommandSender) bool
}

// NewHelpTopic answers a topic of fixed text, visible to senders with
// permission (everyone when it is empty). Bukkit: CustomHelpTopic
func NewHelpTopic(name, shortText, fullText, permission string) HelpTopic {
	return customHelpTopic{name, shortText, shortText + "\n" + fullText, permission}
}

type customHelpTopic struct{ name, short, full, permission string }

func (t customHelpTopic) Name() string                         { return t.name }
func (t customHelpTopic) ShortText() string                    { return t.short }
func (t customHelpTopic) FullText(sender CommandSender) string { return t.full }
func (t customHelpTopic) CanSee(sender CommandSender) bool {
	return t.permission == "" || sender.HasPermission(t.permission)
}
