package viperx

import (
	"errors"
	"strings"

	"github.com/bang-go/micro/conf/envx"
	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

type Config struct {
	Name     string // e.g. "application"
	Type     string // e.g. "yaml"
	Path     string // e.g. "./config" or "/etc/myapp/"
	Watch    bool   // Enable hot reload
	OnChange func(fsnotify.Event)
}

func New(conf *Config) (*viper.Viper, error) {
	if conf == nil {
		conf = &Config{
			Name: "application",
			Type: "yaml",
			Path: ".",
		}
	}
	if conf.Name == "" {
		conf.Name = "application"
	}
	if conf.Type == "" {
		conf.Type = "yaml"
	}
	if conf.Path == "" {
		conf.Path = "."
	}

	v := viper.New()
	v.SetConfigName(conf.Name)
	v.SetConfigType(conf.Type)
	v.AddConfigPath(conf.Path)

	// Environment variables support
	// Priority: Env > Config File > Default
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Try to load config specific to the environment
	// e.g. application.dev.yaml, application.prod.yaml
	envName := conf.Name + "." + envx.Active()
	v.SetConfigName(envName)
	err := v.ReadInConfig()
	if err != nil {
		// Fallback to base config name
		v.SetConfigName(conf.Name)
		if err := v.ReadInConfig(); err != nil {
			// It's okay if config file doesn't exist, maybe using only ENV
			var configFileNotFoundError viper.ConfigFileNotFoundError
			if !errors.As(err, &configFileNotFoundError) {
				return nil, err
			}
		}
	}

	if conf.Watch {
		v.OnConfigChange(func(e fsnotify.Event) {
			if conf.OnChange != nil {
				conf.OnChange(e)
			}
		})
		v.WatchConfig()
	}

	return v, nil
}
