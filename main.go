// sf-typings generates declarations from selected Salesforce object describes.
package main

import (
	"os"

	"github.com/Hacerx/sf-typings/internal/cli"
)

func main() { os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr)) }
