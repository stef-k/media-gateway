// Package config loads the startup configuration without contacting the provider.
// Errors identify fields or operations, never input values or secret contents.
package config

import (
	"errors"
	"io"
	"os"
	"time"
)

// Config describes connectivity and policy, not publication authorization.
// Do not log whole configuration values: they contain private topology.
type Config struct {
	Server   Server   `toml:"server"`
	Provider Provider `toml:"provider"`
	Policy   Policy   `toml:"policy"`
	Delivery Delivery `toml:"delivery"`
	Consumer Consumer `toml:"consumer"`
}

// Server restricts the future service to a numeric loopback listener.
type Server struct {
	Listen        string `toml:"listen"`
	PublicBaseURL string `toml:"public_base_url"`
}

// Provider specifies the single trusted Immich authority and separate secret.
type Provider struct {
	Type           string        `toml:"type"`
	BaseURL        string        `toml:"base_url"`
	APIKeyFile     string        `toml:"api_key_file"`
	RequestTimeout time.Duration `toml:"request_timeout"`
}

// Policy declares provider-visible roots and exact literal directory rules.
type Policy struct {
	AllowedRoots []string `toml:"allowed_roots"`
	Rules        []Rule   `toml:"rules"`
}

// Rule classifies media eligibility; it does not enable delivery implementations.
type Rule struct {
	Segment string   `toml:"segment"`
	Media   []string `toml:"media"`
}

// Delivery reserves only the preview representation; originals remain forbidden.
type Delivery struct {
	AllowOriginal bool   `toml:"allow_original"`
	ImageVariant  string `toml:"image_variant"`
}

// Consumer controls only trusted eligible-consumer coordinates; zero means disabled.
type Consumer struct {
	ExposeCoordinates bool `toml:"expose_coordinates"`
}

// Load reads an explicit TOML file, validates it, and loads the credential.
// On failure it returns no partial configuration or credential. It emits no logs.
func Load(filename string) (Config, string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return Config{}, "", errors.New("config: cannot open configuration file")
	}
	defer f.Close()
	var c Config
	// Bound startup input; parser errors can quote private input, so never wrap them.
	const maxConfig = 1 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxConfig+1))
	if err != nil || len(data) > maxConfig {
		return Config{}, "", errors.New("config: cannot read configuration file within 1 MiB limit")
	}
	if err := decode(data, &c); err != nil {
		return Config{}, "", err
	}
	if err := c.validate(); err != nil {
		return Config{}, "", err
	}
	key, err := readCredential(c.Provider.APIKeyFile)
	if err != nil {
		return Config{}, "", err
	}
	return c, key, nil
}
