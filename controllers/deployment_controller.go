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
	"github.com/go-logr/logr"
	"github.com/salesforce/kube-synthetic-scaler/livenessprobe"
	"github.com/salesforce/kube-synthetic-scaler/metrics"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"strconv"
	"time"
)

// DeploymentReconciler reconciles a Deployment object
type DeploymentReconciler struct {
	client.Client
	Log                           logr.Logger
	Scheme                        *runtime.Scheme
	LivenessProbe                 *livenessprobe.LivenessProbeListener
	Clock                         clock.Clock
	DefaultScalingInterval        time.Duration
	ScalingSignalAnnotation       string
	ScalingDurationAnnotation     string
	LastUpdateTimeAnnotation      string
	ScaleUpReplicaCountAnnotation string
	MaxConcurrentReconciles       int
}

const (
	notAvailable float64 = 0
	available    float64 = 1
)

var (
	replicaZero    int32 = 0
	maxScaleUpTime       = 1 * time.Minute
)

// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get;update;patch

func (r *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	startTime := r.Clock.Now()
	log := r.Log
	// send a pulse to avoid liveness probe failing at least once per reconciliation loop.
	defer r.LivenessProbe.Pulse()

	// TODO Make into separate functions and add tests
	// TODO Maybe create CRD to store when last updated and result etc

	// 1. Get Deployment object
	var deployment appsv1.Deployment
	if err := r.Get(ctx, req.NamespacedName, &deployment); err != nil {
		log.Error(err, "unable to fetch deployment")
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// 2. Check if Deployment has the annotation we are looking for
	scalingSignal, _ := r.checkForAnnotation(deployment, r.ScalingSignalAnnotation)

	if scalingSignal != "true" {
		return ctrl.Result{}, nil
	}
	// If Deployment has annotation
	lastUpdateTime, _ := time.Parse(time.RFC822Z, deployment.Annotations[r.LastUpdateTimeAnnotation])
	scalingInterval, err := r.getScalingInterval(deployment)
	if err != nil {
		return ctrl.Result{}, err
	}
	timeSinceLastUpdate := time.Since(lastUpdateTime)
	log.Info("", "KubeSyntheticScaler", req.NamespacedName, "Was last updated mins ago", timeSinceLastUpdate)

	// 4. If it's been more than n minutes since update scale up and down
	if timeSinceLastUpdate > scalingInterval {
		log.Info("", "KubeSyntheticScaler", req.NamespacedName, "Time since last update", timeSinceLastUpdate, "is more than scaling interval", scalingInterval)
		// 6. Scale down by doing a patch to 0 replica
		replicaCount := r.getScaleToReplicaCount(deployment)
		err := r.scaleDeployment(ctx, log, deployment, replicaZero, &replicaCount)
		if err != nil {
			return ctrl.Result{}, err
		}
		// Metric for number of scale downs partitioned by namespaced deployment
		metrics.ScalerScaleDownCount.WithLabelValues(req.NamespacedName.Namespace, req.NamespacedName.Name).Inc()
		log.Info("", "KubeSyntheticScaler", req.NamespacedName, "was scaled down to 0 replicas", "")

		time.Sleep(2 * time.Second)

		// 6. Scale up by doing a patch to the previous number of replicas
		if err := r.Get(ctx, req.NamespacedName, &deployment); err != nil {
			log.Error(err, "unable to fetch deployment")
			// we'll ignore not-found errors, since they can't be fixed by an immediate
			// requeue (we'll need to wait for a new notification), and we can get them
			// on deleted requests.
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		err = r.scaleDeployment(ctx, log, deployment, replicaCount, nil)
		if err != nil {
			return ctrl.Result{}, err
		}
		// check if deployment is available after scale up within maxScaleUpTime
		availability, err := r.getDeploymentStatusWithDeadline(ctx, log, req.NamespacedName.Namespace, req.NamespacedName.Name)
		if err != nil {
			log.Info("This deployment is not available after scale up", "deployment", req.NamespacedName, "availability", availability, "err", err)
		} else {
			log.Info("This deployment is available after scale up", "deployment", req.NamespacedName, "availability", availability)
		}
		// Metric that specifies if a deployment is available with desired replica count one minute after scale up
		metrics.DeploymentAvailability.WithLabelValues(req.NamespacedName.Namespace, req.NamespacedName.Name).Set(availability)
		// Metric for number of scale ups partitioned by namespaced deployment
		metrics.ScalerScaleUpCount.WithLabelValues(req.NamespacedName.Namespace, req.NamespacedName.Name).Inc()

		log.Info(fmt.Sprintf("Deployment was scaled up to %d replica", replicaCount), "deployment", req.NamespacedName)
	}
	timeTaken := time.Since(startTime).Seconds() * 1000
	log.Info("Deployment reconciler has duration", "duration", timeTaken)
	metrics.ScalerReconcilerDuration.Observe(timeTaken)

	return ctrl.Result{}, nil
}

func (r *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Complete(r)
}

func (r *DeploymentReconciler) getScalingInterval(deployment appsv1.Deployment) (time.Duration, error) {
	var err error
	scalingInterval := r.DefaultScalingInterval
	if scalingDuration, ok := r.checkForAnnotation(deployment, r.ScalingDurationAnnotation); ok {
		scalingInterval, err = time.ParseDuration(scalingDuration)
		if err != nil {
			return scalingInterval, err
		}
	}
	return scalingInterval, nil
}

func (r *DeploymentReconciler) checkForAnnotation(deployment appsv1.Deployment, annotation string) (string, bool) {
	scalingSignal, ok := deployment.Annotations[annotation]
	if !ok {
		r.Log.Info("Deployment is missing annotation", "deployment", deployment.Name, "namespace", deployment.Namespace, "annotation", annotation)
	} else {
		r.Log.Info("Deployment contains annotation", "deployment", deployment.Name, "namespace", deployment.Namespace, "annotation", annotation)
	}
	return scalingSignal, ok
}

// getScaleToReplicaCount Gets the replica count of the provided deployment and returns an integer safely.
// It uses an annotation to save the replica count in case this controller is restarted after scaling the
// deployment to zero but before it has time to scale it back up.
func (r *DeploymentReconciler) getScaleToReplicaCount(deployment appsv1.Deployment) int32 {
	var replicaCount int32
	annotationReplicaCount, _ := r.getDeploymentAnnotationInt(deployment, r.ScaleUpReplicaCountAnnotation)
	deploymentReplicaCount := deployment.Spec.Replicas

	if deploymentReplicaCount != nil && *deploymentReplicaCount != 0 {
		replicaCount = *deploymentReplicaCount

	} else { // fallback on annotation

		// The annotation will be `0` if the annotation does not exist. In this case, use the default value.
		// From the k8s docs, the default value should be `1`.
		if annotationReplicaCount == 0 {
			replicaCount = 1
		} else {
			replicaCount = annotationReplicaCount
		}
	}
	return replicaCount
}

func (r *DeploymentReconciler) getDeploymentAnnotationInt(deployment appsv1.Deployment, annotation string) (int32, error) {
	// Don't throw error if annotation doesn't exist. But do throw if the value exists and is illegal
	if annotationValueStr, ok := r.checkForAnnotation(deployment, annotation); ok {
		annotationValueInt, err := strconv.Atoi(annotationValueStr)
		return int32(annotationValueInt), err
	}
	return 0, nil
}

// scaleDeployment scales the existing deployment to `scaleTo` number of replicas and sets an annotation
// to save the previous replica value in case this application is interrupted using the `scaleFrom` value.
func (r *DeploymentReconciler) scaleDeployment(ctx context.Context, log logr.Logger, deployment appsv1.Deployment, scaleTo int32, scaleFrom *int32) error {
	newDeploy := deployment.DeepCopy()
	newDeploy.ResourceVersion = deployment.ResourceVersion
	newDeploy.Spec.Replicas = &scaleTo
	newDeploy.Annotations[r.LastUpdateTimeAnnotation] = time.Now().Format(time.RFC822Z)
	if scaleFrom != nil {
		newDeploy.Annotations[r.ScaleUpReplicaCountAnnotation] = fmt.Sprint(*scaleFrom)
	} else {
		newDeploy.Annotations[r.ScaleUpReplicaCountAnnotation] = "" // Remove the annotation
	}
	err := r.Patch(ctx, newDeploy, client.MergeFrom(&deployment))
	if err != nil {
		log.Error(err, "failed to scale deployment")
		return err
	}
	return nil
}

func (r *DeploymentReconciler) getDeploymentStatusWithDeadline(ctx context.Context, log logr.Logger, namespace string, name string) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, maxScaleUpTime)
	defer cancel()
	latestDeployment := &appsv1.Deployment{}
	lastStatus := ""
	var ready bool
	for {
		select {
		case <-ctx.Done():
			cancel()
			return notAvailable, fmt.Errorf(lastStatus)
		default:
			if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, latestDeployment); err != nil {
				log.Error(err, "unable to fetch deployment")
				return notAvailable, err
			}
			if ready, lastStatus = isDeploymentReady(latestDeployment); ready {
				return available, nil
			}
		}
	}
}

func isDeploymentReady(deploymentObj *appsv1.Deployment) (bool, string) {
	// credit: https://github.com/timoreimann/kubernetes-scripts/blob/master/wait-for-deployment
	// painstakingly checked through manual deployments, seems to work with
	// * creating new deployment
	// * modifying properties (i.e. cmdline) existing deployment
	// * scaling up deployment
	availableCond := getDeploymentCondition(deploymentObj.Status, appsv1.DeploymentAvailable)
	if availableCond != nil && availableCond.Reason == "MinimumReplicasUnavailable" {
		return false, availableCond.Message
	}
	if deploymentObj.Status.ObservedGeneration < deploymentObj.Generation {
		return false, fmt.Sprintf("deployment controller has not observed the latest update (observed generation %v < target generation %v)", deploymentObj.Status.ObservedGeneration, deploymentObj.Generation)
	}
	if deploymentObj.Status.UpdatedReplicas < *deploymentObj.Spec.Replicas {
		return false, fmt.Sprintf("not all pods have been updated (%v updated < %v expected)", deploymentObj.Status.UpdatedReplicas, *deploymentObj.Spec.Replicas)
	}
	if deploymentObj.Status.Replicas > deploymentObj.Status.UpdatedReplicas {
		return false, fmt.Sprintf("found more pods than expected (%v total > %v expected)", deploymentObj.Status.Replicas, deploymentObj.Status.UpdatedReplicas)
	}
	if deploymentObj.Status.AvailableReplicas < deploymentObj.Status.UpdatedReplicas {
		return false, fmt.Sprintf("not all updated pods are ready (%v ready < %v updated)", deploymentObj.Status.AvailableReplicas, deploymentObj.Status.UpdatedReplicas)
	}
	if deploymentObj.Status.ReadyReplicas < 1 {
		return false, fmt.Sprintf("no pods are ready (%v ready)", deploymentObj.Status.ReadyReplicas)
	}

	return true, ""
}

// GetDeploymentCondition returns the condition with the provided type.
func getDeploymentCondition(status appsv1.DeploymentStatus, condType appsv1.DeploymentConditionType) *appsv1.DeploymentCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}
