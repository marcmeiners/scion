// Copyright 2022 ETH Zurich
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/scionproto/scion/go/pkg/app"
	"github.com/spf13/cobra"
)

// CommandPather returns the path to a command.
type CommandPather interface {
	CommandPath() string
}

func main() {
	// Note: code setting up command, etc based on the scion command.
	executable := filepath.Base(os.Args[0])
	cmd := &cobra.Command{
		Use:           executable,
		Short:         "COLIBRI CLI to debug services",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
	}

	cmd.AddCommand(newTraceroute(cmd))

	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		if code := app.ExitCode(err); code != -1 {
			os.Exit(code)
		}
		os.Exit(2)
	}
}
