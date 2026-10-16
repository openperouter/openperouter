// SPDX-License-Identifier:Apache-2.0

package routerconfiguration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/openperouter/openperouter/api/v1alpha1"
)

const coalescenceNodeName = "worker-1"

// Exercise the actual watch wiring, which isolated handler and predicate tests cannot verify.
func TestSetupWithManager(t *testing.T) {
	if testing.Short() {
		t.Skip("needs an API server")
	}
	env := startWiringEnv(t)

	t.Run("a burst of events is coalesced into one pass for the node", func(t *testing.T) {
		g := NewWithT(t)
		env.waitForQuiet(t)
		start := env.router.passCount()

		// Keep the first pass running so the burst must coalesce behind it.
		env.router.hold()
		env.create(t, l2VNIForWiring("burst-0", 100, nil))
		env.router.waitEntered(t)
		const burst = 20
		for i := 1; i < burst; i++ {
			env.create(t, l2VNIForWiring(fmt.Sprintf("burst-%d", i), int32(100+i), nil))
		}
		env.waitForCachedL2VNIs(t, burst)
		env.router.release()
		env.waitForQuiet(t)

		passes := env.router.passesSince(start)
		g.Expect(passes).To(Not(BeEmpty()))
		g.Expect(len(passes)).To(BeNumerically("<=", 2), "passes: %v", passes)
		want := ctrl.Request{NamespacedName: types.NamespacedName{Name: coalescenceNodeName}}.String()
		for _, p := range passes {
			g.Expect(p.request).To(Equal(want))
		}
	})

	t.Run("an object that selects another node does not start a pass", func(t *testing.T) {
		env.waitForQuiet(t)
		start := env.router.passCount()

		env.create(t, l2VNIForWiring("elsewhere", 200, map[string]string{"kubernetes.io/hostname": "other-node"}))
		NewWithT(t).Consistently(func() []pass { return env.router.passesSince(start) },
			time.Second, 20*time.Millisecond).Should(BeEmpty())

		env.create(t, l2VNIForWiring("here", 201, map[string]string{"kubernetes.io/hostname": coalescenceNodeName}))
		env.waitForPassesSince(t, start, 1)
	})

}

type wiringEnv struct {
	client client.Client
	cache  client.Reader
	router *recordingRouterProvider
}

func startWiringEnv(t *testing.T) *wiringEnv {
	t.Helper()
	ctrl.SetLogger(logr.Discard())

	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		// Use binaries installed by make test unless KUBEBUILDER_ASSETS overrides them.
		BinaryAssetsDirectory: filepath.Join("..", "..", "..", "bin", "k8s",
			fmt.Sprintf("1.31.0-%s-%s", runtime.GOOS, runtime.GOARCH)),
	}
	cfg, err := testEnv.Start()
	if err != nil {
		t.Fatalf("failed to start envtest (run make test once, or set KUBEBUILDER_ASSETS): %v", err)
	}
	t.Cleanup(func() {
		NewWithT(t).Expect(testEnv.Stop()).To(Succeed())
	})

	scheme := testScheme(t)
	cli, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	env := &wiringEnv{client: cli, router: &recordingRouterProvider{}}
	env.create(t, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}})
	env.create(t, &corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: coalescenceNodeName, Labels: map[string]string{"kubernetes.io/hostname": coalescenceNodeName},
	}})

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "reload.sock")
	serveFRRReloader(t, socketPath)

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,

		Metrics: metricsserver.Options{BindAddress: "0"},
		// Allow repeated controller registration with go test -count=N.
		Controller: config.Controller{SkipNameValidation: new(true)},
	})
	if err != nil {
		t.Fatalf("failed to create manager: %v", err)
	}
	env.cache = mgr.GetCache()
	reconciler := &PERouterReconciler{
		Client:               mgr.GetClient(),
		Scheme:               scheme,
		MyNode:               coalescenceNodeName,
		MyNamespace:          testNamespace,
		Logger:               testLogger(),
		FRRConfigPath:        filepath.Join(dir, "frr.conf"),
		FRRReloadSocket:      socketPath,
		RouterProvider:       env.router,
		DatapathConfigurator: &noopDatapathConfigurator{},
	}
	NewWithT(t).Expect(reconciler.SetupWithManager(mgr)).To(Succeed())

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- mgr.Start(ctx) }()
	t.Cleanup(func() {
		env.router.release()
		cancel()
		NewWithT(t).Expect(<-stopped).To(Succeed())
	})
	// Exclude startup and its status events from the subtests' pass counts.
	env.waitForPassesSince(t, 0, 1)
	env.waitForQuiet(t)
	return env
}

func (e *wiringEnv) create(t *testing.T, obj client.Object) {
	t.Helper()
	NewWithT(t).Expect(e.client.Create(context.Background(), obj)).To(Succeed())
}

func (e *wiringEnv) waitForQuiet(t *testing.T) {
	t.Helper()
	last, since := e.router.passCount(), time.Now()
	quiet := func() bool {
		if n := e.router.passCount(); n != last {
			last, since = n, time.Now()
			return false
		}
		return time.Since(since) >= 500*time.Millisecond
	}
	NewWithT(t).Eventually(quiet, 20*time.Second, 20*time.Millisecond).Should(BeTrue(),
		"the controller never went quiet")
}

func (e *wiringEnv) waitForPassesSince(t *testing.T, start, n int) {
	t.Helper()
	NewWithT(t).Eventually(func() int { return len(e.router.passesSince(start)) },
		10*time.Second, 10*time.Millisecond).Should(BeNumerically(">=", n))
}

// Cache visibility can precede event delivery; allow time for the last event to reach the queue.
func (e *wiringEnv) waitForCachedL2VNIs(t *testing.T, n int) {
	t.Helper()
	var since time.Time
	NewWithT(t).Eventually(func() bool {
		var l2vnis v1alpha1.L2VNIList
		if err := e.cache.List(context.Background(), &l2vnis); err != nil || len(l2vnis.Items) < n {
			return false
		}
		if since.IsZero() {
			since = time.Now()
		}
		return time.Since(since) >= 200*time.Millisecond
	}, 10*time.Second, 10*time.Millisecond).Should(BeTrue(), "the cache never held %d L2VNIs", n)
}

type pass struct {
	request string
	at      time.Time
}

func (p pass) String() string { return fmt.Sprintf("%s@%s", p.request, p.at.Format("15:04:05.000")) }

type recordingRouterProvider struct {
	mu      sync.Mutex
	passes  []pass
	gate    chan struct{}
	entered chan struct{}
}

func (p *recordingRouterProvider) New(ctx context.Context) (Router, error) {
	request, _ := ctx.Value(requestKey("request")).(string)
	p.mu.Lock()
	p.passes = append(p.passes, pass{request: request, at: time.Now()})
	gate, entered := p.gate, p.entered
	p.mu.Unlock()

	if gate != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-gate
	}
	return &mockRouter{}, nil
}

func (p *recordingRouterProvider) NodeIndex(_ context.Context) (int, error) {
	return 0, nil
}

func (p *recordingRouterProvider) hold() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gate = make(chan struct{})
	p.entered = make(chan struct{}, 1)
}

func (p *recordingRouterProvider) waitEntered(t *testing.T) {
	t.Helper()
	p.mu.Lock()
	entered := p.entered
	p.mu.Unlock()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("no pass started within 10s")
	}
}

func (p *recordingRouterProvider) release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gate != nil {
		close(p.gate)
		p.gate = nil
	}
}

func (p *recordingRouterProvider) passCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.passes)
}

func (p *recordingRouterProvider) passesSince(start int) []pass {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]pass(nil), p.passes[start:]...)
}

func l2VNIForWiring(name string, vni int32, nodeLabels map[string]string) *v1alpha1.L2VNI {
	l2vni := &v1alpha1.L2VNI{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
		Spec:       v1alpha1.L2VNISpec{VNI: vni},
	}
	if nodeLabels != nil {
		l2vni.Spec.NodeSelector = &metav1.LabelSelector{MatchLabels: nodeLabels}
	}
	return l2vni
}

func serveFRRReloader(t *testing.T, socketPath string) {
	t.Helper()
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to listen on %s: %v", socketPath, err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
}
