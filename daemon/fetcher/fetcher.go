// Copyright 2018 ETH Zurich, Anapaya Systems
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

// Package fetcher implements path segment fetching, verification and
// combination logic for SCIOND.
package fetcher

import (
	"context"
	"net"
	"time"

	"github.com/scionproto/scion/daemon/config"
	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/pkg/log"
	"github.com/scionproto/scion/pkg/private/serrors"
	"github.com/scionproto/scion/pkg/snet"
	"github.com/scionproto/scion/private/pathdb"
	"github.com/scionproto/scion/private/revcache"
	"github.com/scionproto/scion/private/segment/segfetcher"
	"github.com/scionproto/scion/private/segment/seghandler"
	infra "github.com/scionproto/scion/private/segment/verifier"
	"github.com/scionproto/scion/private/trust"
)

const (
	DefaultMinWorkerLifetime = 10 * time.Second
)

type TrustStore interface {
	trust.Inspector
}

type Fetcher interface {
	GetPaths(ctx context.Context, src, dst addr.IA, refresh bool, privateOnly bool) ([]snet.Path, error)
}

type fetcher struct {
	defaultIA addr.IA
	pather    *segfetcher.Pather
	perISD    map[addr.ISD]*segfetcher.Pather
	config    config.SDConfig
}

type FetcherConfig struct {
	IA         addr.IA
	MTU        uint16
	Core       bool
	NextHopper interface {
		UnderlayNextHop(uint16) *net.UDPAddr
	}

	RPC       segfetcher.RPC
	PathDB    pathdb.DB
	Inspector trust.Inspector

	Verifier infra.Verifier
	RevCache revcache.RevCache
	Cfg      config.SDConfig
}

func NewFetcher(cfg FetcherConfig) Fetcher {
	localIAs := []addr.IA{cfg.IA}
	seen := map[addr.IA]struct{}{
		cfg.IA: {},
	}
	for _, raw := range cfg.Cfg.LocalIAs {
		ia, err := addr.ParseIA(raw)
		if err != nil {
			log.Debug("Ignoring invalid local IA", "ia", raw, "err", err)
			continue
		}
		if _, ok := seen[ia]; ok {
			continue
		}
		localIAs = append(localIAs, ia)
		seen[ia] = struct{}{}
	}

	perISD := make(map[addr.ISD]*segfetcher.Pather)
	var defaultPather *segfetcher.Pather
	for _, ia := range localIAs {
		p := &segfetcher.Pather{
			IA:         ia,
			MTU:        cfg.MTU,
			NextHopper: cfg.NextHopper,
			RevCache:   cfg.RevCache,
			Fetcher: &segfetcher.Fetcher{
				QueryInterval: cfg.Cfg.QueryInterval.Duration,
				PathDB:        cfg.PathDB,
				Resolver: segfetcher.NewResolver(
					cfg.PathDB,
					cfg.RevCache,
					neverLocal{},
				),
				ReplyHandler: &seghandler.Handler{
					Verifier: &seghandler.DefaultVerifier{Verifier: cfg.Verifier},
					Storage: &seghandler.DefaultStorage{
						PathDB:   cfg.PathDB,
						RevCache: cfg.RevCache,
					},
				},
				Requester: &segfetcher.DefaultRequester{
					RPC:         cfg.RPC,
					DstProvider: &dstProvider{},
				},
				Metrics: segfetcher.NewFetcherMetrics("sd"),
			},
			Splitter: &segfetcher.MultiSegmentSplitter{
				LocalIA:   ia,
				Core:      cfg.Core,
				Inspector: cfg.Inspector,
			},
		}
		perISD[ia.ISD()] = p
	}
	defaultPather = perISD[cfg.IA.ISD()]

	return &fetcher{
		defaultIA: cfg.IA,
		pather:    defaultPather,
		perISD:    perISD,
		config:    cfg.Cfg,
	}
}

// GetPaths uses the pather to get paths from src to dst.
// src may be either zero or the local IA (nothing else).
// privateOnly, if true, restricts the search to private ISD memberships only.
func (f *fetcher) GetPaths(ctx context.Context, src, dst addr.IA,
	refresh bool, privateOnly bool) ([]snet.Path, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, serrors.New("context must have deadline set")
	}

	log.Debug("sd fetcher: GetPaths request",
		"src", src,
		"dst", dst,
		"refresh", refresh,
		"privateOnly", privateOnly,
	)

	type candidate struct {
		pather *segfetcher.Pather
		dst    addr.IA
	}

	candidates := make([]candidate, 0, len(f.perISD))
	seen := make(map[*segfetcher.Pather]struct{}, len(f.perISD))
	addCandidate := func(p *segfetcher.Pather, candidateDst addr.IA) {
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		candidates = append(candidates, candidate{pather: p, dst: candidateDst})
	}

	// if the caller requests a specific source IA (membership), only use the
	// corresponding pather - derive the destination IA in that ISD if needed
	if !src.IsZero() {
		if p, ok := f.perISD[src.ISD()]; ok {
			target := dst
			// only rewrite the destination ISD if the requested source ISD is a
			// private membership
			if src.ISD() != f.defaultIA.ISD() && dst.ISD() != src.ISD() {
				if ia, err := addr.IAFrom(src.ISD(), dst.AS()); err == nil {
					target = ia
				}
			}
			addCandidate(p, target)
			// kkip adding other candidates, honor explicit membership selection
			goto COLLECT
		}
	}

	// if dst is private only return paths within the provided isd within dst
	if privatePather, isInISDList := f.perISD[dst.ISD()]; isInISDList && dst.ISD() != f.defaultIA.ISD() {
		addCandidate(privatePather, dst)
	} else {
		// if dst is public add public paths and all possible private paths
		for isd, p := range f.perISD {
			if isd == f.defaultIA.ISD() {
				// skip default (public) ISD if privateOnly is requested
				if privateOnly {
					continue
				}
				// leave public ia case unchanged
				addCandidate(p, dst)
			} else {
				target := dst
				var err error
				// add a as candidate target the same private isd as in the source, we don't know yet if both are really in the same private ISD
				target, err = addr.IAFrom(isd, dst.AS())
				if err != nil {
					log.Debug("unable to derive membership IA", "isd", isd, "as", dst.AS(), "err", err)
					continue
				}
				addCandidate(p, target)
			}

		}
	}

COLLECT:
	var (
		paths []snet.Path
		errs  serrors.List
	)
	for _, cand := range candidates {
		local := cand.pather.IA
		if !src.IsZero() && !src.Equal(local) {
			errs = append(errs, serrors.New("bad source AS", "src", src, "ia", local))
			continue
		}
		// correct source ia (membershio) is already saved in the pather instance
		ps, err := cand.pather.GetPaths(ctx, cand.dst, refresh)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		paths = append(paths, ps...)
	}
	if len(paths) == 0 && len(errs) > 0 {
		return nil, errs.ToError()
	}
	return paths, nil
}

type dstProvider struct {
}

func (r *dstProvider) Dst(_ context.Context, _ segfetcher.Request) (net.Addr, error) {
	return &snet.SVCAddr{SVC: addr.SvcCS}, nil
}

type neverLocal struct{}

func (neverLocal) IsSegLocal(_ segfetcher.Request) bool {
	return false
}
