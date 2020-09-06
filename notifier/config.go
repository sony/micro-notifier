package notifier

import (
	"encoding/json"
	"io"
	"os"
)

// ConfigApplication is the configuration of individual applications.
type ConfigApplication struct {
	Name   string `json:"name"`
	Key    string `json:"key"`
	Secret string `json:"secret"`
}

// ConfigRedis is an optional Redis configuration parameters.
type ConfigRedis struct {
	Address  string `json:"address"`
	Database int    `json:"database"`
	Password string `json:"password"`
	Sentinel bool   `json:"sentinel"`
	Secure   bool   `json:"secure"`
}

// Config holds the enture configuration parameters.
type Config struct {
	Host         string              `json:"host"`
	Port         int                 `json:"port"`
	Certificate  string              `json:"certificate"`
	PrivateKey   string              `json:"private-key"`
	Redis        ConfigRedis         `json:"redis"`
	Applications []ConfigApplication `json:"applications"`
}

// ConfigError will be returned when something bad occur during reading
// config file.
type ConfigError struct {
	msg   string
	inner error
}

func (e *ConfigError) Error() string {
	if e.inner != nil {
		return e.msg + ":" + e.inner.Error()
	}
	return e.msg
}

// Unwrap returns inner error
func (e *ConfigError) Unwrap() error {
	return e.inner
}

// ReadConfigFile reads a config file and returns Config struct
// or an error.
func ReadConfigFile(file string) (*Config, error) {
	if file == "" {
		return &Config{}, nil // default case
	}

	f, err := os.Open(file)
	if err != nil {
		return nil, &ConfigError{
			msg:   "Can't open the config file",
			inner: err,
		}
	}
	defer f.Close()

	return ReadConfig(f)
}

// ReadConfig reads configuration data and returns Config struct
// or an error.
func ReadConfig(r io.Reader) (*Config, error) {
	decoder := json.NewDecoder(r)
	config := Config{}
	err := decoder.Decode(&config)
	if err != nil {
		return nil, &ConfigError{
			msg:   "Invalid config file format",
			inner: err,
		}
	}

	if config.Certificate != "" {
		_, err = os.Stat(config.Certificate)
		if err != nil {
			return nil, &ConfigError{
				msg: "Cannot access certificate file `" +
					config.Certificate + "' ",
				inner: err,
			}
		}
	}
	if config.PrivateKey != "" {
		_, err = os.Stat(config.PrivateKey)
		if err != nil {
			return nil, &ConfigError{
				msg: "Cannot access private key file `" +
					config.PrivateKey + "' ",
				inner: err,
			}
		}
	}
	if (config.Certificate != "" && config.PrivateKey == "") ||
		(config.Certificate == "" && config.PrivateKey != "") {
		return nil, &ConfigError{
			msg:   "To use https, both certificate and private-key must be specified",
			inner: nil,
		}
	}

	return &config, nil
}

// GetApp extracts ConfigApplication of the named application, or nil
// if no such application is defined in the config.
func (c *Config) GetApp(name string) *ConfigApplication {
	for _, ca := range c.Applications {
		if ca.Name == name {
			return &ca
		}
	}
	return nil
}

// GetAppFromKey extracts ConfigApplication of the application with
// given key, or nil if no such application is defined in the config.
func (c *Config) GetAppFromKey(key string) *ConfigApplication {
	for _, ca := range c.Applications {
		if ca.Key == key {
			return &ca
		}
	}
	return nil
}
