// SPDX-License-Identifier:Apache-2.0

package logging

import (
	"fmt"
	"log/slog"
	"os"
)

const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

func New(level string) (*slog.Logger, error) {
	logLevel, err := levelToSlog(level)
	if err != nil {
		return nil, err
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger, nil
}

func levelToSlog(level string) (slog.Level, error) {
	switch level {
	case LevelDebug:
		return slog.LevelDebug, nil
	case LevelInfo:
		return slog.LevelInfo, nil
	case LevelWarn:
		return slog.LevelWarn, nil
	case LevelError:
		return slog.LevelError, nil
	}
	return slog.LevelInfo, fmt.Errorf("invalid log level %s: possible values are [debug, info, warn, error]", level)
}
