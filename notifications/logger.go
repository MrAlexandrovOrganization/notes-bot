package notifications

import "go.uber.org/zap"

var logger = zap.NewNop()

func SetLogger(l *zap.Logger) {
	logger = l
}
