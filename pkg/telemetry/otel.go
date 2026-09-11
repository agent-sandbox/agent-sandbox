/*
 * Copyright 2025 The https://github.com/agent-sandbox/agent-sandbox Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package telemetry

import (
	"context"
	"fmt"
	"os"

	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/log"
	otellog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"k8s.io/klog/v2"
)

const (
	// ServiceName is the OTel Resource "service.name" attribute emitted with
	// every TLog record. VictoriaLogs maps Resource attributes to stream
	// fields by default, so this is the LogsQL _stream label to filter on.
	ServiceName = "agent-sandbox"
	loggerScope = "agent-sandbox/telemetry"
)

// Init configures the OTel logger provider and wires it into the emitter.
// If endpoint is empty or exporter init fails, the emitter still runs and
// silently drops OTel records.
func Init(ctx context.Context, cfg Settings, endpoint, urlPath string, insecure bool) {
	if !cfg.Enabled {
		Configure(cfg, nil, nil)
		klog.Info("Telemetry disabled")
		return
	}

	if endpoint == "" {
		Configure(cfg, nil, nil)
		klog.Warning("Telemetry enabled but TELEMETRY_OTLP_ENDPOINT is empty — events will be dropped")
		return
	}

	opts := []otlploghttp.Option{otlploghttp.WithEndpoint(endpoint)}
	if urlPath != "" {
		opts = append(opts, otlploghttp.WithURLPath(urlPath))
	}
	if insecure {
		opts = append(opts, otlploghttp.WithInsecure())
	}

	exporter, err := otlploghttp.New(ctx, opts...)
	if err != nil {
		Configure(cfg, nil, nil)
		klog.Fatalf("Telemetry exporter init failed: %v", err)
		return
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("env", config.Cfg.EnvName),
			attribute.String("service.name", ServiceName),
			attribute.String("service.instance.id", instanceID()),
		),
	)
	if err != nil {
		klog.Fatalf("Telemetry resource init failed: %v", err)
		res = resource.Empty()
	}

	provider := otellog.NewLoggerProvider(
		otellog.WithResource(res),
		otellog.WithProcessor(otellog.NewBatchProcessor(exporter)),
	)

	var l log.Logger = provider.Logger(loggerScope)

	Configure(cfg, l, provider.Shutdown)
	klog.Infof("Telemetry initialized endpoint=%s path=%s insecure=%v", endpoint, urlPath, insecure)
}

func instanceID() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return fmt.Sprintf("pid-%d", os.Getpid())
}
