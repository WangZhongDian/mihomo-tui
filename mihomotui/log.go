package mihomotui

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	logger *slog.Logger
	logW   *logWriter
	logMu  sync.Mutex
)

// logWriter 实现 io.Writer，支持按日自动切换日志文件
type logWriter struct {
	mu   sync.Mutex
	dir  string
	date string
	file *os.File
}

func (w *logWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return 0, fmt.Errorf("日志文件未打开")
	}

	now := time.Now().Format("20060102")
	if now != w.date {
		if err := w.rotate(now); err != nil {
			return 0, err
		}
	}

	return w.file.Write(p)
}

func (w *logWriter) rotate(date string) error {
	if w.file != nil {
		w.file.Close()
	}
	logPath := filepath.Join(w.dir, fmt.Sprintf("mihomo-tui-%s.log", date))
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	w.file = f
	w.date = date
	return nil
}

func parseSlogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// InitLogger 初始化日志系统
func InitLogger(dir, level string) error {
	logMu.Lock()
	defer logMu.Unlock()

	if dir == "" {
		dir = filepath.Join(GetConfigDir(), "logs")
	}
	// 如果指定目录不可写，回退到默认目录
	if err := os.MkdirAll(dir, 0755); err != nil {
		fallback := filepath.Join(GetConfigDir(), "logs")
		if fallback != dir {
			if err2 := os.MkdirAll(fallback, 0755); err2 == nil {
				dir = fallback
			} else {
				return fmt.Errorf("创建日志目录失败: %w", err)
			}
		} else {
			return fmt.Errorf("创建日志目录失败: %w", err)
		}
	}

	// 关闭旧日志文件
	if logW != nil && logW.file != nil {
		logW.file.Close()
	}

	logW = &logWriter{dir: dir}
	date := time.Now().Format("20060102")
	if err := logW.rotate(date); err != nil {
		return fmt.Errorf("打开日志文件失败: %w", err)
	}

	handler := slog.NewTextHandler(logW, &slog.HandlerOptions{
		Level: parseSlogLevel(level),
	})
	logger = slog.New(handler)

	logger.Info("日志系统初始化完成", "level", level, "dir", dir)
	return nil
}

// CloseLogger 关闭日志文件
func CloseLogger() {
	logMu.Lock()
	defer logMu.Unlock()
	if logW != nil && logW.file != nil {
		logW.file.Close()
		logW.file = nil
	}
}

// Debugf 打印 Debug 级别日志
func Debugf(format string, args ...any) {
	if logger != nil {
		logger.Debug(fmt.Sprintf(format, args...))
	}
}

// Infof 打印 Info 级别日志
func Infof(format string, args ...any) {
	if logger != nil {
		logger.Info(fmt.Sprintf(format, args...))
	}
}

// Warnf 打印 Warn 级别日志
func Warnf(format string, args ...any) {
	if logger != nil {
		logger.Warn(fmt.Sprintf(format, args...))
	}
}

// Errorf 打印 Error 级别日志
func Errorf(format string, args ...any) {
	if logger != nil {
		logger.Error(fmt.Sprintf(format, args...))
	}
}
