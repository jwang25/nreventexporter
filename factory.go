// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nreventexporter // import "github.com/shelson/nreventexporter"
import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/jwang25/nreventexporter/internal/metadata"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configcompression"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/config/configopaque"
	"go.opentelemetry.io/collector/config/configretry"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/exporter/otlphttpexporter"
)

// NewFactory creates a factory for OTLP exporter.
func NewFactory() exporter.Factory {
	otlpHttpExporterFactory:= otlphttpexporter.NewFactory()
	return exporter.NewFactory(
		metadata.Type,
		createDefaultConfig,
		exporter.WithMetrics(createMetrics, otlpHttpExporterFactory.MetricsStability()),
	)
}
func createDefaultConfig() component.Config {
	return &Config{
		RetryConfig: configretry.NewDefaultBackOffConfig(),
		ClientConfig: confighttp.ClientConfig{
			Endpoint: "",
			Timeout:  30 * time.Second,
			Headers:  map[string]configopaque.String{},
			// Default to gzip compression
			Compression: configcompression.TypeGzip,
			// We almost read 0 bytes, so no need to tune ReadBufferSize.
			WriteBufferSize: 512 * 1024,
		},
	}
}

// composeSignalURL composes the final URL for the signal (traces, metrics, logs) based on the configuration.
// oCfg is the configuration of the exporter.
// signalOverrideURL is the URL specified in the signal specific configuration (empty if not specified).
// signalName is the name of the signal, e.g. "traces", "metrics", "logs".
// signalVersion is the version of the signal, e.g. "v1" or "v1development".
func composeSignalURL(oCfg *Config, signalOverrideURL string, signalName string, signalVersion string) (string, error) {
	switch {
	case signalOverrideURL != "":
		_, err := url.Parse(signalOverrideURL)
		if err != nil {
			return "", fmt.Errorf("%s_endpoint must be a valid URL", signalName)
		}
		return signalOverrideURL, nil
	case oCfg.Endpoint == "":
		return "", fmt.Errorf("either endpoint or %s_endpoint must be specified", signalName)
	default:
		if strings.HasSuffix(oCfg.Endpoint, "/") {
			return oCfg.Endpoint + signalVersion + "/" + signalName, nil
		}
		return oCfg.Endpoint + "/" + signalVersion + "/" + signalName, nil
	}
}



func createMetrics(ctx context.Context, set exporter.Settings,cfg component.Config) (exporter.Metrics, error) {
		oce, err := newExporter(cfg, set)
			if err != nil {
		return nil, err
	}
	oCfg := cfg.(*Config)
	oce.metricsURL, err = composeSignalURL(oCfg, oCfg.MetricsEndpoint, "metrics", "v1")
	if err != nil {
		return nil, err
	}
	

	return exporterhelper.NewMetrics(ctx, set, cfg,
		oce.pushMetrics,
		exporterhelper.WithStart(oce.Start),
		exporterhelper.WithCapabilities(consumer.Capabilities{MutatesData: false}),
		// explicitly disable since we rely on http.Client timeout logic.
		exporterhelper.WithTimeout(exporterhelper.TimeoutConfig{Timeout: 0}),
		exporterhelper.WithRetry(oCfg.RetryConfig),
		exporterhelper.WithQueue(oCfg.QueueConfig))
}