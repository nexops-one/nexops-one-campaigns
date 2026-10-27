// SPDX-License-Identifier: Apache-2.0

// Command compliance-engine is the standalone compliance engine: HTTP API,
// migrations, offline validation and catalog tooling.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/nexops-one/compliance-engine/pkg/cli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	ed := cli.OpenCore()
	ed.Version = version
	code := cli.Main(ctx, os.Args[1:], ed)
	stop()
	os.Exit(code)
}
