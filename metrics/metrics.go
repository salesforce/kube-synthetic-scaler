/*
 * Copyright (c) 2022, Salesforce, Inc.
 * All rights reserved.
 * SPDX-License-Identifier: BSD-3-Clause
 * For full license text, see the LICENSE file in the repo root or https://opensource.org/licenses/BSD-3-Clause
 */

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	DeploymentAvailability = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "deployment_availability",
		Help: "Specifies whether a namespaced deployment is available or not after scale up",
	}, []string{"ns", "deployment"})
	ScalerReconcilerDuration = prometheus.NewSummary(prometheus.SummaryOpts{
		Name:       "scaler_reconciler_duration_ms",
		Objectives: map[float64]float64{0.5: 0.05, 0.95: 0.01},
		Help:       "The time duration for synthetic scaler reconciler responses",
	})
	ScalerScaleDownCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "scaler_scale_down_count",
		Help: "The number of scale downs by synthetic scaler partitioned by namespaced deployment",
	}, []string{"ns", "deployment"})
	ScalerScaleUpCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "scaler_scale_up_count",
		Help: "The number of scale ups by synthetic scaler partitioned by namespaced deployment",
	}, []string{"ns", "deployment"})
)

func init() {
	registerMetrics()
}

func registerMetrics() {
	metrics.Registry.MustRegister(DeploymentAvailability)
	metrics.Registry.MustRegister(ScalerReconcilerDuration)
	metrics.Registry.MustRegister(ScalerScaleDownCount)
	metrics.Registry.MustRegister(ScalerScaleUpCount)
}
