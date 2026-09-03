// Package applog creates the desktop launcher log and bounds its disk usage.
package applog

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const (
	defaultMaximumSize = 5 * 1024 * 1024
	defaultBackups     = 3
)

// Path returns the launcher log path, honoring DSH_LAUNCHER_LOG.
func Path(applicationName string) (string, error) {
	if configured := strings.TrimSpace(os.Getenv("DSH_LAUNCHER_LOG")); configured != "" {
		return filepath.Abs(configured)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Logs", applicationName, "launcher.log"), nil
	case "windows":
		base := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
		if base == "" {
			base = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(base, applicationName, "launcher.log"), nil
	default:
		base := strings.TrimSpace(os.Getenv("XDG_STATE_HOME"))
		if base == "" {
			base = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(base, "dsh-desktop", "launcher.log"), nil
	}
}

// New creates a timestamped logger that rotates on size and mirrors output to
// stdout when one is available.
func New(path string) (*log.Logger, io.Closer, error) {
	writer, err := newRotatingWriter(path, defaultMaximumSize, defaultBackups)
	if err != nil {
		return nil, nil, err
	}
	writers := []io.Writer{writer}
	if _, statErr := os.Stdout.Stat(); statErr == nil {
		writers = append(writers, os.Stdout)
	}
	logger := log.New(io.MultiWriter(writers...), "", log.Ldate|log.Ltime|log.Lmicroseconds)
	return logger, writer, nil
}

type rotatingWriter struct {
	path        string
	maximumSize int64
	backups     int
	mu          sync.Mutex
	file        *os.File
	size        int64
}

func newRotatingWriter(path string, maximumSize int64, backups int) (*rotatingWriter, error) {
	if maximumSize <= 0 || backups < 1 {
		return nil, fmt.Errorf("invalid log rotation settings")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	writer := &rotatingWriter{path: path, maximumSize: maximumSize, backups: backups}
	if err := writer.open(); err != nil {
		return nil, err
	}
	return writer, nil
}

func (writer *rotatingWriter) open() error {
	file, err := os.OpenFile(writer.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	writer.file = file
	writer.size = info.Size()
	return nil
}

func (writer *rotatingWriter) Write(data []byte) (int, error) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.file == nil {
		return 0, os.ErrClosed
	}
	if writer.size > 0 && writer.size+int64(len(data)) > writer.maximumSize {
		if err := writer.rotate(); err != nil {
			return 0, err
		}
	}
	written, err := writer.file.Write(data)
	writer.size += int64(written)
	return written, err
}

func (writer *rotatingWriter) Close() error {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.file == nil {
		return nil
	}
	err := writer.file.Close()
	writer.file = nil
	return err
}

func (writer *rotatingWriter) rotate() error {
	if err := writer.file.Close(); err != nil {
		return err
	}
	writer.file = nil
	oldest := fmt.Sprintf("%s.%d", writer.path, writer.backups)
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return err
	}
	for index := writer.backups - 1; index >= 1; index-- {
		from := fmt.Sprintf("%s.%d", writer.path, index)
		to := fmt.Sprintf("%s.%d", writer.path, index+1)
		if err := os.Rename(from, to); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(writer.path, writer.path+".1"); err != nil && !os.IsNotExist(err) {
		return err
	}
	return writer.open()
}
