// SPDX-License-Identifier: GPL-3.0-or-later

package xray

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/features/stats"
	"github.com/xtls/xray-core/infra/conf"

	// Registers every Xray feature (protocols, transports, app services).
	_ "github.com/xtls/xray-core/main/distro/all"
)

// Instance is a running Xray core.
type Instance struct {
	core *core.Instance
}

// Build converts cfg to Xray's internal protobuf config using the upstream
// JSON loader, so validation errors match what the xray binary reports.
func Build(cfg Config) (*core.Config, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	return BuildJSON(raw)
}

// BuildJSON converts an Xray JSON config to Xray's internal config.
func BuildJSON(raw []byte) (*core.Config, error) {
	var jc conf.Config
	if err := json.Unmarshal(raw, &jc); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	pb, err := jc.Build()
	if err != nil {
		return nil, fmt.Errorf("build config: %w", err)
	}
	return pb, nil
}

// Start builds cfg and starts an Xray instance in this process.
func Start(cfg Config) (*Instance, error) {
	pb, err := Build(cfg)
	if err != nil {
		return nil, err
	}
	return start(pb)
}

// StartJSON starts an Xray instance from a JSON config.
func StartJSON(raw []byte) (*Instance, error) {
	pb, err := BuildJSON(raw)
	if err != nil {
		return nil, err
	}
	return start(pb)
}

func start(pb *core.Config) (*Instance, error) {
	inst, err := core.New(pb)
	if err != nil {
		return nil, fmt.Errorf("create xray instance: %w", err)
	}
	if err := inst.Start(); err != nil {
		_ = inst.Close()
		return nil, fmt.Errorf("start xray instance: %w", err)
	}
	return &Instance{core: inst}, nil
}

// Traffic returns bytes sent (up) and received (down) through all tagged
// outbounds since start. Outbound counters are used because Xray's TUN
// inbound bypasses the inbound counters. Stats must be enabled (WithSOCKS
// and WithTUN do it); otherwise both are 0.
func (i *Instance) Traffic() (up, down int64) {
	m, ok := i.core.GetFeature(stats.ManagerType()).(interface {
		VisitCounters(func(string, stats.Counter) bool)
	})
	if !ok {
		return 0, 0
	}
	m.VisitCounters(func(name string, c stats.Counter) bool {
		if !strings.HasPrefix(name, "outbound>>>") {
			return true
		}
		switch {
		case strings.HasSuffix(name, ">>>uplink"):
			up += c.Value()
		case strings.HasSuffix(name, ">>>downlink"):
			down += c.Value()
		}
		return true
	})
	return up, down
}

// Close stops the instance and releases its listeners.
func (i *Instance) Close() error {
	return i.core.Close()
}
