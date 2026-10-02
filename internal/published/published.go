// Package published names the template reference modules yasaku publishes, so boot and the CLI gate them with one switch.
package published

// NOTE: blog and todo stay as reference code for the skills and howto docs; yasaku publishes neither, and webhooks have no event without blog.
const (
	Blog            = false
	Todo            = false
	WebhooksConsole = false
)
