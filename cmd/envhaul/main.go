// Command envhaul generates .env files from a config file, resolving values
// from AWS SSM Parameter Store and Secrets Manager.
package main

import (
	"os"

	"github.com/davidtucker/envhaul/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.Options{}))
}
