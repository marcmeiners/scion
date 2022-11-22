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
	"context"
	"fmt"
	"time"

	"github.com/scionproto/scion/go/co/reservation/translate"
	"github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/colibri/reservation"
	"github.com/scionproto/scion/go/lib/serrors"
	sgrpc "github.com/scionproto/scion/go/pkg/grpc"
	colpb "github.com/scionproto/scion/go/pkg/proto/colibri"
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
		Use:   "new segR_ID",
		Short: "Create and confirm a new index",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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

	ctx, cancelF := context.WithTimeout(context.Background(), time.Second)
	defer cancelF()

	grpcDialer := sgrpc.TCPDialer{}
	conn, err := grpcDialer.Dial(ctx, cliAddr)
	if err != nil {
		return serrors.WrapStr("dialing to the local debug service", err)
	}
	client := colpb.NewColibriDebugCommandsServiceClient(conn)

	// new index
	req := &colpb.CmdIndexNewRequest{
		Id: translate.PBufID(id),
	}
	res, err := client.CmdIndexNew(ctx, req)
	if err != nil {
		return err
	}
	if res.ErrorFound != nil {
		return serrors.New(
			fmt.Sprintf("at IA %s: %s\n", addr.IA(res.ErrorFound.Ia), res.ErrorFound.Message))
	}
	fmt.Printf("Index with ID %d created", res.Index)

	if flags.Activate {
		// if activate is requested, do it here
		req := &colpb.CmdIndexActivateRequest{
			Id:    translate.PBufID(id),
			Index: res.Index,
		}
		res, err := client.CmdIndexActivate(ctx, req)
		if err != nil {
			return err
		}
		if res.ErrorFound != nil {
			return serrors.New(
				fmt.Sprintf("at IA %s: %s\n", addr.IA(res.ErrorFound.Ia), res.ErrorFound.Message))
		}
		fmt.Printf(" and activated")
	}
	fmt.Println(".")

	return nil
}
