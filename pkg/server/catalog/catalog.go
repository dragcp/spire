package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/andres-erbsen/clock"
	"github.com/sirupsen/logrus"
	"github.com/spiffe/spire/pkg/common/catalog"
	common_services "github.com/spiffe/spire/pkg/common/plugin/hostservices"
	"github.com/spiffe/spire/pkg/common/telemetry"
	datastore_telemetry "github.com/spiffe/spire/pkg/common/telemetry/server/datastore"
	keymanager_telemetry "github.com/spiffe/spire/pkg/common/telemetry/server/keymanager"
	"github.com/spiffe/spire/pkg/server/cache/dscache"
	"github.com/spiffe/spire/pkg/server/plugin/datastore"
	ds_sql "github.com/spiffe/spire/pkg/server/plugin/datastore/sql"
	"github.com/spiffe/spire/pkg/server/plugin/hostservices"
	"github.com/spiffe/spire/pkg/server/plugin/keymanager"
	km_disk "github.com/spiffe/spire/pkg/server/plugin/keymanager/disk"
	km_memory "github.com/spiffe/spire/pkg/server/plugin/keymanager/memory"
	"github.com/spiffe/spire/pkg/server/plugin/nodeattestor"
	na_aws_iid "github.com/spiffe/spire/pkg/server/plugin/nodeattestor/aws"
	na_azure_msi "github.com/spiffe/spire/pkg/server/plugin/nodeattestor/azure"
	na_gcp_iit "github.com/spiffe/spire/pkg/server/plugin/nodeattestor/gcp"
	na_join_token "github.com/spiffe/spire/pkg/server/plugin/nodeattestor/jointoken"
	na_k8s_psat "github.com/spiffe/spire/pkg/server/plugin/nodeattestor/k8s/psat"
	na_k8s_sat "github.com/spiffe/spire/pkg/server/plugin/nodeattestor/k8s/sat"
	na_sshpop "github.com/spiffe/spire/pkg/server/plugin/nodeattestor/sshpop"
	na_x509pop "github.com/spiffe/spire/pkg/server/plugin/nodeattestor/x509pop"
	"github.com/spiffe/spire/pkg/server/plugin/noderesolver"
	nr_aws_iid "github.com/spiffe/spire/pkg/server/plugin/noderesolver/aws"
	nr_azure_msi "github.com/spiffe/spire/pkg/server/plugin/noderesolver/azure"
	nr_noop "github.com/spiffe/spire/pkg/server/plugin/noderesolver/noop"
	"github.com/spiffe/spire/pkg/server/plugin/notifier"
	no_gcs_bundle "github.com/spiffe/spire/pkg/server/plugin/notifier/gcsbundle"
	no_k8sbundle "github.com/spiffe/spire/pkg/server/plugin/notifier/k8sbundle"
	"github.com/spiffe/spire/pkg/server/plugin/upstreamauthority"
	up_awspca "github.com/spiffe/spire/pkg/server/plugin/upstreamauthority/awspca"
	up_awssecret "github.com/spiffe/spire/pkg/server/plugin/upstreamauthority/awssecret"
	up_disk "github.com/spiffe/spire/pkg/server/plugin/upstreamauthority/disk"
	up_spire "github.com/spiffe/spire/pkg/server/plugin/upstreamauthority/spire"
	up_vault "github.com/spiffe/spire/pkg/server/plugin/upstreamauthority/vault"
	keymanagerv0 "github.com/spiffe/spire/proto/spire/plugin/server/keymanager/v0"
	nodeattestorv0 "github.com/spiffe/spire/proto/spire/plugin/server/nodeattestor/v0"
	noderesolverv0 "github.com/spiffe/spire/proto/spire/plugin/server/noderesolver/v0"
	notifierv0 "github.com/spiffe/spire/proto/spire/plugin/server/notifier/v0"
	upstreamauthorityv0 "github.com/spiffe/spire/proto/spire/plugin/server/upstreamauthority/v0"
)

var (
	builtIns = []catalog.Plugin{
		// NodeAttestors
		na_aws_iid.BuiltIn(),
		na_gcp_iit.BuiltIn(),
		na_x509pop.BuiltIn(),
		na_sshpop.BuiltIn(),
		na_azure_msi.BuiltIn(),
		na_k8s_sat.BuiltIn(),
		na_k8s_psat.BuiltIn(),
		na_join_token.BuiltIn(),
		// NodeResolvers
		nr_noop.BuiltIn(),
		nr_aws_iid.BuiltIn(),
		nr_azure_msi.BuiltIn(),
		// UpstreamAuthorities
		up_awspca.BuiltIn(),
		up_awssecret.BuiltIn(),
		up_spire.BuiltIn(),
		up_disk.BuiltIn(),
		up_vault.BuiltIn(),
		// KeyManagers
		km_disk.BuiltIn(),
		km_memory.BuiltIn(),
		// Notifiers
		no_k8sbundle.BuiltIn(),
		no_gcs_bundle.BuiltIn(),
	}
)

type Catalog interface {
	GetDataStore() datastore.DataStore
	GetNodeAttestorNamed(name string) (nodeattestor.NodeAttestor, bool)
	GetNodeResolverNamed(name string) (noderesolver.NodeResolver, bool)
	GetKeyManager() keymanager.KeyManager
	GetNotifiers() []notifier.Notifier
	GetUpstreamAuthority() (upstreamauthority.UpstreamAuthority, bool)
}

type GlobalConfig = catalog.GlobalConfig
type HCLPluginConfig = catalog.HCLPluginConfig
type HCLPluginConfigMap = catalog.HCLPluginConfigMap

func KnownPlugins() []catalog.PluginClient {
	return []catalog.PluginClient{
		nodeattestorv0.PluginClient,
		noderesolverv0.PluginClient,
		upstreamauthorityv0.PluginClient,
		keymanagerv0.PluginClient,
		notifierv0.PluginClient,
	}
}

func KnownServices() []catalog.ServiceClient {
	return []catalog.ServiceClient{}
}

func BuiltIns() []catalog.Plugin {
	return append([]catalog.Plugin(nil), builtIns...)
}

type Plugins struct {
	// DataStore isn't actually a plugin.
	DataStore datastore.DataStore

	NodeAttestors     map[string]nodeattestor.NodeAttestor
	NodeResolvers     map[string]noderesolver.NodeResolver
	UpstreamAuthority upstreamauthority.UpstreamAuthority
	KeyManager        keymanager.KeyManager
	Notifiers         []notifier.Notifier
}

var _ Catalog = (*Plugins)(nil)

func (p *Plugins) GetDataStore() datastore.DataStore {
	return p.DataStore
}

func (p *Plugins) GetNodeAttestorNamed(name string) (nodeattestor.NodeAttestor, bool) {
	n, ok := p.NodeAttestors[name]
	return n, ok
}

func (p *Plugins) GetNodeResolverNamed(name string) (noderesolver.NodeResolver, bool) {
	n, ok := p.NodeResolvers[name]
	return n, ok
}

func (p *Plugins) GetKeyManager() keymanager.KeyManager {
	return p.KeyManager
}

func (p *Plugins) GetNotifiers() []notifier.Notifier {
	return p.Notifiers
}

func (p *Plugins) GetUpstreamAuthority() (upstreamauthority.UpstreamAuthority, bool) {
	return p.UpstreamAuthority, p.UpstreamAuthority != nil
}

type Config struct {
	Log          logrus.FieldLogger
	GlobalConfig *GlobalConfig
	PluginConfig HCLPluginConfigMap

	Metrics          telemetry.Metrics
	IdentityProvider hostservices.IdentityProviderServer
	AgentStore       hostservices.AgentStoreServer
	MetricsService   common_services.MetricsServiceServer
}

type Repository struct {
	Catalog
	catalog.Closer
}

func Load(ctx context.Context, config Config) (*Repository, error) {
	// Strip out the Datastore plugin configuration and load the SQL plugin
	// directly. This allows us to bypass gRPC and get rid of response limits.
	dataStoreConfig := config.PluginConfig[datastore.Type]
	delete(config.PluginConfig, datastore.Type)
	ds, err := loadSQLDataStore(config.Log, dataStoreConfig)
	if err != nil {
		return nil, err
	}

	pluginConfigs, err := catalog.PluginConfigsFromHCL(config.PluginConfig)
	if err != nil {
		return nil, err
	}

	p := new(versionedPlugins)
	closer, err := catalog.Fill(ctx, catalog.Config{
		Log:           config.Log,
		GlobalConfig:  config.GlobalConfig,
		PluginConfig:  pluginConfigs,
		KnownPlugins:  KnownPlugins(),
		KnownServices: KnownServices(),
		BuiltIns:      BuiltIns(),
		HostServices: []catalog.HostServiceServer{
			hostservices.IdentityProviderHostServiceServer(config.IdentityProvider),
			hostservices.AgentStoreHostServiceServer(config.AgentStore),
			common_services.MetricsServiceHostServiceServer(config.MetricsService),
		},
	}, p)
	if err != nil {
		return nil, err
	}

	ds = datastore_telemetry.WithMetrics(ds, config.Metrics)
	ds = dscache.New(ds, clock.New())

	p.KeyManager.Plugin = keymanager_telemetry.WithMetrics(p.KeyManager.Plugin, config.Metrics)

	nodeAttestors := make(map[string]nodeattestor.NodeAttestor)
	for _, na := range p.NodeAttestors {
		nodeAttestors[na.Name()] = na
	}

	nodeResolvers := make(map[string]noderesolver.NodeResolver)
	for _, nr := range p.NodeResolvers {
		nodeResolvers[nr.Name()] = nr
	}

	var notifiers []notifier.Notifier
	for _, n := range p.Notifiers {
		notifiers = append(notifiers, n)
	}

	var upstreamAuthority upstreamauthority.UpstreamAuthority
	if p.UpstreamAuthority != nil {
		upstreamAuthority = p.UpstreamAuthority
	}

	return &Repository{
		Catalog: &Plugins{
			DataStore:         ds,
			NodeAttestors:     nodeAttestors,
			NodeResolvers:     nodeResolvers,
			UpstreamAuthority: upstreamAuthority,
			KeyManager:        p.KeyManager,
			Notifiers:         notifiers,
		},
		Closer: closer,
	}, nil
}

// versionedPlugins is a temporary struct with the v0 version shims as they are
// introduced. The catalog will fill this struct, which is then converted to
// the Plugins struct which contains the facade interfaces. It will be removed
// when the catalog is refactored to leverage the new common catalog with
// native versioning support (see issue #2153).
type versionedPlugins struct {
	NodeAttestors     map[string]nodeattestor.V0
	NodeResolvers     map[string]noderesolver.V0
	UpstreamAuthority *upstreamauthority.V0
	KeyManager        keymanager.V0
	Notifiers         []notifier.V0
}

func loadSQLDataStore(log logrus.FieldLogger, datastoreConfig map[string]catalog.HCLPluginConfig) (datastore.DataStore, error) {
	switch {
	case len(datastoreConfig) == 0:
		return nil, errors.New("expecting a DataStore plugin")
	case len(datastoreConfig) > 1:
		return nil, errors.New("only one DataStore plugin is allowed")
	}

	sqlHCLConfig, ok := datastoreConfig[ds_sql.PluginName]
	if !ok {
		return nil, fmt.Errorf("pluggability for the DataStore is deprecated; only the built-in %q plugin is supported", ds_sql.PluginName)
	}

	sqlConfig, err := catalog.PluginConfigFromHCL(datastore.Type, ds_sql.PluginName, sqlHCLConfig)
	if err != nil {
		return nil, err
	}

	// Is the plugin external?
	if sqlConfig.Path != "" {
		return nil, fmt.Errorf("pluggability for the DataStore is deprecated; only the built-in %q plugin is supported", ds_sql.PluginName)
	}

	ds := ds_sql.New(log.WithField(telemetry.SubsystemName, sqlConfig.Name))
	if err := ds.Configure(sqlConfig.Data); err != nil {
		return nil, err
	}
	return ds, nil
}
