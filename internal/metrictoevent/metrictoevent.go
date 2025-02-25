package metrictoevent

import (
	"bytes"
	"compress/gzip"
	"encoding/json"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

type nrEvent map[string]interface{}

// resourceToDimensions will return a set of dimension from the
// resource attributes, including a cloud host id (AWSUniqueId, gcp_id, etc.)
// if it can be constructed from the provided metadata.
func resourceToEventMap(res pcommon.Resource, eventMap nrEvent) error {

	res.Attributes().Range(func(k string, val pcommon.Value) bool {
		eventMap[k] = val.AsString()
		return true
	})

	return nil
}

// metricToEventMap will return a set of dimensions from the
// metric attributes.
func metricToEventMap(currentMetric pmetric.Metric) nrEvent {
	nrEventMap := make(nrEvent)
	nrEventMap["eventType"] = "otel_statsd"
	nrEventMap["name"] = currentMetric.Name()
	nrEventMap["type"] = currentMetric.Type().String()
	if nrEventMap["type"] == "Gauge" {
		nrEventMap["valueType"] = currentMetric.Gauge().DataPoints().At(0).ValueType().String()
		if nrEventMap["valueType"] == "Double" {
			nrEventMap["value"] = currentMetric.Gauge().DataPoints().At(0).DoubleValue()
		} else {
			nrEventMap["value"] = currentMetric.Gauge().DataPoints().At(0).IntValue()
		}
		nrEventMap["timestamp"] = currentMetric.Gauge().DataPoints().At(0).Timestamp().String()

		currentMetric.Gauge().DataPoints().At(0).Attributes().Range(func(k string, val pcommon.Value) bool {
			nrEventMap[k] = val.AsString()
			return true
		})
	} else if nrEventMap["type"] == "Sum" {
		nrEventMap["valueType"] = currentMetric.Sum().DataPoints().At(0).ValueType().String()
		if nrEventMap["valueType"] == "Double" {
			nrEventMap["value"] = currentMetric.Sum().DataPoints().At(0).DoubleValue()
		} else {
			nrEventMap["value"] = currentMetric.Sum().DataPoints().At(0).IntValue()
		}
		nrEventMap["timestamp"] = currentMetric.Sum().DataPoints().At(0).Timestamp().String()

		currentMetric.Sum().DataPoints().At(0).Attributes().Range(func(k string, val pcommon.Value) bool {
			nrEventMap[k] = val.AsString()
			return true
		})
	} else {
		// We need to handle Histogram and Summary most likely
	}
	return nrEventMap
}

// MetricsToNREvents converts pdata.Metrics to New Relic event json.
func MetricsToNREvents(logger *zap.Logger, md pmetric.Metrics) []nrEvent {
	var nrEventList []nrEvent
	rms := md.ResourceMetrics()
	logger.Debug("MetricsExporter", zap.Int("ResourceMetricsCount", rms.Len()))
	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)
		//resourceToEventMap(rm.Resource(), nrEventMap)

		for j := 0; j < rm.ScopeMetrics().Len(); j++ {
			ilm := rm.ScopeMetrics().At(j)
			for k := 0; k < ilm.Metrics().Len(); k++ {
				currentMetric := ilm.Metrics().At(k)
				nrEventMap := metricToEventMap(currentMetric)
				nrEventList = append(nrEventList, nrEventMap)
			}
		}
	}

	return nrEventList
}

// Build the compressed JSON payload from pdata.Metrics
func BuildNREventPayload(logger *zap.Logger, md pmetric.Metrics) ([]byte, int) {
	nrEventList := MetricsToNREvents(logger, md)
	// Convert the slice of maps to a JSON string
	request, _ := json.Marshal(nrEventList)

	// we gzip this and return it
	return CompressNREventPayload(string(request)), len(nrEventList)
}

func CompressNREventPayload(payload string) []byte {
	// Compress the JSON string
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	gz.Write([]byte(payload))
	gz.Close()
	return buffer.Bytes()
}
