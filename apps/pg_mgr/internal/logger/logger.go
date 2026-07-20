package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pg_mgr/internal/config"
)

type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

var currentLevel Level = ERROR
var logFile *os.File

func InitLogger() {
	levelStr := strings.ToLower(config.Global.LogLevel)
	switch levelStr {
	case "debug":
		currentLevel = DEBUG
	case "info":
		currentLevel = INFO
	case "warn":
		currentLevel = WARN
	case "error":
		currentLevel = ERROR
	default:
		currentLevel = ERROR
	}

	logDir := config.Global.LogDir
	if logDir == "" {
		logDir = "/var/log/pg_mgr"
	}
	os.MkdirAll(logDir, 0755)

	logPath := filepath.Join(logDir, "daemon.log")
	var err error
	logFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("Failed to open log file %s: %v", logPath, err)
		return
	}
}

func Close() {
	if logFile != nil {
		logFile.Close()
	}
}

func logMsg(level Level, levelStr string, format string, args ...interface{}) {
	if level < currentLevel {
		return
	}
	msg := fmt.Sprintf(format, args...)
	timeStr := time.Now().Format("2006-01-02 15:04:05")
	logLine := fmt.Sprintf("[%s] [%s] %s\n", timeStr, levelStr, msg)

	if logFile != nil {
		logFile.WriteString(logLine)
	}
	// Also print to stdout/stderr in case we are running in foreground (e.g. daemon run)
	fmt.Print(logLine)
}

func Debug(format string, args ...interface{}) {
	logMsg(DEBUG, "DEBUG", format, args...)
}

func Info(format string, args ...interface{}) {
	logMsg(INFO, "INFO", format, args...)
}

func Warn(format string, args ...interface{}) {
	logMsg(WARN, "WARN", format, args...)
}

func Error(format string, args ...interface{}) {
	logMsg(ERROR, "ERROR", format, args...)
}
