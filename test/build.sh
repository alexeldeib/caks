#!/usr/env/bin bash
set -eux

KUBEBUILDER_VERSION=2.2.0
KIND_VERSION=0.6.1
KUSTOMIZE_VERSION=3.5.1
CONTROLLER_GEN_VERSION=0.2.4
# export PATH=$(go env GOPATH)/bin:/usr/local/kubebuilder/bin:$PATH
go env
go version
mkdir -p /usr/local/kubebuilder
WORK=$(mktemp -d)
pushd $WORK 
go get golang.org/x/tools/cmd/goimports
go get github.com/onsi/ginkgo/ginkgo
go get sigs.k8s.io/kind@v${KIND_VERSION}
rm -rf $WORK/*
go get sigs.k8s.io/kustomize/kustomize/v3@v${KUSTOMIZE_VERSION}
rm -rf $WORK/*
go get sigs.k8s.io/controller-tools/cmd/controller-gen@v${CONTROLLER_GEN_VERSION}
popd
os=$(go env GOOS)
arch=$(go env GOARCH)
curl -sL https://go.kubebuilder.io/dl/${KUBEBUILDER_VERSION}/${os}/${arch} -o kubebuilder_${KUBEBUILDER_VERSION}_${os}_${arch}.tar.gz
tar -xzf kubebuilder_${KUBEBUILDER_VERSION}_${os}_${arch}.tar.gz -C /usr/local/kubebuilder --strip-components=1
curl -sL https://storage.googleapis.com/kubernetes-release/release/v1.17.0/bin/linux/amd64/kubectl -o $(go env GOPATH)/bin/kubectl