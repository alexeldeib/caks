#!/usr/env/bin bash
set -eux

mkdir $GITHUB_WORKSPACE/bin
cp /usr/local/bin/goimports $GITHUB_WORKSPACE/bin
cp /usr/local/kubebuilder/bin/* $GITHUB_WORKSPACE/bin
cp /usr/local/bin/controller-gen $GITHUB_WORKSPACE/bin
cp /usr/local/bin/kustomize $GITHUB_WORKSPACE/bin
cp /usr/local/bin/kind $GITHUB_WORKSPACE/bin
cp /usr/local/bin/kubectl $GITHUB_WORKSPACE/bin
