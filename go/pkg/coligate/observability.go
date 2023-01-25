package coligate

import (
	"io"

	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/scionproto/scion/go/lib/env"
	"github.com/scionproto/scion/go/lib/metrics"
	"github.com/scionproto/scion/go/lib/prom"
	"github.com/scionproto/scion/go/lib/topology"
)

type Metrics struct {
	UpdateSigmasTotal             *prometheus.CounterVec
	DataPacketInTotal             *prometheus.CounterVec
	DataPacketInInvalid           *prometheus.CounterVec
	DataPacketInDropped           *prometheus.CounterVec
	LoadActiveReservationsTotal   *prometheus.CounterVec
	WorkerPacketInTotal           *prometheus.CounterVec
	WorkerPacketOutTotal          *prometheus.CounterVec
	WorkerPacketOutError          *prometheus.CounterVec
	WorkerPacketInInvalid         *prometheus.CounterVec
	WorkerReservationUpdateTotal  *prometheus.CounterVec
	CleanupReservationUpdateTotal *prometheus.CounterVec
	CleanupReservationUpdateNew   *prometheus.CounterVec
	CleanupReservationDeleted     *prometheus.CounterVec
}

// InitTracer initializes the tracer
func InitTracer(tracing env.Tracing, id string) (io.Closer, error) {
	tracer, trCloser, err := tracing.NewTracer(id)
	if err != nil {
		return nil, err
	}
	opentracing.SetGlobalTracer(tracer)
	return trCloser, nil
}

// NewMetrics creates a new Metrics struct and returns it.
func NewMetrics() *Metrics {
	return &Metrics{
		UpdateSigmasTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "coligate_service_update_sigmas_total",
				Help: "Total number of update sigmas requests in the grpc endpoint.",
			},
			[]string{},
		),
		DataPacketInTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "coligate_data_packet_in_total",
				Help: "Total number of data packets received by the colibri gateway.",
			},
			[]string{},
		),
		DataPacketInInvalid: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "coligate_data_packet_in_invalid",
				Help: "Total number of data packets received by the colibri gateway " +
					"whose headers cannot be parsed correctly.",
			},
			[]string{},
		),
		DataPacketInDropped: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "coligate_data_packet_in_dropped",
				Help: "Total number of data packets received by the colibri gateway " +
					"that were dropped because of busy workers.",
			},
			[]string{},
		),
		LoadActiveReservationsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "coligate_init_load_active_reservations_total",
				Help: "Total number of active reservations received on startup by colibri service.",
			},
			[]string{},
		),
		WorkerPacketInTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "coligate_worker_packet_in_total",
				Help: "Total number of data packets processed by the workers.",
			},
			[]string{},
		),
		WorkerPacketOutTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "coligate_worker_packet_out_total",
				Help: "Total number of data packets forwarded by the workers.",
			},
			[]string{},
		),
		WorkerPacketOutError: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "coligate_worker_packet_out_error",
				Help: "Total number of data packets dropped by workers because of writing errors",
			},
			[]string{},
		),
		WorkerPacketInInvalid: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "coligate_worker_packet_in_invalid",
				Help: "Total number of invalid data packets processed by the workers.",
			},
			[]string{},
		),
		WorkerReservationUpdateTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "coligate_worker_reservation_update",
				Help: "Total number of reservation updates processed by the workers.",
			},
			[]string{},
		),
		CleanupReservationUpdateTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "coligate_cleanup_reservation_update_total",
				Help: "Total number of reservation updates registered in the cleanup routine.",
			},
			[]string{},
		),
		CleanupReservationUpdateNew: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "coligate_cleanup_reservation_update_new",
				Help: "Total number of reservation updates registered in the cleanup " +
					"routine that extend the validity of a reservation.",
			},
			[]string{},
		),
		CleanupReservationDeleted: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "coligate_cleanup_reservation_deleted",
				Help: "Total number of reservation deletion tasks distributed to the workers.",
			},
			[]string{},
		),
	}
}

// NewTopologyLoader returns new topology.LoaderMetrics.
func (m *Metrics) NewTopologyLoader() topology.LoaderMetrics {
	updates := prom.NewCounterVec("", "",
		"topology_updates_total",
		"The total number of updates.",
		[]string{prom.LabelResult},
	)
	return topology.LoaderMetrics{
		ValidationErrors: metrics.NewPromCounter(updates).With(prom.LabelResult, "err_validate"),
		ReadErrors:       metrics.NewPromCounter(updates).With(prom.LabelResult, "err_read"),
		LastUpdate: metrics.NewPromGauge(
			prom.NewGaugeVec("", "",
				"topology_last_update_time",
				"Timestamp of the last successful update.",
				[]string{},
			),
		),
		Updates: metrics.NewPromCounter(updates).With(prom.LabelResult, prom.Success),
	}
}
