package apiserver

import (
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/equinor/flowify-workflows-server/auth"
	"github.com/equinor/flowify-workflows-server/storage"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

type KubernetesKonfig struct {
	KubeConfigPath string `mapstructure:"kubeconfigpath"`
	Namespace      string `mapstructure:"namespace"`
}

type LogConfig struct {
	LogLevel string `mapstructure:"loglevel"`
}

type ServerConfig struct {
	Port int `mapstructure:"port"`
}

type Config struct {
	DbConfig         storage.DbConfig `mapstructure:"db"`
	KubernetesKonfig KubernetesKonfig `mapstructure:"kubernetes"`
	AuthConfig       auth.AuthConfig  `mapstructure:"auth"`

	LogConfig    LogConfig    `mapstructure:"logging"`
	ServerConfig ServerConfig `mapstructure:"server"`
}

func (cfg Config) String() string {
	bytes, err := yaml.Marshal(cfg)
	if err != nil {
		log.Error("Could not stringify config", err)
		return ""
	}
	return string(bytes)
}

func (cfg Config) Dump(path string) error {
	str := cfg.String()
	switch path {
	case "-":
		// stdout
		fmt.Println(str)
	default:
		err := os.WriteFile(path, []byte(str), 0666)
		if err != nil {
			log.Error("Could write config to file ", path)
			return err
		}
	}
	return nil
}

func viperConfig() {
	viper.SetConfigType("yaml")
	viper.AutomaticEnv() // let env override config if available

	// to allow environment parse nested config
	viper.SetEnvKeyReplacer(strings.NewReplacer(`.`, `_`))

	// prefix all envs for uniqueness
	viper.SetEnvPrefix("FLOWIFY")
}

func viperDecodeHook() viper.DecoderConfigOption {
	return viper.DecodeHook(
		mapstructure.ComposeDecodeHookFunc(
			// Try to silent convert string to int
			// Port env var can be set as the string, not as required int
			func(f reflect.Type, t reflect.Type, data interface{}) (interface{}, error) {
				if f.Kind() != reflect.String {
					return data, nil
				}
				if t.Kind() != reflect.Interface {
					return data, nil
				}
				v, err := strconv.Atoi(data.(string))
				fmt.Printf("Converting (%v, %v) %v => %d. (%v)\n", f, t, data, v, err)
				if err != nil {
					return data, nil
				}
				return v, nil
			},
		),
	)
}

func LoadConfigFromReader(stream io.Reader) (Config, error) {
	viperConfig()
	config := Config{}
	if err := viper.ReadConfig(stream); err != nil {
		return Config{}, errors.Wrap(err, "Cannot load config from reader")
	}

	err := viper.Unmarshal(&config, viperDecodeHook())
	if err != nil {
		return Config{}, errors.Wrap(err, "Cannot load config from reader")
	}

	return config, nil

}

func LoadConfigFromPath(path string) (Config, error) {
	viper.AddConfigPath(path)
	viperConfig()

	err := viper.ReadInConfig()
	if err != nil {
		return Config{}, errors.Wrap(err, "Cannot not read config from path")
	}

	config := Config{}
	err = viper.Unmarshal(&config, viperDecodeHook())
	if err != nil {
		return Config{}, errors.Wrap(err, "Cannot not read config from path")
	}
	return config, nil
}
