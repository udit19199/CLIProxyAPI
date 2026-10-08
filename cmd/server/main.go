// Package main provides the entry point for the CLI Proxy API server.
// This server acts as a proxy that provides OpenAI/Gemini/Claude compatible API interfaces
// for CLI models, allowing CLI models to be used with tools and libraries designed for standard AI APIs.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	configaccess "github.com/router-for-me/CLIProxyAPI/v8/internal/access/config_access"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/api"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/buildinfo"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/cmd"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/githubauth"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/home"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/homeplugins"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/logging"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/managementasset"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/pluginhost"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/redisqueue"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/safemode"
	_ "github.com/router-for-me/CLIProxyAPI/v8/internal/translator"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/util"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v8/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	sdkpluginstore "github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginstore"
	log "github.com/sirupsen/logrus"
)

var (
	Version           = "dev"
	Commit            = "none"
	BuildDate         = "unknown"
	DefaultConfigPath = ""
)

// init initializes the shared logger setup.
func init() {
	logging.SetupBaseLogger()
	buildinfo.Version = Version
	buildinfo.Commit = Commit
	buildinfo.BuildDate = BuildDate
}

func shouldEnableExampleAPIKeySafeMode(cfg *config.Config, commandMode, cloudConfigMissing, homeMode bool) bool {
	if cfg == nil || commandMode || homeMode || cloudConfigMissing {
		return false
	}
	return safemode.HasExampleAPIKeys(cfg.APIKeys)
}

// main is the entry point of the application.
// It parses command-line flags, loads configuration, and starts the appropriate
// service based on the provided flags (login, codex-login, or server mode).
func main() {
	if len(os.Args) > 1 && os.Args[1] == "discover" {
		discoverFlags := flag.NewFlagSet("discover", flag.ExitOnError)
		timeoutSec := discoverFlags.Int("timeout", 3, "Discovery timeout in seconds")
		jsonOut := discoverFlags.Bool("json", false, "Output in JSON format")
		serviceType := discoverFlags.String("service-type", "", "DNS-SD service type (default _ai-gateway._tcp)")
		configPathFlag := discoverFlags.String("config", DefaultConfigPath, "Configure File Path")
		var include, exclude []string
		discoverFlags.Func("include", "Comma-separated interface names to scan (overrides default physical LAN filter)", appendCSV(&include))
		discoverFlags.Func("exclude", "Comma-separated interface names to skip", appendCSV(&exclude))
		_ = discoverFlags.Parse(os.Args[2:])
		if !*jsonOut {
			fmt.Fprintf(os.Stderr, "CLIProxyAPI Version: %s, Commit: %s, BuiltAt: %s\n", buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate)
		}
		cfgInclude, cfgExclude := cmd.LoadDiscoveryScanFilters(*configPathFlag)
		include, exclude = cmd.ResolveDiscoveryInterfaceFilters(include, exclude, cfgInclude, cfgExclude)
		code := cmd.DoDiscoverWithOptions(cmd.DiscoverOptions{
			Timeout:     time.Duration(*timeoutSec) * time.Second,
			JSONOutput:  *jsonOut,
			ServiceType: *serviceType,
			Include:     include,
			Exclude:     exclude,
		})
		os.Exit(code)
	}

	// For legacy --discover-json flag or JSON requests, keep stdout clean
	isJSONDiscover := argvEnablesBoolFlag(os.Args[1:], "discover-json")
	isDiscoverMode := isJSONDiscover || argvEnablesBoolFlag(os.Args[1:], "discover")
	if !isJSONDiscover {
		fmt.Printf("CLIProxyAPI Version: %s, Commit: %s, BuiltAt: %s\n", buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate)
	}

	// Command-line flags to control the application's behavior.
	var codexLogin bool
	var codexDeviceLogin bool
	var claudeLogin bool
	var noBrowser bool
	var oauthCallbackPort int
	var xaiLogin bool
	var discoverGateways bool
	var discoverTimeout int
	var discoverJSON bool
	var discoverServiceType string
	var discoverInclude []string
	var discoverExclude []string
	var configPath string
	var password string
	var homeJWT string
	var homeDisableClusterDiscovery bool
	var localModel bool

	// Define command-line flags for different operation modes.
	flag.BoolVar(&codexLogin, "codex-login", false, "Login to Codex using OAuth")
	flag.BoolVar(&codexDeviceLogin, "codex-device-login", false, "Login to Codex using device code flow")
	flag.BoolVar(&claudeLogin, "claude-login", false, "Login to Claude using OAuth")
	flag.BoolVar(&noBrowser, "no-browser", false, "Don't open browser automatically for OAuth")
	flag.IntVar(&oauthCallbackPort, "oauth-callback-port", 0, "Override OAuth callback port (defaults to provider-specific port)")
	flag.BoolVar(&xaiLogin, "xai-login", false, "Login to xAI using OAuth")
	flag.BoolVar(&discoverGateways, "discover", false, "Discover local AI gateways and CPA instances on the LAN")
	flag.IntVar(&discoverTimeout, "discover-timeout", 3, "Timeout in seconds for LAN discovery (default 3s)")
	flag.BoolVar(&discoverJSON, "discover-json", false, "Output discovered gateways in JSON format")
	flag.StringVar(&discoverServiceType, "discover-service-type", "", "DNS-SD service type for LAN discovery (default _ai-gateway._tcp)")
	flag.Func("discover-include", "Comma-separated interface names to scan during LAN discovery", appendCSV(&discoverInclude))
	flag.Func("discover-exclude", "Comma-separated interface names to skip during LAN discovery", appendCSV(&discoverExclude))
	flag.StringVar(&configPath, "config", DefaultConfigPath, "Configure File Path")
	flag.StringVar(&password, "password", "", "")
	flag.StringVar(&homeJWT, "home-jwt", "", "Home control plane JWT for mTLS certificate bootstrap and connection")
	flag.BoolVar(&homeDisableClusterDiscovery, "home-disable-cluster-discovery", false, "Disable Home CLUSTER NODES discovery and keep using the configured -home-jwt address")
	flag.BoolVar(&localModel, "local-model", false, "Use embedded model catalogs unless models.catalog or models.codex-catalog explicitly overrides the source")

	flag.CommandLine.Usage = func() {
		out := flag.CommandLine.Output()
		_, _ = fmt.Fprintf(out, "Usage of %s\n", os.Args[0])
		flag.CommandLine.VisitAll(func(f *flag.Flag) {
			if f.Name == "password" {
				return
			}
			s := fmt.Sprintf("  -%s", f.Name)
			name, unquoteUsage := flag.UnquoteUsage(f)
			if name != "" {
				s += " " + name
			}
			if len(s) <= 4 {
				s += "	"
			} else {
				s += "\n    "
			}
			if unquoteUsage != "" {
				s += unquoteUsage
			}
			if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" {
				s += fmt.Sprintf(" (default %s)", f.DefValue)
			}
			_, _ = fmt.Fprint(out, s+"\n")
		})
	}

	pluginHost := pluginhost.New()
	if !isDiscoverMode {
		if bootstrapCfg := loadPluginBootstrapConfig(pluginBootstrapConfigPath(os.Args[1:], DefaultConfigPath)); bootstrapCfg != nil {
			pluginHost.ApplyConfig(context.Background(), bootstrapCfg)
			pluginHost.RegisterCommandLineFlags(context.Background(), flag.CommandLine)
		}
	}

	// Parse the command-line flags.
	flag.Parse()

	if discoverGateways || discoverJSON {
		cfgInclude, cfgExclude := cmd.LoadDiscoveryScanFilters(configPath)
		include, exclude := cmd.ResolveDiscoveryInterfaceFilters(discoverInclude, discoverExclude, cfgInclude, cfgExclude)
		code := cmd.DoDiscoverWithOptions(cmd.DiscoverOptions{
			Timeout:     time.Duration(discoverTimeout) * time.Second,
			JSONOutput:  discoverJSON,
			ServiceType: discoverServiceType,
			Include:     include,
			Exclude:     exclude,
		})
		os.Exit(code)
	}

	// Core application variables.
	var err error
	var cfg *config.Config
	var isCloudDeploy bool
	var configLoadedFromHome bool
	var homeClient *home.Client
	var homePluginSyncReport homeplugins.SyncReport
	var homePluginStatusReady bool

	wd, err := os.Getwd()
	if err != nil {
		log.Errorf("failed to get working directory: %v", err)
		return
	}

	// Load environment variables from .env if present.
	if errLoad := godotenv.Load(filepath.Join(wd, ".env")); errLoad != nil {
		if !errors.Is(errLoad, os.ErrNotExist) {
			log.WithError(errLoad).Warn("failed to load .env file")
		}
	}

	lookupEnv := func(keys ...string) (string, bool) {
		for _, key := range keys {
			if value, ok := os.LookupEnv(key); ok {
				if trimmed := strings.TrimSpace(value); trimmed != "" {
					return trimmed, true
				}
			}
		}
		return "", false
	}
	if strings.TrimSpace(homeJWT) == "" {
		if v, ok := lookupEnv("HOME_JWT", "home_jwt"); ok {
			homeJWT = v
		}
	}

	// Check for cloud deploy mode only on first execution
	// Read env var name in uppercase: DEPLOY
	deployEnv := os.Getenv("DEPLOY")
	if deployEnv == "cloud" {
		isCloudDeploy = true
	}

	// Determine and load the configuration file.
	// Prefer the Postgres store when configured, otherwise fallback to git or local files.
	var configFilePath string
	if strings.TrimSpace(homeJWT) != "" {
		configLoadedFromHome = true
		ctxHome, cancelHome := context.WithTimeout(context.Background(), 30*time.Second)
		homeCfg, errHomeCfg := home.ConfigFromJWT(ctxHome, homeJWT)
		cancelHome()
		if errHomeCfg != nil {
			log.Errorf("invalid -home-jwt: %v", errHomeCfg)
			return
		}
		if homeDisableClusterDiscovery {
			homeCfg.DisableClusterDiscovery = true
		}
		homeClient = home.New(homeCfg)
		defer func() {
			if homeClient != nil {
				homeClient.Close()
			}
		}()

		ctxHomeConfig, cancelHomeConfig := context.WithTimeout(context.Background(), 30*time.Second)
		raw, errGetConfig := homeClient.GetConfig(ctxHomeConfig)
		cancelHomeConfig()
		if errGetConfig != nil {
			log.Errorf("failed to fetch config from home: %v", errGetConfig)
			return
		}

		parsed, errParseConfig := config.ParseConfigBytes(raw)
		if errParseConfig != nil {
			log.Errorf("failed to parse config payload from home: %v", errParseConfig)
			return
		}
		if parsed == nil {
			parsed = &config.Config{}
		}
		parsed.Home = homeCfg
		parsed.Port = config.NormalizeHomePort(parsed.Port)
		parsed.UsageStatisticsEnabled = true
		pluginSyncCfg := *parsed
		parsed.Plugins.StoreAuth = nil
		var errHomePlugins error
		platform := homeplugins.CurrentPlatform()
		if pluginSyncCfg.Plugins.Enabled {
			ctxHomePlugins, cancelHomePlugins := context.WithTimeout(context.Background(), 30*time.Second)
			installedVersions, errInstalledPlugins := homeplugins.InstalledVersions(&pluginSyncCfg)
			if errInstalledPlugins != nil {
				homePluginStatusReady = true
				errHomePlugins = errInstalledPlugins
				homePluginSyncReport = homeplugins.CompletedSyncReport(platform, errInstalledPlugins)
			} else {
				pluginSyncRequest := sdkpluginstore.PluginSyncRequest{
					SchemaVersion:     sdkpluginstore.PluginSyncSchemaVersion,
					GOOS:              platform.GOOS,
					GOARCH:            platform.GOARCH,
					InstalledVersions: installedVersions,
				}
				pluginSyncResponse, errFetchPlugins := homeClient.GetPluginSync(ctxHomePlugins, pluginSyncRequest)
				errHomePlugins = errFetchPlugins
				switch {
				case errHomePlugins == nil:
					homePluginStatusReady = true
					homePluginSyncReport, errHomePlugins = homeplugins.SyncResolvedWithReport(ctxHomePlugins, &pluginSyncCfg, pluginSyncResponse.Items, pluginSyncResponse.ExpiresAt, pluginSyncRequest.InstalledVersions, pluginHost)
				case errors.Is(errHomePlugins, home.ErrPluginSyncUnsupported):
					homePluginStatusReady = true
					homePluginSyncReport, errHomePlugins = homeplugins.SyncWithReport(ctxHomePlugins, &pluginSyncCfg, pluginHost)
				default:
					homePluginStatusReady = true
					homePluginSyncReport = homeplugins.CompletedSyncReport(platform, errHomePlugins)
				}
				pluginSyncRequest.Clear()
				pluginSyncResponse.Clear()
			}
			cancelHomePlugins()
		} else {
			homePluginStatusReady = true
			homePluginSyncReport = homeplugins.CompletedSyncReport(platform, nil)
		}
		if errHomePlugins != nil {
			log.Errorf("failed to sync plugins from home: %v", errHomePlugins)
		}
		if homePluginStatusReady {
			errReportPlugins := home.ReportPluginStatus(context.Background(), homeClient, homeCfg.NodeID, homePluginSyncReport)
			if errReportPlugins != nil {
				log.Warnf("failed to report home plugin sync status: %v", errReportPlugins)
			}
		}
		if errHomePlugins != nil {
			return
		}
		cfg = parsed

		// Keep a non-empty config path for downstream components (log paths, management assets, etc),
		// but do not require the file to exist when loading config from home.
		if strings.TrimSpace(configPath) != "" {
			configFilePath = configPath
		} else {
			configFilePath = filepath.Join(wd, "config.yaml")
		}
	} else if configPath != "" {
		configFilePath = configPath
		cfg, err = config.LoadConfigOptional(configPath, isCloudDeploy)
	} else {
		wd, err = os.Getwd()
		if err != nil {
			log.Errorf("failed to get working directory: %v", err)
			return
		}
		configFilePath = filepath.Join(wd, "config.yaml")
		cfg, err = config.LoadConfigOptional(configFilePath, isCloudDeploy)
	}
	if err != nil {
		log.Errorf("failed to load config: %v", err)
		return
	}
	if cfg == nil {
		cfg = &config.Config{}
	}

	// In cloud deploy mode, check if we have a valid configuration
	var configFileExists bool
	if isCloudDeploy {
		if configLoadedFromHome && cfg != nil {
			configFileExists = cfg.Port != 0
		} else {
			if info, errStat := os.Stat(configFilePath); errStat != nil {
				// Don't mislead: API server will not start until configuration is provided.
				log.Info("Cloud deploy mode: No configuration file detected; standing by for configuration")
				configFileExists = false
			} else if info.IsDir() {
				log.Info("Cloud deploy mode: Config path is a directory; standing by for configuration")
				configFileExists = false
			} else if cfg.Port == 0 {
				// LoadConfigOptional returns empty config when file is empty or invalid.
				// Config file exists but is empty or invalid; treat as missing config
				log.Info("Cloud deploy mode: Configuration file is empty or invalid; standing by for valid configuration")
				configFileExists = false
			} else {
				log.Info("Cloud deploy mode: Configuration file detected; starting service")
				configFileExists = true
			}
		}
	}
	redisqueue.SetUsageStatisticsEnabled(cfg.UsageStatisticsEnabled)
	redisqueue.SetRetentionSeconds(cfg.RedisUsageQueueRetentionSeconds)
	coreauth.SetQuotaCooldownDisabled(cfg.DisableCooling)
	coreauth.SetTransientErrorCooldownSeconds(cfg.TransientErrorCooldownSeconds)

	if err = logging.ConfigureLogOutput(cfg); err != nil {
		log.Errorf("failed to configure log output: %v", err)
		return
	}

	log.Infof("CLIProxyAPI Version: %s, Commit: %s, BuiltAt: %s", buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate)

	// Set the log level based on the configuration.
	util.SetLogLevel(cfg)

	if resolvedAuthDir, errResolveAuthDir := util.ResolveAuthDir(cfg.AuthDir); errResolveAuthDir != nil {
		log.Errorf("failed to resolve auth directory: %v", errResolveAuthDir)
		return
	} else {
		cfg.AuthDir = resolvedAuthDir
	}
	githubauth.SetToken(cfg.GitHubToken)
	managementasset.SetCurrentConfig(cfg)

	commandMode := codexLogin || codexDeviceLogin || claudeLogin || xaiLogin

	// Create login options to be used in authentication flows.
	options := &cmd.LoginOptions{
		NoBrowser:    noBrowser,
		CallbackPort: oauthCallbackPort,
	}

	cloudConfigMissing := isCloudDeploy && !configFileExists
	homeMode := configLoadedFromHome || (cfg != nil && cfg.Home.Enabled)
	exampleAPIKeySafeMode := shouldEnableExampleAPIKeySafeMode(cfg, commandMode, cloudConfigMissing, homeMode)
	serverOptions := []api.ServerOption(nil)
	if exampleAPIKeySafeMode {
		matches := safemode.ExampleAPIKeys(cfg.APIKeys)
		log.WithField("api_keys", strings.Join(matches, ",")).Error("unsafe example API key configured; proxy API endpoints disabled until api-keys is updated")
		serverOptions = append(serverOptions, api.WithExampleAPIKeySafeMode())
	}

	// Register the shared token store once so all components use the same persistence backend.
	sdkAuth.RegisterTokenStore(sdkAuth.NewFileTokenStore())

	// Register built-in access providers before constructing services.
	configaccess.Register(&cfg.SDKConfig)
	pluginHost.ApplyConfig(context.Background(), cfg)
	if configLoadedFromHome && homePluginStatusReady {
		errHomePluginLoad := homeplugins.MarkLoadResults(&homePluginSyncReport, pluginHost)
		errReportPlugins := home.ReportPluginStatus(context.Background(), homeClient, cfg.Home.NodeID, homePluginSyncReport)
		if errHomePluginLoad != nil {
			log.Errorf("failed to load home plugins: %v", errHomePluginLoad)
		}
		if errReportPlugins != nil {
			log.Warnf("failed to report home plugin load status: %v", errReportPlugins)
		}
		if errHomePluginLoad != nil {
			return
		}
	}
	if homeClient != nil {
		// The bootstrap client is not owned by the runtime service. Close it after
		// the final startup report so it cannot retain an idle RESP connection.
		homeClient.Close()
		homeClient = nil
	}
	if pluginHost.HasTriggeredCommandLineFlags() {
		if exitCode, handled := pluginHost.ExecuteCommandLine(context.Background(), os.Args[0], os.Args[1:], configFilePath, flag.CommandLine); handled {
			if exitCode != 0 {
				os.Exit(exitCode)
			}
			return
		}
	}

	// Handle different command modes based on the provided flags.

	if codexLogin {
		// Handle Codex login
		cmd.DoCodexLogin(cfg, options)
	} else if codexDeviceLogin {
		// Handle Codex device-code login
		cmd.DoCodexDeviceLogin(cfg, options)
	} else if claudeLogin {
		// Handle Claude login
		cmd.DoClaudeLogin(cfg, options)
	} else if xaiLogin {
		cmd.DoXAILogin(cfg, options)
	} else {
		// In cloud deploy mode without config file, just wait for shutdown signals
		if isCloudDeploy && !configFileExists {
			// No config file available, just wait for shutdown
			cmd.WaitForCloudDeploy()
			return
		}
		if localModel {
			log.Info("Local model mode: using embedded catalogs unless an explicit catalog source is configured")
		}
		// Start the main proxy service
		managementasset.StartAutoUpdater(context.Background(), configFilePath)
		registry.SetLocalModelCatalogs(localModel)
		cmd.StartServiceWithPluginHost(cfg, configFilePath, password, pluginHost, serverOptions...)
	}
}

func pluginBootstrapConfigPath(args []string, defaultPath string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			return defaultPluginBootstrapConfigPath(defaultPath)
		case arg == "-config" || arg == "--config":
			if i+1 < len(args) {
				return args[i+1]
			}
			return defaultPluginBootstrapConfigPath(defaultPath)
		case strings.HasPrefix(arg, "-config="):
			return strings.TrimPrefix(arg, "-config=")
		case strings.HasPrefix(arg, "--config="):
			return strings.TrimPrefix(arg, "--config=")
		}
	}
	return defaultPluginBootstrapConfigPath(defaultPath)
}

func defaultPluginBootstrapConfigPath(defaultPath string) string {
	if strings.TrimSpace(defaultPath) != "" {
		return defaultPath
	}
	wd, errGetwd := os.Getwd()
	if errGetwd != nil {
		return "config.yaml"
	}
	return filepath.Join(wd, "config.yaml")
}

func loadPluginBootstrapConfig(path string) *config.Config {
	raw, errReadFile := os.ReadFile(path)
	if errReadFile != nil {
		if !errors.Is(errReadFile, os.ErrNotExist) {
			log.Warnf("failed to read plugin bootstrap config: %v", errReadFile)
		}
		cfg := &config.Config{}
		cfg.NormalizePluginsConfig()
		return cfg
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		cfg := &config.Config{}
		cfg.NormalizePluginsConfig()
		return cfg
	}
	cfg, errParseConfig := config.ParseConfigBytes(raw)
	if errParseConfig != nil {
		log.Warnf("failed to parse plugin bootstrap config: %v", errParseConfig)
		cfg = &config.Config{}
		cfg.NormalizePluginsConfig()
		return cfg
	}
	return cfg
}

func appendCSV(dst *[]string) func(string) error {
	return func(raw string) error {
		*dst = append(*dst, cmd.ParseInterfaceList(raw)...)
		return nil
	}
}

func argvEnablesBoolFlag(args []string, name string) bool {
	enabled := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			break
		}
		flagName, value, hasValue := splitArgvFlag(arg)
		if flagName == name {
			if !hasValue {
				enabled = true
			} else if parsed, errParse := strconv.ParseBool(value); errParse == nil {
				enabled = parsed
			}
		}
		if !hasValue && argvFlagConsumesValue(flagName) {
			if i+1 < len(args) && args[i+1] != "--" {
				i++
			}
		}
	}
	return enabled
}

func argvFlagConsumesValue(name string) bool {
	switch name {
	case "codex-login", "codex-device-login", "claude-login", "no-browser",
		"xai-login",
		"discover", "discover-json", "home-disable-cluster-discovery",
		"local-model":
		return false
	default:
		return name != ""
	}
}

func splitArgvFlag(arg string) (name, value string, hasValue bool) {
	if !strings.HasPrefix(arg, "-") {
		return "", "", false
	}
	arg = strings.TrimPrefix(arg, "-")
	arg = strings.TrimPrefix(arg, "-")
	name, value, hasValue = strings.Cut(arg, "=")
	return name, value, hasValue
}
