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

package processing

import (
	"context"
	"math/rand"
	"net"
	"syscall"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.com/scionproto/scion/go/coligate/storage"
	libaddr "github.com/scionproto/scion/go/lib/addr"
	"github.com/scionproto/scion/go/lib/log"
	libmetrics "github.com/scionproto/scion/go/lib/metrics"
	"github.com/scionproto/scion/go/lib/serrors"
	"github.com/scionproto/scion/go/lib/topology"
	"github.com/scionproto/scion/go/lib/util"
	"github.com/scionproto/scion/go/pkg/app"
	common "github.com/scionproto/scion/go/pkg/coligate"
	"github.com/scionproto/scion/go/pkg/coligate/config"
	cggrpc "github.com/scionproto/scion/go/pkg/coligate/grpc"
	libgrpc "github.com/scionproto/scion/go/pkg/grpc"
	copb "github.com/scionproto/scion/go/pkg/proto/colibri"
	cgpb "github.com/scionproto/scion/go/pkg/proto/coligate"
)

type Processor struct {
	localIA                   libaddr.IA
	dataChannels              []chan *dataPacket
	controlUpdateChannels     []chan *storage.UpdateTask
	controlDeletionChannels   []chan *storage.DeletionTask
	cleanupChannel            chan *storage.UpdateTask
	packetForwarderContainers map[uint16]*packetForwarderContainer
	saltHasher                common.SaltHasher
	exit                      bool
	metrics                   *ColigateMetrics
	numWorkers                int
}

// BufSize is the maximum size of a datapacket including all the headers.
const bufSize int = 9000

// NumMessages is the maximum number of messages that are read as a batch from the socket.
const numMessages int = 32

// ReceiverChannelSize is the size of the channel where the receiver writes its received
// packets and from where the parser reads them
const receiverChannelSize int = 256

func (c *Processor) shutdown() {
	if c.exit {
		return
	}
	c.exit = true
	c.cleanupChannel <- nil
	for i := 0; i < len(c.dataChannels); i++ {
		c.dataChannels[i] <- nil
	}
	for i := 0; i < len(c.controlUpdateChannels); i++ {
		c.controlUpdateChannels[i] <- nil
	}
	for i := 0; i < len(c.controlDeletionChannels); i++ {
		c.controlDeletionChannels[i] <- nil
	}
	for _, v := range c.packetForwarderContainers {
		for _, f := range v.Forwarders {
			f.ForwardChannel <- nil
		}
	}
}

// Init initializes the colibri gateway. Configures the channels, goroutines,
// and the control plane and the data plane.
func Init(ctx context.Context, cfg *config.Config, cleanup *app.Cleanup,
	g *errgroup.Group, topo *topology.Loader, metrics *common.Metrics) error {
	coligateInfo, err := topo.ColibriGateway(cfg.General.ID)
	if err != nil {
		return err
	}

	coligateConfig := &cfg.Coligate
	coligateMetrics := initializeMetrics(metrics)
	forwarderContainers := make(map[uint16]*packetForwarderContainer)
	for ifid, info := range topo.InterfaceInfoMap() {
		found := false
		// We skip all interface ids that are not used by this instance of Colibri Gateway
		for _, eggr := range coligateInfo.Egresses {
			if eggr == uint32(ifid) {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		// If a configuration for that interface exists use it otherwise use default values
		found = false
		var forwarderConfig config.ForwarderConfig
		for _, fw := range coligateConfig.Forwarder {
			if uint32(fw.InterfaceId) == uint32(ifid) {
				found = true
				forwarderConfig = fw
				break
			}
		}
		if !found {
			forwarderConfig = config.ForwarderConfig{
				InterfaceId: int(ifid),
				BatchSize:   16,
				Count:       1,
			}
		}
		container := NewPacketForwarderContainer(info.InternalAddr, forwarderConfig.BatchSize,
			coligateMetrics, uint32(forwarderConfig.Count))
		for i := 0; i < forwarderConfig.Count; i++ {
			pf := container.NewPacketForwarder()
			func(pf *packetForwarder) {
				g.Go(func() error {
					defer log.HandlePanic()
					log.Debug("Started Packet forwarder", "addr", pf.Container.addr.String())
					return pf.Start()
				})
			}(pf)
		}
		forwarderContainers[uint16(ifid)] = container
	}

	grpcAddr, err := net.ResolveTCPAddr("tcp", cfg.Coligate.ColigateGRPCAddr)
	if err != nil {
		return err
	}

	colibriServiceAddresses := topo.ColibriServiceAddresses()
	if len(colibriServiceAddresses) < 1 {
		return serrors.New("No instance of colibri service found in local AS.")
	}

	localAS := topo.IA().AS()

	// Loads the salt for load balancing from the config.
	// If the salt is empty a random value will be chosen
	salt := []byte(coligateConfig.Salt)
	if coligateConfig.Salt == "" {
		salt := make([]byte, 16)
		rand.Read(salt)
	}

	p := Processor{
		localIA: topo.IA(),
		// TODO(rohrerj) check cleanupChannel capacity
		cleanupChannel:            make(chan *storage.UpdateTask, 1000),
		dataChannels:              make([]chan *dataPacket, coligateConfig.NumWorkers),
		controlUpdateChannels:     make([]chan *storage.UpdateTask, coligateConfig.NumWorkers),
		controlDeletionChannels:   make([]chan *storage.DeletionTask, coligateConfig.NumWorkers),
		saltHasher:                common.NewFnv1aHasher(salt),
		metrics:                   coligateMetrics,
		numWorkers:                coligateConfig.NumWorkers,
		packetForwarderContainers: forwarderContainers,
	}

	cleanup.Add(func() error {
		p.shutdown()
		return nil
	})

	// Creates all the channels and starts the go routines
	for i := 0; i < p.numWorkers; i++ {
		p.dataChannels[i] = make(chan *dataPacket, coligateConfig.MaxQueueSizePerWorker)
		p.controlUpdateChannels[i] = make(chan *storage.UpdateTask,
			coligateConfig.MaxQueueSizePerWorker)
		// TODO(rohrerj) Check control deletion channel size
		p.controlDeletionChannels[i] = make(chan *storage.DeletionTask, 1000)
		func(i int) {
			g.Go(func() error {
				defer log.HandlePanic()
				return p.workerReceiveEntry(coligateConfig,
					uint32(i), uint32(coligateConfig.ColibriGatewayID), localAS,
				)
			})
		}(i)
	}

	g.Go(func() error {
		defer log.HandlePanic()
		p.initCleanupRoutine()
		return nil
	})

	g.Go(func() error {
		defer log.HandlePanic()
		return p.initControlPlane(coligateConfig, cleanup, grpcAddr)
	})

	// We start the data plane as soon as we retrieved the active reservations from colibri service
	if err := p.loadActiveReservationsFromColibriService(ctx, coligateConfig,
		colibriServiceAddresses[0], coligateConfig.COSyncTimeout, cfg.General.ID); err != nil {
		return err
	}
	if err := p.initDataPlane(coligateConfig, coligateInfo.Addr, g, cleanup); err != nil {
		return err
	}

	return nil
}

type ColigateMetrics struct {
	LoadActiveReservationsTotal   libmetrics.Counter
	CleanupReservationUpdateTotal libmetrics.Counter
	CleanupReservationUpdateNew   libmetrics.Counter
	CleanupReservationDeleted     libmetrics.Counter
	UpdateSigmasTotal             libmetrics.Counter
	DataPacketInTotal             libmetrics.Counter
	DataPacketInInvalid           libmetrics.Counter
	DataPacketInDropped           libmetrics.Counter
	WorkerPacketInTotal           libmetrics.Counter
	WorkerPacketInInvalid         libmetrics.Counter
	WorkerPacketOutTotal          libmetrics.Counter
	WorkerPacketOutError          libmetrics.Counter
	WorkerReservationUpdateTotal  libmetrics.Counter
}

func initializeMetrics(metrics *common.Metrics) *ColigateMetrics {
	c := &ColigateMetrics{
		LoadActiveReservationsTotal: libmetrics.NewPromCounter(
			metrics.LoadActiveReservationsTotal),
		CleanupReservationUpdateTotal: libmetrics.NewPromCounter(
			metrics.CleanupReservationUpdateTotal),
		CleanupReservationUpdateNew: libmetrics.NewPromCounter(
			metrics.CleanupReservationUpdateNew),
		CleanupReservationDeleted: libmetrics.NewPromCounter(
			metrics.CleanupReservationDeleted),
		UpdateSigmasTotal: libmetrics.NewPromCounter(
			metrics.UpdateSigmasTotal),
		DataPacketInTotal: libmetrics.NewPromCounter(
			metrics.DataPacketInTotal),
		DataPacketInInvalid: libmetrics.NewPromCounter(
			metrics.DataPacketInInvalid),
		DataPacketInDropped: libmetrics.NewPromCounter(
			metrics.DataPacketInDropped),
		WorkerPacketInTotal: libmetrics.NewPromCounter(
			metrics.WorkerPacketInTotal),
		WorkerPacketInInvalid: libmetrics.NewPromCounter(
			metrics.WorkerPacketInInvalid),
		WorkerPacketOutTotal: libmetrics.NewPromCounter(
			metrics.WorkerPacketOutTotal),
		WorkerPacketOutError: libmetrics.NewPromCounter(
			metrics.WorkerPacketOutError),
		WorkerReservationUpdateTotal: libmetrics.NewPromCounter(
			metrics.WorkerReservationUpdateTotal),
	}
	return c
}

// Loads the active EE Reservations from the colibri service
func (p *Processor) loadActiveReservationsFromColibriService(ctx context.Context,
	config *config.ColigateConfig, colibiServiceAddr *net.UDPAddr, timeout int,
	coligateId string) error {

	log.Info("Loading active reservation indices from colibri service")
	var response *copb.ActiveIndicesResponse
	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	for {
		if time.Now().After(deadline) {
			return serrors.New(
				"Loading active reservation indices from colibri service failed after timeout")
		}
		// TODO(rohrerj) Add security for connection with colibri service.
		grpcconn, err := grpc.Dial(colibiServiceAddr.String(), grpc.WithInsecure())
		if err != nil {
			continue
		}
		copbservice := copb.NewColibriServiceClient(grpcconn)
		response, err = copbservice.ActiveIndices(ctx, &copb.ActiveIndicesRequest{
			ColigateId: coligateId,
		})
		if err != nil {
			continue
		}
		break
	}
	p.metrics.LoadActiveReservationsTotal.Add(float64(len(response.Reservations)))
	for _, respReservation := range response.Reservations {
		highestValidity := time.Unix(0, 0)
		newId := [12]byte{}
		copy(newId[:], respReservation.Id.Suffix)

		res := storage.NewReservation(newId,
			[]storage.HopField{
				{
					EgressId: uint16(respReservation.Egress),
				},
			})
		for _, respResIndex := range respReservation.Indices {
			resIndex := storage.NewIndex(uint8(respResIndex.Index),
				util.SecsToTime(respResIndex.ExpirationTime),
				uint8(respResIndex.AllocBw), respResIndex.Sigmas)

			res.Indices[resIndex.Index] = resIndex
			if resIndex.Validity.Sub(highestValidity) > 0 {
				highestValidity = resIndex.Validity
			}
		}
		task := storage.NewUpdateTask(res, highestValidity)

		// Registers the reservation for deletion once it expires
		p.cleanupChannel <- task

		p.controlUpdateChannels[p.getWorkerForResId(task.Reservation.Id)] <- task
	}
	log.Info("Successfully loaded active reservation indices from colibri service")
	return nil
}

func (p *Processor) getWorkerForResId(resId [12]byte) uint32 {
	return p.saltHasher.Hash(resId[:]) % uint32(p.numWorkers)
}

// Initializes the cleanup routine that removes outdated reservations
func (p *Processor) initCleanupRoutine() {
	log.Info("Init cleanup routine")

	reservationExpirations := make(map[[12]byte]time.Time)

	updateExpirationMapping := (func(task *storage.UpdateTask) {
		p.metrics.CleanupReservationUpdateTotal.Add(1)
		res, exists := reservationExpirations[task.Reservation.Id]
		if !exists || task.HighestValidity.After(res) {
			p.metrics.CleanupReservationUpdateNew.Add(1)
			reservationExpirations[task.Reservation.Id] = task.HighestValidity
		}
	})
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()

	// The cleanup routine gets notified about all reservation updates and keeps track
	// of reservation validities. If a reservation is expired it sends an reservation
	// deletion task to the worker. This check is done periodically but gets delayed
	// if a lot of reservation updates are incoming.
	for !p.exit {
		select {
		case task := <-p.cleanupChannel:
			if task == nil {
				return
			}
			updateExpirationMapping(task)
		case now := <-t.C:
			updateTime := false
			for resId, val := range reservationExpirations {
			out:
				for {
					select {
					case task := <-p.cleanupChannel:
						if task == nil {
							return
						}
						updateExpirationMapping(task)
						if task.Reservation.Id == resId {
							// Updated current value in case it got changed
							val = reservationExpirations[resId]
						}
						updateTime = true
					default:
						if updateTime {
							// a long time might have passed and therfore
							// the time "now" might not be valid anymore
							now = time.Now()
							updateTime = false
						}
						break out
					}
				}
				if val.Before(now) {
					p.metrics.CleanupReservationDeleted.Add(1)

					workerId := p.getWorkerForResId(resId)
					p.controlDeletionChannels[workerId] <- storage.NewDeletionTask(resId)
					delete(reservationExpirations, resId)
				}
			}
		}
	}
}

// The function to initialize the control plane of the colibri gateway.
func (p *Processor) initControlPlane(config *config.ColigateConfig, cleanup *app.Cleanup,
	serverAddr *net.TCPAddr) error {

	log.Info("Init control plane", "addr", serverAddr)
	lis, err := net.ListenTCP("tcp", serverAddr)
	if err != nil {
		return err
	}

	s := grpc.NewServer(libgrpc.UnaryServerInterceptor())
	coligate := &cggrpc.Coligate{
		LocalIA:                      p.localIA,
		ReservationChannels:          p.controlUpdateChannels,
		CleanupChannel:               p.cleanupChannel,
		UpdateSigmasTotalPromCounter: p.metrics.UpdateSigmasTotal,
		FindWorker:                   p.getWorkerForResId,
	}
	cgpb.RegisterColibriGatewayServiceServer(s, coligate)
	cleanup.Add(func() error { s.GracefulStop(); return nil })

	return s.Serve(lis)
}

// The function to initialize the data plane of the colibri gateway.
func (p *Processor) initDataPlane(config *config.ColigateConfig, gatewayAddr *net.UDPAddr,
	g *errgroup.Group, cleanup *app.Cleanup) error {

	log.Info("Init data plane")
	udpConn, err := net.ListenUDP("udp", gatewayAddr)
	if err != nil {
		return err
	}
	cleanup.Add(func() error { udpConn.Close(); return nil })
	msgs := make([]ipv4.Message, numMessages)
	for i := 0; i < numMessages; i++ {
		msgs[i].Buffers = [][]byte{make([]byte, bufSize)}
	}

	var ipv4Conn *ipv4.PacketConn = ipv4.NewPacketConn(udpConn)
	recvChannel := make(chan []byte, receiverChannelSize)
	g.Go(func() error {
		defer log.HandlePanic()
		dataPacketInInvalidPromCounter := p.metrics.DataPacketInInvalid
		dataPacketInDroppedPromCounter := p.metrics.DataPacketInDropped
		for !p.exit {
			task := <-recvChannel
			if task == nil {
				return nil
			}
			d, err := Parse(task)
			if err != nil {
				log.Debug("error while parsing headers", "err", err)
				dataPacketInInvalidPromCounter.Add(1)
				continue
			}

			select {
			case p.dataChannels[p.getWorkerForResId(d.id)] <- d:
			default:
				dataPacketInDroppedPromCounter.Add(1)
				continue // Packet dropped
			}

		}
		return nil
	})

	g.Go(func() error {
		defer log.HandlePanic()
		defer func() {
			recvChannel <- nil
		}()
		dataPacketInTotalPromCounter := p.metrics.DataPacketInTotal
		dataPacketInDroppedPromCounter := p.metrics.DataPacketInDropped
		for !p.exit {
			numPkts, err := ipv4Conn.ReadBatch(msgs, syscall.MSG_WAITFORONE)
			if err != nil {
				log.Debug("error while reading from network", "err", err)
				continue
			}
			if numPkts == 0 {
				continue
			}
			dataPacketInTotalPromCounter.Add(float64(numPkts))
			for _, pkt := range msgs[:numPkts] {
				rawPacket := make([]byte, pkt.N)
				copy(rawPacket, pkt.Buffers[0][:pkt.N])
				select {
				case recvChannel <- rawPacket:
				default:
					dataPacketInDroppedPromCounter.Add(1)
				}
			}

		}
		return nil
	})
	return nil
}

// Configures a goroutine to listen for the data plane channel and control plane reservation updates
func (p *Processor) workerReceiveEntry(config *config.ColigateConfig, workerId uint32,
	gatewayId uint32, localAS libaddr.AS) error {

	log.Info("Init worker", "workerId", workerId)
	worker := NewWorker(config, workerId, gatewayId, localAS,
		p.packetForwarderContainers, p.metrics)
	workerPacketInTotalPromCounter := p.metrics.WorkerPacketInTotal
	workerPacketInInvalidPromCounter := p.metrics.WorkerPacketInInvalid
	workerReservationUpdateTotalPromCounter := p.metrics.WorkerReservationUpdateTotal

	ch := p.dataChannels[workerId]
	chres := p.controlUpdateChannels[workerId]
	chresD := p.controlDeletionChannels[workerId]
	for !p.exit {
		// Check whether new reservation indices have to be processed.
		// This has priority above the data plane packets.
		select {
		case task := <-chres:
			if task == nil {
				return nil
			}
			workerReservationUpdateTotalPromCounter.Add(1)
			task.Execute(worker.Storage)
			continue
		default:
		}

		select {
		case d := <-ch: // Data plane packet received
			if d == nil { // If d is nil it is meant to be a exit sequence
				return nil
			}
			workerPacketInTotalPromCounter.Add(1)
			if err := worker.realParse(d); err != nil {
				log.Debug("Worker received error while parsing.", "workerId", workerId,
					"error", err.Error())
				workerPacketInInvalidPromCounter.Add(1)
				continue
			}
			if err := worker.process(d); err != nil {
				log.Debug("Worker received error while processing.", "workerId", workerId,
					"error", err.Error())
				workerPacketInInvalidPromCounter.Add(1)
				continue
			}
		case task := <-chres:
			if task == nil {
				return nil
			}
			workerReservationUpdateTotalPromCounter.Add(1)
			task.Execute(worker.Storage)
		case task := <-chresD: // Reservation deletion received.
			if task == nil {
				return nil
			}
			workerReservationUpdateTotalPromCounter.Add(1)
			task.Execute(worker.Storage)
		}

	}
	return nil
}
