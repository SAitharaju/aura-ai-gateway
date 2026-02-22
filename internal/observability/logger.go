package observability

import (
	"log/slog"
	"os"
)

// SetupLogger configures the global structured logger for the app.
func SetupLogger() *slog.Logger {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)
	return logger
}
