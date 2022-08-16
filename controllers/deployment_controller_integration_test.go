//go:build integration
// +build integration

/*
 * Copyright (c) 2021, salesforce.com, inc.
 * All rights reserved.
 * SPDX-License-Identifier: BSD-3-Clause
 * For full license text, see the LICENSE file in the repo root or https://opensource.org/licenses/BSD-3-Clause
 */

package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	apps "k8s.io/api/apps/v1"
	api "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Default values for the names of annotations in deployments
const (
	scalingSignalAnnotation       string = "synthetic-scaler.salesforce.com/enable"
	scalingDurationAnnotation     string = "synthetic-scaler.salesforce.com/duration"
	lastUpdateTimeAnnotation      string = "synthetic-scaler.salesforce.com/lastUpdateTime"
	scaleUpReplicaCountAnnotation string = "synthetic-scaler.salesforce.com/replicaCount"
)

var (
	deploymentName = "foo-deployment"
)

var _ = Context("Inside of a new deploymentNamespace", func() {
	ctx := context.TODO()
	ns := SetupTest(ctx)

	Describe("when a deployment exist with scaling annotation", func() {

		It("should update deployment with one replica", func() {
			deployment := validNewDeployment(ns.Name)

			err := k8sClient.Create(ctx, deployment)
			Expect(err).NotTo(HaveOccurred(), "failed to create test deployment resource")

			deployment = &apps.Deployment{}
			time.Sleep(4 * time.Second)
			Eventually(
				getResourceFunc(ctx, client.ObjectKey{Name: deploymentName, Namespace: ns.Name}, deployment),
				time.Second*10, time.Second*10).Should(BeNil())

			Expect(*deployment.Spec.Replicas).To(Equal(int32(1)))
		})
	})
})

func validNewDeployment(namespace string) *apps.Deployment {
	return &apps.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:        deploymentName,
			Namespace:   namespace,
			Annotations: map[string]string{scalingSignalAnnotation: "true"},
		},
		Spec: apps.DeploymentSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"a": "b"}},
			Template: api.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"a": "b"},
				},
				Spec: api.PodSpec{
					Containers: []api.Container{
						{
							Name:                     "test",
							Image:                    "test_image",
							ImagePullPolicy:          api.PullIfNotPresent,
							TerminationMessagePolicy: api.TerminationMessageReadFile,
						},
					},
					RestartPolicy: api.RestartPolicyAlways,
					DNSPolicy:     api.DNSClusterFirst,
				},
			},
			Replicas: &replicaZero,
		},
	}
}

func getResourceFunc(ctx context.Context, key client.ObjectKey, obj runtime.Object) func() error {
	return func() error {
		return k8sClient.Get(ctx, key, obj)
	}
}

func getDeploymentReplicasFunc(ctx context.Context, key client.ObjectKey) func() int32 {
	return func() int32 {
		depl := &apps.Deployment{}
		err := k8sClient.Get(ctx, key, depl)
		Expect(err).NotTo(HaveOccurred(), "failed to get Deployment resource")

		return *depl.Spec.Replicas
	}
}
