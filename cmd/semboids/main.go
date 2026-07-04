// Command semboids hosts the flock simulation on the SemStreams substrate:
// the sim input component ticks the physics engine and publishes frames;
// the websocket output streams them to the browser. Mirrors the downstream
// host shape in the sibling semdragons repo.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	_ "net/http/pprof" // Register pprof handlers on DefaultServeMux (served by service.MaybeStartPProf)
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/c360studio/semstreams/component"
	"github.com/c360studio/semstreams/config"
	"github.com/c360studio/semstreams/metric"
	"github.com/c360studio/semstreams/natsclient"
	"github.com/c360studio/semstreams/payloadbuiltins"
	"github.com/c360studio/semstreams/payloadregistry"
	"github.com/c360studio/semstreams/service"
	"github.com/c360studio/semstreams/types"
	"github.com/joho/godotenv"

	"github.com/c360studio/semboids/componentregistry"
	"github.com/c360studio/semboids/internal/api"
	"github.com/c360studio/semboids/internal/zone"
)

const appName = "semboids"

// Version metadata injected at build time via -ldflags.
var (
	// Version is the release version.
	Version = "dev"
	// BuildTime is the build timestamp.
	BuildTime = "unknown"
)

// CLIConfig holds parsed command-line flags.
type CLIConfig struct {
	ConfigPath      string
	LogLevel        string
	LogFormat       string
	Debug           bool
	DebugPort       int
	Validate        bool
	ShowVersion     bool
	ShutdownTimeout time.Duration

	// Sim overrides (0 = use config value).
	Boids  int
	TickHz float64
	Seed   uint64
	// Zones toggles zone steering (false strips zones for a plain run).
	Zones bool
	// GraphHz overrides the snapshot cadence (-1 = use config).
	GraphHz float64
}

func main() {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			_, _ = fmt.Fprintf(os.Stderr, "PANIC: %v\nStack trace:\n%s\n", r, string(buf[:n]))
			os.Exit(2)
		}
	}()

	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// .env is optional; production injects env via the OS environment.
	_ = godotenv.Load()

	cliCfg, shouldExit := parseCLI()
	if shouldExit {
		return nil
	}

	// Start pprof before NATS so a wedged boot stays profilable (ADR-058).
	service.MaybeStartPProf(cliCfg.Debug, cliCfg.DebugPort)

	cfg, err := loadConfig(cliCfg.ConfigPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	applySimOverrides(cfg, cliCfg)

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	if cliCfg.Validate {
		fmt.Println("Configuration is valid")
		return nil
	}

	ctx := context.Background()
	natsClient, err := connectToNATS(ctx, cfg)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		natsClient.Close(closeCtx)
	}()

	// Ensure JetStream streams (system streams + the ENTITY stream that
	// carries zone entities to graph-ingest).
	streamsManager := config.NewStreamsManager(natsClient, slog.Default())
	if err := streamsManager.EnsureStreams(ctx, cfg); err != nil {
		return fmt.Errorf("ensure streams: %w", err)
	}

	logger := setupLogger(cliCfg.LogLevel, cliCfg.LogFormat)
	slog.SetDefault(logger)
	slog.Info("SemBoids ready", "version", Version, "build_time", BuildTime)

	metricsRegistry := metric.NewMetricsRegistry()
	platform := extractPlatformMeta(cfg)

	configManager, err := config.NewConfigManager(cfg, natsClient, logger)
	if err != nil {
		return fmt.Errorf("create config manager: %w", err)
	}
	if err := configManager.Start(ctx); err != nil {
		return fmt.Errorf("start config manager: %w", err)
	}
	defer configManager.Stop(5 * time.Second)

	componentRegistry, manager, err := setupRegistriesAndManager(cfg)
	if err != nil {
		return err
	}

	payloadReg := payloadregistry.New()
	if err := payloadbuiltins.Register(payloadReg); err != nil {
		return fmt.Errorf("register builtin payloads: %w", err)
	}
	if err := zone.RegisterPayloads(payloadReg); err != nil {
		return fmt.Errorf("register zone payloads: %w", err)
	}

	svcDeps := &service.Dependencies{
		NATSClient:        natsClient,
		MetricsRegistry:   metricsRegistry,
		Logger:            logger,
		Platform:          platform,
		Manager:           configManager,
		ComponentRegistry: componentRegistry,
		PayloadRegistry:   payloadReg,
	}

	if err := configureAndCreateServices(cfg, manager, svcDeps); err != nil {
		return err
	}

	// Land the configured zones in the graph (ADR-001: zones are real
	// entities from day one). graph-ingest consumes them once it starts —
	// JetStream retains the publishes.
	if zones := simZones(cfg); len(zones) > 0 {
		if err := zone.Ingest(ctx, natsClient, zones, platform.Org, platform.Platform); err != nil {
			return fmt.Errorf("ingest zones: %w", err)
		}
		slog.Info("Zones published to graph", "count", len(zones))
	}

	return runWithSignalHandling(ctx, manager, cliCfg.ShutdownTimeout)
}

// parseCLI parses flags; the bool result requests immediate clean exit.
func parseCLI() (*CLIConfig, bool) {
	cliCfg := &CLIConfig{}
	flag.StringVar(&cliCfg.ConfigPath, "config", "configs/flock.json", "Path to flow configuration")
	flag.StringVar(&cliCfg.LogLevel, "log-level", "info", "Log level (debug|info|warn|error)")
	flag.StringVar(&cliCfg.LogFormat, "log-format", "text", "Log format (text|json)")
	flag.BoolVar(&cliCfg.Debug, "debug", false, "Enable debug mode (pprof server)")
	flag.IntVar(&cliCfg.DebugPort, "debug-port", 6060, "pprof server port (with --debug)")
	flag.BoolVar(&cliCfg.Validate, "validate", false, "Validate configuration and exit")
	flag.BoolVar(&cliCfg.ShowVersion, "version", false, "Show version and exit")
	flag.DurationVar(&cliCfg.ShutdownTimeout, "shutdown-timeout", 10*time.Second, "Graceful shutdown timeout")
	flag.IntVar(&cliCfg.Boids, "boids", 0, "Override boid population (0 = use config)")
	flag.Float64Var(&cliCfg.TickHz, "tick-hz", 0, "Override tick rate in Hz (0 = use config)")
	flag.Uint64Var(&cliCfg.Seed, "seed", 0, "Override sim seed (0 = use config)")
	flag.BoolVar(&cliCfg.Zones, "zones", true, "Enable zone steering (false strips configured zones)")
	flag.Float64Var(&cliCfg.GraphHz, "graph-hz", -1, "Override graph snapshot cadence in Hz (-1 = use config; 0 disables)")
	flag.Parse()

	if cliCfg.ShowVersion {
		fmt.Printf("%s version %s\n", appName, Version)
		return cliCfg, true
	}
	if cliCfg.Debug {
		cliCfg.LogLevel = "debug"
	}
	return cliCfg, false
}

// loadConfig reads and parses the flow configuration.
func loadConfig(path string) (*config.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	loader := config.NewLoader()
	cfg, err := loader.LoadFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

// applySimOverrides patches the sim component config with CLI overrides so
// quick experiments don't require editing the flow file.
func applySimOverrides(cfg *config.Config, cliCfg *CLIConfig) {
	if cliCfg.Boids <= 0 && cliCfg.TickHz <= 0 && cliCfg.Seed == 0 && cliCfg.Zones && cliCfg.GraphHz < 0 {
		return
	}
	simCfg, ok := cfg.Components["sim"]
	if !ok {
		return
	}

	var raw map[string]any
	if err := json.Unmarshal(simCfg.Config, &raw); err != nil {
		slog.Warn("cannot parse sim config for overrides", "error", err)
		return
	}
	if cliCfg.Boids > 0 {
		raw["boids"] = cliCfg.Boids
	}
	if cliCfg.TickHz > 0 {
		raw["tick_hz"] = cliCfg.TickHz
	}
	if cliCfg.Seed != 0 {
		raw["seed"] = cliCfg.Seed
	}
	if !cliCfg.Zones {
		delete(raw, "zones")
	}
	if cliCfg.GraphHz >= 0 {
		raw["graph_hz"] = cliCfg.GraphHz
	}
	patched, err := json.Marshal(raw)
	if err != nil {
		slog.Warn("cannot marshal sim config overrides", "error", err)
		return
	}
	simCfg.Config = patched
	cfg.Components["sim"] = simCfg
}

// simZones extracts the zone set from the sim component config — the single
// source of truth shared by the sim and boot-time graph ingestion.
func simZones(cfg *config.Config) []zone.Zone {
	simCfg, ok := cfg.Components["sim"]
	if !ok {
		return nil
	}
	var parsed struct {
		Zones []zone.Zone `json:"zones"`
	}
	if err := json.Unmarshal(simCfg.Config, &parsed); err != nil {
		slog.Warn("cannot parse sim config for zones", "error", err)
		return nil
	}
	return parsed.Zones
}

// connectToNATS connects to NATS. NATS is a hard requirement.
func connectToNATS(ctx context.Context, cfg *config.Config) (*natsclient.Client, error) {
	fmt.Print("Connecting to NATS... ")

	natsURLs := "nats://localhost:4222"
	if envURL := os.Getenv("SEMBOIDS_NATS_URLS"); envURL != "" {
		natsURLs = envURL
	} else if envURL := os.Getenv("SEMSTREAMS_NATS_URLS"); envURL != "" {
		natsURLs = envURL
	} else if len(cfg.NATS.URLs) > 0 {
		natsURLs = strings.Join(cfg.NATS.URLs, ",")
	}

	natsClient, err := natsclient.NewClient(natsURLs)
	if err != nil {
		fmt.Println("FAILED")
		return nil, fmt.Errorf("create NATS client: %w", err)
	}
	if err := natsClient.Connect(ctx); err != nil {
		fmt.Println("FAILED")
		return nil, fmt.Errorf("connect to NATS: %w", err)
	}

	connCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := natsClient.WaitForConnection(connCtx); err != nil {
		fmt.Println("FAILED")
		return nil, fmt.Errorf("NATS connection timeout: %w", err)
	}

	fmt.Println("OK")
	return natsClient, nil
}

// setupLogger creates a structured logger.
func setupLogger(level, format string) *slog.Logger {
	logLevel := parseLogLevel(level)
	opts := &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: logLevel == slog.LevelDebug,
	}
	var handler slog.Handler
	switch strings.ToLower(format) {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	default:
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// extractPlatformMeta extracts platform identity from config.
func extractPlatformMeta(cfg *config.Config) types.PlatformMeta {
	platformID := cfg.Platform.InstanceID
	if platformID == "" {
		platformID = cfg.Platform.ID
	}
	return types.PlatformMeta{
		Org:      cfg.Platform.Org,
		Platform: platformID,
	}
}

// setupRegistriesAndManager creates registries and the service manager.
func setupRegistriesAndManager(cfg *config.Config) (*component.Registry, *service.Manager, error) {
	componentRegistry := component.NewRegistry()
	if err := componentregistry.RegisterAll(componentRegistry); err != nil {
		return nil, nil, fmt.Errorf("register components: %w", err)
	}
	factories := componentRegistry.ListFactories()
	slog.Info("Component factories registered", "count", len(factories), "factories", factories)

	serviceRegistry := service.NewServiceRegistry()
	if err := service.RegisterAll(serviceRegistry); err != nil {
		return nil, nil, fmt.Errorf("register semstreams services: %w", err)
	}
	if err := serviceRegistry.Register(api.ServiceName, api.New); err != nil {
		return nil, nil, fmt.Errorf("register boids service: %w", err)
	}

	manager := service.NewServiceManager(serviceRegistry)
	ensureServiceManagerConfig(cfg)
	return componentRegistry, manager, nil
}

// ensureServiceManagerConfig ensures service-manager config exists with defaults.
func ensureServiceManagerConfig(cfg *config.Config) {
	if cfg.Services == nil {
		cfg.Services = make(types.ServiceConfigs)
	}
	if _, exists := cfg.Services["service-manager"]; !exists {
		defaultConfig := map[string]any{
			"http_port":  8080,
			"swagger_ui": true,
			"server_info": map[string]string{
				"title":       "SemBoids API",
				"description": "Reynolds boids simulator on the SemStreams substrate",
				"version":     Version,
			},
		}
		defaultConfigJSON, _ := json.Marshal(defaultConfig)
		cfg.Services["service-manager"] = types.ServiceConfig{
			Name:    "service-manager",
			Enabled: true,
			Config:  defaultConfigJSON,
		}
	}
}

// configureAndCreateServices configures the manager and creates all services.
func configureAndCreateServices(
	cfg *config.Config,
	manager *service.Manager,
	svcDeps *service.Dependencies,
) error {
	if err := manager.ConfigureFromServices(cfg.Services, svcDeps); err != nil {
		return fmt.Errorf("configure service manager: %w", err)
	}

	for name, svcConfig := range cfg.Services {
		if name == "service-manager" {
			continue
		}
		if !svcConfig.Enabled {
			slog.Info("Service disabled in config", "name", name)
			continue
		}
		if !manager.HasConstructor(name) {
			slog.Warn("Service configured but not registered",
				"key", name,
				"available_constructors", manager.ListConstructors())
			continue
		}
		if _, err := manager.CreateService(name, svcConfig.Config, svcDeps); err != nil {
			return fmt.Errorf("create service %s: %w", name, err)
		}
		slog.Info("Created service", "name", name)
	}
	return nil
}

// runWithSignalHandling starts services and handles shutdown signals.
func runWithSignalHandling(ctx context.Context, manager *service.Manager, shutdownTimeout time.Duration) error {
	signalCtx, signalCancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer signalCancel()

	slog.Info("Starting all services")
	if err := manager.StartAll(signalCtx); err != nil {
		return fmt.Errorf("start services: %w", err)
	}
	slog.Info("All services started")

	<-signalCtx.Done()
	slog.Info("Received shutdown signal")

	if err := manager.StopAll(shutdownTimeout); err != nil {
		slog.Error("Error stopping services", "error", err)
		return fmt.Errorf("graceful shutdown failed: %w", err)
	}

	slog.Info("SemBoids shutdown complete")
	return nil
}
