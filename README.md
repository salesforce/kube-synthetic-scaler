# kube-synthetic-scaler

![Main Branch Build Status](https://github.com/salesforce/kube-synthetic-scaler/actions/workflows/docker-image.yaml/badge.svg)

This is a Kubernetes controller that scales deployments up and down.

### How it Works

kube-synthetic-scaler is a controller that watches deployment objects for a ScalingSignalAnnotation and regularly scales deployments that have opted in down to 0 and back up to the original number of replicas at a specified interval. The interval defaults to the value set by the default-scaling-interval flag, unless configured separately by the deployment using the ScalingDurationAnnotation. It also sends a health check saying whether the scaling was successful or not.

This controller was originally created as part of a synthetic test framework to ensure that mutating webhooks are running and properly mutating new deployments. As such, dummy test deployments should be used with the scaler, rather than actual production deployments.

### Setup

Make sure you have [kubectl](https://kubernetes.io/docs/tasks/tools/) and [Helm](https://helm.sh/docs/intro/install/) installed. For building and testing Docker images locally, ensure you have [Docker](https://docs.docker.com/get-docker/) installed.

### To run locally

1. You can choose between several options for running kube-synthetic-scaler:
    - To run the Go binary locally, run `make run`
    - To run using Docker image + Helm chart, run `make install-chart`
    - If using minikube, to run using local Docker image + Helm chart, run `eval $(minikube docker-env)`, then `make docker-build install-chart`
2. kube-synthetic-scaler will operate on the current context according to your kubeconfig file.

### To uninstall Helm chart:
1. Run `make uninstall-chart`
