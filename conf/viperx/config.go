package viperx

import (
	"errors"
	"strings"

	"github.com/bang-go/micro/conf/envx"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

const (
	defaultConfigName = "application"
	defaultConfigType = "yaml"
)

type Config struct {
	Name string
	Type string

	Paths []string
	File  string

	Mode envx.Mode

	RequireFile   bool
	Watch         bool
	EnvPrefix     string
	AllowEmptyEnv bool

	OnChange func(*viper.Viper, fsnotify.Event)
}

func Open(conf *Config) (*viper.Viper, error) {
	config, err := prepareConfig(conf)
	if err != nil {
		return nil, err
	}

	v := viper.New()
	configureEnv(v, config)

	loaded, err := load(v, config)
	if err != nil {
		return nil, err
	}

	if config.Watch && loaded {
		v.OnConfigChange(func(event fsnotify.Event) {
			if config.OnChange != nil {
				config.OnChange(v, event)
			}
		})
		v.WatchConfig()
	}

	return v, nil
}

func New(conf *Config) (*viper.Viper, error) {
	return Open(conf)
}

func prepareConfig(conf *Config) (*Config, error) {
	if conf == nil {
		conf = &Config{}
	}

	cloned := *conf
	cloned.Name = strings.TrimSpace(cloned.Name)
	cloned.Type = strings.TrimSpace(cloned.Type)
	cloned.File = strings.TrimSpace(cloned.File)
	cloned.EnvPrefix = strings.TrimSpace(cloned.EnvPrefix)
	cloned.Paths = normalizePaths(cloned.Paths)
	cloned.Mode = envx.Normalize(strings.TrimSpace(string(cloned.Mode)))

	if cloned.File == "" {
		if cloned.Name == "" {
			cloned.Name = defaultConfigName
		}
		if cloned.Type == "" {
			cloned.Type = defaultConfigType
		}
	}

	if len(cloned.Paths) == 0 && cloned.File == "" {
		cloned.Paths = []string{"."}
	}
	if cloned.Mode == "" {
		cloned.Mode = envx.Active()
	}

	return &cloned, nil
}

func configureEnv(v *viper.Viper, conf *Config) {
	if conf.EnvPrefix != "" {
		v.SetEnvPrefix(conf.EnvPrefix)
	}
	v.AutomaticEnv()
	v.AllowEmptyEnv(conf.AllowEmptyEnv)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
}

func load(v *viper.Viper, conf *Config) (bool, error) {
	if conf.File != "" {
		v.SetConfigFile(conf.File)
		if conf.Type != "" {
			v.SetConfigType(conf.Type)
		}
		if err := v.ReadInConfig(); err != nil {
			return false, err
		}
		return true, nil
	}

	v.SetConfigType(conf.Type)
	for _, path := range conf.Paths {
		v.AddConfigPath(path)
	}

	var notFoundErr error
	for _, name := range configNames(conf) {
		v.SetConfigName(name)
		err := v.ReadInConfig()
		if err == nil {
			return true, nil
		}

		var notFound viper.ConfigFileNotFoundError
		if errors.As(err, &notFound) {
			notFoundErr = err
			continue
		}
		return false, err
	}

	if conf.RequireFile {
		return false, notFoundErr
	}
	return false, nil
}

func configNames(conf *Config) []string {
	if conf.Mode == "" {
		return []string{conf.Name}
	}
	names := []string{conf.Name + "." + string(conf.Mode), conf.Name}
	return uniqueStrings(names)
}

func normalizePaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		normalized = append(normalized, path)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	unique := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}
