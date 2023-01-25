// Copyright 2023 ETH Zurich
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package processing

import (
	"net"
	"syscall"

	"github.com/scionproto/scion/go/lib/log"
	"github.com/scionproto/scion/go/lib/serrors"
	"golang.org/x/net/ipv4"
)

type packetForwarderContainer struct {
	// The address of the border router
	addr *net.UDPAddr
	// ForwarderCount is the amount of forwarders for a border router interface
	ForwarderCount uint32
	// The packet forwarders of this container
	Forwarders []*packetForwarder
	metrics    *ColigateMetrics
	// The maximum batch size when calling writeBatch()
	batchSize int
}

type packetForwarder struct {
	// The channel through which the workers provide their raw packets to send
	ForwardChannel chan []byte
	// The packet forwarder container to which this forwarder belongs to
	Container *packetForwarderContainer
}

// Creates a new packet forwarder container
func NewPacketForwarderContainer(addr *net.UDPAddr, batchSize int,
	metrics *ColigateMetrics, forwarderCount uint32) *packetForwarderContainer {
	return &packetForwarderContainer{
		addr:           addr,
		metrics:        metrics,
		batchSize:      batchSize,
		ForwarderCount: forwarderCount,
		Forwarders:     make([]*packetForwarder, 0, forwarderCount),
	}
}

// Creates a new packet forwarder inside a packet forwarder container
func (container *packetForwarderContainer) NewPacketForwarder() *packetForwarder {
	p := &packetForwarder{
		ForwardChannel: make(chan []byte),
		Container:      container,
	}
	container.Forwarders = append(container.Forwarders, p)
	return p
}

// Starts the packet forwarder. This should be called in a new
// go routine.
func (p *packetForwarder) Start() error {
	workerPacketOutTotalPromCounter := p.Container.metrics.WorkerPacketOutTotal
	workerPacketOutErrorPromCounter := p.Container.metrics.WorkerPacketOutError
	batchSize := p.Container.batchSize
	writeMsgs := make([]ipv4.Message, batchSize)
	for i := 0; i < batchSize; i++ {
		writeMsgs[i].Buffers = [][]byte{make([]byte, 1)}
	}
	conn, err := net.DialUDP("udp", nil, p.Container.addr)
	if err != nil {
		return serrors.New("PacketForwarder error while dialing", "err", err)
	}
	packetConn := ipv4.NewPacketConn(conn)
	defer packetConn.Close()
	exit := false
	for !exit {
		task := <-p.ForwardChannel
		if task == nil {
			return nil
		}
		writeMsgs[0].Buffers[0] = task
		i := 1
		// do we have more messages to send right now?
	loop:
		for i < batchSize {
			// we load packets from the channel till the batchSize is reached
			// or no new packets are available at the moment. In the latter
			// case we break out of the for loop
			select {
			case task := <-p.ForwardChannel:
				if task == nil {
					// process all current packets, then exit
					exit = true
					break loop
				}
				writeMsgs[i].Buffers[0] = task
				i++
			default:
				break loop
			}
		}
		k, err := packetConn.WriteBatch(writeMsgs[:i], syscall.MSG_DONTWAIT)
		if err != nil {
			log.Debug("PacketForwarder error while sending", "err", err)
			workerPacketOutErrorPromCounter.Add(float64(i))
			continue
		}

		// check whether all packets have been sent or not
		if k != i {
			workerPacketOutErrorPromCounter.Add(float64(i - k))
		}
		workerPacketOutTotalPromCounter.Add(float64(i))
	}
	return nil
}
