    #!/usr/bin/env bash
set -e

kind create cluster --name=clusterapi
kubectl config use-context kind-clusterapi
kubectl create -f https://github.com/kubernetes-sigs/cluster-api/releases/download/v0.2.7/cluster-api-components.yaml
kubectl create -f https://github.com/kubernetes-sigs/cluster-api-bootstrap-provider-kubeadm/releases/download/v0.1.5/bootstrap-components.yaml
make install