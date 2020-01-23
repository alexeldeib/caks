// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.
package main

import (
	"flag"
	"os"

	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	infrastructurev1alpha1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
	// +kubebuilder:scaffold:imports

	infrav1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
	"github.com/Azure/cluster-api-provider-aks/controllers"
	"github.com/Azure/cluster-api-provider-aks/pkg/services/agentpools"
	"github.com/Azure/cluster-api-provider-aks/pkg/services/managedclusters"
	"github.com/Azure/cluster-api-provider-aks/pkg/services/scalesetvms"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = infrav1.AddToScheme(scheme)
	_ = clusterv1.AddToScheme(scheme)
	_ = infrastructurev1alpha1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	flag.StringVar(&metricsAddr, "metrics-addr", "0", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctrl.SetLogger(zap.New(func(o *zap.Options) {
		o.Development = false
	}))

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		LeaderElection:     enableLeaderElection,
		Port:               9443,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	authorizer, err := auth.NewAuthorizerFromFile(azure.PublicCloud.ResourceManagerEndpoint)
	if err != nil {
		setupLog.Error(err, "unabled to create azure authorizer")
		os.Exit(1)
	}

	managedClusterService := managedclusters.NewService(authorizer)
	agentPoolService := agentpools.NewService(authorizer)
	vmssInstanceService := scalesetvms.NewService(authorizer)

	_ = managedClusterService
	_ = agentPoolService
	_ = vmssInstanceService

	if err = (&controllers.AzureManagedClusterReconciler{
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controllers").WithName("AzureManagedCluster"),
		Scheme:                mgr.GetScheme(),
		ManagedClusterService: managedClusterService,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AzureManagedCluster")
		os.Exit(1)
	}

	if err = (&controllers.AzureManagedMachineReconciler{
		Client:                mgr.GetClient(),
		Log:                   ctrl.Log.WithName("controllers").WithName("AzureManagedMachine"),
		Scheme:                mgr.GetScheme(),
		ManagedClusterService: managedClusterService,
		AgentPoolService:      agentPoolService,
		VMSSInstanceService:   vmssInstanceService,
		NodeListerFunc:        controllers.DefaultNodeListerFunc,
		NodeSetterFunc:        controllers.DefaultNodeSetterFunc,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AzureManagedMachine")
		os.Exit(1)
	}

	if err = (&controllers.AzureManagedMachinePoolReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("AzureManagedMachinePool"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "AzureManagedMachinePool")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
