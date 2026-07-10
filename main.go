package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/agent-sandbox/agent-sandbox/pkg/activator"
	"github.com/agent-sandbox/agent-sandbox/pkg/capacity"
	"github.com/agent-sandbox/agent-sandbox/pkg/config"
	"github.com/agent-sandbox/agent-sandbox/pkg/handler"
	"github.com/agent-sandbox/agent-sandbox/pkg/leader"
	"github.com/agent-sandbox/agent-sandbox/pkg/sandbox"
	"github.com/agent-sandbox/agent-sandbox/pkg/scaler"
	"github.com/agent-sandbox/agent-sandbox/pkg/telemetry"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	metrics "k8s.io/metrics/pkg/client/clientset/versioned"
	configmapinformer "knative.dev/pkg/configmap/informer"
	"knative.dev/pkg/controller"
	"knative.dev/pkg/injection"
	"knative.dev/pkg/version"

	kubeclient "knative.dev/pkg/client/injection/kube/client"
)

func main() {
	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	//init config for global
	cfg := config.InitConfig()

	var fs flag.FlagSet
	var err error

	klog.InitFlags(&fs)
	klog.Infof("Loaded config %+v", config.Cfg)

	// set log level
	err = fs.Set("v", cfg.LogLevel)
	if err != nil {
		log.Fatalf("Failed to set log level: %v", err)
	}

	klog.Info("Setup k8s cluster connection and start informers")

	kubecfg := injection.ParseAndGetRESTConfigOrDie()
	klog.Info("Cluster info ", "host=", kubecfg.Host)

	log.Printf("Registering %d clients", len(injection.Default.GetClients()))
	log.Printf("Registering %d informer factories", len(injection.Default.GetInformerFactories()))
	log.Printf("Registering %d informers", len(injection.Default.GetInformers()))
	log.Printf("Registering %d filtered informers", len(injection.Default.GetFilteredInformers()))

	kubecfg.QPS = 200 * rest.DefaultQPS
	kubecfg.Burst = 20 * rest.DefaultBurst
	rootCtx = injection.WithNamespaceScope(rootCtx, config.Cfg.SandboxNamespace)
	rootCtx, informers := injection.Default.SetupInformers(rootCtx, kubecfg)

	kubeClient := kubeclient.Get(rootCtx)
	metricsClient, err := metrics.NewForConfig(kubecfg)
	if err != nil {
		klog.Fatalf("Failed to initialize metrics client: %v", err)
	}

	// Watch the runtime ConfigMap on every replica (read-only). Leader writes
	// the initial ConfigMap once it's elected (below); followers pick up the
	// change via this watcher and apply it to their local config.Cfg.
	cfg.KubeClient = kubeClient
	configMapWatcher := configmapinformer.NewInformedWatcher(kubeClient, cfg.SandboxNamespace)
	configMapWatcher.Watch(config.Cfg.ConfigmapName, config.WatchConfigMap())
	if err := configMapWatcher.Start(rootCtx.Done()); err != nil {
		klog.Fatal("Failed to start configuration manager", zap.Error(err))
	}

	// check k8s version is matched
	// We sometimes start up faster than we can reach kube-api. Poll on failure to prevent us terminating
	if perr := wait.PollUntilContextTimeout(rootCtx, time.Second, 60*time.Second, true, func(ctx context.Context) (bool, error) {
		if err = version.CheckMinimumVersion(kubeClient.Discovery()); err != nil {
			ctx.Done()
			log.Print("Failed to get k8s version ", err)
		}
		return err == nil, nil
	}); perr != nil {
		log.Fatal("Timed out attempting to get k8s version: ", err)
	}

	if err := controller.StartInformers(rootCtx.Done(), informers...); err != nil {
		log.Fatalln("Failed to start informers", zap.Error(err))
	}
	log.Printf("Starting informers %v", len(informers))

	eventRecoder := activator.GetRecorder(rootCtx)
	pl := sandbox.NewPoolManager(rootCtx)
	a := activator.NewActivator(rootCtx, eventRecoder)
	c := sandbox.NewController(rootCtx, kubecfg, pl, eventRecoder)
	c.MetricsClient = metricsClient

	// Start the capacity manager (read-only against informers — safe on every replica).
	capacity.Init(c)

	// Run cluster-wide side-effecting work on a single replica only. The
	// elected leader bootstraps the runtime ConfigMap, runs the scaler, and
	// drives the pool syncer. Every other replica keeps serving HTTP, reading
	// the ConfigMap watcher, and proxying sandbox traffic.
	go leader.RunAsLeader(rootCtx, leader.Config{
		LeaseName: "agent-sandbox-leader",
		Namespace: cfg.SandboxNamespace,
		Client:    kubeClient,
	}, func(ctx context.Context) {
		cfg.CheckAndSaveConfigToConfigmap()

		go func() {
			s := scaler.NewScaler(ctx, a, c)
			s.RunScaling()
		}()

		if cfg.PoolEnable {
			go func() {
				pl.StartPoolSyncing(ctx)
			}()
		}
	})

	// Init lifecycle-event telemetry. No-op when Telemetry.Enabled is false.
	telemetry.Init(rootCtx, telemetry.Settings{
		Enabled:    cfg.Telemetry.Enabled,
		BufferSize: cfg.Telemetry.BufferSize,
		SampleRate: cfg.Telemetry.SampleRate,
	}, cfg.Telemetry.OTLPEndpoint, cfg.Telemetry.OTLPURLPath, cfg.Telemetry.OTLPInsecure)
	defer func() {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		if err := telemetry.Shutdown(shutdownCtx); err != nil {
			klog.Warningf("Telemetry shutdown error: %v", err)
		}
	}()

	klog.Info("Starting the api server")
	apiServer := handler.New(rootCtx, a, c)
	if err := apiServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Print("Failed to run HTTP server", zap.Error(err))
	}

}
