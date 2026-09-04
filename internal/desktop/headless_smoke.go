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
	"github.com/the-soloist/dsh-desktop/internal/npmregistry"
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
	logger.Printf("[smoke] %s %s", metadata.DisplayName, version)

	supervisor := newBackendSupervisor(metadata, logger)
	defer func() {
		if closeErr := supervisor.Close(); closeErr != nil {
			result = errors.Join(result, closeErr)
		}
	}()

	switch supervisor.Probe(context.Background()) {
	case backend.ProbeReady:
		logger.Printf("[smoke] reused DSH at %s", metadata.DSHURL)
		return nil
	case backend.ProbeAuthenticationRequired:
		return fmt.Errorf("%s is served by an authenticated external DSH process", metadata.DSHURL)
	case backend.ProbeUnexpected:
		return fmt.Errorf("%s is occupied by a non-DSH service", metadata.DSHURL)
	}

	runtimeEnvironment, err := dshenv.Resolve(os.Environ())
	if err != nil {
		return fmt.Errorf("cannot resolve the DSH runtime environment: %w", err)
	}
	logger.Printf("[smoke] runner=%s (%s), node=%s", runtimeEnvironment.Runner.Name, runtimeEnvironment.Runner.Path, runtimeEnvironment.NodePath)
	registryClient, err := npmregistry.NewClient(runtimeEnvironment.RegistryURL, nil)
	if err != nil {
		return fmt.Errorf("cannot configure the npm registry: %w", err)
	}
	latestVersion, err := registryClient.LatestVersion(context.Background(), metadata.DSHPackage)
	if err != nil {
		return fmt.Errorf("cannot resolve the latest DSH version: %w", err)
	}
	packageReference := npmregistry.ExactReference(metadata.DSHPackage, latestVersion)
	logger.Printf("[smoke] package=%s", packageReference)
	output := newStartupOutputRecorder(logger, nil)
	defer output.Flush()
	process, err := supervisor.Start(
		context.Background(),
		runtimeEnvironment.Runner.Path,
		packageReference,
		runtimeEnvironment.Workspace,
		runtimeEnvironment.Environment,
		output,
	)
	if err != nil {
		return fmt.Errorf("cannot start DSH: %w", err)
	}
	if err = supervisor.WaitForReady(context.Background(), process, startTimeout()); err != nil {
		return fmt.Errorf("DSH did not become ready: %w", err)
	}
	output.Flush()
	logger.Printf("[smoke] DSH ready at %s", metadata.DSHURL)
	return nil
}
