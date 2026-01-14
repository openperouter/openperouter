/*
Copyright 2024.

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

package operator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/openperouter/openperouter/internal/logging"
	operatorapi "github.com/openperouter/openperouter/operator/api/v1alpha1"
	"github.com/openperouter/openperouter/operator/internal/envconfig"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var ctx context.Context
var cancel context.CancelFunc

const (
	testChartPath             = "../bindata/deployment/openperouter"
	openperouterTestNamespace = "openperouter-test-namespace"
	controllerDaemonSetName   = "controller"
	routerDaemonSetName       = "router"
	nodemarkerDeploymentName  = "nodemarker"
)

var defaultEnvConfig = envconfig.EnvConfig{
	ControllerImage: envconfig.ImageInfo{
		Repo: "quay.io/openperouter/router",
		Tag:  "test",
	},
	FRRImage: envconfig.ImageInfo{
		Repo: "quay.io/openperouter/router",
		Tag:  "test",
	},
	MetricsPort:    7472,
	FRRMetricsPort: 7473,
	Namespace:      openperouterTestNamespace,
}

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.TODO())

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,

		// The BinaryAssetsDirectory is only required if you want to run the tests directly
		// without call the makefile target test. If not informed it will look for the
		// default path defined in controller-runtime which is /usr/local/kubebuilder/.
		// Note that you must have the required binaries setup under the bin directory to perform
		// the tests directly. When we run make test it will be setup and used automatically.
		BinaryAssetsDirectory: filepath.Join("..", "..", "bin", "k8s",
			fmt.Sprintf("1.31.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = operatorapi.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	// +kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // Disable metrics server to avoid port conflicts
		},
	})
	Expect(err).ToNot(HaveOccurred())

	logger, err := logging.New("debug")
	if err != nil {
		fmt.Println("unable to init logger", err)
		os.Exit(1)
	}
	ctrl.SetLogger(logr.FromSlogHandler(logger.Handler()))

	testNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: openperouterTestNamespace,
		},
	}

	err = k8sClient.Create(context.Background(), testNamespace)
	Expect(err).ToNot(HaveOccurred())

	openperouterChartPath = testChartPath

	reconciler := &OpenPERouterReconciler{
		Client:    k8sClient,
		Scheme:    scheme.Scheme,
		Logger:    logger,
		Namespace: openperouterTestNamespace,
		EnvConfig: defaultEnvConfig,
	}

	err = reconciler.SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()

})

var _ = AfterSuite(func() {
	openperouterChartPath = chartPath // restore to real value
	By("tearing down the test environment")
	cancel()
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
