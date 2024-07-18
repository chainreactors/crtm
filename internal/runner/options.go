package runner

import (
	"github.com/chainreactors/crtm/pkg/update"
	updateutils "github.com/projectdiscovery/utils/update"
	"os"
	"path/filepath"

	"github.com/logrusorgru/aurora/v4"
	"github.com/projectdiscovery/goflags"
	"github.com/projectdiscovery/gologger"
	"github.com/projectdiscovery/gologger/formatter"
	"github.com/projectdiscovery/gologger/levels"
	fileutil "github.com/projectdiscovery/utils/file"
)

var (
	// retrieve home directory or fail
	homeDir = func() string {
		home, err := os.UserHomeDir()
		if err != nil {
			gologger.Fatal().Msgf("Failed to get user home directory: %s", err)
		}
		return home
	}()

	defaultConfigLocation = filepath.Join(homeDir, ".config/crtm/config.yaml")
	cacheFile             = filepath.Join(homeDir, ".config/crtm/cache.json")
	defaultPath           = filepath.Join(homeDir, ".crtm/go/bin")
)

var au *aurora.Aurora

// Options contains the configuration options for tuning the enumeration process.
type Options struct {
	ConfigFile string
	Path       string
	NoColor    bool
	SetPath    bool
	UnSetPath  bool

	Install goflags.StringSlice
	Update  goflags.StringSlice
	Remove  goflags.StringSlice

	InstallAll bool
	UpdateAll  bool
	RemoveAll  bool

	Verbose            bool
	Silent             bool
	Version            bool
	ShowPath           bool
	DisableUpdateCheck bool
	DisableChangeLog   bool
}

// ParseOptions parses the command line flags provided by a user
func ParseOptions() *Options {
	options := &Options{}
	flagSet := goflags.NewFlagSet()

	flagSet.SetDescription(`crtm is a simple and easy-to-use golang based tool for managing open source projects from ProjectDiscovery`)

	flagSet.CreateGroup("config", "Config",
		flagSet.StringVar(&options.ConfigFile, "config", defaultConfigLocation, "cli flag configuration file"),
		flagSet.StringVarP(&options.Path, "binary-path", "bp", defaultPath, "custom location to download project binary"),
	)

	flagSet.CreateGroup("install", "Install",
		flagSet.StringSliceVarP(&options.Install, "install", "i", nil, "install single or multiple project by name (comma separated)", goflags.NormalizedStringSliceOptions),
		flagSet.BoolVarP(&options.InstallAll, "install-all", "ia", false, "install all the projects"),
		flagSet.BoolVarP(&options.SetPath, "install-path", "ip", false, "append path to PATH environment variables"),
	)

	flagSet.CreateGroup("update", "Update",
		flagSet.StringSliceVarP(&options.Update, "update", "u", nil, "update single or multiple project by name (comma separated)", goflags.NormalizedStringSliceOptions),
		flagSet.BoolVarP(&options.UpdateAll, "update-all", "ua", false, "update all the projects"),
		flagSet.CallbackVarP(GetUpdateCallback(), "self-update", "up", "update crtm to latest version"),
		flagSet.BoolVarP(&options.DisableUpdateCheck, "disable-update-check", "duc", false, "disable automatic crtm update check"),
	)

	flagSet.CreateGroup("remove", "Remove",
		flagSet.StringSliceVarP(&options.Remove, "remove", "r", nil, "remove single or multiple project by name (comma separated)", goflags.NormalizedStringSliceOptions),
		flagSet.BoolVarP(&options.RemoveAll, "remove-all", "ra", false, "remove all the projects"),
		flagSet.BoolVarP(&options.UnSetPath, "remove-path", "rp", false, "remove path from PATH environment variables"),
	)

	flagSet.CreateGroup("debug", "Debug",
		flagSet.BoolVarP(&options.ShowPath, "show-path", "sp", false, "show the current binary path then exit"),
		flagSet.BoolVar(&options.Version, "version", false, "show version of the project"),
		flagSet.BoolVarP(&options.Verbose, "verbose", "v", false, "show verbose output"),
		flagSet.BoolVarP(&options.NoColor, "no-color", "nc", false, "disable output content coloring (ANSI escape codes)"),
		flagSet.BoolVarP(&options.DisableChangeLog, "dc", "disable-changelog", false, "disable release changelog in output"),
	)

	if err := flagSet.Parse(); err != nil {
		gologger.Fatal().Msgf("%s\n", err)
	}

	// configure aurora for logging
	au = aurora.New(aurora.WithColors(true))

	options.configureOutput()

	//showBanner()

	if options.Version {
		gologger.Info().Msgf("Current Version: %s\n", version)
		os.Exit(0)
	}

	if options.ShowPath {
		// prints default path if not modified
		gologger.Silent().Msg(options.Path)
		os.Exit(0)
	}

	gologger.Info().Msgf("Current crtm version %v", version)
	if !options.DisableUpdateCheck {
		latestVersion, err := update.GetToolVersionCallback("crtm", version)()
		if err != nil {
			if options.Verbose {
				gologger.Error().Msgf("crtm version check failed: %v", err.Error())
			}
		} else {
			gologger.Info().Msgf("Current crtm version %v %v", version, updateutils.GetVersionDescription(version, latestVersion))
		}
	}

	if options.ConfigFile != defaultConfigLocation {
		_ = options.loadConfigFrom(options.ConfigFile)
	}

	return options
}

// configureOutput configures the output on the screen
func (options *Options) configureOutput() {
	// If the user desires verbose output, show verbose output
	if options.Verbose {
		gologger.DefaultLogger.SetMaxLevel(levels.LevelVerbose)
	}
	if options.NoColor {
		gologger.DefaultLogger.SetFormatter(formatter.NewCLI(true))
		au = aurora.New(aurora.WithColors(false))
	}
	if options.Silent {
		gologger.DefaultLogger.SetMaxLevel(levels.LevelSilent)
	}
}

func (options *Options) loadConfigFrom(location string) error {
	return fileutil.Unmarshal(fileutil.YAML, []byte(location), options)
}
