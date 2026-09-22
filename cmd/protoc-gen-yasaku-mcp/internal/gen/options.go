package gen

import (
	"fmt"
	"net/url"
)

// Options are the plugin parameters protoc passes through ParamFunc.
type Options struct {
	UIPrefix string
}

// Set records one plugin parameter; it rejects anything the generator does not define.
func (o *Options) Set(name, value string) error {
	if name != "ui_prefix" {
		return fmt.Errorf("unknown parameter %q", name)
	}
	u, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("ui_prefix %q: %w", value, err)
	}
	if u.Scheme != "ui" {
		return fmt.Errorf("ui_prefix %q: scheme must be ui", value)
	}
	o.UIPrefix = value
	return nil
}
