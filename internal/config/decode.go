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
		Policy  Policy  `toml:"policy"`
		Privacy Privacy `toml:"privacy"`
	}
	if err := toml.NewDecoder(bytes.NewReader(data)).DisallowUnknownFields().Decode(&raw); err != nil {
		// Inspect only the unsupported section after a failed strict decode; never return raw input.
		var unsupported map[string]any
		if toml.Unmarshal(data, &unsupported) == nil {
			if _, present := unsupported["delivery"]; present {
				return errors.New("config: delivery section is not supported; preview/original are fixed product representations; remove the delivery section")
			}
		}
		var missing *toml.StrictMissingError
		if errors.As(err, &missing) {
			for _, field := range missing.Errors {
				key := field.Key()
				if len(key) == 2 && key[0] == "policy" && key[1] == "allowed_roots" {
					return errors.New("config: policy.allowed_roots is not supported; use policy.roots")
				}
			}
		}
		return errors.New("config: invalid TOML schema or syntax (check field names and types)")
	}
	timeout, err := time.ParseDuration(raw.Provider.RequestTimeout)
	if err != nil || timeout <= 0 {
		return errors.New("config: provider.request_timeout must be a positive Go duration")
	}
	*c = Config{
		Server:   raw.Server,
		Provider: Provider{Type: raw.Provider.Type, BaseURL: raw.Provider.BaseURL, APIKeyFile: raw.Provider.APIKeyFile, RequestTimeout: timeout},
		Policy:   raw.Policy,
		Privacy:  raw.Privacy,
	}
	return nil
}
