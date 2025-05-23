/*
Copyright 2023 The K8sGPT Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package k8sgpt

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	corev1alpha1 "github.com/k8sgpt-ai/k8sgpt-operator/api/v1alpha1"
	"github.com/k8sgpt-ai/k8sgpt-operator/pkg/integrations"
	"github.com/k8sgpt-ai/k8sgpt-operator/pkg/metrics"
	"github.com/k8sgpt-ai/k8sgpt-operator/pkg/sinks"

	//+kubebuilder:scaffold:imports
	ctrl "sigs.k8s.io/controller-runtime"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var ctx context.Context
var cancel context.CancelFunc

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	useExistingCluster := false
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		UseExistingCluster:    &useExistingCluster,
	}

	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		localAssetsPath := filepath.Join("..", "..", "..", "bin", "k8s", "1.26.0-darwin-arm64")
		if _, err := os.Stat(localAssetsPath); err == nil {
			testEnv.BinaryAssetsDirectory = localAssetsPath
			logf.Log.Info("Using local KUBEBUILDER_ASSETS", "path", localAssetsPath)
		} else {
			logf.Log.Info("KUBEBUILDER_ASSETS not set and local assets not found. envtest will attempt to download.")
		}
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = corev1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
	})
	Expect(err).ToNot(HaveOccurred())

	integration, err := integrations.NewIntegrations(k8sManager.GetClient(), context.Background())
	Expect(err).ToNot(HaveOccurred())

	timeout, exists := os.LookupEnv("OPERATOR_SINK_WEBHOOK_TIMEOUT_SECONDS")
	if !exists {
		timeout = "35s"
	}

	sinkTimeout, err := time.ParseDuration(timeout)
	Expect(err).ToNot(HaveOccurred())

	sinkClient := sinks.NewClient(sinkTimeout)

	metricsBuilder := metrics.InitializeMetrics()

	ctx, cancel = context.WithCancel(context.TODO())

	err = (&K8sGPTReconciler{
		Client:         k8sManager.GetClient(),
		Scheme:         k8sManager.GetScheme(),
		Integrations:   integration,
		SinkClient:     sinkClient,
		MetricsBuilder: metricsBuilder,
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

})

var _ = AfterSuite(func() {
	By("cleaning test context")

	// https://github.com/kubernetes-sigs/controller-runtime/issues/1571
	cancel()
	By("tearing down the test environment,but I do nothing here.")
	err := testEnv.Stop()
	// Set 4 with random
	if err != nil {
		time.Sleep(4 * time.Second)
	}
	err = testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
