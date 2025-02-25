// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package nreventexporter // import "github.com/shelson/nreventexporter"
import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/otlphttpexporter"
)

// NewFactory creates a factory for OTLP exporter.
func NewFactory() exporter.Factory {
	otlpHttpExporterFactory:= otlphttpexporter.NewFactory()
	return exporter.NewFactory(
		otlpHttpExporterFactory.Type(),
		createDefaultConfig(otlpHttpExporterFactory),
		exporter.WithMetrics(createMetricsExporter(otlpHttpExporterFactory), otlpHttpExporterFactory.MetricsStability()),
	)
}

func createDefaultConfig(otlpHttpExporterFactory exporter.Factory) component.CreateDefaultConfigFunc {
	return func() component.Config {
		otlpHttpExporterConfig:=otlpHttpExporterFactory.CreateDefaultConfig().(*otlphttpexporter.Config)
		otlpHttpExporterConfig.Endpoint=""
		return otlpHttpExporterConfig
	}
}

func createMetricsExporter(otlpHttpExporterFactory exporter.Factory) exporter.CreateMetricsFunc{
	return func(ctx context.Context, set exporter.Settings,cfg component.Config,) (exporter.Metrics, error) {
		return otlpHttpExporterFactory.CreateMetrics(ctx, set, cfg)
	}
}