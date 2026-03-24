package ginx

import (
	"context"

	"github.com/gin-gonic/gin"
)

func validateContext(ctx context.Context) error {
	if ctx == nil {
		return ErrContextRequired
	}
	return nil
}

func newSkipPathSet(defaults []string, extras []string) map[string]struct{} {
	skipPaths := make(map[string]struct{}, len(defaults)+len(extras))
	for _, path := range defaults {
		if path == "" {
			continue
		}
		skipPaths[path] = struct{}{}
	}
	for _, path := range extras {
		if path == "" {
			continue
		}
		skipPaths[path] = struct{}{}
	}
	return skipPaths
}

func matchesPath(skipPaths map[string]struct{}, path string) bool {
	_, ok := skipPaths[path]
	return ok
}

func normalizeMode(mode string) string {
	switch mode {
	case gin.DebugMode, gin.ReleaseMode, gin.TestMode:
		return mode
	case "":
		return gin.Mode()
	default:
		return gin.ReleaseMode
	}
}

func defaultObservabilitySkipPaths(conf *ServerConfig) []string {
	paths := []string{"/metrics", "/favicon.ico"}
	if !conf.DisableHealthEndpoint && conf.HealthPath != "" {
		paths = append(paths, conf.HealthPath)
	}
	return paths
}
