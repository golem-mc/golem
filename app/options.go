package app

import (
	"errors"
	"flag"
	"log/slog"
	"os"

	"github.com/golem-mc/golem/internal/config"
)

// errStartFailed ends a run whose failure has already been logged.
var errStartFailed = errors.New("failed to start")

// options are the command-line options. vanilla: Main.main
type options struct {
	flags *flag.FlagSet

	nogui, initSettings, demo, bonusChest, safeMode, help     bool
	forceUpgrade, eraseCache, recreateRegionFiles, jfrProfile bool
	universe, world, serverID, pidFile                        string
	// port is -1 when not given, leaving server-port as it is.
	port int
}

// parseOptions reads args, the command line without the program name. A
// word that is not an option (vanilla's non-options, nogui among them) is
// skipped rather than ending the parse, and either - or -- starts an option.
func parseOptions(args []string) (options, error) {
	fs := flag.NewFlagSet("golem", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	o := options{flags: fs}
	// Accepted so vanilla start scripts run unchanged; there is no GUI.
	fs.BoolVar(&o.nogui, "nogui", false, "")
	fs.BoolVar(&o.initSettings, "initSettings", false, "Initializes 'server.properties', then quits")
	fs.BoolVar(&o.demo, "demo", false, "")
	fs.BoolVar(&o.bonusChest, "bonusChest", false, "")
	fs.BoolVar(&o.forceUpgrade, "forceUpgrade", false, "")
	fs.BoolVar(&o.eraseCache, "eraseCache", false, "")
	fs.BoolVar(&o.recreateRegionFiles, "recreateRegionFiles", false, "")
	fs.BoolVar(&o.safeMode, "safeMode", false, "Loads level with vanilla datapack only")
	fs.BoolVar(&o.jfrProfile, "jfrProfile", false, "")
	fs.BoolVar(&o.help, "help", false, "")
	fs.StringVar(&o.universe, "universe", ".", "")
	fs.StringVar(&o.world, "world", "", "")
	fs.StringVar(&o.serverID, "serverId", "", "")
	fs.StringVar(&o.pidFile, "pidFile", "", "")
	fs.IntVar(&o.port, "port", -1, "")
	for {
		if err := fs.Parse(args); err != nil {
			return o, err
		}
		if fs.NArg() == 0 {
			return o, nil
		}
		args = fs.Args()[1:]
	}
}

// apply puts the options into cfg and warns about those this server only
// accepts.
func (o options) apply(cfg *config.Config) {
	if o.world != "" {
		cfg.LevelName = o.world
	}
	cfg.Universe = o.universe
	// vanilla: DedicatedServer.initServer keeps server-port for a negative port
	if o.port >= 0 {
		cfg.ServerPort = o.port
	}
	cfg.SafeMode = o.safeMode
	cfg.ServerID = o.serverID
	cfg.Demo = o.demo
	cfg.BonusChest = o.bonusChest
	cfg.ForceUpgrade, cfg.EraseCache, cfg.RecreateRegionFiles = o.forceUpgrade, o.eraseCache, o.recreateRegionFiles
	if o.jfrProfile {
		slog.Warn("JFR profiling is not available; set PPROF_ADDR to profile")
	}
}
