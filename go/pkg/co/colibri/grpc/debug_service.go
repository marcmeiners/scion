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

package grpc

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/scionproto/scion/go/co/reservation/segment"
	"github.com/scionproto/scion/go/co/reservation/translate"
	"github.com/scionproto/scion/go/co/reservationstorage"
	"github.com/scionproto/scion/go/co/reservationstorage/backend"
	"github.com/scionproto/scion/go/lib/addr"
	caddr "github.com/scionproto/scion/go/lib/colibri/addr"
	"github.com/scionproto/scion/go/lib/colibri/coliquic"
	"github.com/scionproto/scion/go/lib/log"
	"github.com/scionproto/scion/go/lib/topology"
	"github.com/scionproto/scion/go/lib/util"
	colpb "github.com/scionproto/scion/go/pkg/proto/colibri"
)

type DebugService struct {
	DB       backend.DB
	Operator *coliquic.ServiceClientOperator
	Topo     *topology.Loader
	Store    reservationstorage.Store
}

var _ colpb.ColibriDebugCommandsServiceServer = (*DebugService)(nil)
var _ colpb.ColibriDebugServiceServer = (*DebugService)(nil)

func (s *DebugService) CmdTraceroute(ctx context.Context, req *colpb.CmdTracerouteRequest,
) (*colpb.CmdTracerouteResponse, error) {

	res, err := s.Traceroute(ctx, (*colpb.TracerouteRequest)(req))
	return (*colpb.CmdTracerouteResponse)(res), err
}

func (s *DebugService) Traceroute(ctx context.Context, req *colpb.TracerouteRequest,
) (*colpb.TracerouteResponse, error) {

	localIA := s.Topo.IA()
	reqTimeStamp := uint64(time.Now().UnixMicro())
	errF := func(err error) (*colpb.TracerouteResponse, error) {
		return &colpb.TracerouteResponse{
			ErrorFound: &colpb.ErrorInIA{
				Ia:      uint64(localIA),
				Message: err.Error(),
			},
		}, nil
	}
	segR, err := s.getSegR(ctx, req.Id)
	if err != nil {
		return errF(err)
	}

	var colAddr *caddr.Colibri
	if req.UseColibri {
		if segR.CurrentStep == 0 {
			// since this is the source of the traffic, retrieve the colibri transport path here
			if segR.TransportPath != nil {
				colAddr = &caddr.Colibri{
					Path: *segR.TransportPath,
					Src:  *caddr.NewEndpointWithAddr(segR.Steps.SrcIA(), addr.SvcCOL.Base()),
				}
			}
		} else {
			colAddr, err = colAddrFromCtx(ctx)
			if err != nil {
				return errF(status.Errorf(codes.Internal,
					"error retrieving path at transit: %s", err))
			}
			log.Debug("deleteme got a colibri transport from the network",
				"SRC", colAddr.Src,
				"DST", colAddr.Dst,
				"PATH", colAddr.Path,
			)
		}
		if colAddr == nil {
			return errF(status.Errorf(codes.FailedPrecondition, "there is no colibri transport"))
		}
		// complete the destination address with the destination stored in the reservation
		colAddr.Dst = *caddr.NewEndpointWithAddr(segR.Steps.DstIA(), addr.SvcCOL.Base())

		// deleteme
		log.Debug("debug service info about the colibri transport path",
			"SRC", colAddr.Src,
			"DST", colAddr.Dst,
			"S", colAddr.Path.InfoField.S,
			"C", colAddr.Path.InfoField.C,
			"R", colAddr.Path.InfoField.R,
			"expiration", util.SecsToTime(colAddr.Path.InfoField.ExpTick),
			"curr_hopfield", colAddr.Path.InfoField.CurrHF,
			"idx", colAddr.Path.InfoField.Ver,
			"bwcls", colAddr.Path.InfoField.BwCls,
		)
	}

	res := &colpb.TracerouteResponse{}
	if segR.Egress() != 0 { // destination not reached yet, forward to next debug service
		// TODO(juagargi) fix this by allowing a parameter.
		// XXX(juagargi) hacky: reduce the timeout by 100 ms to be able to answer back in
		// case of next hop timing out. This will work sometimes, when there had been no hop
		// that needs more than 100ms to send back the answer.
		deadline, _ := ctx.Deadline()
		ctx, cancelF := context.WithDeadline(ctx, deadline.Add(-100*time.Millisecond))
		defer cancelF()

		client, err := s.Operator.DebugClient(ctx, segR.Egress(), colAddr)
		if err != nil {
			return errF(status.Errorf(codes.FailedPrecondition, "error using operator: %s", err))
		}

		res, err = client.Traceroute(ctx, req)
		if err != nil {
			return errF(status.Errorf(codes.Internal,
				"error forwarding to next (%s, egress_id = %d) service: %s",
				s.Operator.Neighbor(segR.Egress()), segR.Egress(), err))
		}
	}

	res.IaStamp = append(res.IaStamp, uint64(localIA))
	res.TimeStampFromRequest = append(res.TimeStampFromRequest, reqTimeStamp)
	res.TimeStampAtResponse = append(res.TimeStampAtResponse, uint64(time.Now().UnixMicro()))
	return res, nil
}

func (s *DebugService) getSegR(ctx context.Context, id *colpb.ReservationID,
) (*segment.Reservation, error) {

	ID := translate.ID(id)
	segR, err := s.DB.GetSegmentRsvFromID(ctx, ID)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "error retrieving segment: %s", err)
	}
	if segR == nil {
		return nil, status.Errorf(codes.NotFound, "segment not found: %s", id)
	}
	return segR, nil
}
