// Command trafico is the entry point of the trafico-cli binary.
package main

import (
	"os"

	"github.com/aam9063/trafico-cli/internal/cli"
)

func main() {
	os.Exit(int(cli.Execute()))
}
