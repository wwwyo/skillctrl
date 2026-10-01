// Command skillctrl manages agent skills in a Git repository while preserving
// the intent recorded for local adaptations.
package main

import (
	"os"

	"github.com/wwwyo/skillctrl/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
