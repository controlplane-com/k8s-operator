package main

import (
	"flag"
	"fmt"
	"github.com/controlplane-com/k8s-operator/pkg/common"
	"github.com/controlplane-com/k8s-operator/pkg/controllers"
	"net/http"
	"os"
	"runtime"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	k8sRuntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// scheme is a global scheme for all known types.
var scheme = k8sRuntime.NewScheme()

func init() {
	// Register core K8s types.
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
}

func main() {
	// Set up logging
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false, "Enable leader election for controller manager. "+
		"Enabling this will ensure there is only one active controller manager.")

	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	setupLog := ctrl.Log.WithName("setup")
	setupLog.Info(fmt.Sprintf("Go Version: %s", runtime.Version()))
	setupLog.Info(fmt.Sprintf("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH))

	// Create the manager
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                server.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "example-operator-lock",
		WebhookServer: webhook.NewServer(webhook.Options{
			Port:     common.GetEnvInt("WEBHOOK_PORT", 9443),
			CertDir:  common.GetEnvStr("TLS_CERT_DIR", "/cert"),
			CertName: common.GetEnvStr("TLS_CERT_NAME", "tls.crt"),
			KeyName:  common.GetEnvStr("TLS_KEY_NAME", "tls.key"),
		}),
	})
	if err != nil {
		setupLog.Error(err, "Unable to start manager")
		os.Exit(1)
	}

	if err = controllers.BuildControllers(mgr); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "CplnCRDController")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", func(req *http.Request) error {
		return nil
	}); err != nil {
		setupLog.Error(err, "Failed to set up healthz")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", func(req *http.Request) error {
		return nil
	}); err != nil {
		setupLog.Error(err, "Failed to set up readyz")
		os.Exit(1)
	}

	mgr.GetWebhookServer().Register("/mutate", &admission.Webhook{
		Handler: controllers.CrMutator{},
	})

	// Start the manager
	setupLog.Info("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Problem running manager")
		os.Exit(1)
	}
}
