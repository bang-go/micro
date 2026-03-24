package envx

import (
	"os"
	"strings"
)

type Mode string

const (
	AppEnvKey = "APP_ENV"
	GoEnvKey  = "GO_ENV"
)

const (
	Development Mode = "dev"
	Test        Mode = "test"
	Production  Mode = "prod"
)

const (
	ModeDev  = Development
	ModeTest = Test
	ModeProd = Production
)

func Active() Mode {
	if mode, ok := Lookup(); ok {
		return mode
	}
	return Development
}

func Lookup() (Mode, bool) {
	if mode, ok := lookupKey(AppEnvKey); ok {
		return mode, true
	}
	return lookupKey(GoEnvKey)
}

func Normalize(raw string) Mode {
	value := strings.ToLower(strings.TrimSpace(raw))
	switch value {
	case "dev", "development", "local":
		return Development
	case "test", "testing", "ci":
		return Test
	case "prod", "production":
		return Production
	default:
		return Mode(value)
	}
}

func IsDev() bool {
	return Active() == Development
}

func IsTest() bool {
	return Active() == Test
}

func IsProd() bool {
	return Active() == Production
}

func Hostname() string {
	return HostnameOr("")
}

func HostnameOr(fallback string) string {
	hostname, err := os.Hostname()
	if err != nil {
		return fallback
	}

	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		return fallback
	}
	return hostname
}

func lookupKey(key string) (Mode, bool) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return "", false
	}

	mode := Normalize(value)
	if mode == "" {
		return "", false
	}
	return mode, true
}
