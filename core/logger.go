package core

import (
	"notes_bot/core/features"
	"notes_bot/internal/applog"

	"go.uber.org/zap"
)

var Logger *zap.Logger
var logger *zap.Logger

func init() {
	Logger = applog.New()
	logger = Logger
	zap.ReplaceGlobals(Logger)
	features.SetLogger(Logger)
}
