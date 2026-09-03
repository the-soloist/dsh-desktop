package desktop

import (
	"io"
	"strings"
	"sync"
)

const startupOutputHistoryLimit = 12

type startupOutputRecorder struct {
	destination io.Writer
	onLine      func(string)
	mu          sync.Mutex
	pending     string
	recent      []string
}

func newStartupOutputRecorder(destination io.Writer, onLine func(string)) *startupOutputRecorder {
	return &startupOutputRecorder{destination: destination, onLine: onLine}
}

func (recorder *startupOutputRecorder) Write(data []byte) (int, error) {
	written, err := recorder.destination.Write(data)
	if written == 0 {
		return written, err
	}

	recorder.mu.Lock()
	combined := recorder.pending + string(data[:written])
	parts := strings.Split(combined, "\n")
	recorder.pending = parts[len(parts)-1]
	lines := make([]string, 0, len(parts)-1)
	for _, part := range parts[:len(parts)-1] {
		line := strings.TrimSpace(strings.TrimSuffix(part, "\r"))
		if line == "" {
			continue
		}
		lines = append(lines, line)
		recorder.recent = append(recorder.recent, line)
		if overflow := len(recorder.recent) - startupOutputHistoryLimit; overflow > 0 {
			recorder.recent = append([]string(nil), recorder.recent[overflow:]...)
		}
	}
	recorder.mu.Unlock()

	if recorder.onLine != nil {
		for _, line := range lines {
			recorder.onLine(line)
		}
	}
	return written, err
}

func (recorder *startupOutputRecorder) recentOutput() string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recent := append([]string(nil), recorder.recent...)
	if pending := strings.TrimSpace(recorder.pending); pending != "" {
		recent = append(recent, pending)
	}
	if overflow := len(recent) - startupOutputHistoryLimit; overflow > 0 {
		recent = recent[overflow:]
	}
	return strings.Join(recent, "\n")
}

func dshOutputSummary(line, dshURL string) (key, status, detail string, ok bool) {
	normalised := strings.ToLower(strings.TrimSpace(line))
	switch {
	case strings.Contains(normalised, "resolving dependencies"):
		return "dependencies-resolving", "正在准备 DSH 依赖", "正在解析并检查运行依赖…", true
	case strings.HasPrefix(normalised, "resolved"):
		return "dependencies-ready", "DSH 依赖已就绪", "运行依赖已经准备完成。", true
	case strings.Contains(normalised, "dsh web:"):
		return "web-listening", "DSH Web 服务已启动", "本地服务已开始监听 " + dshURL + "。", true
	default:
		return "", "", "", false
	}
}
