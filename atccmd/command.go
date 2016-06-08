package atccmd

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/concourse/atc"
	"github.com/concourse/atc/api"
	"github.com/concourse/atc/api/buildserver"
	"github.com/concourse/atc/auth"
	"github.com/concourse/atc/auth/provider"
	"github.com/concourse/atc/buildreaper"
	"github.com/concourse/atc/builds"
	"github.com/concourse/atc/config"
	"github.com/concourse/atc/db"
	"github.com/concourse/atc/db/migrations"
	"github.com/concourse/atc/engine"
	"github.com/concourse/atc/exec"
	"github.com/concourse/atc/leaserunner"
	"github.com/concourse/atc/lostandfound"
	"github.com/concourse/atc/metric"
	"github.com/concourse/atc/pipelines"
	"github.com/concourse/atc/radar"
	"github.com/concourse/atc/resource"
	"github.com/concourse/atc/scheduler"
	"github.com/concourse/atc/web"
	"github.com/concourse/atc/web/webhandler"
	"github.com/concourse/atc/worker"
	"github.com/concourse/atc/worker/image"
	"github.com/concourse/atc/worker/transport"
	"github.com/concourse/atc/wrappa"
	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/context"
	"github.com/hashicorp/go-multierror"
	"github.com/lib/pq"
	"github.com/pivotal-golang/clock"
	"github.com/pivotal-golang/lager"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/grouper"
	"github.com/tedsuo/ifrit/http_server"
	"github.com/tedsuo/ifrit/sigmon"
	"github.com/xoebus/zest"
)

type ATCCommand struct {
	BindIP   IPFlag `long:"bind-ip"   default:"0.0.0.0" description:"IP address on which to listen for web traffic."`
	BindPort uint16 `long:"bind-port" default:"8080"    description:"Port on which to listen for HTTP traffic."`

	TLSBindPort uint16   `long:"tls-bind-port" description:"Port on which to listen for HTTPS traffic."`
	TLSCert     FileFlag `long:"tls-cert"      description:"File containing an SSL certificate."`
	TLSKey      FileFlag `long:"tls-key"       description:"File containing an RSA private key, used to encrypt HTTPS traffic."`

	ExternalURL URLFlag `long:"external-url" default:"http://127.0.0.1:8080" description:"URL used to reach any ATC from the outside world."`
	PeerURL     URLFlag `long:"peer-url"     default:"http://127.0.0.1:8080" description:"URL used to reach this ATC from other ATCs in the cluster."`

	OAuthBaseURL URLFlag `long:"oauth-base-url" description:"URL used as the base of OAuth redirect URIs. If not specified, the external URL is used."`

	PostgresDataSource string `long:"postgres-data-source" default:"postgres://127.0.0.1:5432/atc?sslmode=disable" description:"PostgreSQL connection string."`

	DebugBindIP   IPFlag `long:"debug-bind-ip"   default:"127.0.0.1" description:"IP address on which to listen for the pprof debugger endpoints."`
	DebugBindPort uint16 `long:"debug-bind-port" default:"8079"      description:"Port on which to listen for the pprof debugger endpoints."`

	PubliclyViewable bool `short:"p" long:"publicly-viewable" default:"false" description:"If true, anonymous users can view pipelines and public jobs."`

	SessionSigningKey FileFlag `long:"session-signing-key" description:"File containing an RSA private key, used to sign session tokens."`

	ResourceCheckingInterval     time.Duration `long:"resource-checking-interval" default:"1m" description:"Interval on which to check for new versions of resources."`
	OldResourceGracePeriod       time.Duration `long:"old-resource-grace-period" default:"5m" description:"How long to cache the result of a get step after a newer version of the resource is found."`
	ResourceCacheCleanupInterval time.Duration `long:"resource-cache-cleanup-interval" default:"30s" description:"Interval on which to cleanup old caches of resources."`

	ContainerRetention struct {
		SuccessDuration time.Duration `long:"success-duration" default:"5m" description:"The duration to keep a succeeded step's containers before expiring them."`
		FailureDuration time.Duration `long:"failure-duration" default:"1h" description:"The duration to keep a failed step's containers before expiring them."`
	} `group:"Container Retention" namespace:"container-retention"`

	CLIArtifactsDir DirFlag `long:"cli-artifacts-dir" description:"Directory containing downloadable CLI binaries."`

	Developer struct {
		DevelopmentMode bool `short:"d" long:"development-mode"  description:"Lax security rules to make local development easier."`
		Noop            bool `short:"n" long:"noop"              description:"Don't actually do any automatic scheduling or checking."`
	} `group:"Developer Options"`

	Worker struct {
		GardenURL       URLFlag           `long:"garden-url"       description:"A Garden API endpoint to register as a worker."`
		BaggageclaimURL URLFlag           `long:"baggageclaim-url" description:"A Baggageclaim API endpoint to register with the worker."`
		ResourceTypes   map[string]string `long:"resource"         description:"A resource type to advertise for the worker. Can be specified multiple times." value-name:"TYPE:IMAGE"`
	} `group:"Static Worker (optional)" namespace:"worker"`

	BasicAuth struct {
		Username string `long:"username" description:"Username to use for basic auth."`
		Password string `long:"password" description:"Password to use for basic auth."`
	} `group:"Basic Authentication" namespace:"basic-auth"`

	GitHubAuth struct {
		ClientID      string           `long:"client-id"     description:"Application client ID for enabling GitHub OAuth."`
		ClientSecret  string           `long:"client-secret" description:"Application client secret for enabling GitHub OAuth."`
		Organizations []string         `long:"organization"  description:"GitHub organization whose members will have access." value-name:"ORG"`
		Teams         []GitHubTeamFlag `long:"team"          description:"GitHub team whose members will have access." value-name:"ORG/TEAM"`
		Users         []string         `long:"user"          description:"GitHub user to permit access." value-name:"LOGIN"`
		AuthURL       string           `long:"auth-url"      description:"Override default endpoint AuthURL for Github Enterprise."`
		TokenURL      string           `long:"token-url"     description:"Override default endpoint TokenURL for Github Enterprise."`
		APIURL        string           `long:"api-url"       description:"Override default API endpoint URL for Github Enterprise."`
	} `group:"GitHub Authentication" namespace:"github-auth"`

	UAAAuth UAAAuth `group:"UAA Authentication" namespace:"uaa-auth"`

	Metrics struct {
		HostName   string            `long:"metrics-host-name"   description:"Host string to attach to emitted metrics."`
		Tags       []string          `long:"metrics-tag"         description:"Tag to attach to emitted metrics. Can be specified multiple times." value-name:"TAG"`
		Attributes map[string]string `long:"metrics-attribute"   description:"A key-value attribute to attach to emitted metrics. Can be specified multiple times." value-name:"NAME:VALUE"`

		YellerAPIKey      string `long:"yeller-api-key"     description:"Yeller API key. If specified, all errors logged will be emitted."`
		YellerEnvironment string `long:"yeller-environment" description:"Environment to tag on all Yeller events emitted."`

		RiemannHost string `long:"riemann-host"                description:"Riemann server address to emit metrics to."`
		RiemannPort uint16 `long:"riemann-port" default:"5555" description:"Port of the Riemann server to emit metrics to."`
	} `group:"Metrics & Diagnostics"`
}

type UAAAuth struct {
	ClientID     string   `long:"client-id"     description:"Application client ID for enabling UAA OAuth."`
	ClientSecret string   `long:"client-secret" description:"Application client secret for enabling UAA OAuth."`
	AuthURL      string   `long:"auth-url"      description:"UAA AuthURL endpoint."`
	TokenURL     string   `long:"token-url"     description:"UAA TokenURL endpoint."`
	CFSpaces     []string `long:"cf-space"      description:"Space GUID for a CF space whose developers will have access."`
	CFURL        string   `long:"cf-url"        description:"CF API endpoint."`
}

func (auth *UAAAuth) IsConfigured() bool {
	return auth.ClientID != "" ||
		auth.ClientSecret != "" ||
		len(auth.CFSpaces) > 0 ||
		auth.AuthURL != "" ||
		auth.TokenURL != "" ||
		auth.CFURL != ""
}

func (cmd *ATCCommand) Execute(args []string) error {
	runner, err := cmd.Runner(args)
	if err != nil {
		return err
	}

	return <-ifrit.Invoke(sigmon.New(runner)).Wait()
}

func (cmd *ATCCommand) Runner(args []string) (ifrit.Runner, error) {
	err := cmd.validate()
	if err != nil {
		return nil, err
	}

	logger, reconfigurableSink := cmd.constructLogger()

	if cmd.Metrics.RiemannHost != "" {
		cmd.configureMetrics(logger)
	}

	dbConn, err := cmd.constructDBConn(logger)
	if err != nil {
		return nil, err
	}
	listener := pq.NewListener(cmd.PostgresDataSource, time.Second, time.Minute, nil)
	bus := db.NewNotificationsBus(listener, dbConn)

	sqlDB := db.NewSQL(dbConn, bus)
	trackerFactory := resource.TrackerFactory{}
	workerClient := cmd.constructWorkerPool(logger, sqlDB, trackerFactory)

	tracker := resource.NewTracker(workerClient)
	buildDBFactory := db.NewBuildDBFactory(dbConn, bus)
	teamDBFactory := db.NewTeamDBFactory(dbConn, buildDBFactory)
	engine := cmd.constructEngine(workerClient, tracker, teamDBFactory, buildDBFactory)

	radarSchedulerFactory := pipelines.NewRadarSchedulerFactory(
		tracker,
		cmd.ResourceCheckingInterval,
		engine,
		buildDBFactory,
	)

	radarScannerFactory := radar.NewScannerFactory(
		tracker,
		cmd.ResourceCheckingInterval,
		cmd.ExternalURL.String(),
	)

	signingKey, err := cmd.loadOrGenerateSigningKey()
	if err != nil {
		return nil, err
	}

	err = sqlDB.CreateDefaultTeamIfNotExists()
	if err != nil {
		return nil, err
	}

	err = cmd.updateBasicAuthCredentials(teamDBFactory)
	if err != nil {
		return nil, err
	}

	err = cmd.configureOAuthProviders(logger, teamDBFactory)
	if err != nil {
		return nil, err
	}

	drain := make(chan struct{})

	pipelineDBFactory := db.NewPipelineDBFactory(dbConn, bus)

	members := []grouper.Member{
		{"drainer", drainer(drain)},

		{"debug", http_server.New(
			cmd.debugBindAddr(),
			http.DefaultServeMux,
		)},

		{"pipelines", pipelines.SyncRunner{
			Syncer: cmd.constructPipelineSyncer(
				logger.Session("syncer"),
				sqlDB,
				pipelineDBFactory,
				radarSchedulerFactory,
			),
			Interval: 10 * time.Second,
			Clock:    clock.NewClock(),
		}},

		{"builds", builds.TrackerRunner{
			Tracker: builds.NewTracker(
				logger.Session("build-tracker"),
				sqlDB,
				buildDBFactory,
				engine,
			),
			Interval: 10 * time.Second,
			Clock:    clock.NewClock(),
		}},

		{"lostandfound", leaserunner.NewRunner(
			logger.Session("lost-and-found"),
			lostandfound.NewBaggageCollector(
				logger.Session("baggage-collector"),
				workerClient,
				sqlDB,
				pipelineDBFactory,
				buildDBFactory,
				cmd.OldResourceGracePeriod,
				24*time.Hour,
			),
			"baggage-collector",
			sqlDB,
			clock.NewClock(),
			cmd.ResourceCacheCleanupInterval,
		)},

		{"buildreaper", leaserunner.NewRunner(
			logger.Session("build-reaper-runner"),
			buildreaper.NewBuildReaper(
				logger.Session("build-reaper"),
				sqlDB,
				pipelineDBFactory,
				500,
			),
			"build-reaper",
			sqlDB,
			clock.NewClock(),
			30*time.Second,
		)},
	}

	if cmd.Worker.GardenURL.URL() != nil {
		members = cmd.appendStaticWorker(logger, sqlDB, members)
	}

	providerFactory := provider.NewOAuthFactory(
		teamDBFactory,
		cmd.oauthBaseURL(),
		auth.OAuthRoutes,
		auth.OAuthCallback,
	)
	if err != nil {
		return nil, err
	}

	apiHandler, err := cmd.constructAPIHandler(
		logger,
		reconfigurableSink,
		sqlDB,
		teamDBFactory,
		buildDBFactory,
		providerFactory,
		signingKey,
		pipelineDBFactory,
		engine,
		workerClient,
		drain,
		radarSchedulerFactory,
		radarScannerFactory,
	)
	if err != nil {
		return nil, err
	}

	oauthHandler, err := auth.NewOAuthHandler(
		logger,
		providerFactory,
		teamDBFactory,
		signingKey,
	)
	if err != nil {
		return nil, err
	}

	webHandler, err := webhandler.NewHandler(
		logger,
		wrappa.NewWebMetricsWrappa(logger),
		web.NewClientFactory(cmd.internalURL(), cmd.Developer.DevelopmentMode),
	)
	if err != nil {
		return nil, err
	}

	httpHandler := cmd.constructHTTPHandler(
		webHandler,
		apiHandler,
		oauthHandler,
	)

	if cmd.TLSBindPort != 0 {
		httpHandler = cmd.httpsRedirectingHandler(httpHandler)

		var err error
		members, err = cmd.appendTLSMember(webHandler, apiHandler, oauthHandler, members)
		if err != nil {
			return nil, err
		}
	}

	members = append(members, grouper.Member{"web", http_server.New(
		cmd.nonTLSBindAddr(),
		httpHandler,
	)})

	return onReady(grouper.NewParallel(os.Interrupt, members), func() {
		logData := lager.Data{
			"http":  cmd.nonTLSBindAddr(),
			"debug": cmd.debugBindAddr(),
		}

		if cmd.TLSBindPort != 0 {
			logData["https"] = cmd.tlsBindAddr()
		}

		logger.Info("listening", logData)
	}), nil
}

func (cmd *ATCCommand) httpsRedirectingHandler(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET", "HEAD":
			u := url.URL{
				Scheme:   "https",
				Host:     cmd.ExternalURL.URL().Host,
				Path:     r.URL.Path,
				RawQuery: r.URL.RawQuery,
			}

			http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
		default:
			handler.ServeHTTP(w, r)
		}
	})
}

func onReady(runner ifrit.Runner, cb func()) ifrit.Runner {
	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		process := ifrit.Background(runner)

		subExited := process.Wait()
		subReady := process.Ready()

		for {
			select {
			case <-subReady:
				cb()
				subReady = nil
			case err := <-subExited:
				return err
			case sig := <-signals:
				process.Signal(sig)
			}
		}
	})
}

func (cmd *ATCCommand) oauthBaseURL() string {
	baseURL := cmd.OAuthBaseURL.String()
	if baseURL == "" {
		baseURL = cmd.ExternalURL.String()
	}
	return baseURL
}

func (cmd *ATCCommand) authConfigured() bool {
	return cmd.basicAuthConfigured() || cmd.gitHubAuthConfigured() || cmd.UAAAuth.IsConfigured()
}

func (cmd *ATCCommand) basicAuthConfigured() bool {
	return cmd.BasicAuth.Username != "" || cmd.BasicAuth.Password != ""
}

func (cmd *ATCCommand) gitHubAuthConfigured() bool {
	return len(cmd.GitHubAuth.Organizations) > 0 ||
		len(cmd.GitHubAuth.Teams) > 0 ||
		len(cmd.GitHubAuth.Users) > 0
}

func (cmd *ATCCommand) validate() error {
	var errs *multierror.Error

	if !cmd.authConfigured() && !cmd.Developer.DevelopmentMode {
		errs = multierror.Append(
			errs,
			errors.New("must configure basic auth, OAuth, or turn on development mode"),
		)
	}

	if cmd.gitHubAuthConfigured() {
		if cmd.ExternalURL.URL() == nil {
			errs = multierror.Append(
				errs,
				errors.New("must specify --external-url to use OAuth"),
			)
		}

		if cmd.GitHubAuth.ClientID == "" || cmd.GitHubAuth.ClientSecret == "" {
			errs = multierror.Append(
				errs,
				errors.New("must specify --github-auth-client-id and --github-auth-client-secret to use GitHub OAuth"),
			)
		}
	}

	if cmd.basicAuthConfigured() {
		if cmd.BasicAuth.Username == "" {
			errs = multierror.Append(
				errs,
				errors.New("must specify --basic-auth-username to use basic auth"),
			)
		}
		if cmd.BasicAuth.Password == "" {
			errs = multierror.Append(
				errs,
				errors.New("must specify --basic-auth-password to use basic auth"),
			)
		}
	}

	if cmd.UAAAuth.IsConfigured() {
		if cmd.UAAAuth.ClientID == "" || cmd.UAAAuth.ClientSecret == "" {
			errs = multierror.Append(
				errs,
				errors.New("must specify --uaa-auth-client-id and --uaa-auth-client-secret to use UAA OAuth"),
			)
		}
		if len(cmd.UAAAuth.CFSpaces) == 0 {
			errs = multierror.Append(
				errs,
				errors.New("must specify --uaa-auth-cf-space to use UAA OAuth"),
			)
		}
		if cmd.UAAAuth.AuthURL == "" || cmd.UAAAuth.TokenURL == "" || cmd.UAAAuth.CFURL == "" {
			errs = multierror.Append(
				errs,
				errors.New("must specify --uaa-auth-auth-url, --uaa-auth-token-url and --uaa-auth-cf-url to use UAA OAuth"),
			)
		}
	}

	tlsFlagCount := 0
	if cmd.TLSBindPort != 0 {
		tlsFlagCount++
	}
	if cmd.TLSCert != "" {
		tlsFlagCount++
	}
	if cmd.TLSKey != "" {
		tlsFlagCount++
	}

	if tlsFlagCount == 3 {
		if cmd.ExternalURL.URL().Scheme != "https" {
			errs = multierror.Append(
				errs,
				errors.New("must specify HTTPS external-url to use TLS"),
			)
		}
	} else if tlsFlagCount != 0 {
		errs = multierror.Append(
			errs,
			errors.New("must specify --tls-bind-port, --tls-cert, --tls-key to use TLS"),
		)
	}

	return errs.ErrorOrNil()
}

func (cmd *ATCCommand) nonTLSBindAddr() string {
	return fmt.Sprintf("%s:%d", cmd.BindIP, cmd.BindPort)
}

func (cmd *ATCCommand) tlsBindAddr() string {
	return fmt.Sprintf("%s:%d", cmd.BindIP, cmd.TLSBindPort)
}

func (cmd *ATCCommand) internalURL() string {
	if cmd.TLSBindPort != 0 {
		if strings.Contains(cmd.ExternalURL.String(), ":") {
			return cmd.ExternalURL.String()
		} else {
			return fmt.Sprintf("%s:%d", cmd.ExternalURL, cmd.TLSBindPort)
		}
	} else {
		return fmt.Sprintf("http://127.0.0.1:%d", cmd.BindPort)
	}
}

func (cmd *ATCCommand) debugBindAddr() string {
	return fmt.Sprintf("%s:%d", cmd.DebugBindIP, cmd.DebugBindPort)
}

func (cmd *ATCCommand) constructLogger() (lager.Logger, *lager.ReconfigurableSink) {
	logger := lager.NewLogger("atc")

	logLevel := lager.INFO
	if cmd.Developer.DevelopmentMode {
		logLevel = lager.DEBUG
	}

	reconfigurableSink := lager.NewReconfigurableSink(lager.NewWriterSink(os.Stdout, lager.DEBUG), logLevel)
	logger.RegisterSink(reconfigurableSink)

	if cmd.Metrics.YellerAPIKey != "" {
		yellerSink := zest.NewYellerSink(cmd.Metrics.YellerAPIKey, cmd.Metrics.YellerEnvironment)
		logger.RegisterSink(yellerSink)
	}

	return logger, reconfigurableSink
}

func (cmd *ATCCommand) configureMetrics(logger lager.Logger) {
	host := cmd.Metrics.HostName
	if host == "" {
		host, _ = os.Hostname()
	}

	metric.Initialize(
		logger.Session("metrics"),
		fmt.Sprintf("%s:%d", cmd.Metrics.RiemannHost, cmd.Metrics.RiemannPort),
		host,
		cmd.Metrics.Tags,
		cmd.Metrics.Attributes,
	)
}

func (cmd *ATCCommand) constructDBConn(logger lager.Logger) (db.Conn, error) {
	driverName := "connection-counting"
	metric.SetupConnectionCountingDriver("postgres", cmd.PostgresDataSource, driverName)

	dbConn, err := migrations.LockDBAndMigrate(logger.Session("db.migrations"), driverName, cmd.PostgresDataSource)
	if err != nil {
		return nil, fmt.Errorf("failed to migrate database: %s", err)
	}

	explainDBConn := db.Explain(logger, dbConn, clock.NewClock(), 500*time.Millisecond)
	countingDBConn := metric.CountQueries(explainDBConn)

	return countingDBConn, nil
}

func (cmd *ATCCommand) constructWorkerPool(logger lager.Logger, sqlDB *db.SQLDB, trackerFactory resource.TrackerFactory) worker.Client {
	return worker.NewPool(
		worker.NewDBWorkerProvider(
			logger,
			sqlDB,
			keepaliveDialer,
			transport.ExponentialRetryPolicy{
				Timeout: 5 * time.Minute,
			},
			image.NewFetcher(trackerFactory),
		),
	)
}

func (cmd *ATCCommand) loadOrGenerateSigningKey() (*rsa.PrivateKey, error) {
	var signingKey *rsa.PrivateKey

	if cmd.SessionSigningKey == "" {
		generatedKey, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, fmt.Errorf("failed to generate session signing key: %s", err)
		}

		signingKey = generatedKey
	} else {
		rsaKeyBlob, err := ioutil.ReadFile(string(cmd.SessionSigningKey))
		if err != nil {
			return nil, fmt.Errorf("failed to read session signing key file: %s", err)
		}

		signingKey, err = jwt.ParseRSAPrivateKeyFromPEM(rsaKeyBlob)
		if err != nil {
			return nil, fmt.Errorf("failed to parse session signing key as RSA: %s", err)
		}
	}

	return signingKey, nil
}

func (cmd *ATCCommand) configureOAuthProviders(logger lager.Logger, teamDBFactory db.TeamDBFactory) error {
	var err error
	team := db.Team{
		Name: atc.DefaultTeamName,
	}
	teamDB := teamDBFactory.GetTeamDB(team.Name)

	var gitHubAuth *db.GitHubAuth
	if len(cmd.GitHubAuth.Organizations) > 0 ||
		len(cmd.GitHubAuth.Teams) > 0 ||
		len(cmd.GitHubAuth.Users) > 0 {

		gitHubTeams := []db.GitHubTeam{}
		for _, gitHubTeam := range cmd.GitHubAuth.Teams {
			gitHubTeams = append(gitHubTeams, db.GitHubTeam{
				TeamName:         gitHubTeam.TeamName,
				OrganizationName: gitHubTeam.OrganizationName,
			})
		}

		gitHubAuth = &db.GitHubAuth{
			ClientID:      cmd.GitHubAuth.ClientID,
			ClientSecret:  cmd.GitHubAuth.ClientSecret,
			Organizations: cmd.GitHubAuth.Organizations,
			Teams:         gitHubTeams,
			Users:         cmd.GitHubAuth.Users,
			AuthURL:       cmd.GitHubAuth.AuthURL,
			TokenURL:      cmd.GitHubAuth.TokenURL,
			APIURL:        cmd.GitHubAuth.APIURL,
		}
	}

	_, err = teamDB.UpdateGitHubAuth(gitHubAuth)
	if err != nil {
		return err
	}

	var uaaAuth *db.UAAAuth
	if cmd.UAAAuth.IsConfigured() {
		uaaAuth = &db.UAAAuth{
			ClientID:     cmd.UAAAuth.ClientID,
			ClientSecret: cmd.UAAAuth.ClientSecret,
			CFSpaces:     cmd.UAAAuth.CFSpaces,
			AuthURL:      cmd.UAAAuth.AuthURL,
			TokenURL:     cmd.UAAAuth.TokenURL,
			CFURL:        cmd.UAAAuth.CFURL,
		}
	}

	_, err = teamDB.UpdateUAAAuth(uaaAuth)
	if err != nil {
		return err
	}

	return nil
}

func (cmd *ATCCommand) constructValidator(signingKey *rsa.PrivateKey, teamDBFactory db.TeamDBFactory) auth.Validator {
	if !cmd.authConfigured() {
		return auth.NoopValidator{}
	}

	jwtValidator := auth.JWTValidator{
		PublicKey: &signingKey.PublicKey,
	}

	var validator auth.Validator
	if cmd.BasicAuth.Username != "" && cmd.BasicAuth.Password != "" {
		validator = auth.ValidatorBasket{
			auth.BasicAuthValidator{
				TeamDBFactory: teamDBFactory,
			},
			jwtValidator,
		}
	} else {
		validator = jwtValidator
	}

	return validator
}

func (cmd *ATCCommand) updateBasicAuthCredentials(teamDBFactory db.TeamDBFactory) error {
	var basicAuth *db.BasicAuth
	if cmd.BasicAuth.Username != "" || cmd.BasicAuth.Password != "" {
		basicAuth = &db.BasicAuth{
			BasicAuthUsername: cmd.BasicAuth.Username,
			BasicAuthPassword: cmd.BasicAuth.Password,
		}
	}
	teamDB := teamDBFactory.GetTeamDB(atc.DefaultTeamName)
	_, err := teamDB.UpdateBasicAuth(basicAuth)
	return err
}

func (cmd *ATCCommand) constructEngine(
	workerClient worker.Client,
	tracker resource.Tracker,
	teamDBFactory db.TeamDBFactory,
	buildDBFactory db.BuildDBFactory,
) engine.Engine {
	gardenFactory := exec.NewGardenFactory(
		workerClient,
		tracker,
		cmd.ContainerRetention.SuccessDuration,
		cmd.ContainerRetention.FailureDuration,
	)

	execV2Engine := engine.NewExecEngine(
		gardenFactory,
		engine.NewBuildDelegateFactory(),
		teamDBFactory,
		cmd.ExternalURL.String(),
	)

	execV1Engine := engine.NewExecV1DummyEngine()

	return engine.NewDBEngine(engine.Engines{execV2Engine, execV1Engine}, buildDBFactory)
}

func (cmd *ATCCommand) constructHTTPHandler(
	webHandler http.Handler,
	apiHandler http.Handler,
	oauthHandler http.Handler,
) http.Handler {
	webMux := http.NewServeMux()
	webMux.Handle("/api/v1/", apiHandler)
	webMux.Handle("/auth/", oauthHandler)
	webMux.Handle("/", webHandler)

	var httpHandler http.Handler

	httpHandler = webMux

	// proxy Authorization header to/from auth cookie,
	// to support auth from JS (EventSource) and custom JWT auth
	httpHandler = auth.CookieSetHandler{
		Handler: httpHandler,
	}

	// don't leak gorilla context per-request
	httpHandler = context.ClearHandler(httpHandler)

	return httpHandler
}

func (cmd *ATCCommand) constructAPIHandler(
	logger lager.Logger,
	reconfigurableSink *lager.ReconfigurableSink,
	sqlDB *db.SQLDB,
	teamDBFactory db.TeamDBFactory,
	buildDBFactory db.BuildDBFactory,
	providerFactory provider.OAuthFactory,
	signingKey *rsa.PrivateKey,
	pipelineDBFactory db.PipelineDBFactory,
	engine engine.Engine,
	workerClient worker.Client,
	drain <-chan struct{},
	radarSchedulerFactory pipelines.RadarSchedulerFactory,
	radarScannerFactory radar.ScannerFactory,
) (http.Handler, error) {
	apiWrapper := wrappa.MultiWrappa{
		wrappa.NewAPIAuthWrappa(
			cmd.PubliclyViewable,
			cmd.constructValidator(signingKey, teamDBFactory),
			auth.JWTReader{PublicKey: &signingKey.PublicKey},
		),
		wrappa.NewAPIMetricsWrappa(logger),
		wrappa.NewConcourseVersionWrappa(Version),
	}

	return api.NewHandler(
		logger,
		cmd.ExternalURL.String(),
		apiWrapper,

		auth.NewTokenGenerator(signingKey),
		providerFactory,
		cmd.oauthBaseURL(),

		pipelineDBFactory,
		teamDBFactory,
		buildDBFactory,

		sqlDB, // teamserver.TeamsDB
		sqlDB, // workerserver.WorkerDB
		sqlDB, // containerserver.ContainerDB
		sqlDB, // volumeserver.VolumesDB
		sqlDB, // pipes.PipeDB

		config.ValidateConfig,
		cmd.PeerURL.String(),
		buildserver.NewEventHandler,
		drain,

		engine,
		workerClient,
		radarSchedulerFactory,
		radarScannerFactory,

		reconfigurableSink,

		cmd.CLIArtifactsDir.Path(),
		Version,
	)
}

func (cmd *ATCCommand) constructPipelineSyncer(
	logger lager.Logger,
	sqlDB *db.SQLDB,
	pipelineDBFactory db.PipelineDBFactory,
	radarSchedulerFactory pipelines.RadarSchedulerFactory,
) *pipelines.Syncer {
	return pipelines.NewSyncer(
		logger,
		sqlDB,
		pipelineDBFactory,
		func(pipelineDB db.PipelineDB) ifrit.Runner {
			return grouper.NewParallel(os.Interrupt, grouper.Members{
				{
					pipelineDB.ScopedName("radar"),
					radar.NewRunner(
						logger.Session(pipelineDB.ScopedName("radar")),
						cmd.Developer.Noop,
						radarSchedulerFactory.BuildScanRunnerFactory(pipelineDB, cmd.ExternalURL.String()),
						pipelineDB,
						1*time.Minute,
					),
				},
				{
					pipelineDB.ScopedName("scheduler"),
					&scheduler.Runner{
						Logger: logger.Session(pipelineDB.ScopedName("scheduler")),

						DB: pipelineDB,

						Scheduler: radarSchedulerFactory.BuildScheduler(pipelineDB, cmd.ExternalURL.String()),

						Noop: cmd.Developer.Noop,

						Interval: 10 * time.Second,
					},
				},
			})
		},
	)
}

func (cmd *ATCCommand) appendStaticWorker(
	logger lager.Logger,
	sqlDB *db.SQLDB,
	members []grouper.Member,
) []grouper.Member {
	var resourceTypes []atc.WorkerResourceType
	for t, resourcePath := range cmd.Worker.ResourceTypes {
		resourceTypes = append(resourceTypes, atc.WorkerResourceType{
			Type:  t,
			Image: resourcePath,
		})
	}

	return append(members,
		grouper.Member{
			Name: "static-worker",
			Runner: worker.NewHardcoded(
				logger,
				sqlDB,
				clock.NewClock(),
				cmd.Worker.GardenURL.URL().Host,
				cmd.Worker.BaggageclaimURL.String(),
				resourceTypes,
			),
		},
	)
}

func (cmd *ATCCommand) appendTLSMember(
	webHandler http.Handler,
	apiHandler http.Handler,
	oauthHandler http.Handler,
	members []grouper.Member,
) ([]grouper.Member, error) {
	cert, err := tls.LoadX509KeyPair(string(cmd.TLSCert), string(cmd.TLSKey))
	if err != nil {
		return []grouper.Member{}, err
	}

	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}
	members = append(members, grouper.Member{"web-tls", http_server.NewTLSServer(
		cmd.tlsBindAddr(),
		cmd.constructHTTPHandler(
			webHandler,
			apiHandler,
			oauthHandler,
		),
		tlsConfig,
	)})

	return members, nil
}
