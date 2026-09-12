package config

import (
	"bytes"
	"errors"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// decode uses a string at the TOML boundary for explicit Go duration parsing.
func decode(data []byte, c *Config) error {
	var raw struct {
		Server   Server `toml:"server"`
		Provider struct {
			Type           string `toml:"type"`
			BaseURL        string `toml:"base_url"`
			APIKeyFile     string `toml:"api_key_file"`
			RequestTimeout string `toml:"request_timeout"`
		} `toml:"provider"`
		Policy   Policy   `toml:"policy"`
		Delivery Delivery `toml:"delivery"`
	}
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&raw); err != nil {
		return errors.New("config: invalid TOML schema or syntax (check field names and types)")
	}
	timeout, err := time.ParseDuration(raw.Provider.RequestTimeout)
	if err != nil || timeout <= 0 {
		return errors.New("config: provider.request_timeout must be a positive Go duration")
	}
	*c = Config{
		Server:   raw.Server,
		Provider: Provider{Type: raw.Provider.Type, BaseURL: raw.Provider.BaseURL, APIKeyFile: raw.Provider.APIKeyFile, RequestTimeout: timeout},
		Policy:   raw.Policy, Delivery: raw.Delivery,
	}
	return nil
}
