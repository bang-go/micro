package envx

import (
	"os"
	"strings"
)

const (
	ModeDev  = "dev"
	ModeTest = "test"
	ModeProd = "prod"
)

// Active returns the current running environment mode.
// Priority: APP_ENV > GO_ENV > ModeDev
func Active() string {
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = os.Getenv("GO_ENV")
	}
	if env == "" {
		return ModeDev
	}
	return strings.ToLower(env)
}

func IsDev() bool {
	return Active() == ModeDev
}

func IsTest() bool {
	return Active() == ModeTest
}

func IsProd() bool {
	return Active() == ModeProd
}

// Hostname returns the hostname of the machine.
func Hostname() string {
	hostname, _ := os.Hostname()
	return hostname
}
