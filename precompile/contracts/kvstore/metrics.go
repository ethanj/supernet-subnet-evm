// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package kvstore

import (
	"time"

	"github.com/ava-labs/libevm/metrics"
)

// KV Precompile Metrics
// These metrics track the performance and usage of the KV precompile operations
var (
	// Operation counters
	kvSetCounter    = metrics.NewRegisteredCounter("precompile/kvstore/set/count", nil)
	kvGetCounter    = metrics.NewRegisteredCounter("precompile/kvstore/get/count", nil)
	kvDeleteCounter = metrics.NewRegisteredCounter("precompile/kvstore/delete/count", nil)
	kvExistsCounter = metrics.NewRegisteredCounter("precompile/kvstore/exists/count", nil)
	kvKeysCounter   = metrics.NewRegisteredCounter("precompile/kvstore/keys/count", nil)

	// Success/failure counters
	kvSetSuccessCounter    = metrics.NewRegisteredCounter("precompile/kvstore/set/success", nil)
	kvGetSuccessCounter    = metrics.NewRegisteredCounter("precompile/kvstore/get/success", nil)
	kvDeleteSuccessCounter = metrics.NewRegisteredCounter("precompile/kvstore/delete/success", nil)
	kvExistsSuccessCounter = metrics.NewRegisteredCounter("precompile/kvstore/exists/success", nil)
	kvKeysSuccessCounter   = metrics.NewRegisteredCounter("precompile/kvstore/keys/success", nil)

	kvSetFailureCounter    = metrics.NewRegisteredCounter("precompile/kvstore/set/failure", nil)
	kvGetFailureCounter    = metrics.NewRegisteredCounter("precompile/kvstore/get/failure", nil)
	kvDeleteFailureCounter = metrics.NewRegisteredCounter("precompile/kvstore/delete/failure", nil)
	kvExistsFailureCounter = metrics.NewRegisteredCounter("precompile/kvstore/exists/failure", nil)
	kvKeysFailureCounter   = metrics.NewRegisteredCounter("precompile/kvstore/keys/failure", nil)

	// Gas usage histograms
	kvSetGasHistogram    = metrics.NewRegisteredHistogram("precompile/kvstore/set/gas", nil, metrics.NewExpDecaySample(1028, 0.015))
	kvGetGasHistogram    = metrics.NewRegisteredHistogram("precompile/kvstore/get/gas", nil, metrics.NewExpDecaySample(1028, 0.015))
	kvDeleteGasHistogram = metrics.NewRegisteredHistogram("precompile/kvstore/delete/gas", nil, metrics.NewExpDecaySample(1028, 0.015))
	kvExistsGasHistogram = metrics.NewRegisteredHistogram("precompile/kvstore/exists/gas", nil, metrics.NewExpDecaySample(1028, 0.015))
	kvKeysGasHistogram   = metrics.NewRegisteredHistogram("precompile/kvstore/keys/gas", nil, metrics.NewExpDecaySample(1028, 0.015))

	// Execution time histograms (in nanoseconds)
	kvSetExecutionTimer    = metrics.NewRegisteredTimer("precompile/kvstore/set/execution_time", nil)
	kvGetExecutionTimer    = metrics.NewRegisteredTimer("precompile/kvstore/get/execution_time", nil)
	kvDeleteExecutionTimer = metrics.NewRegisteredTimer("precompile/kvstore/delete/execution_time", nil)
	kvExistsExecutionTimer = metrics.NewRegisteredTimer("precompile/kvstore/exists/execution_time", nil)
	kvKeysExecutionTimer   = metrics.NewRegisteredTimer("precompile/kvstore/keys/execution_time", nil)

	// Data size metrics
	kvValueSizeHistogram = metrics.NewRegisteredHistogram("precompile/kvstore/value_size", nil, metrics.NewExpDecaySample(1028, 0.015))
	kvKeyCountGauge      = metrics.NewRegisteredGauge("precompile/kvstore/total_keys", nil)

	// Overall precompile metrics
	kvTotalOperationsCounter = metrics.NewRegisteredCounter("precompile/kvstore/total_operations", nil)
	kvTotalGasUsed          = metrics.NewRegisteredCounter("precompile/kvstore/total_gas_used", nil)
	kvAverageGasPerOp       = metrics.NewRegisteredGaugeFloat64("precompile/kvstore/avg_gas_per_op", nil)
)

// MetricsCollector provides methods to record KV precompile metrics
type MetricsCollector struct{}

// NewMetricsCollector creates a new metrics collector for KV precompile
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{}
}

// RecordSetOperation records metrics for a set operation
func (m *MetricsCollector) RecordSetOperation(gasUsed uint64, valueSize int, executionTime time.Duration, success bool) {
	kvSetCounter.Inc(1)
	kvTotalOperationsCounter.Inc(1)
	kvTotalGasUsed.Inc(int64(gasUsed))
	kvSetGasHistogram.Update(int64(gasUsed))
	kvSetExecutionTimer.Update(executionTime)
	kvValueSizeHistogram.Update(int64(valueSize))

	if success {
		kvSetSuccessCounter.Inc(1)
	} else {
		kvSetFailureCounter.Inc(1)
	}

	m.updateAverageGasPerOp()
}

// RecordGetOperation records metrics for a get operation
func (m *MetricsCollector) RecordGetOperation(gasUsed uint64, executionTime time.Duration, success bool) {
	kvGetCounter.Inc(1)
	kvTotalOperationsCounter.Inc(1)
	kvTotalGasUsed.Inc(int64(gasUsed))
	kvGetGasHistogram.Update(int64(gasUsed))
	kvGetExecutionTimer.Update(executionTime)

	if success {
		kvGetSuccessCounter.Inc(1)
	} else {
		kvGetFailureCounter.Inc(1)
	}

	m.updateAverageGasPerOp()
}

// RecordDeleteOperation records metrics for a delete operation
func (m *MetricsCollector) RecordDeleteOperation(gasUsed uint64, executionTime time.Duration, success bool) {
	kvDeleteCounter.Inc(1)
	kvTotalOperationsCounter.Inc(1)
	kvTotalGasUsed.Inc(int64(gasUsed))
	kvDeleteGasHistogram.Update(int64(gasUsed))
	kvDeleteExecutionTimer.Update(executionTime)

	if success {
		kvDeleteSuccessCounter.Inc(1)
	} else {
		kvDeleteFailureCounter.Inc(1)
	}

	m.updateAverageGasPerOp()
}

// RecordExistsOperation records metrics for an exists operation
func (m *MetricsCollector) RecordExistsOperation(gasUsed uint64, executionTime time.Duration, success bool) {
	kvExistsCounter.Inc(1)
	kvTotalOperationsCounter.Inc(1)
	kvTotalGasUsed.Inc(int64(gasUsed))
	kvExistsGasHistogram.Update(int64(gasUsed))
	kvExistsExecutionTimer.Update(executionTime)

	if success {
		kvExistsSuccessCounter.Inc(1)
	} else {
		kvExistsFailureCounter.Inc(1)
	}

	m.updateAverageGasPerOp()
}

// RecordKeysOperation records metrics for a keys operation
func (m *MetricsCollector) RecordKeysOperation(gasUsed uint64, keyCount int, executionTime time.Duration, success bool) {
	kvKeysCounter.Inc(1)
	kvTotalOperationsCounter.Inc(1)
	kvTotalGasUsed.Inc(int64(gasUsed))
	kvKeysGasHistogram.Update(int64(gasUsed))
	kvKeysExecutionTimer.Update(executionTime)

	if success {
		kvKeysSuccessCounter.Inc(1)
	} else {
		kvKeysFailureCounter.Inc(1)
	}

	m.updateAverageGasPerOp()
}

// UpdateKeyCount updates the total key count gauge
func (m *MetricsCollector) UpdateKeyCount(count int64) {
	kvKeyCountGauge.Update(count)
}

// updateAverageGasPerOp calculates and updates the average gas per operation
func (m *MetricsCollector) updateAverageGasPerOp() {
	totalOps := kvTotalOperationsCounter.Snapshot().Count()
	totalGas := kvTotalGasUsed.Snapshot().Count()
	
	if totalOps > 0 {
		avgGas := float64(totalGas) / float64(totalOps)
		kvAverageGasPerOp.Update(avgGas)
	}
}

// GetMetricsSummary returns a summary of current metrics
func (m *MetricsCollector) GetMetricsSummary() map[string]interface{} {
	return map[string]interface{}{
		"total_operations": kvTotalOperationsCounter.Snapshot().Count(),
		"total_gas_used":   kvTotalGasUsed.Snapshot().Count(),
		"avg_gas_per_op":   kvAverageGasPerOp.Snapshot().Value(),
		"total_keys":       kvKeyCountGauge.Snapshot().Value(),
		"operations": map[string]interface{}{
			"set": map[string]interface{}{
				"count":   kvSetCounter.Snapshot().Count(),
				"success": kvSetSuccessCounter.Snapshot().Count(),
				"failure": kvSetFailureCounter.Snapshot().Count(),
			},
			"get": map[string]interface{}{
				"count":   kvGetCounter.Snapshot().Count(),
				"success": kvGetSuccessCounter.Snapshot().Count(),
				"failure": kvGetFailureCounter.Snapshot().Count(),
			},
			"delete": map[string]interface{}{
				"count":   kvDeleteCounter.Snapshot().Count(),
				"success": kvDeleteSuccessCounter.Snapshot().Count(),
				"failure": kvDeleteFailureCounter.Snapshot().Count(),
			},
			"exists": map[string]interface{}{
				"count":   kvExistsCounter.Snapshot().Count(),
				"success": kvExistsSuccessCounter.Snapshot().Count(),
				"failure": kvExistsFailureCounter.Snapshot().Count(),
			},
			"keys": map[string]interface{}{
				"count":   kvKeysCounter.Snapshot().Count(),
				"success": kvKeysSuccessCounter.Snapshot().Count(),
				"failure": kvKeysFailureCounter.Snapshot().Count(),
			},
		},
	}
}

// Global metrics collector instance
var metricsCollector = NewMetricsCollector()

// GetMetricsCollector returns the global metrics collector instance
func GetMetricsCollector() *MetricsCollector {
	return metricsCollector
}
