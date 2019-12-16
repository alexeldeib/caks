set -eux

export PATH=$GITHUB_WORKSPACE/bin:$PATH
echo $PATH

which kustomize
which controller-gen
which kind

kind create cluster
kind export kubeconfig --name kind

kubectl config view
kubectl cluster-info
kubectl create -f https://github.com/kubernetes-sigs/cluster-api/releases/download/v0.2.7/cluster-api-components.yaml
kubectl create -f https://github.com/kubernetes-sigs/cluster-api-bootstrap-provider-kubeadm/releases/download/v0.1.5/bootstrap-components.yaml

make install
make test