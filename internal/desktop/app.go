package desktop

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	dshdesktop "github.com/the-soloist/dsh-desktop"
	"github.com/the-soloist/dsh-desktop/internal/appicon"
	"github.com/the-soloist/dsh-desktop/internal/applog"
	"github.com/the-soloist/dsh-desktop/internal/backend"
	"github.com/the-soloist/dsh-desktop/internal/windowstate"
)

const (
	defaultWidth       = 1200
	defaultHeight      = 780
	minimumWidth       = 800
	minimumHeight      = 560
	defaultStartWait   = 5 * time.Minute
	readinessInterval  = 250 * time.Millisecond
	readinessStability = time.Second
	requestTimeout     = 2 * time.Second
	processStopTimeout = 5 * time.Second
)

// Main starts the desktop application and reports fatal startup errors using a
// platform-native warning dialog.
func Main() {
	if headlessSmokeTestEnabled() {
		if err := runHeadlessSmokeTest(); err != nil {
			log.Printf("headless smoke test failed: %s", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		message := err.Error()
		log.Printf("fatal: %s", message)
		if smokeTestEnabled() {
			os.Exit(1)
		}
		title := "Application"
		if metadata, metadataErr := dshdesktop.CurrentMetadata(); metadataErr == nil {
			title = metadata.DisplayName
		}
		showPlatformWarning(title, message)
		os.Exit(1)
	}
}

func run() error {
	metadata, err := dshdesktop.CurrentMetadata()
	if err != nil {
		return fmt.Errorf("cannot determine application metadata: %w", err)
	}
	version, err := dshdesktop.CurrentVersion()
	if err != nil {
		return fmt.Errorf("cannot determine application version: %w", err)
	}

	startupConsoleOwned := ownsStartupConsole()
	logPath, err := applog.Path(metadata.InternalName)
	if err != nil {
		return fmt.Errorf("cannot resolve the launcher log path: %w", err)
	}
	logger, logFile, err := applog.New(logPath)
	if err != nil {
		return fmt.Errorf("cannot initialise the launcher log: %w", err)
	}
	defer logFile.Close()
	logger.Printf("starting %s %s on %s/%s", metadata.DisplayName, version, runtime.GOOS, runtime.GOARCH)

	statePath, err := windowstate.Path(metadata.InternalName)
	if err != nil {
		return fmt.Errorf("cannot resolve the window-state path: %w", err)
	}
	stateStore := windowstate.New(statePath, windowstate.Bounds{
		DefaultWidth:  defaultWidth,
		DefaultHeight: defaultHeight,
		MinimumWidth:  minimumWidth,
		MinimumHeight: minimumHeight,
	}, logger)

	app := application.New(application.Options{
		Name:        metadata.InternalName,
		Description: fmt.Sprintf("%s\nVersion %s", metadata.Description, version),
		Icon:        appicon.ApplicationPNG,
		Assets: application.AssetOptions{
			Handler:        startupAssetHandler(metadata.DisplayName),
			DisableLogging: true,
		},
		Mac:     application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true},
		Linux: application.LinuxOptions{
			DisableQuitOnLastWindowClosed: true,
			ProgramName:                   metadata.DisplayName,
		},
	})
	window := newWindowManager(app, stateStore, metadata.DisplayName, logger)
	supervisor := backend.NewSupervisor(backend.Config{
		Package:            metadata.DSHPackage,
		URL:                metadata.DSHURL,
		PageMarker:         metadata.DSHPageMarker,
		ReadinessInterval:  readinessInterval,
		ReadinessStability: readinessStability,
		RequestTimeout:     requestTimeout,
		StopTimeout:        processStopTimeout,
		Logger:             logger,
	})
	controller := newController(app, window, supervisor, metadata, logger, startupConsoleOwned)
	controller.bind()

	if err = app.Run(); err != nil {
		return fmt.Errorf("desktop window failed: %w", err)
	}
	logger.Printf("application stopped")
	return controller.smokeFailure()
}

func startTimeout() time.Duration {
	return durationFromSeconds("DSH_START_TIMEOUT_SECONDS", defaultStartWait)
}

func smokeTestEnabled() bool {
	if value := strings.TrimSpace(os.Getenv("DSH_SMOKE_TEST")); value == "1" || strings.EqualFold(value, "true") {
		return true
	}
	for _, argument := range os.Args[1:] {
		if argument == "--smoke-test" {
			return true
		}
	}
	return false
}

func headlessSmokeTestEnabled() bool {
	value := strings.TrimSpace(os.Getenv("DSH_HEADLESS_SMOKE_TEST"))
	return value == "1" || strings.EqualFold(value, "true")
}

func smokeTestDuration() time.Duration {
	return durationFromSeconds("DSH_SMOKE_TEST_SECONDS", 8*time.Second)
}

func durationFromSeconds(name string, fallback time.Duration) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || seconds < 1 {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}
