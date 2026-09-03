package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"path/filepath"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
	dshdesktop "github.com/the-soloist/dsh-desktop"
)

func main() {
	input := flag.String("input", "internal/appicon/dsh-desktop-icon.png", "source PNG")
	icoOutput := flag.String("ico", "dist/intermediate/windows/DshDesktop.ico", "ICO output")
	resourceOutput := flag.String("syso", "dist/intermediate/windows/rsrc_windows_amd64.syso", "COFF resource output")
	architecture := flag.String("arch", "amd64", "target architecture")
	flag.Parse()

	if err := generateResources(*input, *icoOutput, *resourceOutput, *architecture); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generateResources(inputPath, icoPath, resourcePath, architecture string) error {
	applicationVersion, err := dshdesktop.CurrentVersion()
	if err != nil {
		return fmt.Errorf("parse embedded VERSION: %w", err)
	}
	input, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open icon: %w", err)
	}
	image, err := png.Decode(input)
	closeErr := input.Close()
	if err != nil {
		return fmt.Errorf("decode icon: %w", err)
	}
	if closeErr != nil {
		return closeErr
	}

	icon, err := winres.NewIconFromResizedImage(image, []int{256, 128, 64, 48, 32, 16})
	if err != nil {
		return fmt.Errorf("create Windows icon: %w", err)
	}
	if err = os.MkdirAll(filepath.Dir(icoPath), 0o755); err != nil {
		return err
	}
	ico, err := os.Create(icoPath)
	if err != nil {
		return err
	}
	if err = icon.SaveICO(ico); err != nil {
		_ = ico.Close()
		return fmt.Errorf("write ICO: %w", err)
	}
	if err = ico.Close(); err != nil {
		return err
	}

	resourceSet := winres.ResourceSet{}
	if err = resourceSet.SetIcon(winres.Name("APPICON"), icon); err != nil {
		return fmt.Errorf("add icon resource: %w", err)
	}
	resourceSet.SetManifest(winres.AppManifest{
		Description:         "Desktop client for DeepSeek DSH",
		Compatibility:       winres.Win10AndAbove,
		ExecutionLevel:      winres.AsInvoker,
		DPIAwareness:        winres.DPIPerMonitorV2,
		LongPathAware:       true,
		UseCommonControlsV6: true,
	})
	resourceVersion := [4]uint16{
		applicationVersion.Major,
		applicationVersion.Minor,
		applicationVersion.Patch,
		0,
	}
	versionInfo := version.Info{
		FileVersion:    resourceVersion,
		ProductVersion: resourceVersion,
	}
	versionInfo.Set(0, version.ProductName, "DSH Desktop")
	versionInfo.Set(0, version.FileDescription, "Desktop client for DeepSeek DSH")
	versionInfo.Set(0, version.OriginalFilename, "DshDesktop.exe")
	resourceSet.SetVersionInfo(versionInfo)

	arch, ok := map[string]winres.Arch{
		"amd64": winres.ArchAMD64,
		"arm64": winres.ArchARM64,
	}[architecture]
	if !ok {
		return fmt.Errorf("unsupported architecture: %s", architecture)
	}
	if err = os.MkdirAll(filepath.Dir(resourcePath), 0o755); err != nil {
		return err
	}
	resource, err := os.Create(resourcePath)
	if err != nil {
		return err
	}
	if err = resourceSet.WriteObject(resource, arch); err != nil {
		_ = resource.Close()
		return fmt.Errorf("write COFF resource: %w", err)
	}
	return resource.Close()
}
