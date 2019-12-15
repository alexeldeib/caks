// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package controllers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"os"
	goruntime "runtime"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2019-07-01/compute"
	"github.com/Azure/azure-sdk-for-go/services/containerservice/mgmt/2019-10-01/containerservice"
	infrav1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
	"github.com/Azure/go-autorest/autorest"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/alexeldeib/stringslice"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const finalizer = "azuremanagedclusters.infrastructure.cluster.x-k8s.io"

var (
	stateSucceeded = "Succeeded"
	stateFailed    = "Failed"
)

// AzureManagedClusterReconciler reconciles a AzureManagedCluster object
type AzureManagedClusterReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=azuremanagedclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *AzureManagedClusterReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	log := r.Log.WithValues("azuremanagedcluster", req.NamespacedName)

	var cluster infrav1.AzureManagedCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		log.Info("error during fetch from api server")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Fetch the Cluster.
	ownerCluster, err := util.GetOwnerCluster(ctx, r.Client, cluster.ObjectMeta)
	if err != nil {
		return reconcile.Result{}, err
	}
	if ownerCluster == nil {
		log.Info("Cluster Controller has not yet set OwnerRef")
		return reconcile.Result{}, nil
	}

	log = log.WithValues("cluster", ownerCluster.Name)

	if err := r.setKubeconfig(ctx, log, ownerCluster); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.setStatus(ctx, log, &cluster, ownerCluster); err != nil {
		return ctrl.Result{}, err
	}

	if cluster.DeletionTimestamp.IsZero() {
		if !stringslice.Has(cluster.GetFinalizers(), finalizer) {
			controllerutil.AddFinalizer(&cluster, finalizer)
			return ctrl.Result{}, r.Update(ctx, &cluster)
		}
	} else {
		log.Info("deletion not implemented yet")
		os.Exit(1)
	}

	// Reconcile cluster
	return ctrl.Result{}, nil // r.ensure(ctx, &cluster)
}

func (r *AzureManagedClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.AzureManagedCluster{}).
		Complete(r)
}

func (r *AzureManagedClusterReconciler) setKubeconfig(ctx context.Context, log logr.Logger, cluster *clusterv1.Cluster) error {
	kubeconfig := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secret.Name(cluster.Name, secret.Kubeconfig),
			Namespace: cluster.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "Cluster",
					Name:       cluster.Name,
					UID:        cluster.UID,
				},
			},
		},
	}
	key := types.NamespacedName{
		Name:      kubeconfig.Name,
		Namespace: kubeconfig.Namespace,
	}

	found := &corev1.Secret{}
	err := r.Client.Get(ctx, key, found)

	if apierrs.IsNotFound(err) {
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, found, func() error {
			if found.Data == nil {
				found.Data = map[string][]byte{}
			}
			return nil
		}); err != nil {
			log.Info("failed to create or update kubeconfig")
			return err
		}
	}

	if client.IgnoreNotFound(err) != nil {
		return err
	}

	return nil
}

func (r *AzureManagedClusterReconciler) setStatus(ctx context.Context, log logr.Logger, cluster *infrav1.AzureManagedCluster, ownerCluster *clusterv1.Cluster) error {
	cluster.Status.Ready = true
	if err := r.Status().Update(ctx, cluster); err != nil {
		log.Info("failed to patch infra cluster")
		return err
	}

	ownerCluster.Status.ControlPlaneInitialized = true
	if err := r.Status().Update(ctx, ownerCluster); err != nil {
		log.Info("failed to patch owner cluster")
		return err
	}
	return nil
}

func (r *AzureManagedClusterReconciler) ensure(ctx context.Context, cluster *infrav1.AzureManagedCluster) (ctrl.Result, error) {
	log := r.Log.WithName("ensure")

	// if err := validate(cluster); err != nil {
	// 	log.Error(err, "failed cluster validation")
	// 	return ctrl.Result{}, nil
	// }

	log.Info("creating cluster client")
	az, err := NewManagedClustersClient(cluster.Spec.SubscriptionID)
	if err != nil {
		return ctrl.Result{}, err
	}

	log.Info("fetching existing cluster")
	found, err := az.Get(ctx, cluster.Spec.ResourceGroup, cluster.Spec.Name)
	if err != nil && !found.IsHTTPStatus(http.StatusNotFound) {
		return ctrl.Result{}, err
	}

	log.Info("constructing desired cluster")
	desired, err := convertCRDToAzure(cluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	if found.IsHTTPStatus(http.StatusNotFound) {
		log.Info("creating not-found cluster")
		_, err = az.CreateOrUpdate(ctx, cluster.Spec.ResourceGroup, cluster.Spec.Name, desired)
		return ctrl.Result{Requeue: true}, err
	}

	if !isFinished(found.ProvisioningState) {
		log.Info("requeueing to wait for azure provisioning")
		return ctrl.Result{RequeueAfter: time.Second * 5}, nil
	}

	log.Info("normalizing found cluster for comparison")
	normalized := normalize(found)

	log.Info("diffing normalized found cluster with desired")
	ignored := cmpopts.IgnoreFields(containerservice.ManagedCluster{}, "ManagedClusterProperties.ServicePrincipalProfile.Secret")
	diff := cmp.Diff(desired, normalized, ignored)
	if diff == "" {
		log.Info("normalized and desired matched, no update needed (go-cmp)")
		return ctrl.Result{}, nil
	}
	fmt.Printf("update required (-want +got):\n%s", diff)

	log.Info("applying update to existing cluster")
	_, err = az.CreateOrUpdate(ctx, cluster.Spec.ResourceGroup, cluster.Spec.Name, desired)
	return ctrl.Result{Requeue: true}, err
}

// func convertAzureToCRD(in containerservice.ManagedCluster) (infrav1.AzureManagedClusterSpec, error) {
// 	tokens := strings.Split(to.String(in.ID), "/")
// 	if len(tokens) < 8 {
// 		return infrav1.AzureManagedClusterSpec{}, errors.New("failed to parse resource id")
// 	}

// 	out := infrav1.AzureManagedClusterSpec{
// 		SubscriptionID: tokens[1],
// 		ResourceGroup:  tokens[3],
// 		Name:           tokens[7],
// 		Location:       to.String(in.Location),
// 	}

// 	if in.ManagedClusterProperties != nil {
// 		if in.ManagedClusterProperties.KubernetesVersion != nil {
// 			out.Version = *in.ManagedClusterProperties.KubernetesVersion
// 		}
// 		if in.ManagedClusterProperties.AgentPoolProfiles != nil {
// 			out.NodePools = []infrav1.AzureMachinePoolSpec{}
// 			for _, agentPool := range *in.ManagedClusterProperties.AgentPoolProfiles {
// 				np := infrav1.AzureMachinePoolSpec{
// 					Name:     to.String(agentPool.Name),
// 					SKU:      string(agentPool.VMSize),
// 					Capacity: to.Int32(agentPool.Count),
// 				}
// 				out.NodePools = append(out.NodePools, np)
// 			}
// 		}
// 	}

// 	return out, nil
// }

func convertCRDToAzure(local *infrav1.AzureManagedCluster) (containerservice.ManagedCluster, error) {
	settings, err := auth.GetSettingsFromFile()
	if err != nil {
		return containerservice.ManagedCluster{}, err
	}
	return containerservice.ManagedCluster{
		Name:     &local.Spec.Name,
		Location: &local.Spec.Location,
		ManagedClusterProperties: &containerservice.ManagedClusterProperties{
			KubernetesVersion: &local.Spec.Version,
			DNSPrefix:         &local.Spec.Name,
			LinuxProfile: &containerservice.LinuxProfile{
				AdminUsername: to.StringPtr("azureuser"),
				SSH: &containerservice.SSHConfiguration{
					PublicKeys: &[]containerservice.SSHPublicKey{{KeyData: &local.Spec.SSHPublicKey}},
				},
			},
			// AgentPoolProfiles: makeMachinePool(local.Spec.NodePools),
			ServicePrincipalProfile: &containerservice.ManagedClusterServicePrincipalProfile{
				ClientID: to.StringPtr(settings.Values[auth.ClientID]),
				Secret:   to.StringPtr(settings.Values[auth.ClientSecret]),
			},
		},
	}, nil
}

func makeMachinePools(machinePools []infrav1.AzureMachinePoolSpec) *[]containerservice.ManagedClusterAgentPoolProfile {
	var result []containerservice.ManagedClusterAgentPoolProfile
	for _, np := range machinePools {
		result = append(result, makeMachinePool(np))
	}
	return &result
}

func makeMachinePool(machinePool infrav1.AzureMachinePoolSpec) containerservice.ManagedClusterAgentPoolProfile {
	name := ""
	result := containerservice.ManagedClusterAgentPoolProfile{
		VMSize: containerservice.VMSizeTypes(machinePool.SKU),
		Type:   containerservice.VirtualMachineScaleSets,
		Name:   &name,
	}
	*result.Name = machinePool.Name
	return result
}

// func validate(cluster *infrav1.AzureManagedCluster) error {
// 	if cluster.Spec.NodePools[0].Capacity < 1 {
// 		return errors.New("default node pool must have at least one node")
// 	}
// 	return nil
// }

// normalize constructs a fresh containerservice.ManagedCluster with only the fields set by the controller, not fields defaulted by Azure.
func normalize(before containerservice.ManagedCluster) containerservice.ManagedCluster {
	after := containerservice.ManagedCluster{
		Name:     before.Name,
		Location: before.Location,
	}
	if before.ManagedClusterProperties != nil {
		after.ManagedClusterProperties = &containerservice.ManagedClusterProperties{
			KubernetesVersion:       before.ManagedClusterProperties.KubernetesVersion,
			DNSPrefix:               before.ManagedClusterProperties.DNSPrefix,
			ServicePrincipalProfile: before.ManagedClusterProperties.ServicePrincipalProfile,
			LinuxProfile:            before.ManagedClusterProperties.LinuxProfile,
		}
		if before.ManagedClusterProperties.AgentPoolProfiles != nil {
			after.ManagedClusterProperties.AgentPoolProfiles = &[]containerservice.ManagedClusterAgentPoolProfile{}
			for _, dirty := range *before.ManagedClusterProperties.AgentPoolProfiles {
				clean := containerservice.ManagedClusterAgentPoolProfile{
					Name:   dirty.Name,
					Count:  dirty.Count,
					VMSize: dirty.VMSize,
					Type:   containerservice.AgentPoolType("VirtualMachineScaleSets"),
				}
				*after.ManagedClusterProperties.AgentPoolProfiles = append(*after.ManagedClusterProperties.AgentPoolProfiles, clean)
			}
		}
	}
	return after
}

func isFinished(state *string) bool {
	if state == nil {
		return true
	}
	switch *state {
	case stateFailed:
		return true
	case stateSucceeded:
		return true
	default:
		return false
	}
}

func decorate(log logr.Logger) logr.Logger {
	programCounter, filename, line, _ := goruntime.Caller(1)
	fn := goruntime.FuncForPC(programCounter).Name()
	return log.WithName(fmt.Sprintf("[%s]%s:%d", fn, filename, line))
}

func decorateFailure(log logr.Logger) logr.Logger {
	programCounter, filename, line, _ := goruntime.Caller(1)
	fn := goruntime.FuncForPC(programCounter).Name()
	return log.WithValues("failedAt", fmt.Sprintf("[%s]%s:%d", fn, filename, line))
}

func NewManagedClustersClient(subscriptionID string) (containerservice.ManagedClustersClient, error) {
	client := containerservice.NewManagedClustersClient(subscriptionID)
	if err := client.AddToUserAgent("cluster-api-provider-aks"); err != nil {
		return containerservice.ManagedClustersClient{}, err
	}
	authorizer, err := auth.NewAuthorizerFromFile(azure.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
		return containerservice.ManagedClustersClient{}, err
	}
	client.Authorizer = authorizer
	// client.RequestInspector = LogRequest()
	// client.ResponseInspector = LogResponse()
	return client, nil
}

func NewAgentPoolsClient(subscriptionID string) (containerservice.AgentPoolsClient, error) {
	client := containerservice.NewAgentPoolsClient(subscriptionID)
	if err := client.AddToUserAgent("cluster-api-provider-aks"); err != nil {
		return containerservice.AgentPoolsClient{}, err
	}
	authorizer, err := auth.NewAuthorizerFromFile(azure.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
		return containerservice.AgentPoolsClient{}, err
	}
	client.Authorizer = authorizer
	// client.RequestInspector = LogRequest()
	// client.ResponseInspector = LogResponse()
	return client, nil
}

func NewVirtualMachineScaleSetVMsClient(subscriptionID string) (compute.VirtualMachineScaleSetVMsClient, error) {
	client := compute.NewVirtualMachineScaleSetVMsClient(subscriptionID)
	if err := client.AddToUserAgent("cluster-api-provider-aks"); err != nil {
		return compute.VirtualMachineScaleSetVMsClient{}, err
	}
	authorizer, err := auth.NewAuthorizerFromFile(azure.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
		return compute.VirtualMachineScaleSetVMsClient{}, err
	}
	client.Authorizer = authorizer
	client.RequestInspector = LogRequest()
	client.ResponseInspector = LogResponse()
	return client, nil
}

func NewVirtualMachineScaleSetsClient(subscriptionID string) (compute.VirtualMachineScaleSetsClient, error) {
	client := compute.NewVirtualMachineScaleSetsClient(subscriptionID)
	if err := client.AddToUserAgent("cluster-api-provider-aks"); err != nil {
		return compute.VirtualMachineScaleSetsClient{}, err
	}
	authorizer, err := auth.NewAuthorizerFromFile(azure.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
		return compute.VirtualMachineScaleSetsClient{}, err
	}
	client.Authorizer = authorizer
	client.RequestInspector = LogRequest()
	client.ResponseInspector = LogResponse()
	return client, nil
}

// LogRequest logs full autorest requests for any Azure client.
func LogRequest() autorest.PrepareDecorator {
	return func(p autorest.Preparer) autorest.Preparer {
		return autorest.PreparerFunc(func(r *http.Request) (*http.Request, error) {
			r, err := p.Prepare(r)
			if err != nil {
				fmt.Println(err)
			}
			dump, _ := httputil.DumpRequestOut(r, true)
			fmt.Println(string(dump))
			return r, err
		})
	}
}

// LogResponse logs full autorest responses for any Azure client.
func LogResponse() autorest.RespondDecorator {
	return func(p autorest.Responder) autorest.Responder {
		return autorest.ResponderFunc(func(r *http.Response) error {
			err := p.Respond(r)
			if err != nil {
				fmt.Println(err)
			}
			dump, _ := httputil.DumpResponse(r, true)
			fmt.Println(string(dump))
			return err
		})
	}
}
