package middleware

import "github.com/gin-gonic/gin"

func newSkipPathSet(paths []string) map[string]struct{} {
	skipPaths := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		skipPaths[path] = struct{}{}
	}
	return skipPaths
}

func shouldSkip(skipPaths map[string]struct{}, path string) bool {
	_, ok := skipPaths[path]
	return ok
}

func routeLabel(c *gin.Context) string {
	if route := c.FullPath(); route != "" {
		return route
	}
	return "unmatched"
}

func bytesWritten(size int) int {
	if size < 0 {
		return 0
	}
	return size
}
