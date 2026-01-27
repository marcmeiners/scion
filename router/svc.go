// Copyright 2020 Anapaya Systems
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

package router

import (
	"math/rand/v2"
	"slices"
	"sync"

	"github.com/scionproto/scion/pkg/addr"
)

// SvcKey identifies a service instance for a specific destination IA.
type SvcKey struct {
	Svc addr.SVC
	IA  addr.IA
}

// MakeSvcKey builds a SvcKey for convenience when callers don't want to fill the struct literal.
func MakeSvcKey(svc addr.SVC, ia addr.IA) SvcKey {
	return SvcKey{Svc: svc, IA: ia}
}

// Anycast address map keyed by service and destination IA.
type Services[addrT comparable] struct {
	mtx sync.Mutex
	m   map[SvcKey][]addrT
}

func NewServices[addrT comparable]() *Services[addrT] {
	return &Services[addrT]{m: make(map[SvcKey][]addrT)}
}

func (s *Services[addrT]) AddSvc(svc addr.SVC, ia addr.IA, a addrT) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	key := SvcKey{Svc: svc, IA: ia}
	addrs := s.m[key]
	if slices.Contains(addrs, a) {
		return
	}
	s.m[key] = append(addrs, a)
}

func (s *Services[addrT]) DelSvc(svc addr.SVC, ia addr.IA, a addrT) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	key := SvcKey{Svc: svc, IA: ia}
	addrs := s.m[key]
	index := slices.Index(addrs, a)
	if index == -1 {
		return
	}
	addrs[index] = addrs[len(addrs)-1]
	var zeroAddr addrT
	addrs[len(addrs)-1] = zeroAddr
	s.m[key] = addrs[:len(addrs)-1]
}

func (s *Services[addrT]) Any(svc addr.SVC, ia addr.IA) (addrT, bool) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	key := SvcKey{Svc: svc, IA: ia}
	addrs := s.m[key]
	if len(addrs) == 0 {
		var zeroAddr addrT
		return zeroAddr, false
	}
	return addrs[rand.IntN(len(addrs))], true
}
