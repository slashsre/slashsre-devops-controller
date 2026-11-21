// Command slashsre-devops-controller runs the Kubernetes controller manager and HTTP server.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-logr/zapr"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/slashsre/slashsre-devops-controller/api/v1alpha1"
	"github.com/slashsre/slashsre-devops-controller/internal/controller"
	"github.com/slashsre/slashsre-devops-controller/internal/k8sutil"
	"github.com/slashsre/slashsre-devops-controller/internal/logging"
	"github.com/slashsre/slashsre-devops-controller/internal/posttest"
	"github.com/slashsre/slashsre-devops-controller/internal/providers/pipeline"
	"github.com/slashsre/slashsre-devops-controller/internal/providers/pipeline/bitbucket"
	"github.com/slashsre/slashsre-devops-controller/internal/providers/pipeline/github"
	"github.com/slashsre/slashsre-devops-controller/internal/providers/queue"
	"github.com/slashsre/slashsre-devops-controller/internal/providers/queue/awssqs"
	"github.com/slashsre/slashsre-devops-controller/internal/remediation"
	"github.com/slashsre/slashsre-devops-controller/internal/server"
	"github.com/slashsre/slashsre-devops-controller/internal/version"
)

const appName = "slashsre-devops-controller"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func initFlags() *pflag.FlagSet {
	fs := pflag.NewFlagSet(appName, pflag.ContinueOnError)
	fs.String("config", "", "optional config file (yaml); defaults to ./config.yaml or /etc/"+appName+"/config.yaml when present")
	fs.String("host", "", "HTTP bind address")
	fs.Int("port", 9898, "HTTP port (API, webhooks, UI, metrics, probes)")
	fs.String("domain", "localhost:9898", "public host used in the Swagger UI")
	fs.String("level", "info", "log level: debug, info, warn, error")
	fs.String("logstash-host", "", "Logstash host for TCP log shipping (disabled when empty)")
	fs.String("logstash-port", "9800", "Logstash TCP port")
	fs.String("ui-message", "", "message shown in the UI")
	fs.String("allowed-origins", "", "CORS Access-Control-Allow-Origin for /api/v1 (disabled when empty)")
	fs.String("api-token", "", "bearer token required by mutating /api/v1 endpoints (open when empty)")
	fs.String("webhook-token", "", "bearer token required by webhook endpoints (open when empty)")
	fs.Bool("leader-elect", false, "enable leader election (required when running more than one replica)")
	fs.String("leader-election-namespace", "", "namespace for the leader election lease (defaults to the pod namespace)")
	fs.Duration("shutdown-timeout", 10*time.Second, "HTTP graceful shutdown timeout")
	return fs
}

func loadConfig(fs *pflag.FlagSet) error {
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	if err := viper.BindPFlags(fs); err != nil {
		return err
	}
	viper.SetEnvPrefix("DEVOPS")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	if path := viper.GetString("config"); path != "" {
		viper.SetConfigFile(path)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath("/etc/" + appName)
		viper.AddConfigPath(".")
	}
	if err := viper.ReadInConfig(); err != nil {
		if _, notFound := err.(viper.ConfigFileNotFoundError); !notFound || viper.GetString("config") != "" {
			return fmt.Errorf("reading config: %w", err)
		}
	}
	hostname, _ := os.Hostname()
	viper.Set("hostname", hostname)
	viper.Set("version", version.VERSION)
	viper.Set("revision", version.REVISION)
	return nil
}

func run() error {
	if err := loadConfig(initFlags()); err != nil {
		return err
	}
	ctx := ctrl.SetupSignalHandler()

	zapLogger, err := logging.InitLogger(viper.GetString("level"), viper.GetString("logstash-host"), viper.GetString("logstash-port"), ctx.Done())
	if err != nil {
		return fmt.Errorf("initializing logger: %w", err)
	}
	defer func() { _ = zapLogger.Sync() }()
	ctrl.SetLogger(zapr.NewLogger(zapLogger))
	zapLogger.Info("starting "+appName, zap.String("version", version.VERSION), zap.String("revision", version.REVISION))

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))

	restCfg := ctrl.GetConfigOrDie()
	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme: scheme,
		// Read these directly from the API server: caching them would require
		// cluster-wide list/watch (secrets!) and hold every pod in memory.
		Client: client.Options{Cache: &client.CacheOptions{DisableFor: []client.Object{
			&corev1.Secret{}, &corev1.Pod{},
			&appsv1.ReplicaSet{}, &appsv1.Deployment{}, &appsv1.StatefulSet{}, &appsv1.DaemonSet{},
		}}},
		// Metrics and probes are served by our own HTTP server on --port.
		Metrics:                 metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress:  "0",
		LeaderElection:          viper.GetBool("leader-elect"),
		LeaderElectionID:        "slashsre-devops-controller.slashsre.controller.io",
		LeaderElectionNamespace: viper.GetString("leader-election-namespace"),
	})
	if err != nil {
		return fmt.Errorf("creating manager: %w", err)
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("creating clientset: %w", err)
	}

	queues := queue.NewRegistry()
	queues.Register(v1alpha1.QueueProviderAWSSQS, awssqs.New)

	pipelines := pipeline.NewRegistry()
	httpClient := pipeline.DefaultHTTPClient()
	pipelines.Register(v1alpha1.PipelineProviderGitHub, github.New(httpClient))
	pipelines.Register(v1alpha1.PipelineProviderBitbucket, bitbucket.New(httpClient))

	recorder := mgr.GetEventRecorderFor(appName)
	logr := ctrl.Log.WithName("controller")

	qtj := &controller.QueueTriggeredJobReconciler{Client: mgr.GetClient(), Scheme: scheme, Queues: queues, Recorder: recorder}
	if err := qtj.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up QueueTriggeredJob controller: %w", err)
	}
	nlr := &controller.NodeLifecycleRuleReconciler{
		Client:   mgr.GetClient(),
		Queues:   queues,
		Drainer:  &k8sutil.Drainer{Clientset: clientset, Log: logr.WithName("drain")},
		FindNode: k8sutil.FindNodeByInstanceID,
		Recorder: recorder,
	}
	if err := nlr.SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up NodeLifecycleRule controller: %w", err)
	}

	executor := &remediation.Executor{Client: mgr.GetClient(), Recorder: recorder, Log: logr.WithName("alert-remediation")}
	runner := &posttest.Runner{Client: mgr.GetClient(), Scheme: scheme, Pipelines: pipelines, Recorder: recorder, Log: logr.WithName("post-deploy-test")}

	srv := server.New(server.Config{
		Host:            viper.GetString("host"),
		Port:            viper.GetInt("port"),
		Domain:          viper.GetString("domain"),
		Hostname:        viper.GetString("hostname"),
		UIMessage:       viper.GetString("ui-message"),
		AllowedOrigins:  viper.GetString("allowed-origins"),
		ShutdownTimeout: viper.GetDuration("shutdown-timeout"),
		APIToken:        viper.GetString("api-token"),
		WebhookToken:    viper.GetString("webhook-token"),
	}, zapLogger, mgr.GetClient(), executor, runner, qtj, mgr.GetCache().WaitForCacheSync)
	if err := mgr.Add(srv); err != nil {
		return fmt.Errorf("adding HTTP server: %w", err)
	}

	return mgr.Start(ctx)
}
