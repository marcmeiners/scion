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

package config

import (
	"io"
	"math"

	"github.com/scionproto/scion/go/lib/config"
	"github.com/scionproto/scion/go/lib/serrors"
)

type ColigateConfig struct {
	//we have 4 bytes = 32 bits available for GatewayId+WorkerId+PerWorkerCounter
	NumBitsForGatewayId        int `toml:"NumBitsForGatewayId"`
	NumBitsForWorkerId         int `toml:"NumBitsForWorkerId"`
	NumBitsForPerWorkerCounter int `toml:"NumBitsForPerWorkerCounter"`

	//Timeout in sec
	COSyncTimeout         int               `toml:"COSyncTimeout"`
	ColibriGatewayID      int               `toml:"ColibriGatewayID"`
	NumWorkers            int               `toml:"NumWorkers"`
	MaxQueueSizePerWorker int               `toml:"MaxQueueSizePerWorker"`
	Salt                  string            `toml:"Salt"`
	ColigateGRPCAddr      string            `toml:"ColigateGRPCAddr"`
	Forwarder             []ForwarderConfig `toml:"Forwarder"`
}

type ForwarderConfig struct {
	InterfaceId int `toml:"InterfaceId"`
	BatchSize   int `toml:"BatchSize"`
	Count       int `toml:"Count"`
}

func (cfg *ColigateConfig) Validate() error {
	if cfg.NumBitsForGatewayId+cfg.NumBitsForWorkerId+cfg.NumBitsForPerWorkerCounter != 32 {
		return serrors.New(`"NumBitsForGatewayId + NumBitsForWorkerId +
		NumBitsForPerWorkerCounter != 32"`)
	}
	if cfg.NumBitsForPerWorkerCounter < 8 {
		return serrors.New("NumBitsForPerWorkerCounter < 8")
	}
	if cfg.NumWorkers < 1 {
		return serrors.New("NumWorkers < 1")
	}
	if int(math.Pow(2, float64(cfg.NumBitsForWorkerId))-1) < cfg.NumWorkers {
		return serrors.New("Too small bit count for too many workers", "NumBitsForWorkerId",
			cfg.NumBitsForWorkerId, "NumWorkers", cfg.NumWorkers)
	}
	for _, fw := range cfg.Forwarder {
		if fw.BatchSize < 1 {
			return serrors.New("ForwarderBatchSize < 1")
		}
	}
	return nil
}

func (cfg *ColigateConfig) InitDefaults() {
	if cfg.NumBitsForGatewayId == 0 || cfg.NumBitsForWorkerId == 0 ||
		cfg.NumBitsForPerWorkerCounter == 0 {

		cfg.NumBitsForGatewayId = 1
		cfg.NumBitsForWorkerId = 8
		cfg.NumBitsForPerWorkerCounter = 23
	}
	if cfg.NumWorkers == 0 {
		cfg.NumWorkers = 8
	}
	if cfg.MaxQueueSizePerWorker == 0 {
		cfg.MaxQueueSizePerWorker = 256
	}
	if cfg.ColibriGatewayID == 0 {
		cfg.ColibriGatewayID = 1
	}
	if cfg.COSyncTimeout == 0 {
		cfg.COSyncTimeout = 10
	}
	for _, fw := range cfg.Forwarder {
		if fw.BatchSize == 0 {
			fw.BatchSize = 16
		}
		if fw.Count == 0 {
			fw.Count = 1
		}
	}
}

func (cfg *ColigateConfig) Sample(dst io.Writer, _ config.Path, _ config.CtxMap) {
	config.WriteString(dst, "") //TODO(rohrerj) write coligate configuration sample
}

func (cfg *ColigateConfig) ConfigName() string {
	return "coligate"
}
