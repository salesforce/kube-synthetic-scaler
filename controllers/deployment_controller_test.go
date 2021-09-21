// +build unit

/*
 * Copyright (c) 2021, salesforce.com, inc.
 * All rights reserved.
 * SPDX-License-Identifier: BSD-3-Clause
 * For full license text, see the LICENSE file in the repo root or https://opensource.org/licenses/BSD-3-Clause
 */

package controllers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/clock"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/salesforce/kube-synthetic-scaler/livenessprobe"
	"github.com/salesforce/kube-synthetic-scaler/metrics"
)

// Default values for the names of annotations in deployments
const (
	scalingSignalAnnotation       string = "synthetic-scaler.salesforce.com/enable"
	scalingDurationAnnotation     string = "synthetic-scaler.salesforce.com/duration"
	lastUpdateTimeAnnotation      string = "synthetic-scaler.salesforce.com/lastUpdateTime"
	scaleUpReplicaCountAnnotation string = "synthetic-scaler.salesforce.com/replicaCount"
)

func Test_CheckForAnnotation_HasAnnotation(t *testing.T) {
	var annotationTestCases = []struct {
		annotationName  string
		annotationValue string
	}{
		{"synthetic-scaler.salesforce.com/enable", "true"},
		{"synthetic-scaler2.salesforce.com/enable", "true"},
		{"enableScaling", "true"},
		{"synthetic-scaler.salesforce.com/duration", "1m"},
		{"synthetic-scaler2.salesforce.com/duration", "1m"},
		{"synthetic-scaler.salesforce.com/replicaCount", "1"},
		{"synthetic-scaler2.salesforce.com/replicaCount", "1"},
		{"scalingDuration", "1m"},
	}
	for _, tc := range annotationTestCases {
		t.Run(fmt.Sprintf("name %s and value %s", tc.annotationName, tc.annotationValue), func(t *testing.T) {
			dummyDeploymentChart := getValidDeploymentChart(map[string]string{tc.annotationName: tc.annotationValue}, "test-deployment")
			scalingSignal, _ := checkForAnnotation(dummyDeploymentChart, tc.annotationName)
			assert.Equal(t, tc.annotationValue, scalingSignal)
		})
	}
}

func Test_CheckForAnnotation_NoAnnotation(t *testing.T) {
	dummyDeploymentChart := getValidDeploymentChart(nil, "test-deployment")
	_, ok := checkForAnnotation(dummyDeploymentChart, scalingSignalAnnotation)
	assert.Equal(t, false, ok)
}

func Test_GetDeploymentAnnotation_AnnotationEqualOne(t *testing.T) {
	replicaCount := int32(1)
	namespacedName := types.NamespacedName{Name: "test-deployment1", Namespace: "test-namespace"}
	dummyDeploymentChart := getValidDeploymentChart(map[string]string{scaleUpReplicaCountAnnotation: fmt.Sprint(replicaCount)}, namespacedName.Name)
	value, err := getDeploymentAnnotationInt(dummyDeploymentChart, scaleUpReplicaCountAnnotation)
	assert.Equal(t, replicaCount, value)
	assert.NoError(t, err)
}

func Test_GetDeploymentAnnotation_NoAnnotation(t *testing.T) {
	namespacedName := types.NamespacedName{Name: "test-deployment1", Namespace: "test-namespace"}
	dummyDeploymentChart := getValidDeploymentChart(map[string]string{}, namespacedName.Name)
	value, err := getDeploymentAnnotationInt(dummyDeploymentChart, scaleUpReplicaCountAnnotation)
	assert.Equal(t, int32(0), value)
	assert.NoError(t, err)
}

func Test_GetScaleToReplicaCount_NoAnnotation(t *testing.T) {
	var replicaCount int32 = 8
	namespacedName := types.NamespacedName{Name: "test-deployment1", Namespace: "test-namespace"}
	dummyDeploymentChart := getValidDeploymentChart(map[string]string{}, namespacedName.Name)
	controller := getController(&dummyDeploymentChart)
	dummyDeploymentChart.Spec.Replicas = &replicaCount

	value := controller.getScaleToReplicaCount(dummyDeploymentChart)

	assert.Equal(t, replicaCount, value)
}

func Test_GetScaleToReplicaCount_HasAnnotationWithZeroSpecReplica(t *testing.T) {
	var specReplicaCount int32 = 0
	var annotationReplicaCount int32 = 5
	namespacedName := types.NamespacedName{Name: "test-deployment1", Namespace: "test-namespace"}
	dummyDeploymentChart := getValidDeploymentChart(map[string]string{scaleUpReplicaCountAnnotation: fmt.Sprint(annotationReplicaCount)}, namespacedName.Name)
	controller := getController(&dummyDeploymentChart)
	dummyDeploymentChart.Spec.Replicas = &specReplicaCount

	value := controller.getScaleToReplicaCount(dummyDeploymentChart)

	assert.Equal(t, annotationReplicaCount, value)
}

func Test_GetScaleToReplicaCount_HasAnnotationWithNonZeroSpecReplica(t *testing.T) {
	var specReplicaCount int32 = 3
	var annotationReplicaCount int32 = 5
	namespacedName := types.NamespacedName{Name: "test-deployment1", Namespace: "test-namespace"}
	dummyDeploymentChart := getValidDeploymentChart(map[string]string{scaleUpReplicaCountAnnotation: fmt.Sprint(annotationReplicaCount)}, namespacedName.Name)
	controller := getController(&dummyDeploymentChart)
	dummyDeploymentChart.Spec.Replicas = &specReplicaCount

	value := controller.getScaleToReplicaCount(dummyDeploymentChart)

	assert.Equal(t, specReplicaCount, value)
}

func Test_ScaleDeployment_ScaleSetsReplicaAnnotation(t *testing.T) {
	var replicaCount int32 = 8
	namespacedName := types.NamespacedName{Name: "test-deployment1", Namespace: "test-namespace"}
	dummyDeploymentChart := getValidDeploymentChart(map[string]string{scalingSignalAnnotation: "true"}, namespacedName.Name)
	controller := getController(&dummyDeploymentChart)
	ctx := context.TODO()
	err := controller.scaleDeployment(ctx, controller.Log, dummyDeploymentChart, replicaCount, &replicaCount)
	latestDeployment := getDeploymentWithNamespacedName(controller, namespacedName)
	assert.NotNil(t, latestDeployment)
	assert.Equal(t, fmt.Sprint(replicaCount), latestDeployment.Annotations[scaleUpReplicaCountAnnotation])
	assert.NoError(t, err)
}

func Test_ScaleDeployment_ScaleRemovesReplicaAnnotation(t *testing.T) {
	var replicaCount int32 = 8
	namespacedName := types.NamespacedName{Name: "test-deployment1", Namespace: "test-namespace"}
	dummyDeploymentChart := getValidDeploymentChart(map[string]string{scalingSignalAnnotation: "true"}, namespacedName.Name)
	controller := getController(&dummyDeploymentChart)
	ctx := context.TODO()
	err := controller.scaleDeployment(ctx, controller.Log, dummyDeploymentChart, replicaCount, nil)
	latestDeployment := getDeploymentWithNamespacedName(controller, namespacedName)
	assert.NotNil(t, latestDeployment)
	assert.Equal(t, "", latestDeployment.Annotations[scaleUpReplicaCountAnnotation])
	assert.NoError(t, err)
}

func Test_ScaleDeployment_ScaleUpToOne(t *testing.T) {
	var replicaCount int32 = 1
	namespacedName := types.NamespacedName{Name: "test-deployment1", Namespace: "test-namespace"}
	dummyDeploymentChart := getValidDeploymentChart(map[string]string{scalingSignalAnnotation: "true"}, namespacedName.Name)
	controller := getController(&dummyDeploymentChart)
	ctx := context.TODO()
	err := controller.scaleDeployment(ctx, controller.Log, dummyDeploymentChart, replicaCount, nil)
	latestDeployment := getDeploymentWithNamespacedName(controller, namespacedName)
	assert.NotNil(t, latestDeployment)
	assert.Equal(t, replicaCount, *latestDeployment.Spec.Replicas)
	assert.NoError(t, err)
}

// Tests that the synthetic test will return the deployment to the number of replicas that it previously had.
func Test_ScaleDeployment_ScaleUpToPrevious(t *testing.T) {
	var replicaCount int32 = 5 // any value that is not 1.
	namespacedName := types.NamespacedName{Name: "test-deployment1", Namespace: "test-namespace"}
	dummyDeploymentChart := getValidDeploymentChart(map[string]string{scalingSignalAnnotation: "true"}, namespacedName.Name)
	controller := getController(&dummyDeploymentChart)
	ctx := context.TODO()
	err := controller.scaleDeployment(ctx, controller.Log, dummyDeploymentChart, replicaCount, nil)
	latestDeployment := getDeploymentWithNamespacedName(controller, namespacedName)
	assert.NotNil(t, latestDeployment)
	assert.Equal(t, replicaCount, *latestDeployment.Spec.Replicas)
	assert.NoError(t, err)
}

func Test_Reconciler_IgnoreDeploymentNotFoundErr(t *testing.T) {
	namespacedName := types.NamespacedName{Name: "test-deployment", Namespace: "test-namespace"}
	incorrectNamespacedName := types.NamespacedName{Name: "nonexistent-deployment", Namespace: "test-namespace"}
	dummyDeploymentChart := getValidDeploymentChart(map[string]string{scalingSignalAnnotation: "true"}, namespacedName.Name)
	controller := getController(&dummyDeploymentChart)
	_, err := controller.Reconcile(ctrl.Request{NamespacedName: incorrectNamespacedName})
	assert.NoError(t, err)
}

func Test_Reconciler_NoAnnotation(t *testing.T) {
	namespacedName := types.NamespacedName{Name: "test-deployment", Namespace: "test-namespace"}
	dummyDeploymentChart := getValidDeploymentChart(nil, namespacedName.Name)
	controller := getController(&dummyDeploymentChart)
	_, err := controller.Reconcile(ctrl.Request{NamespacedName: namespacedName})
	checkScaleUpDownValues(t, namespacedName, 0)
	assert.NoError(t, err)
}

func Test_Reconciler_ScalingIntervalReached(t *testing.T) {
	namespacedName := types.NamespacedName{Name: "test-deployment", Namespace: "test-namespace"}
	dummyDeploymentChart := getValidDeploymentChart(map[string]string{scalingSignalAnnotation: "true"}, namespacedName.Name)
	controller := getController(&dummyDeploymentChart)
	checkScaleUpDownValues(t, namespacedName, 0)
	_, err := controller.Reconcile(ctrl.Request{NamespacedName: namespacedName})
	checkScaleUpDownValues(t, namespacedName, 1)
	assert.NoError(t, err)
}

func Test_Reconciler_ScalingIntervalNotReached(t *testing.T) {
	namespacedName := types.NamespacedName{Name: "test-deployment2", Namespace: "test-namespace"}
	annotations := map[string]string{scalingSignalAnnotation: "true", lastUpdateTimeAnnotation: time.Now().Format(time.RFC822Z)}
	dummyDeploymentChart := getValidDeploymentChart(annotations, namespacedName.Name)
	controller := getController(&dummyDeploymentChart)
	checkScaleUpDownValues(t, namespacedName, 0)
	_, err := controller.Reconcile(ctrl.Request{NamespacedName: namespacedName})
	checkScaleUpDownValues(t, namespacedName, 0)
	assert.NoError(t, err)
}

func Test_Reconciler_DeploymentAvailableAfterScaleUp(t *testing.T) {
	var replicaCount int32 = 1
	namespacedName := types.NamespacedName{Name: "test-deployment3", Namespace: "test-namespace"}
	dummyDeploymentChart := getValidDeploymentChart(map[string]string{scalingSignalAnnotation: "true"}, namespacedName.Name)
	dummyDeploymentChart.Spec.Replicas = &replicaCount
	dummyDeploymentChart.Status = appsv1.DeploymentStatus{
		ObservedGeneration:  0,
		Replicas:            replicaCount,
		UpdatedReplicas:     replicaCount,
		ReadyReplicas:       replicaCount,
		AvailableReplicas:   replicaCount,
		UnavailableReplicas: 0,
		Conditions:          []appsv1.DeploymentCondition{{Type: appsv1.DeploymentAvailable, Reason: "MinimumReplicasAvailable"}},
		CollisionCount:      nil,
	}
	controller := getController(&dummyDeploymentChart)
	_, err := controller.Reconcile(ctrl.Request{NamespacedName: namespacedName})
	assert.NoError(t, err)
	collector, err := metrics.DeploymentAvailability.GetMetricWith(map[string]string{"ns": namespacedName.Namespace, "deployment": namespacedName.Name})
	assert.NoError(t, err)
	availability, err := getGaugeMetricValue(collector)
	assert.NoError(t, err)
	assert.Equal(t, available, availability)
}

func Test_GetScalingInterval_NoAnnotation(t *testing.T) {
	dummyDeploymentChart := getValidDeploymentChart(map[string]string{scalingSignalAnnotation: "true"}, "test-deployment")
	controller := getController(&dummyDeploymentChart)
	defaultScalingInterval := controller.DefaultScalingInterval
	scalingInterval, err := controller.getScalingInterval(dummyDeploymentChart)
	assert.NoError(t, err)
	assert.Equal(t, defaultScalingInterval, scalingInterval)
}

func Test_GetScalingInterval_WithCorrectAnnotation(t *testing.T) {
	scalingDurationString := "5m"
	expectedScalingDurationMs := time.Duration(300000000000)
	annotations := map[string]string{scalingSignalAnnotation: "true", scalingDurationAnnotation: scalingDurationString}
	dummyDeploymentChart := getValidDeploymentChart(annotations, "test-deployment")
	controller := getController(&dummyDeploymentChart)
	scalingInterval, err := controller.getScalingInterval(dummyDeploymentChart)
	assert.NoError(t, err)
	assert.Equal(t, expectedScalingDurationMs, scalingInterval)
}

func Test_GetScalingInterval_WithIncorrectAnnotation(t *testing.T) {
	incorrectScalingDurationString := "abc"
	annotations := map[string]string{scalingSignalAnnotation: "true", scalingDurationAnnotation: incorrectScalingDurationString}
	dummyDeploymentChart := getValidDeploymentChart(annotations, "test-deployment")
	controller := getController(&dummyDeploymentChart)
	_, err := controller.getScalingInterval(dummyDeploymentChart)
	assert.Error(t, err)
}

func Test_GetDeploymentCondition(t *testing.T) {
	expectedCondition := appsv1.DeploymentAvailable
	depStatus := appsv1.DeploymentStatus{Conditions: []appsv1.DeploymentCondition{{Type: expectedCondition}}}
	cond := getDeploymentCondition(depStatus, appsv1.DeploymentAvailable)
	assert.Equal(t, expectedCondition, cond.Type)
}

func Test_GetDeploymentStatusWithDeadline_DeadlineReached(t *testing.T) {
	maxScaleUpTime = 2 * time.Second
	var replicaCount int32 = 1

	dummyDeploymentChart := getValidDeploymentChart(nil, "test-deployment")
	dummyDeploymentChart.Spec.Replicas = &replicaCount
	dummyDeploymentChart.Generation = 0
	dummyDeploymentChart.Status = appsv1.DeploymentStatus{
		ObservedGeneration:  0,
		Replicas:            0,
		UpdatedReplicas:     1,
		ReadyReplicas:       0,
		AvailableReplicas:   0,
		UnavailableReplicas: 0,
		Conditions:          []appsv1.DeploymentCondition{{Type: appsv1.DeploymentProgressing}},
		CollisionCount:      nil,
	}
	expectedErrMsg := "not all updated pods are ready (0 ready < 1 updated)"
	controller := getController(&dummyDeploymentChart)
	availability, err := controller.getDeploymentStatusWithDeadline(context.TODO(), controller.Log, "test-namespace", "test-deployment")
	assert.Equal(t, notAvailable, availability)
	assert.EqualError(t, err, expectedErrMsg)
}

func getController(initObjs ...runtime.Object) *DeploymentReconciler {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	listener := livenessprobe.NewLivenessProbeListener("some path", "1234", time.Minute*3, logf.Log)

	controller := &DeploymentReconciler{
		Client:                        fake.NewFakeClientWithScheme(scheme, initObjs...),
		Log:                           logf.Log,
		LivenessProbe:                 listener,
		Clock:                         clock.NewFakeClock(time.Date(2000, 1, 1, 1, 1, 0, 0, time.UTC)),
		DefaultScalingInterval:        2 * time.Minute,
		ScalingSignalAnnotation:       scalingSignalAnnotation,
		ScalingDurationAnnotation:     scalingDurationAnnotation,
		LastUpdateTimeAnnotation:      lastUpdateTimeAnnotation,
		ScaleUpReplicaCountAnnotation: scaleUpReplicaCountAnnotation,
		MaxConcurrentReconciles:       1,
	}
	return controller
}

func getDeploymentWithNamespacedName(controller *DeploymentReconciler, namespacedName types.NamespacedName) *appsv1.Deployment {
	latestDeployment := &appsv1.Deployment{}
	err := controller.Get(context.Background(), client.ObjectKey{
		Namespace: namespacedName.Namespace,
		Name:      namespacedName.Name,
	}, latestDeployment)
	if err != nil {
		return nil
	}
	return latestDeployment
}

func getValidDeploymentChart(annotations map[string]string, deploymentName string) appsv1.Deployment {
	return appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        deploymentName,
			Namespace:   "test-namespace",
			Annotations: annotations,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"a": "b"},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:                     "test",
							Image:                    "test_image",
							ImagePullPolicy:          v1.PullIfNotPresent,
							TerminationMessagePolicy: v1.TerminationMessageReadFile,
						},
					},
					RestartPolicy: v1.RestartPolicyAlways,
					DNSPolicy:     v1.DNSClusterFirst,
				},
			},
		},
	}
}

func checkScaleUpDownValues(t *testing.T, namespacedName types.NamespacedName, expectedVal int) {
	t.Helper()
	scaleDownCounter, err := getScalingCollector(metrics.ScalerScaleDownCount, namespacedName)
	assert.NoError(t, err)
	scaleUpCounter, err := getScalingCollector(metrics.ScalerScaleDownCount, namespacedName)
	assert.NoError(t, err)
	scaledDownVal, err := getCounterMetricValue(scaleDownCounter)
	assert.NoError(t, err)
	scaleUpVal, err := getCounterMetricValue(scaleUpCounter)
	assert.NoError(t, err)
	assert.Equal(t, float64(expectedVal), scaledDownVal)
	assert.Equal(t, float64(expectedVal), scaleUpVal)
}

func getScalingCollector(counterVec *prometheus.CounterVec, namespacedName types.NamespacedName) (prometheus.Collector, error) {
	scalingCol, err := counterVec.GetMetricWith(map[string]string{"ns": namespacedName.Namespace, "deployment": namespacedName.Name})
	return scalingCol, err
}

func getCounterMetricValue(col prometheus.Collector) (float64, error) {
	c := make(chan prometheus.Metric, 1)
	col.Collect(c) // collect current metric value into the channel
	m := dto.Metric{}
	_ = (<-c).Write(&m) // read metric value from the channel
	if m.Counter == nil {
		return -1, fmt.Errorf("metric value not found")
	}
	return *m.Counter.Value, nil
}

func getGaugeMetricValue(col prometheus.Collector) (float64, error) {
	c := make(chan prometheus.Metric, 1)
	col.Collect(c) // collect current metric value into the channel
	m := dto.Metric{}
	_ = (<-c).Write(&m) // read metric value from the channel
	if m.Gauge == nil {
		return -1, fmt.Errorf("metric value not found")
	}
	return *m.Gauge.Value, nil
}
