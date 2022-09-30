/*
 * Copyright (c) 2022, Salesforce, Inc.
 * All rights reserved.
 * SPDX-License-Identifier: BSD-3-Clause
 * For full license text, see the LICENSE file in the repo root or https://opensource.org/licenses/BSD-3-Clause
 */

package main

import (
	"flag"
	"os"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/clock"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/salesforce/kube-synthetic-scaler/controllers"
	// +kubebuilder:scaffold:imports

	"github.com/salesforce/kube-synthetic-scaler/livenessprobe"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = appsv1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var syncPeriod time.Duration
	var metricsAddr string
	var livenessProbePath string
	var livenessProbePort string
	var scalingSignalAnnotation string
	var scalingDurationAnnotation string
	var lastUpdateTimeAnnotation string
	var scaleUpReplicaCountAnnotation string
	var resyncInterval time.Duration
	var enableLeaderElection bool
	var defaultScalingInterval time.Duration
	var maxConcurrentReconciles int
	flag.DurationVar(&syncPeriod, "sync-period", 90*time.Second, "Minimum frequency at which watched resources are reconciled.")
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&livenessProbePath, "liveness-probe-path", "/healthz", "HTTP Request Endpoint for the k8s liveness checker")
	flag.StringVar(&livenessProbePort, "liveness-probe-port", "3000", "Port to use for the kubernetes liveness probe")
	flag.StringVar(&scalingSignalAnnotation, "scaling-signal-annotation", "synthetic-scaler.salesforce.com/enable", "The name of the annotation in a deployment that signals to the scaler that this deployment should be scaled")
	flag.StringVar(&scalingDurationAnnotation, "scaling-duration-annotation", "synthetic-scaler.salesforce.com/duration", "The name of the annotation in a deployment whose value tells the scaler how often to scale up and down")
	flag.StringVar(&lastUpdateTimeAnnotation, "last-update-time-annotation", "synthetic-scaler.salesforce.com/lastUpdateTime", "The name of the annotation in a deployment that indicates when synthetic scaler last scaled it up and down")
	flag.StringVar(&scaleUpReplicaCountAnnotation, "replica-count-annotation", "synthetic-scaler.salesforce.com/replicaCount", "The name of the annotation in a deployment that hints to the sythetic scaler what the deployment scale up value should be if the deployment currently has zero replicas")
	flag.DurationVar(&resyncInterval, "resync-interval", 3*time.Minute, "Duration after which re-sync loop is triggered")
	flag.DurationVar(&defaultScalingInterval, "default-scaling-interval", 2*time.Minute, "Default scaling interval value")
	flag.IntVar(&maxConcurrentReconciles, "max-concurrent-reconciles", 20, "Maximum number of concurrent Reconciles which can be run")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		SyncPeriod:         &syncPeriod,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		Namespace:          "",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	liveness := livenessprobe.NewLivenessProbeListener(livenessProbePath, livenessProbePort, resyncInterval,
		ctrl.Log.WithName("livenessprobe").WithName("livenessprobe"))
	go liveness.StartLivenessProbeListener()

	if err = (&controllers.DeploymentReconciler{
		Client:                        mgr.GetClient(),
		Log:                           ctrl.Log.WithName("controllers").WithName("KubeSyntheticScaler"),
		Scheme:                        mgr.GetScheme(),
		LivenessProbe:                 liveness,
		Clock:                         clock.RealClock{},
		DefaultScalingInterval:        defaultScalingInterval,
		ScalingSignalAnnotation:       scalingSignalAnnotation,
		ScalingDurationAnnotation:     scalingDurationAnnotation,
		LastUpdateTimeAnnotation:      lastUpdateTimeAnnotation,
		ScaleUpReplicaCountAnnotation: scaleUpReplicaCountAnnotation,
		MaxConcurrentReconciles:       maxConcurrentReconciles,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "KubeSyntheticScaler")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
