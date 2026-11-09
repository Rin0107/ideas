// {{NAME}}: {{DESCRIPTION}}
package main

import (
	"fmt"
	"os"

	"github.com/{{REPO}}/internal/app"
)

func main() {
	if err := app.Run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "{{NAME}}:", err)
		os.Exit(1)
	}
}
