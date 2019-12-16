// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT license.

package controllers

import (
	"encoding/json"
	"os"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	infrav1 "github.com/Azure/cluster-api-provider-aks/api/v1alpha1"
	"github.com/Azure/cluster-api-provider-aks/pkg/services"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var mgr ctrl.Manager
var doneMgr = make(chan struct{})

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{envtest.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	logf.SetLogger(zap.LoggerTo(GinkgoWriter, true))

	By("bootstrapping test environment")
	useExistingCluster := true
	testEnv = &envtest.Environment{
		UseExistingCluster: &useExistingCluster,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	authFile := os.Getenv("AZURE_AUTH_FILE")
	settings := map[string]string{}
	err = json.Unmarshal([]byte(authFile), settings)
	Expect(err).NotTo(HaveOccurred())

	authorizer, err := auth.NewClientCredentialsConfig(app, key, tenant).Authorizer()
	Expect(err).NotTo(HaveOccurred())

	log := logf.Log.WithName("testmanager")
	log.WithValues("app", app, "tenant", tenant).Info("using client configuration")

	Expect(err).NotTo(HaveOccurred())

	managedClusterService := services.NewManagedClusterService(authorizer)
	agentPoolService := services.NewAgentPoolService(authorizer)
	vmssService := services.NewVMSSService(authorizer)
	vmssInstanceService := services.NewVMSSInstanceService(authorizer)

	err = infrav1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).ToNot(HaveOccurred())
	Expect(k8sClient).ToNot(BeNil())

	By("setting up a new manager")
	mgr, err = ctrl.NewManager(cfg, ctrl.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0",
	})
	Expect(err).ToNot(HaveOccurred())

	err = (&AzureManagedClusterReconciler{
		Client:                mgr.GetClient(),
		Log:                   log.WithName("controllers").WithName("AzureManagedCluster"),
		Scheme:                mgr.GetScheme(),
		ManagedClusterService: managedClusterService,
	}).SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	err = (&AzureManagedMachineReconciler{
		Client:                mgr.GetClient(),
		Log:                   log.WithName("controllers").WithName("AzureManagedMachine"),
		Scheme:                mgr.GetScheme(),
		ManagedClusterService: managedClusterService,
		AgentPoolService:      agentPoolService,
		VMSSService:           vmssService,
		VMSSInstanceService:   vmssInstanceService,
	}).SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	err = (&AzureMachinePoolReconciler{
		Client: mgr.GetClient(),
		Log:    log.WithName("controllers").WithName("AzureMachinePool"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr)
	Expect(err).ToNot(HaveOccurred())

	By("starting the manager")
	go func() {
		Expect(mgr.Start(doneMgr)).ToNot(HaveOccurred())
	}()

	close(done)
}, 60)

var _ = AfterSuite(func() {
	By("closing the manager stop channel")
	close(doneMgr)
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})
