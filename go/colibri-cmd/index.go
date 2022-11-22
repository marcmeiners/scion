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

	"github.com/scionproto/scion/go/lib/colibri/reservation"
	"github.com/scionproto/scion/go/lib/serrors"
	"github.com/spf13/cobra"
)

type indexFlags struct {
	RootFlags
	// DebugServerAddr string
	Activate bool
}

func newIndex() *cobra.Command {
	var flags indexFlags

	cmd := &cobra.Command{
		Use:   "index ",
		Short: "Manipulate segment reservation indices",
		Long:  "'index' allows the manipulation of segment reservation indices.",
		Args:  cobra.NoArgs,
	}

	cmd.AddCommand(
		newIndexCreate(&flags),
	)

	return cmd
}

func newIndexCreate(flags *indexFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create segR_ID",
		Short: "Create and confirm a new index",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("args: %v\nflags: %v\n", args, flags)
			return indexCreateCmd(cmd, flags, args)
		},
	}

	addRootFlags(cmd, &flags.RootFlags)
	cmd.PersistentFlags().BoolVar(&flags.Activate, "activate", false, "also activate the index")

	return cmd
}

func indexCreateCmd(cmd *cobra.Command, flags *indexFlags, args []string) error {
	cliAddr, err := flags.DebugServer()
	if err != nil {
		return err
	}
	id, err := reservation.IDFromString(args[0])
	if err != nil {
		return serrors.WrapStr("parsing the ID of the segment reservation", err)
	}
	cmd.SilenceUsage = true

	fmt.Printf("Will use debug service at %s and segment with ID %s to create a new index. "+
		"Will I activate it? %v\n", cliAddr, id, flags.Activate)

	return nil
}
