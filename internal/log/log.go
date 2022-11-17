package log

import (
	"fmt"
	"io"
	"log"
	"os"
	"path"

	"github.com/eduvpn/eduvpn-common/internal/util"
	"github.com/eduvpn/eduvpn-common/types"
)

type FileLogger struct {
	Level LogLevel
	File  *os.File
}

type LogLevel int8

const (
	// No level set, not allowed
	LOG_NOTSET LogLevel = iota
	// Log debug, this message is not an error but is there for debugging
	LOG_DEBUG
	// Log info, this message is not an error but is there for additional information
	LOG_INFO
	// Log only to provide a warning, the app still functions
	LOG_WARNING
	// Log to provide a generic error, the app still functions but some functionality might not work
	LOG_ERROR
	// Log to provide a fatal error, the app cannot function correctly when such an error occurs
	LOG_FATAL
)

func (e LogLevel) String() string {
	switch e {
	case LOG_NOTSET:
		return "NOTSET"
	case LOG_DEBUG:
		return "DEBUG"
	case LOG_INFO:
		return "INFO"
	case LOG_WARNING:
		return "WARNING"
	case LOG_ERROR:
		return "ERROR"
	case LOG_FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

func (logger *FileLogger) Init(level LogLevel, directory string) error {
	errorMessage := "failed creating log"

	configDirErr := util.EnsureDirectory(directory)
	if configDirErr != nil {
		return types.NewWrappedError(errorMessage, configDirErr)
	}
	logFile, logOpenErr := os.OpenFile(
		logger.getFilename(directory),
		os.O_RDWR|os.O_CREATE|os.O_APPEND,
		0o666,
	)
	if logOpenErr != nil {
		return types.NewWrappedError(errorMessage, logOpenErr)
	}
	multi := io.MultiWriter(os.Stdout, logFile)
	log.SetOutput(multi)
	logger.File = logFile
	logger.Level = level
	return nil
}

func (logger *FileLogger) Inherit(label string, err error) {
	level := types.GetErrorLevel(err)

	msg := fmt.Sprintf("%s with err: %s", label, types.GetErrorTraceback(err))
	switch level {
	case types.ERR_INFO:
		logger.Info(msg)
	case types.ERR_WARNING:
		logger.Warning(msg)
	case types.ERR_OTHER:
		logger.Error(msg)
	case types.ERR_FATAL:
		logger.Fatal(msg)
	}
}

func (logger *FileLogger) Debug(msg string, params ...interface{}) {
	logger.log(LOG_DEBUG, msg, params...)
}

func (logger *FileLogger) Info(msg string, params ...interface{}) {
	logger.log(LOG_INFO, msg, params...)
}

func (logger *FileLogger) Warning(msg string, params ...interface{}) {
	logger.log(LOG_WARNING, msg, params...)
}

func (logger *FileLogger) Error(msg string, params ...interface{}) {
	logger.log(LOG_ERROR, msg, params...)
}

func (logger *FileLogger) Fatal(msg string, params ...interface{}) {
	logger.log(LOG_FATAL, msg, params...)
}

func (logger *FileLogger) Close() {
	logger.File.Close()
}

func (logger *FileLogger) getFilename(directory string) string {
	return path.Join(directory, "log")
}

func (logger *FileLogger) log(level LogLevel, msg string, params ...interface{}) {
	if level >= logger.Level && logger.Level != LOG_NOTSET {
		formatted_msg := fmt.Sprintf(msg, params...)
		format := fmt.Sprintf("- Go - %s - %s", level.String(), formatted_msg)
		// To log file
		log.Println(format)
	}
}
