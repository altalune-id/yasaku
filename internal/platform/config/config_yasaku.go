package config

import "strings"

// OpensheetConfig points yasaku at the one opensheet server every project mirrors to; an empty BaseURL leaves the Opensheet module unmounted.
type OpensheetConfig struct {
	// SECURITY: server config, never a tenant field, so a tenant cannot aim yasaku at an internal host.
	BaseURL string `yaml:"baseURL" mapstructure:"baseURL" awareness:"bootstrap" validate:"omitempty,http_url"`
	// SECURITY: lets BaseURL resolve to a private address, as Railway private networking does; leave false on a public host.
	AllowPrivateHosts bool `yaml:"allowPrivateHosts" mapstructure:"allowPrivateHosts" awareness:"bootstrap"`
}

// Mounted reports whether the Opensheet module is mounted at all.
func (c OpensheetConfig) Mounted() bool { return strings.TrimSpace(c.BaseURL) != "" }
