package app

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
)

// Config type represents the settings fields
type Config struct {
	// KubeContext is the kube context you want Helmsman to use or create
	KubeContext string `yaml:"kubeContext,omitempty"`
	// Username to be used for kubectl credentials
	Username string `yaml:"username,omitempty"`
	// Password to be used for kubectl credentials
	Password string `yaml:"password,omitempty"`
	// ClusterURI is the URI for your cluster API or the name of an environment variable (starting with `$`) containing the URI
	ClusterURI string `yaml:"clusterURI,omitempty"`
	// ServiceAccount to be used for tiller (deprecated)
	ServiceAccount string `yaml:"serviceAccount,omitempty"`
	// StorageBackend indicates the storage backened used by helm, defaults to secret
	StorageBackend string `yaml:"storageBackend,omitempty"`
	// SlackWebhook is the slack webhook URL for slack notifications
	SlackWebhook string `yaml:"slackWebhook,omitempty"`
	// MSTeamsWebhook is the Microsoft teams webhook URL for teams notifications
	MSTeamsWebhook string `yaml:"msTeamsWebhook,omitempty"`
	// ReverseDelete indicates if the applications should be deleted in reverse orderin relation to the installation order
	ReverseDelete bool `yaml:"reverseDelete,omitempty"`
	// BearerToken indicates whether you want helmsman to connect to the cluster using a bearer token
	BearerToken bool `yaml:"bearerToken,omitempty"`
	// BearerTokenPath allows specifying a custom path for the token
	BearerTokenPath string `yaml:"bearerTokenPath,omitempty"`
	// NamespaceLabelsAuthoritativei indicates whether helmsman should remove namespace labels that are not in the DSF
	NamespaceLabelsAuthoritative bool `yaml:"namespaceLabelsAuthoritative,omitempty"`
	// EyamlEnabled indicates whether eyaml is used for encrypted files
	EyamlEnabled bool `yaml:"eyamlEnabled,omitempty"`
	// EyamlPrivateKeyPath is the path to the eyaml private key
	EyamlPrivateKeyPath string `yaml:"eyamlPrivateKeyPath,omitempty"`
	// EyamlPublicKeyPath is the path to the eyaml public key
	EyamlPublicKeyPath string `yaml:"eyamlPublicKeyPath,omitempty"`
	// GlobalHooks is a set of global lifecycle hooks
	GlobalHooks map[string]interface{} `yaml:"globalHooks,omitempty"`
	// GlobalMaxHistory sets the global max number of historical release revisions to keep
	GlobalMaxHistory int `yaml:"globalMaxHistory,omitempty"`
	// SkipIgnoredApps if set to true, ignored apps will not be considered in the plan
	SkipIgnoredApps bool `yaml:"skipIgnoredApps,omitempty"`
	// SkipPendingApps is set to true,apps in a pending state will be ignored
	SkipPendingApps bool `yaml:"skipPendingApps,omitempty"`
}

// State type represents the desired State of applications on a k8s cluster.
type State struct {
	// Metadata for human reader of the desired state file
	Metadata map[string]string `yaml:"metadata,omitempty"`
	// Certificates are used to connect kubectl to a cluster
	Certificates map[string]string `yaml:"certificates,omitempty"`
	// Settings for configuring helmsman
	Settings Config `yaml:"settings,omitempty"`
	// Context defines an helmsman scope
	Context string `yaml:"context,omitempty"`
	// HelmRepos from where to find the application helm charts
	HelmRepos map[string]string `yaml:"helmRepos,omitempty"`
	// PreconfiguredHelmRepos is a list of helm repos that are configured outside of the DSF
	PreconfiguredHelmRepos []string `yaml:"preconfiguredHelmRepos,omitempty"`
	// Namespaces where helmsman will deploy applications
	Namespaces map[string]*Namespace `yaml:"namespaces"`
	// Apps holds the configuration for each helm release managed by helmsman
	Apps map[string]*Release `yaml:"apps"`
	// AppsTemplates allow defining YAML objects thatcan be used as a reference with YAML anchors to keep the configuration DRY
	AppsTemplates map[string]*Release `yaml:"appsTemplates,omitempty"`
	targetMap     map[string]bool
	chartInfo     map[string]map[string]*ChartInfo
}

func (s *State) init() {
	s.setDefaults()
	s.initializeNamespaces()
}

func (s *State) setDefaults() {
	if s.Settings.StorageBackend != "" {
		os.Setenv("HELM_DRIVER", s.Settings.StorageBackend)
	} else {
		// set default storage background to secret if not set by user
		s.Settings.StorageBackend = "secret"
	}

	// if there is no user-defined context name in the DSF(s), use the default context name
	if s.Context == "" {
		s.Context = defaultContextName
	}

	for name, r := range s.Apps {
		// Default app.Name to state name when unset
		if r.Name == "" {
			r.Name = name
		}
		// inherit globalHooks if local ones are not set
		r.inheritHooks(s)
		r.inheritMaxHistory(s)
	}
}

func (s *State) initializeNamespaces() {
	for nsName, ns := range s.Namespaces {
		if ns == nil {
			s.Namespaces[nsName] = &Namespace{}
		}
	}
}

// validate validates that the values specified in the desired state are valid according to the desired state spec.
// check https://github.com/Praqma/helmsman/blob/master/docs/desired_state_specification.md for the detailed specification
func (s *State) validate() error {
	// apps
	if s.Apps == nil {
		log.Info("No apps specified. Nothing to be executed.")
		os.Exit(0)
	}

	// settings
	// use reflect.DeepEqual to compare Settings are empty, since it contains a map
	if (reflect.DeepEqual(s.Settings, Config{}) || s.Settings.KubeContext == "") && !getKubeContext() {
		return errors.New("settings validation failed -- you have not defined a " +
			"kubeContext to use. Either define it in the desired state file or pass a kubeconfig with --kubeconfig to use an existing context")
	}

	_, caClient := s.Certificates["caClient"]

	if s.Settings.ClusterURI != "" {
		if _, err := url.ParseRequestURI(s.Settings.ClusterURI); err != nil {
			return errors.New("settings validation failed -- clusterURI must have a valid URL set in an env variable or passed directly. Either the env var is missing/empty or the URL is invalid")
		}
		if s.Settings.KubeContext == "" {
			return errors.New("settings validation failed -- KubeContext needs to be provided in the settings stanza")
		}
		if !s.Settings.BearerToken && !caClient {
			if s.Settings.Username == "" {
				return errors.New("settings validation failed -- username needs to be provided in the settings stanza")
			}
			if s.Settings.Password == "" {
				return errors.New("settings validation failed -- password needs to be provided (directly or from env var) in the settings stanza")
			}
		}
		if s.Settings.BearerToken && s.Settings.BearerTokenPath != "" {
			if _, err := os.Stat(s.Settings.BearerTokenPath); err != nil {
				return errors.New("settings validation failed -- bearer token path " + s.Settings.BearerTokenPath + " is not found. The path has to be relative to the desired state file")
			}
		}
	} else if s.Settings.BearerToken && s.Settings.ClusterURI == "" {
		return errors.New("settings validation failed -- bearer token is enabled but no cluster URI provided")
	}

	// lifecycle hooks validation
	if len(s.Settings.GlobalHooks) != 0 {
		if err := validateHooks(s.Settings.GlobalHooks); err != nil {
			return err
		}
	}

	// slack webhook validation (if provided)
	if s.Settings.SlackWebhook != "" {
		if _, err := url.ParseRequestURI(s.Settings.SlackWebhook); err != nil {
			return errors.New("settings validation failed -- slackWebhook must be a valid URL")
		}
	}

	// ms teams webhook validation (if provided)
	if s.Settings.MSTeamsWebhook != "" {
		if _, err := url.ParseRequestURI(s.Settings.MSTeamsWebhook); err != nil {
			return errors.New("settings validation failed -- msTeamsWebhook must be a valid URL")
		}
	}

	// certificates
	if s.Certificates != nil && len(s.Certificates) != 0 {

		for key, value := range s.Certificates {
			if !isValidCert(value) {
				return errors.New("certifications validation failed -- [ " + key + " ] must be a valid S3, GCS, AZ bucket/container URL or a valid relative file path")
			}
		}

		_, caCrt := s.Certificates["caCrt"]
		_, caKey := s.Certificates["caKey"]

		if s.Settings.ClusterURI != "" && !s.Settings.BearerToken {
			if !caCrt || !caKey {
				return errors.New("certificates validation failed -- connection to cluster is required " +
					"but no cert/key was given. Please add [caCrt] and [caKey] under Certifications. You might also need to provide [clientCrt]")
			}
		} else if s.Settings.ClusterURI != "" && s.Settings.BearerToken {
			if !caCrt {
				return errors.New("certificates validation failed -- cluster connection with bearer token is enabled but " +
					"[caCrt] is missing. Please provide [caCrt] in the Certifications stanza")
			}
		}

	} else if s.Settings.ClusterURI != "" {
		return errors.New("certificates validation failed -- kube context setup is required but no certificates stanza provided")
	}

	if (s.Settings.EyamlPrivateKeyPath != "" && s.Settings.EyamlPublicKeyPath == "") || (s.Settings.EyamlPrivateKeyPath == "" && s.Settings.EyamlPublicKeyPath != "") {
		return errors.New("both EyamlPrivateKeyPath and EyamlPublicKeyPath are required")
	}

	// namespaces
	if flags.nsOverride == "" {
		if s.Namespaces == nil || len(s.Namespaces) == 0 {
			return errors.New("namespaces validation failed -- at least one namespace is required")
		}
	} else {
		log.Info("ns-override is used to override all namespaces with [ " + flags.nsOverride + " ] Skipping defined namespaces validation.")
	}

	// repos
	for k, v := range s.HelmRepos {
		_, err := url.ParseRequestURI(v)
		if err != nil {
			return errors.New("repos validation failed -- repo [" + k + " ] " +
				"must have a valid URL")
		}
	}

	names := make(map[string]map[string]bool)
	for appLabel, r := range s.Apps {
		if err := r.validate(appLabel, names, s); err != nil {
			return fmt.Errorf("apps validation failed -- for app ["+appLabel+" ]. %w", err)
		}
	}

	return nil
}

// getReleaseChartsInfo retrieves valid chart information.
// Valid charts are the ones that can be found in the defined repos.
func (s *State) getReleaseChartsInfo() error {
	var fail bool
	wg := sync.WaitGroup{}
	mutex := sync.Mutex{}
	sem := make(chan struct{}, resourcePool)
	chartErrors := make(chan error, len(s.Apps))

	charts := make(map[string]map[string][]string)
	s.chartInfo = make(map[string]map[string]*ChartInfo)

	for app, r := range s.Apps {
		if !r.isConsideredToRun() {
			continue
		}

		if charts[r.Chart] == nil {
			charts[r.Chart] = make(map[string][]string)
		}

		if charts[r.Chart][r.Version] == nil {
			charts[r.Chart][r.Version] = make([]string, 0)
		}

		if s.chartInfo[r.Chart] == nil {
			s.chartInfo[r.Chart] = make(map[string]*ChartInfo)
		}

		charts[r.Chart][r.Version] = append(charts[r.Chart][r.Version], app)
	}

	for chart, versions := range charts {
		for version, apps := range versions {
			concattedApps := strings.Join(apps, ", ")

			sem <- struct{}{}
			wg.Add(1)
			go func(apps, chart, version string) {
				defer func() {
					wg.Done()
					<-sem
				}()

				info, err := getChartInfo(chart, version)
				if err != nil {
					chartErrors <- err
				} else {
					log.Verbose(fmt.Sprintf("Extracted chart information from chart [ %s ] with version [ %s ]: %s %s", chart, version, info.Name, info.Version))
					mutex.Lock()
					s.chartInfo[chart][version] = info
					mutex.Unlock()
				}

				// validateChart(concattedApps, chart, version, chartErrors)
			}(concattedApps, chart, version)
		}
	}

	wg.Wait()
	close(chartErrors)
	for err := range chartErrors {
		if err != nil {
			fail = true
			log.Error(err.Error())
		}
	}
	if fail {
		return errors.New("chart validation failed")
	}
	return nil
}

// isNamespaceDefined checks if a given namespace is defined in the namespaces section of the desired state file
func (s *State) isNamespaceDefined(ns string) bool {
	_, ok := s.Namespaces[ns]
	return ok
}

// overrideAppsNamespace replaces all apps namespaces with one specific namespace
func (s *State) overrideAppsNamespace(newNs string) {
	log.Info("Overriding apps namespaces with [ " + newNs + " ] ...")
	for _, r := range s.Apps {
		r.overrideNamespace(newNs)
	}
}

// disable Apps defined as excluded by either their name or their group
// then get only those Apps that exist in TargetMap
func (s *State) disableApps(groups, targets, groupsExcluded, targetsExcluded []string) {
excludeAppsLoop:
	for _, app := range s.Apps {
		for _, groupExcluded := range groupsExcluded {
			if app.Group == groupExcluded {
				app.Disable()
				continue excludeAppsLoop
			}
		}
		for _, targetExcluded := range targetsExcluded {
			if app.Name == targetExcluded {
				app.Disable()
				continue excludeAppsLoop
			}
		}
	}
	if s.targetMap == nil {
		s.targetMap = make(map[string]bool)
	}
	if len(targets) == 0 && len(groups) == 0 {
		return
	}
	for _, t := range targets {
		s.targetMap[t] = true
	}
	groupMap := make(map[string]struct{})
	namespaces := make(map[string]struct{})
	for _, g := range groups {
		groupMap[g] = struct{}{}
	}
	for _, app := range s.Apps {
		if _, ok := s.targetMap[app.Name]; ok {
			namespaces[app.Namespace] = struct{}{}
			continue
		}
		if _, ok := groupMap[app.Group]; ok {
			s.targetMap[app.Name] = true
			namespaces[app.Namespace] = struct{}{}
		} else {
			app.Disable()
		}
	}

	if s.Namespaces == nil || len(s.Namespaces) == 0 {
		return
	}
	for nsName, ns := range s.Namespaces {
		if _, ok := namespaces[nsName]; !ok {
			ns.Disable()
		}
	}
}

// updateContextLabels applies Helmsman labels including overriding any previously-set context with the one found in the DSF
func (s *State) updateContextLabels() {
	for _, r := range s.Apps {
		if r.isConsideredToRun() {
			log.Info("Updating context and reapplying Helmsman labels for release [ " + r.Name + " ]")
			r.mark(s.Settings.StorageBackend)
		} else {
			log.Warning(r.Name + " is not in the target group and therefore context and labels are not changed.")
		}
	}
}

// print prints the desired state
func (s *State) print() {
	fmt.Println("\nMetadata: ")
	fmt.Println("--------- ")
	printMap(s.Metadata, 0)
	fmt.Println("\nContext: ")
	fmt.Println("--------- ")
	fmt.Println(s.Context)
	fmt.Println("\nCertificates: ")
	fmt.Println("--------- ")
	printMap(s.Certificates, 0)
	fmt.Println("\nSettings: ")
	fmt.Println("--------- ")
	fmt.Printf("%+v\n", s.Settings)
	fmt.Println("\nNamespaces: ")
	fmt.Println("------------- ")
	printNamespacesMap(s.Namespaces)
	fmt.Println("\nRepositories: ")
	fmt.Println("------------- ")
	printMap(s.HelmRepos, 0)
	fmt.Println("\nApplications: ")
	fmt.Println("--------------- ")
	for _, r := range s.Apps {
		r.print()
	}
	fmt.Println("\nTargets: ")
	fmt.Println("--------------- ")
	for t := range s.targetMap {
		fmt.Println(t)
	}
}
