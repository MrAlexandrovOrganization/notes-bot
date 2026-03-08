package core

import (
	"notes_bot/core/features"

	"go.uber.org/zap"
)

var Logger *zap.Logger
var logger *zap.Logger

func init() {
	Logger = zap.Must(zap.NewProduction())
	logger = Logger
	zap.ReplaceGlobals(Logger)
	features.SetLogger(Logger)
}
