// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package controllers

import (
	"context"

	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/alexeldeib/stringslice"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"github.com/prometheus/common/log"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha2"
	"sigs.k8s.io/cluster-api/util"
	kcfg "sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	infrav1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
	"github.com/Azure/cluster-api-provider-aks/pkg/services/agentpools"
	"github.com/Azure/cluster-api-provider-aks/pkg/services/managedclusters"
	"github.com/Azure/cluster-api-provider-aks/pkg/services/scalesetvms"
)

// AzureManagedMachineReconciler reconciles a AzureManagedMachine object
type AzureManagedMachineReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	// ManagedClusterService interface can be mocked for Azure
	ManagedClusterService *managedclusters.Service
	AgentPoolService      *agentpools.Service
	VMSSInstanceService   *scalesetvms.Service
	// TODO(ace): better way to mock calls to workload API server?
	NodeListerFunc func(client.Client) func(context.Context, *clusterv1.Cluster) ([]corev1.Node, error)
	NodeSetterFunc func(client.Client) func(context.Context, *clusterv1.Cluster, corev1.Node) error
}

type machineContext struct {
	Machine        *clusterv1.Machine
	Cluster        *clusterv1.Cluster
	InfraCluster   *infrav1.AzureManagedCluster
	InfraPool      *infrav1.AzureMachinePoolSpec
	InfraMachine   *infrav1.AzureManagedMachine
	ManagedCluster *managedclusters.Spec
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch;patch;update

func (r *AzureManagedMachineReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("azuremanagedmachine", req.NamespacedName)
	_ = ctx
	_ = log
	return r.reconcile(req)
}

func (r *AzureManagedMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.AzureManagedMachine{}).
		Watches(
			&source.Kind{Type: &clusterv1.Machine{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: util.MachineToInfrastructureMapFunc(infrav1.GroupVersion.WithKind("AzureManagedMachine")),
			},
		).
		Watches(
			&source.Kind{Type: &infrav1.AzureManagedCluster{}},
			&handler.EnqueueRequestsFromMapFunc{
				ToRequests: handler.ToRequestsFunc(r.AzureClusterToAzureMachines),
			},
		).
		Complete(r)
}

/*
	fn reconcile(req) (res, err) {
		get infra machine
		if deleting -> delete
		if cluster doesn't exist, create with 1 node in the requested pool
		set kubeconfig secret
		set cluster api endpoints on infra cluster
		if pool doesn't exist, create with 1 node
		if free node in pool, assign to machine
		if nodes == expected, increment vmss size
	}

	Reconcile begins by fetching all of the associated CRDs for this request:
	- the infrastructure machine object which triggered this reconcile
	- the upstream "generic" machine object which has an ownerRef on the infra machine
	- the upstream cluster object indicated in the machine object metadata labels, in the same namespace as the machine
	- the infrastructure cluster object specified by the infra ref of the upstream cluster

	When we have all of these objects, we validate that infraMachine.Spec.Pool targets a pool
	which is defined in the infraCluster.Spec.NodePools. Creating a machine translates into a request
	to add a machine to the targeted agent pool in the AKS cluster.

	Cluster creation is a bit tricky. It occurs lazily when the first machine is created.
	The cluster reconcile loop is a no-op and stores a dummy secret in the cluster kubeconfig.
	It also forces the upstream cluster status to ready, since with no control plane machines
	the upstream controller will never set the ready status.

	After cluster provisioning is complete, we search for a node in the correct agent pool
	to map to the requested infraMachine.Spec.ProviderID. We claim the node by assigning a
	label with the name of the machine.

	If there are no available nodes in the given agent pool, we may decide to
	add a new node via the Agent Pool API, but *only* if the number of workload
	cluster nodes for this pool is equal to the expected number based on the Agent Pool API.
	This is to avoid adding extra machines -- if we have 2 infra machines, 1 k8s nodes, and
	do a Get on the Agent Pool API to see it has 2 machines, we should not attempt to add a
	third machine until the second machine has registered as a node.
*/

func (r *AzureManagedMachineReconciler) reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("azuremanagedmachine", req.NamespacedName)

	log.Info("fetching infra machine")
	infraMachine := &infrav1.AzureManagedMachine{}
	if err := r.Client.Get(ctx, req.NamespacedName, infraMachine); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !infraMachine.DeletionTimestamp.IsZero() {
		if err := r.deleteMachine(ctx, infraMachine); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, removeFinalizer(ctx, r.Client, infraMachine)
	}

	if err := addFinalizer(ctx, r.Client, infraMachine); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("fetching owner machine")
	ownerMachine, err := util.GetOwnerMachine(ctx, r.Client, infraMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if ownerMachine == nil {
		log.Info("failed to find upstream owner ref on infra machine")
		return ctrl.Result{Requeue: true}, nil
	}

	log.Info("fetching owner cluster for owner machine")
	ownerCluster, err := util.GetOwnerCluster(ctx, r.Client, ownerMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if ownerCluster == nil {
		log.Info("failed to find cluster owner ref on upstream machine")
		return ctrl.Result{Requeue: true}, nil
	}

	log.Info("fetching infra cluster")
	infraCluster := &infrav1.AzureManagedCluster{}
	infraClusterName := client.ObjectKey{
		Namespace: infraMachine.Namespace,
		Name:      ownerCluster.Spec.InfrastructureRef.Name,
	}
	if err := r.Client.Get(ctx, infraClusterName, infraCluster); err != nil {
		return ctrl.Result{}, err
	}

	if !infraCluster.DeletionTimestamp.IsZero() {
		if err := r.deleteCluster(ctx, infraCluster); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, removeFinalizer(ctx, r.Client, infraCluster)
	}

	log.Info("finding infra pool for machine from cluster spec")
	var infraPool *infrav1.AzureMachinePoolSpec
	for idx, candidate := range infraCluster.Spec.NodePools {
		if candidate.Name == infraMachine.Spec.Pool {
			infraPool = &infraCluster.Spec.NodePools[idx]
			break
		}
	}
	if infraPool == nil {
		log.Info("pool not found in cluster for infra machine, will not requeue")
		return ctrl.Result{Requeue: false}, nil
	}

	log.Info("reconciling cluster")
	if err := r.reconcileCluster(ctx, log, infraCluster, infraPool, infraMachine); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("reconciling kubeconfig")
	if err := r.reconcileKubeconfig(ctx, ownerCluster, infraCluster); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("reconciling pool")
	if err := r.reconcilePool(ctx, log, infraCluster, infraPool, infraMachine); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("reconciling machine")
	if err := r.reconcileMachine(ctx, infraCluster, infraMachine, ownerCluster); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *AzureManagedMachineReconciler) reconcileCluster(ctx context.Context, log logr.Logger, infraCluster *infrav1.AzureManagedCluster, infraPool *infrav1.AzureMachinePoolSpec, infraMachine *infrav1.AzureManagedMachine) error {
	spec, err := r.ManagedClusterService.Get(ctx, infraCluster.Spec.SubscriptionID, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name)
	if err != nil {
		return err
	}

	settings, err := auth.GetSettingsFromFile()
	if err != nil {
		return err
	}

	spec.Set(
		managedclusters.Name(infraCluster.Spec.Name),
		managedclusters.Location(infraCluster.Spec.Location),
		managedclusters.SubscriptionID(infraCluster.Spec.SubscriptionID),
		managedclusters.ResourceGroup(infraCluster.Spec.ResourceGroup),
		managedclusters.KubernetesVersion(infraCluster.Spec.Version),
		managedclusters.DNSPrefix(infraCluster.Spec.Name),
		managedclusters.ServicePrincipal(settings.Values[auth.ClientID], settings.Values[auth.ClientSecret]),
		managedclusters.SSHPublicKey(infraCluster.Spec.SSHPublicKey),
	)

	if !spec.Exists() {
		spec.Set(
			managedclusters.AgentPool(infraPool.Name, infraPool.SKU, 1, infraPool.OSDiskSizeGB),
		)
	}

	log.Info("applying update to cluster")
	if err := r.ManagedClusterService.Ensure(ctx, spec); err != nil {
		log.Error(err, "failed update managed cluster")
		return err
	}

	infraCluster.Status.APIEndpoints = []infrav1.APIEndpoint{
		{
			Host: *spec.FQDN(),
			Port: 443,
		},
	}

	if err := r.Client.Status().Update(ctx, infraCluster); err != nil {
		return err
	}

	return nil
}

func (r *AzureManagedMachineReconciler) reconcilePool(ctx context.Context, log logr.Logger, infraCluster *infrav1.AzureManagedCluster, infraPool *infrav1.AzureMachinePoolSpec, infraMachine *infrav1.AzureManagedMachine) error {
	log.Info("fetching agent pool")
	spec, err := r.AgentPoolService.Get(ctx, infraCluster.Spec.SubscriptionID, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name, infraMachine.Spec.Pool)
	if err != nil {
		return err
	}

	spec.Set(
		agentpools.Name(infraMachine.Spec.Pool),
		agentpools.Cluster(infraCluster.Spec.Name),
		agentpools.SubscriptionID(infraCluster.Spec.SubscriptionID),
		agentpools.ResourceGroup(infraCluster.Spec.ResourceGroup),
		agentpools.KubernetesVersion(infraCluster.Spec.Version),
	)

	return r.AgentPoolService.Ensure(ctx, spec)
}

func (r *AzureManagedMachineReconciler) reconcileKubeconfig(ctx context.Context, ownerCluster *clusterv1.Cluster, infraCluster *infrav1.AzureManagedCluster) error {
	data, err := r.ManagedClusterService.GetCredentials(ctx, infraCluster.Spec.SubscriptionID, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name)
	if err != nil {
		return err
	}
	kubeconfig := makeKubeconfig(ownerCluster)
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, kubeconfig, func() error {
		kubeconfig.Data = map[string][]byte{
			secret.KubeconfigDataName: data,
		}
		return nil
	})
	return err
}

func (r *AzureManagedMachineReconciler) reconcileMachine(ctx context.Context, infraCluster *infrav1.AzureManagedCluster, infraMachine *infrav1.AzureManagedMachine, ownerCluster *clusterv1.Cluster) error {
	// Get all nodes in workload AKS cluster
	nodes, err := r.NodeListerFunc(r.Client)(ctx, ownerCluster)
	if err != nil {
		return err
	}

	// Find all
	nodesInPool := getNodesInPool(nodes, infraMachine.Spec.Pool)
	node := findNodeForMachine(nodesInPool, infraMachine)

	// Found an unclaimed node to assign to this machine
	if node != nil {
		// If the number of Kubernetes nodes in this pool is less than the expected VMSS capacity, wait to add a node until stabilization.
		node.Labels["azure.managed.infrastructure.cluster.x-k8s.io/machine-name"] = infraMachine.Name
		node.Labels["azure.managed.infrastructure.cluster.x-k8s.io/pool-name"] = infraMachine.Spec.Pool
		infraMachine.Spec.ProviderID = &node.Spec.ProviderID
		infraMachine.Status.Ready = true
		if err := r.NodeSetterFunc(r.Client)(ctx, ownerCluster, *node); err != nil {
			return err
		}
		if err := r.Client.Update(ctx, infraMachine); err != nil {
			return err
		}
		if err := r.Client.Status().Update(ctx, infraMachine); err != nil {
			return err
		}
		return nil
	}

	log.Info("fetching agent pool for machine assignment")
	spec, err := r.AgentPoolService.Get(ctx, infraCluster.Spec.SubscriptionID, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name, infraMachine.Spec.Pool)
	if err != nil {
		return err
	}

	// If we find nodeCount equal to the expected number of node according to Azure,
	// but could not find an unassigned node, we should add a machine via Azure API.
	if len(nodesInPool) == int(spec.Count()) {
		spec.Set(
			agentpools.Count(spec.Count() + 1),
		)
		return r.AgentPoolService.Ensure(ctx, spec)
	}

	return nil
}

func (r *AzureManagedMachineReconciler) deleteMachine(ctx context.Context, infraMachine *infrav1.AzureManagedMachine) error {
	if infraMachine.Spec.ProviderID != nil {
		tokens, err := tokenizeProviderID(*infraMachine.Spec.ProviderID)
		if err != nil {
			return err
		}
		subscription, resourceGroup, vmss, instance := tokens[1], tokens[3], tokens[7], tokens[9]
		if err := r.VMSSInstanceService.Delete(ctx, subscription, resourceGroup, vmss, instance); err != nil {
			return err
		}
	}
	return nil
}

func (r *AzureManagedMachineReconciler) deleteCluster(ctx context.Context, infraCluster *infrav1.AzureManagedCluster) error {
	return r.ManagedClusterService.Delete(ctx, infraCluster.Spec.SubscriptionID, infraCluster.Spec.ResourceGroup, infraCluster.Spec.Name)
}

func (r *AzureManagedMachineReconciler) AzureClusterToAzureMachines(o handler.MapObject) []ctrl.Request {
	result := []ctrl.Request{}

	c, ok := o.Object.(*infrav1.AzureManagedCluster)
	if !ok {
		r.Log.Error(errors.Errorf("expected a AzureManagedCluster but got a %T", o.Object), "failed to get AzureManagedMachine for AzureManagedCluster")
		return nil
	}
	log := r.Log.WithValues("AzureManagedCluster", c.Name, "Namespace", c.Namespace)

	cluster, err := util.GetOwnerCluster(context.TODO(), r.Client, c.ObjectMeta)
	switch {
	case apierrors.IsNotFound(err) || cluster == nil:
		return result
	case err != nil:
		log.Error(err, "failed to get owning cluster")
		return result
	}

	labels := map[string]string{clusterv1.MachineClusterLabelName: cluster.Name}
	machineList := &clusterv1.MachineList{}
	if err := r.List(context.TODO(), machineList, client.InNamespace(c.Namespace), client.MatchingLabels(labels)); err != nil {
		log.Error(err, "failed to list Machines")
		return nil
	}
	for _, m := range machineList.Items {
		if m.Spec.InfrastructureRef.Name == "" {
			continue
		}
		name := client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.InfrastructureRef.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}

	return result
}

type RuntimeMeta interface {
	runtime.Object
	metav1.Object
}

func addFinalizer(ctx context.Context, kubeclient client.Client, obj RuntimeMeta) error {
	finalizers := obj.GetFinalizers()
	if !stringslice.Has(finalizers, clusterv1.MachineFinalizer) {
		obj.SetFinalizers(append(finalizers, clusterv1.MachineFinalizer))
		if err := kubeclient.Update(ctx, obj); err != nil {
			return err
		}
	}
	return nil
}

func removeFinalizer(ctx context.Context, kubeclient client.Client, obj RuntimeMeta) error {
	finalizers := obj.GetFinalizers()
	for idx, val := range finalizers {
		if val == clusterv1.MachineFinalizer {
			obj.SetFinalizers(append(finalizers[:idx], finalizers[idx+1:]...))
			if err := kubeclient.Update(ctx, obj); err != nil {
				return err
			}
		}
	}
	return nil
}

// TODO(ace): consider switching to a map. Do we *need* ordering guarantees provided by an array?
func hasPool(infraCluster *infrav1.AzureManagedCluster, pool string) bool {
	for _, got := range infraCluster.Spec.NodePools {
		if got.Name == pool {
			return true
		}
	}
	return false
}

func getNodes(ctx context.Context, kubeclient client.Client) ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	if err := kubeclient.List(ctx, nodeList); err != nil {
		return nil, err
	}

	return nodeList.Items, nil
}

func getNodesInPool(nodes []corev1.Node, pool string) (hasMachine []corev1.Node) {
	result := []corev1.Node{}
	for _, node := range nodes {
		val := node.Labels["agentpool"]
		if val == pool {
			result = append(result, node)
		}
	}
	return result
}

func findNodeForMachine(nodes []corev1.Node, infraMachine *infrav1.AzureManagedMachine) *corev1.Node {
	var chosen *corev1.Node
	for _, node := range nodes {
		machineName, ok := node.Labels["azure.managed.infrastructure.cluster.x-k8s.io/machine-name"]
		if !ok || machineName == infraMachine.Name {
			chosen = node.DeepCopy()
		}
	}
	return chosen
}

func DefaultNodeListerFunc(kubeclient client.Client) func(ctx context.Context, cluster *clusterv1.Cluster) ([]corev1.Node, error) {
	return func(ctx context.Context, ownerCluster *clusterv1.Cluster) ([]corev1.Node, error) {
		remote, err := NewClusterClient(kubeclient, ownerCluster)
		if err != nil {
			return nil, err
		}
		nodeList := &corev1.NodeList{}
		if err := remote.List(ctx, nodeList); err != nil {
			return nil, err
		}

		return nodeList.Items, nil
	}
}

func DefaultNodeSetterFunc(kubeclient client.Client) func(ctx context.Context, cluster *clusterv1.Cluster, node corev1.Node) error {
	return func(ctx context.Context, ownerCluster *clusterv1.Cluster, node corev1.Node) error {
		remote, err := NewClusterClient(kubeclient, ownerCluster)
		if err != nil {
			return err
		}
		return remote.Update(ctx, &node)
	}
}

//
// TODO(ace): remove below here when we target v1alpha3, it's copied from there.
//

// NewClusterClient returns a Client for interacting with a remote Cluster using the given scheme for encoding and decoding objects.
func NewClusterClient(c client.Client, cluster *clusterv1.Cluster) (client.Client, error) {
	restConfig, err := RESTConfig(c, cluster)
	if err != nil {
		return nil, err
	}
	mapper, err := apiutil.NewDynamicRESTMapper(restConfig, apiutil.WithLazyDiscovery)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create a DynamicRESTMapper for Cluster %s/%s", cluster.Namespace, cluster.Name)
	}
	ret, err := client.New(restConfig, client.Options{
		Mapper: mapper,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create client for Cluster %s/%s", cluster.Namespace, cluster.Name)
	}

	return ret, nil
}

// RESTConfig returns a configuration instance to be used with a Kubernetes client.
func RESTConfig(c client.Client, cluster *clusterv1.Cluster) (*rest.Config, error) {
	kubeConfig, err := kcfg.FromSecret(c, cluster)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to retrieve kubeconfig secret for Cluster %s/%s", cluster.Namespace, cluster.Name)
	}

	restConfig, err := clientcmd.RESTConfigFromKubeConfig(kubeConfig)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create REST configuration for Cluster %s/%s", cluster.Namespace, cluster.Name)
	}

	return restConfig, nil
}
