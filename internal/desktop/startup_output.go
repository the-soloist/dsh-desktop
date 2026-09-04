package desktop

import (
	"log"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

const startupOutputHistoryLimit = 12

var authenticationTokenPattern = regexp.MustCompile(`(?i)([?&]token=)[^&\s]+`)

type startupOutputRecorder struct {
	logger                 *log.Logger
	onLine                 func(string)
	mu                     sync.Mutex
	pending                string
	recent                 []string
	peerDependencyWarnings int
}

func newStartupOutputRecorder(logger *log.Logger, onLine func(string)) *startupOutputRecorder {
	return &startupOutputRecorder{logger: logger, onLine: onLine}
}

func (recorder *startupOutputRecorder) Write(data []byte) (int, error) {
	recorder.mu.Lock()
	combined := recorder.pending + string(data)
	parts := strings.Split(combined, "\n")
	recorder.pending = parts[len(parts)-1]
	lines := make([]string, 0, len(parts)-1)
	for _, part := range parts[:len(parts)-1] {
		if line := recorder.recordLineLocked(part); line != "" {
			lines = append(lines, line)
		}
	}
	recorder.mu.Unlock()

	if recorder.onLine != nil {
		for _, line := range lines {
			recorder.onLine(line)
		}
	}
	return len(data), nil
}

// Flush records a final unterminated line and any suppressed warning summary.
func (recorder *startupOutputRecorder) Flush() {
	recorder.mu.Lock()
	line := recorder.recordLineLocked(recorder.pending)
	recorder.pending = ""
	recorder.flushWarningsLocked()
	recorder.mu.Unlock()
	if line != "" && recorder.onLine != nil {
		recorder.onLine(line)
	}
}

func (recorder *startupOutputRecorder) recordLineLocked(value string) string {
	line := strings.TrimSpace(strings.TrimSuffix(value, "\r"))
	if line == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(line), "warn: incorrect peer dependency") {
		recorder.peerDependencyWarnings++
		return ""
	}
	if strings.EqualFold(line, "Saved lockfile") {
		return ""
	}
	recorder.flushWarningsLocked()
	sanitized := redactSensitiveOutput(line)
	recorder.logger.Printf("[dsh] %s", sanitized)
	recorder.recent = append(recorder.recent, sanitized)
	if overflow := len(recorder.recent) - startupOutputHistoryLimit; overflow > 0 {
		recorder.recent = append([]string(nil), recorder.recent[overflow:]...)
	}
	return line
}

func (recorder *startupOutputRecorder) flushWarningsLocked() {
	if recorder.peerDependencyWarnings == 0 {
		return
	}
	recorder.logger.Printf("[dsh] 已省略 %d 条 peer dependency 警告", recorder.peerDependencyWarnings)
	recorder.peerDependencyWarnings = 0
}

func (recorder *startupOutputRecorder) recentOutput() string {
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	recent := append([]string(nil), recorder.recent...)
	if pending := strings.TrimSpace(recorder.pending); pending != "" {
		if !strings.HasPrefix(strings.ToLower(pending), "warn: incorrect peer dependency") && !strings.EqualFold(pending, "Saved lockfile") {
			recent = append(recent, redactSensitiveOutput(pending))
		}
	}
	if overflow := len(recent) - startupOutputHistoryLimit; overflow > 0 {
		recent = recent[overflow:]
	}
	return strings.Join(recent, "\n")
}

func redactSensitiveOutput(value string) string {
	return authenticationTokenPattern.ReplaceAllString(value, "${1}<redacted>")
}

func dshWebURL(line, expectedURL string) (string, bool) {
	const marker = "dsh web:"
	markerIndex := strings.Index(strings.ToLower(line), marker)
	if markerIndex < 0 {
		return "", false
	}
	fields := strings.Fields(strings.TrimSpace(line[markerIndex+len(marker):]))
	if len(fields) == 0 {
		return "", false
	}
	candidate, err := url.Parse(fields[0])
	if err != nil || candidate.User != nil {
		return "", false
	}
	expected, err := url.Parse(expectedURL)
	if err != nil || !strings.EqualFold(candidate.Scheme, expected.Scheme) || !strings.EqualFold(candidate.Host, expected.Host) {
		return "", false
	}
	return candidate.String(), true
}

func hasDSHAuthenticationToken(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && strings.TrimSpace(parsed.Query().Get("token")) != ""
}

func dshOutputSummary(line string) (key, status, detail string, ok bool) {
	normalised := strings.ToLower(strings.TrimSpace(line))
	switch {
	case strings.Contains(normalised, "resolving dependencies"):
		return "dependencies-resolving", "正在准备 DSH 依赖", "正在解析并检查运行依赖…", true
	case strings.HasPrefix(normalised, "resolved"):
		return "dependencies-ready", "DSH 依赖已就绪", "运行依赖已经准备完成。", true
	case strings.Contains(normalised, "dsh web:"):
		return "web-listening", "DSH Web 服务已启动", "已获取本地服务地址，正在建立认证会话…", true
	default:
		return "", "", "", false
	}
}
