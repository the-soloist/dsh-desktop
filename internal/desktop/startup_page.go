package desktop

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/the-soloist/dsh-desktop/internal/appicon"
)

const (
	startupUpdateEvent        = "startup:update"
	startupFrontendReadyEvent = "startup:frontend-ready"
	startupRetryEvent         = "startup:retry"
	startupCopyEvent          = "startup:copy-diagnostics"
	startupCopiedEvent        = "startup:diagnostics-copied"
)

type startupPhase string

const (
	startupPreparing  startupPhase = "preparing"
	startupVersion    startupPhase = "version"
	startupLaunching  startupPhase = "launching"
	startupConnecting startupPhase = "connecting"
	startupFailed     startupPhase = "failed"
	startupStopped    startupPhase = "stopped"
)

//go:embed startup
var startupAssets embed.FS

type startupStatus struct {
	Phase     startupPhase `json:"phase,omitempty"`
	Status    string       `json:"status"`
	Detail    string       `json:"detail"`
	StartedAt string       `json:"startedAt"`
	Summary   string       `json:"summary,omitempty"`
	Code      bool         `json:"code,omitempty"`
}

type startupUpdate struct {
	Reset bool            `json:"reset"`
	Steps []startupStatus `json:"steps"`
}

type startupTimeline struct {
	mu    sync.Mutex
	steps []startupStatus
}

type startupIntent uint8

const (
	startupIntentNone startupIntent = iota
	startupIntentInitial
	startupIntentRestart
)

func newStartupTimeline(status, detail string) *startupTimeline {
	return &startupTimeline{steps: []startupStatus{newStartupStatus(startupPreparing, status, detail)}}
}

func newStartupStatus(phase startupPhase, status, detail string) startupStatus {
	return startupStatus{
		Phase:     phase,
		Status:    redactSensitiveOutput(status),
		Detail:    redactSensitiveOutput(detail),
		StartedAt: time.Now().Format(time.RFC3339Nano),
	}
}

func (timeline *startupTimeline) append(phase startupPhase, status, detail string) startupUpdate {
	return timeline.appendStep(newStartupStatus(phase, status, detail))
}

func (timeline *startupTimeline) appendCommand(status, command string) startupUpdate {
	step := newStartupStatus(startupLaunching, status, command)
	step.Code = true
	return timeline.appendStep(step)
}

func (timeline *startupTimeline) appendStep(step startupStatus) startupUpdate {
	timeline.mu.Lock()
	defer timeline.mu.Unlock()
	timeline.steps = append(timeline.steps, step)
	return startupUpdate{Steps: []startupStatus{step}}
}

func (timeline *startupTimeline) reset(phase startupPhase, status, detail string) startupUpdate {
	timeline.mu.Lock()
	defer timeline.mu.Unlock()
	step := newStartupStatus(phase, status, detail)
	if phase == startupFailed || phase == startupStopped {
		step.Summary, _, _ = strings.Cut(step.Detail, "\n")
	}
	timeline.steps = []startupStatus{step}
	return startupUpdate{Reset: true, Steps: []startupStatus{step}}
}

func (timeline *startupTimeline) snapshot() startupUpdate {
	timeline.mu.Lock()
	defer timeline.mu.Unlock()
	steps := append([]startupStatus(nil), timeline.steps...)
	return startupUpdate{Reset: true, Steps: steps}
}

func (timeline *startupTimeline) fail(summary, detail string) startupUpdate {
	step := newStartupStatus(startupFailed, "DSH 启动失败", detail)
	step.Summary = redactSensitiveOutput(summary)
	return timeline.appendStep(step)
}

func (timeline *startupTimeline) diagnostics() string {
	var text strings.Builder
	for _, step := range timeline.snapshot().Steps {
		fmt.Fprintf(&text, "%s %s\n", step.StartedAt, step.Status)
		if step.Detail != "" {
			fmt.Fprintf(&text, "%s\n", step.Detail)
		}
	}
	return text.String()
}

func startupAssetHandler(applicationName string) http.Handler {
	frontend, err := fs.Sub(startupAssets, "startup")
	if err != nil {
		panic(fmt.Errorf("cannot prepare startup assets: %w", err))
	}
	assets := application.BundledAssetFileServer(frontend)
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/" || request.URL.Path == "/index.html" {
			page, readErr := fs.ReadFile(frontend, "index.html")
			if readErr != nil {
				http.Error(response, "startup page unavailable", http.StatusInternalServerError)
				return
			}
			response.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = response.Write([]byte(strings.ReplaceAll(string(page), "{{APPLICATION_NAME}}", applicationName)))
			return
		}
		if request.URL.Path == "/logo.png" {
			response.Header().Set("Content-Type", "image/png")
			response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			_, _ = response.Write(appicon.PNG)
			return
		}
		assets.ServeHTTP(response, request)
	})
}
