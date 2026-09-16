// Command protoc-gen-yasaku-mcp generates MCP tool bindings for every RPC annotated with yasaku.mcp.v1.tool.
package main

import (
	"fmt"
	"os"

	"google.golang.org/protobuf/compiler/protogen"

	"altalune.id/yasaku/cmd/protoc-gen-yasaku-mcp/internal/gen"
)

func main() {
	protogen.Options{}.Run(func(p *protogen.Plugin) error {
		if err := gen.Generate(p); err != nil {
			fmt.Fprintf(os.Stderr, "protoc-gen-yasaku-mcp: %v\n", err)
			os.Exit(1)
		}
		return nil
	})
}
