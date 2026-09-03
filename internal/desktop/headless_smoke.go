package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"

	dshdesktop "github.com/the-soloist/dsh-desktop"
	"github.com/the-soloist/dsh-desktop/internal/applog"
	"github.com/the-soloist/dsh-desktop/internal/backend"
	"github.com/the-soloist/dsh-desktop/internal/dshenv"
)

func runHeadlessSmokeTest() (result error) {
	metadata, err := dshdesktop.CurrentMetadata()
	if err != nil {
		return fmt.Errorf("cannot determine application metadata: %w", err)
	}
	version, err := dshdesktop.CurrentVersion()
	if err != nil {
		return fmt.Errorf("cannot determine application version: %w", err)
	}
	logPath, err := applog.Path(metadata.InternalName)
	if err != nil {
		return fmt.Errorf("cannot resolve the launcher log path: %w", err)
	}
	logger, logFile, err := applog.New(logPath)
	if err != nil {
		return fmt.Errorf("cannot initialise the launcher log: %w", err)
	}
	defer logFile.Close()
	logger.Printf("starting headless smoke test for %s %s", metadata.DisplayName, version)

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
	defer func() {
		if closeErr := supervisor.Close(); closeErr != nil {
			result = errors.Join(result, closeErr)
		}
	}()

	switch supervisor.Probe(context.Background()) {
	case backend.ProbeReady:
		logger.Printf("headless smoke test reused DSH at %s", metadata.DSHURL)
		return nil
	case backend.ProbeUnexpected:
		return fmt.Errorf("%s is occupied by a non-DSH service", metadata.DSHURL)
	}

	runtimeEnvironment, err := dshenv.Resolve(os.Environ())
	if err != nil {
		return fmt.Errorf("cannot resolve the DSH runtime environment: %w", err)
	}
	logger.Printf("headless smoke test using bunx: %s", runtimeEnvironment.BunxPath)
	logger.Printf("headless smoke test using Node.js: %s", runtimeEnvironment.NodePath)
	process, err := supervisor.Start(
		context.Background(),
		runtimeEnvironment.BunxPath,
		runtimeEnvironment.Workspace,
		runtimeEnvironment.Environment,
		logger.Writer(),
	)
	if err != nil {
		return fmt.Errorf("cannot start DSH: %w", err)
	}
	if err = supervisor.WaitForReady(context.Background(), process, startTimeout()); err != nil {
		return fmt.Errorf("DSH did not become ready: %w", err)
	}
	logger.Printf("headless smoke test reached %s", metadata.DSHURL)
	return nil
}
