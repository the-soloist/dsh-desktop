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
	startupNavigateEvent      = "startup:navigate"
)

//go:embed startup
var startupAssets embed.FS

type startupStatus struct {
	Status    string `json:"status"`
	Detail    string `json:"detail"`
	StartedAt string `json:"startedAt"`
	Failed    bool   `json:"failed"`
	Navigate  bool   `json:"navigate"`
	Code      bool   `json:"code,omitempty"`
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
	return &startupTimeline{steps: []startupStatus{newStartupStatus(status, detail, false, false)}}
}

func newStartupStatus(status, detail string, failed, navigate bool) startupStatus {
	return startupStatus{
		Status:    status,
		Detail:    detail,
		StartedAt: time.Now().Format(time.RFC3339Nano),
		Failed:    failed,
		Navigate:  navigate,
	}
}

func (timeline *startupTimeline) append(status, detail string, failed, navigate bool) startupUpdate {
	return timeline.appendStep(newStartupStatus(status, detail, failed, navigate))
}

func (timeline *startupTimeline) appendCommand(status, command string) startupUpdate {
	step := newStartupStatus(status, command, false, false)
	step.Code = true
	return timeline.appendStep(step)
}

func (timeline *startupTimeline) appendStep(step startupStatus) startupUpdate {
	timeline.mu.Lock()
	defer timeline.mu.Unlock()
	timeline.steps = append(timeline.steps, step)
	return startupUpdate{Steps: []startupStatus{step}}
}

func (timeline *startupTimeline) reset(status, detail string, failed bool) startupUpdate {
	timeline.mu.Lock()
	defer timeline.mu.Unlock()
	step := newStartupStatus(status, detail, failed, false)
	timeline.steps = []startupStatus{step}
	return startupUpdate{Reset: true, Steps: []startupStatus{step}}
}

func (timeline *startupTimeline) snapshot() startupUpdate {
	timeline.mu.Lock()
	defer timeline.mu.Unlock()
	steps := append([]startupStatus(nil), timeline.steps...)
	return startupUpdate{Reset: true, Steps: steps}
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
