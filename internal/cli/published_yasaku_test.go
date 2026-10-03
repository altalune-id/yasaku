package cli

import "testing"

// TestRoot_UnpublishedDomainsHaveNoCommand keeps the CLI in step with boot: no command group for a module no surface mounts.
func TestRoot_UnpublishedDomainsHaveNoCommand(t *testing.T) {
	root := NewRootCmd(stubServerBoot, stubClientBoot)
	for _, c := range root.Commands() {
		if c.Name() == "blog" || c.Name() == "todo" {
			t.Errorf("root registers %q, which yasaku does not publish", c.Name())
		}
	}
}
