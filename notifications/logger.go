package notifications

import "go.uber.org/zap"

var logger *zap.Logger

func init() {
	logger = zap.Must(zap.NewProduction())
}
