// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nreventexporter // import "github.com/shelson/nreventexporter"
import (
	"context"
	"fmt"

	"github.com/jwang25/nreventexporter/internal/metadata"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/configopaque"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/exporter/otlphttpexporter"
)

// NewFactory creates a factory for OTLP exporter.
func NewFactory() exporter.Factory {
	otlpHttpExporterFactory := otlphttpexporter.NewFactory()
	return exporter.NewFactory(
		otlpHttpExporterFactory.Type(),
		createDefaultConfig(otlpHttpExporterFactory),
		exporter.WithMetrics(createMetrics(otlpHttpExporterFactory), otlpHttpExporterFactory.MetricsStability()),
	)
}
func createDefaultConfig(otlpHttpExporterFactory exporter.Factory) component.CreateDefaultConfigFunc {
	return func() component.Config {
		otlpHttpExporterDefaultConfig := otlpHttpExporterFactory.CreateDefaultConfig().(*otlphttpexporter.Config)
		otlpHttpExporterDefaultConfig.Endpoint = ""
		otlpHttpExporterDefaultConfig.Headers = map[string]configopaque.String{}
		fmt.Println("Printing otlp default configs", otlpHttpExporterDefaultConfig.MetricsEndpoint)
		return &Config{
			otlpHttpExporterConfig: otlpHttpExporterDefaultConfig,
		}
	}
}
func createMetrics(otlpHttpExporterFactory exporter.Factory) exporter.CreateMetricsFunc {
	return func(ctx context.Context, set exporter.Settings, cfg component.Config) (exporter.Metrics, error) {
		c := cfg.(*Config)
		fmt.Println("create metrics config", c)
		otlpExporter, err := otlpHttpExporterFactory.CreateMetrics(ctx, set, c)
		if err != nil {
			return nil, err
		}
		telemetryBuilder, err := metadata.NewTelemetryBuilder(set.TelemetrySettings)
		if err != nil {
			return nil, err
		}
		oce, err := newExporter(&otlpExporter, *c, set, telemetryBuilder)
		if err != nil {
			return nil, err
		}
		return exporterhelper.NewMetrics(ctx, set, cfg,
			oce.pushMetrics,
			exporterhelper.WithStart(oce.Start),
			exporterhelper.WithCapabilities(otlpExporter.Capabilities()),
			// explicitly disable since we rely on http.Client timeout logic.
			exporterhelper.WithTimeout(exporterhelper.TimeoutConfig{Timeout: 0}),
			exporterhelper.WithRetry(c.otlpHttpExporterConfig.RetryConfig),
			exporterhelper.WithQueue(c.otlpHttpExporterConfig.QueueConfig))
	}
}
