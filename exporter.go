package nreventexporter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"time"

	"github.com/jwang25/nreventexporter/internal/httphelper"
	"github.com/jwang25/nreventexporter/internal/metadata"
	"github.com/jwang25/nreventexporter/internal/metrictoevent"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/proto"
)

var _ exporter.Metrics = (*baseExporter)(nil)

type baseExporter struct {
	// Input configuration.
	config       *Config
	client       *http.Client
	tracesURL    string
	metricsURL   string
	logsURL      string
	logger       *zap.Logger
	settings     exporter.Settings
	otlpExporter *exporter.Metrics
	// Default user-agent header.
	userAgent        string
	telemetryBuilder *metadata.TelemetryBuilder
}

const (
	headerRetryAfter         = "Retry-After"
	maxHTTPResponseReadBytes = 64 * 1024

	jsonContentType     = "application/json"
	protobufContentType = "application/x-protobuf"
)

// Create new exporter.
func newExporter(otlpExporter *exporter.Metrics, cfg Config, set exporter.Settings, telemetryBuilder *metadata.TelemetryBuilder) (*baseExporter, error) {
	if cfg.MetricsEndpoint != "" {
		_, err := url.Parse(cfg.MetricsEndpoint)
		if err != nil {
			return nil, errors.New("endpoint must be a valid URL")
		}
	}

	userAgent := fmt.Sprintf("%s/%s (%s/%s)",
		set.BuildInfo.Description, set.BuildInfo.Version, runtime.GOOS, runtime.GOARCH)

	// client construction is deferred to start
	return &baseExporter{
		otlpExporter:     otlpExporter,
		config:           &cfg,
		logger:           set.Logger,
		userAgent:        userAgent,
		settings:         set,
		telemetryBuilder: telemetryBuilder,
	}, nil
}
func (e *baseExporter) Capabilities() consumer.Capabilities {
	return (*e.otlpExporter).Capabilities()
}

func (e *baseExporter) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	return (*e.otlpExporter).ConsumeMetrics(ctx, md)
}

// start actually creates the HTTP client. The client construction is deferred till this point as this
// is the only place we get hold of Extensions which are required to construct auth round tripper.
func (e *baseExporter) Start(ctx context.Context, host component.Host) error {
	client, err := e.config.ClientConfig.ToClient(ctx, host, e.settings.TelemetrySettings)
	if err != nil {
		return err
	}
	e.client = client
	return nil
}

// Shutdown executes the provided ShutdownFunc if it's not nil.
func (e *baseExporter) Shutdown(ctx context.Context) error {
	return (*e.otlpExporter).Shutdown(ctx)
}

func (e *baseExporter) pushMetrics(ctx context.Context, md pmetric.Metrics) error {
	//tr := pmetricotlp.NewExportRequestFromMetrics(md)

	e.logger.Info("MetricsExporter",
		zap.Int("resource metrics", md.ResourceMetrics().Len()),
		zap.Int("metrics", md.MetricCount()),
		zap.Int("data points", md.DataPointCount()))

	var err error
	var request []byte

	// Build a NR event payload from the metrics data
	var counter int // number of events in the payload
	request, counter = metrictoevent.BuildNREventPayload(e.logger, md)

	e.logger.Debug("MetricsExporter", zap.Int("compressed size", len(request)))

	if err != nil {
		return consumererror.NewPermanent(err)
	}
	return e.export(ctx, e.metricsURL, request, e.metricsPartialSuccessHandler, counter)
}

func (e *baseExporter) export(ctx context.Context, url string, request []byte, partialSuccessHandler partialSuccessHandler, counter int) error {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(request))
	if err != nil {
		return consumererror.NewPermanent(err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Api-Key", e.config.APIKey)
	req.Header.Set("Content-Encoding", "gzip")

	//e.logger.Debug("Headers", zap.Any("headers", req.Header))
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make an HTTP request: %w", err)
	}
	e.recordMetrics(time.Since(start), counter, req, nil)

	defer func() {
		// Discard any remaining response body when we are done reading.
		io.CopyN(io.Discard, resp.Body, maxHTTPResponseReadBytes) // nolint:errcheck
		resp.Body.Close()
	}()

	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return handlePartialSuccessResponse(resp, partialSuccessHandler)
	}

	respStatus := readResponseStatus(resp)

	// Format the error message. Use the status if it is present in the response.
	var errString string
	var formattedErr error
	if respStatus != nil {
		errString = fmt.Sprintf(
			"error exporting items, request to %s responded with HTTP Status Code %d, Message=%s, Details=%v",
			url, resp.StatusCode, respStatus.Message, respStatus.Details)
	} else {
		errString = fmt.Sprintf(
			"error exporting items, request to %s responded with HTTP Status Code %d",
			url, resp.StatusCode)
	}
	formattedErr = httphelper.NewStatusFromMsgAndHTTPCode(errString, resp.StatusCode).Err()

	if isRetryableStatusCode(resp.StatusCode) {
		// A retry duration of 0 seconds will trigger the default backoff policy
		// of our caller (retry handler).
		retryAfter := 0

		// Check if the server is overwhelmed.
		// See spec https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/protocol/otlp.md#otlphttp-throttling
		isThrottleError := resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable
		if val := resp.Header.Get(headerRetryAfter); isThrottleError && val != "" {
			if seconds, err2 := strconv.Atoi(val); err2 == nil {
				retryAfter = seconds
			}
		}

		return exporterhelper.NewThrottleRetry(formattedErr, time.Duration(retryAfter)*time.Second)
	}
	return consumererror.NewPermanent(formattedErr)
}

// Determine if the status code is retryable according to the specification.
// For more, see https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/protocol/otlp.md#failures-1
func isRetryableStatusCode(code int) bool {
	switch code {
	case http.StatusTooManyRequests:
		return true
	case http.StatusBadGateway:
		return true
	case http.StatusServiceUnavailable:
		return true
	case http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func readResponseBody(resp *http.Response) ([]byte, error) {
	if resp.ContentLength == 0 {
		return nil, nil
	}

	maxRead := resp.ContentLength

	// if maxRead == -1, the ContentLength header has not been sent, so read up to
	// the maximum permitted body size. If it is larger than the permitted body
	// size, still try to read from the body in case the value is an error. If the
	// body is larger than the maximum size, proto unmarshaling will likely fail.
	if maxRead == -1 || maxRead > maxHTTPResponseReadBytes {
		maxRead = maxHTTPResponseReadBytes
	}
	protoBytes := make([]byte, maxRead)
	n, err := io.ReadFull(resp.Body, protoBytes)

	// No bytes read and an EOF error indicates there is no body to read.
	if n == 0 && (err == nil || errors.Is(err, io.EOF)) {
		return nil, nil
	}

	// io.ReadFull will return io.ErrorUnexpectedEOF if the Content-Length header
	// wasn't set, since we will try to read past the length of the body. If this
	// is the case, the body will still have the full message in it, so we want to
	// ignore the error and parse the message.
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}

	return protoBytes[:n], nil
}

// Read the response and decode the status.Status from the body.
// Returns nil if the response is empty or cannot be decoded.
func readResponseStatus(resp *http.Response) *status.Status {
	var respStatus *status.Status
	if resp.StatusCode >= 400 && resp.StatusCode <= 599 {
		// Request failed. Read the body. OTLP spec says:
		// "Response body for all HTTP 4xx and HTTP 5xx responses MUST be a
		// Protobuf-encoded Status message that describes the problem."
		respBytes, err := readResponseBody(resp)
		if err != nil {
			return nil
		}

		// Decode it as Status struct. See https://github.com/open-telemetry/opentelemetry-specification/blob/main/specification/protocol/otlp.md#failures
		respStatus = &status.Status{}
		err = proto.Unmarshal(respBytes, respStatus)
		if err != nil {
			return nil
		}
	}

	return respStatus
}

func handlePartialSuccessResponse(resp *http.Response, partialSuccessHandler partialSuccessHandler) error {
	bodyBytes, err := readResponseBody(resp)
	if err != nil {
		return err
	}

	return partialSuccessHandler(bodyBytes, resp.Header.Get("Content-Type"))
}

type partialSuccessHandler func(bytes []byte, contentType string) error

func (e *baseExporter) metricsPartialSuccessHandler(protoBytes []byte, contentType string) error {
	if protoBytes == nil {
		return nil
	}
	exportResponse := pmetricotlp.NewExportResponse()
	switch contentType {
	case protobufContentType:
		err := exportResponse.UnmarshalProto(protoBytes)
		if err != nil {
			return fmt.Errorf("error parsing protobuf response: %w", err)
		}
	case jsonContentType:
		err := exportResponse.UnmarshalJSON(protoBytes)
		if err != nil {
			return fmt.Errorf("error parsing json response: %w", err)
		}
	default:
		return nil
	}

	partialSuccess := exportResponse.PartialSuccess()
	if !(partialSuccess.ErrorMessage() == "" && partialSuccess.RejectedDataPoints() == 0) {
		e.logger.Warn("Partial success response",
			zap.String("message", exportResponse.PartialSuccess().ErrorMessage()),
			zap.Int64("dropped_data_points", exportResponse.PartialSuccess().RejectedDataPoints()),
		)
	}
	return nil
}

func (e *baseExporter) recordMetrics(duration time.Duration, count int, req *http.Request, resp *http.Response) {
	statusCode := 0

	if resp != nil {
		statusCode = resp.StatusCode
	}

	e.logger.Debug("Exporter request", zap.Int("status_code", statusCode), zap.String("endpoint", req.URL.String()), zap.Int("count", count))

	id := "nreventexporter"

	attrs := attribute.NewSet(
		attribute.String("status_code", fmt.Sprint(statusCode)),
		attribute.String("endpoint", req.URL.String()),
		attribute.String("exporter", id),
	)
	e.telemetryBuilder.ExporterRequestsDuration.Add(context.Background(), duration.Milliseconds(), metric.WithAttributeSet(attrs))
	e.telemetryBuilder.ExporterRequestsBytes.Add(context.Background(), req.ContentLength, metric.WithAttributeSet(attrs))
	e.telemetryBuilder.ExporterRequestsRecords.Add(context.Background(), int64(count), metric.WithAttributeSet(attrs))
	e.telemetryBuilder.ExporterRequestsSent.Add(context.Background(), 1, metric.WithAttributeSet(attrs))
}
