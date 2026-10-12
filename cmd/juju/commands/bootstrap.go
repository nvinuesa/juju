// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"fmt"
	"strings"

	jujuclock "github.com/juju/clock"
	"github.com/juju/errors"
	"github.com/juju/gnuflag"
	"github.com/juju/names/v6"

	jujucmd "github.com/juju/juju/cmd"
	"github.com/juju/juju/cmd/cmd"
	"github.com/juju/juju/cmd/juju/application/refresher"
	"github.com/juju/juju/cmd/juju/common"
	"github.com/juju/juju/cmd/modelcmd"
	corebase "github.com/juju/juju/core/base"
	"github.com/juju/juju/core/instance"
	"github.com/juju/juju/core/semversion"
	jujuversion "github.com/juju/juju/core/version"
	"github.com/juju/juju/domain/deployment/charm"
	"github.com/juju/juju/environs/bootstrap"
	_ "github.com/juju/juju/internal/provider/all" // Import all the providers for bootstrap.
)

// provisionalProviders is the names of providers that are hidden behind
// feature flags.
var provisionalProviders = map[string]string{}

var usageBootstrapSummary = `
Initializes a cloud environment.`[1:]

var usageBootstrapDetailsPartOne = `
Used without arguments, bootstrap will step you through the process of
initializing a Juju cloud environment. Initialization consists of creating
a 'controller' model and provisioning a machine to act as controller.

Controller names may only contain lowercase letters, digits and hyphens, and
may not start with a hyphen.
We recommend you call your controller ‘username-region’ e.g. ‘fred-us-east-1’.
See ` + "`--clouds`" + ` for a list of clouds and credentials.
See ` + "`--regions <cloud>`" + ` for a list of available regions for a given cloud.

Credentials are set beforehand and are distinct from any other
configuration (see `[1:] + "`juju add-credential`" + `).

The 'controller' model typically does not run workloads. It should remain
pristine to run and manage Juju's own infrastructure for the corresponding
cloud. Additional models should be created with ` + "`juju add-model`" + ` for workload purposes.

If ` + "`--bootstrap-constraints`" + ` is used, its values will also apply to any
future controllers provisioned for high availability (HA).

If ` + "`--constraints`" + ` is used, its values will be set as the default
constraints for all future workload machines in the model, exactly as if
the constraints were set with ` + "`juju set-model-constraints`" + `.

It is possible to override constraints and the automatic machine selection
algorithm by assigning a "placement directive" via the '--to' option. This
dictates what machine to use for the controller. This would typically be
used with the MAAS provider (` + "`--to <host>.maas`" + `).

You can change the default timeout and retry delays used during the
bootstrap by changing the following settings in your configuration
(all values represent number of seconds):

    # How long to wait for a connection to the controller
    bootstrap-timeout: 1200  # default: 20 minutes
    # How long to wait between connection attempts to a controller address.
    bootstrap-retry-delay: 5  # default: 5 seconds
    # How often to refresh controller addresses from the API server.
    bootstrap-addresses-delay: 10  # default: 10 seconds

It is possible to override the base e.g. ` + "`ubuntu@22.04`" + `, Juju attempts
to bootstrap on to, by supplying a base argument to ` + "`--bootstrap-base`" + `.

An error is emitted if the determined base is not supported. Using the
` + "`--force`" + ` option to override this check:

    juju bootstrap --bootstrap-base=ubuntu@22.04 --force

Private clouds may need to specify their own custom image metadata and
tools/agent. Use ` + "`--metadata-source`" + ` whose value is a local directory.

By default, the Juju version of the agent binary that is downloaded and
installed on all models for the new controller will be the same as that
of the Juju client used to perform the bootstrap.
However, a user can specify a different agent version via the ` + "`--agent-version`" + `
option to bootstrap command. Juju will use this version for models' agents
as long as the client's version is from the same Juju release base.
In other words, a 4.1.1 client can bootstrap any 4.1.x agents but cannot
bootstrap any 4.0.x or 4.2.x agents.
The agent version can be specified a simple numeric version, e.g. 4.1.1.

For example, at the time when 3.6.0, 3.6.1 and 3.6.2 are released and your
agent stream is 'released' (default), then a 3.6.1 client can bootstrap:
   * a 3.6.0 controller by running ` + "`... bootstrap --agent-version=3.6.0 ...`" + `;
   * a 3.6.1 controller by running ` + "`... bootstrap ...`" + `;
   * a 3.6.2 controller by running ` + "`bootstrap --auto-upgrade`" + `.
However, if this client has a copy of the codebase, then a local copy of Juju
will be built and bootstrapped -- 3.6.1.1.

Bootstrapping to a Kubernetes cluster requires that the service set up to handle
requests to the controller be accessible outside the cluster. Typically this
means a service type of LoadBalancer is needed, and Juju does create such a
service if it knows it is supported by the cluster. This is performed by
interrogating the cluster for a well known managed deployment such as microk8s,
GKE or EKS.

When bootstrapping to a Kubernetes cluster Juju does not recognise, there's no
guarantee a load balancer is available, so Juju defaults to a controller
service type of ClusterIP. This may not be suitable, so there are three bootstrap
options available to tell Juju how to set up the controller service. Part of
the solution may require a load balancer for the cluster to be set up manually
first, or perhaps an external k8s service via a FQDN will be used
(this is a cluster specific implementation decision which Juju needs to be
informed about so it can set things up correctly). The three relevant bootstrap
options are (see list of bootstrap config items below for a full explanation):

- ` + "`controller-service-type`" + `
- ` + "`controller-external-name`" + `
- ` + "`controller-external-ips`" + `

Juju advertises those addresses to other controllers, so they must be resolveable from
other controllers for cross-model (cross-controller, actually) relations to work.

If a storage pool is specified using ` + "`--storage-pool`" + `, this will be created
in the controller model.

Authorized keys can be set by using --config authorized-keys and or
--config authorized-keys-path.
`

var usageBootstrapConfigTxt = `

Available keys for use with ` + "`--config`" + ` are:
`

var usageBootstrapDetailsPartTwo = `
`

const usageBootstrapExamples = `
    juju bootstrap
    juju bootstrap --clouds
    juju bootstrap --regions aws
    juju bootstrap aws
    juju bootstrap aws/us-east-1
    juju bootstrap google joe-us-east1
    juju bootstrap --config=~/config-rs.yaml google joe-syd
    juju bootstrap --agent-version=2.2.4 aws joe-us-east-1
    juju bootstrap --config bootstrap-timeout=1200 azure joe-eastus
    juju bootstrap aws --storage-pool name=secret --storage-pool type=ebs --storage-pool encrypted=true
	juju bootstrap lxd --bootstrap-base=ubuntu@22.04

For a bootstrap on Kubernetes, setting the service type of the Juju controller service to LoadBalancer:

    juju bootstrap --config controller-service-type=loadbalancer

For a bootstrap on Kubernetes, setting the service type of the Juju controller service to External:

    juju bootstrap --config controller-service-type=external --config controller-external-name=controller.juju.is
`

func newBootstrapCommand() cmd.Command {
	command := &bootstrapCommand{}
	command.clock = jujuclock.WallClock
	command.CanClearCurrentModel = true
	return modelcmd.Wrap(command,
		modelcmd.WrapSkipModelFlags,
		modelcmd.WrapSkipDefaultModel,
	)
}

// bootstrapCommand is responsible for launching the first machine in a juju
// environment, and setting up everything necessary to continue working.
type bootstrapCommand struct {
	controllerProvisioner
}

func (c *bootstrapCommand) Info() *cmd.Info {
	info := &cmd.Info{
		Name:     "bootstrap",
		Args:     "[<cloud name>[/region] [<controller name>]]",
		Examples: usageBootstrapExamples,
		SeeAlso: []string{
			"add-credential",
			"autoload-credentials",
			"add-model",
			"controller-config",
			"model-config",
			"set-constraints",
			"show-cloud",
		},
		Purpose: usageBootstrapSummary,
	}
	configKeys := c.configDetails()

	info.Doc = fmt.Sprintf("%s%s\n%s%s",
		usageBootstrapDetailsPartOne,
		usageBootstrapConfigTxt,
		configKeys.Format(),
		usageBootstrapDetailsPartTwo)
	return jujucmd.Info(info)
}

// ConfigCategoryKeys represents the collection of keys supported by the
// --config option during bootstrap grouped by the domain category the keys
// apply to within Juju.
type ConfigCategoryKeys struct {
	// BootstrapKeys describes the set of keys supported by --config as
	// bootstrap only config keys.
	BootstrapKeys map[string]common.PrintConfigSchema

	// ControllerKeys describes the set of keys supported by --config as
	// configuration items that will be applied to the controller during
	// bootstrap.
	ControllerKeys map[string]common.PrintConfigSchema

	// ModelKeys describes the set of keys supported by --config as
	// configuration items that will be applied to the controllers model during
	// bootstrap.
	ModelKeys map[string]common.PrintConfigSchema
}

// Format is responsible for returning a formatted categorised string of all
// the --config keys supported by bootstrap. This is used directly when
// generating help docs.
func (c ConfigCategoryKeys) Format() string {
	builder := strings.Builder{}

	if c.BootstrapKeys != nil {
		fmt.Fprint(&builder, "Bootstrap configuration keys:\n\n")
		output, _ := common.FormatConfigSchema(c.BootstrapKeys)
		fmt.Fprintln(&builder, output)
	}

	if c.ControllerKeys != nil {
		fmt.Fprint(&builder, "Controller configuration keys:\n\n")
		output, _ := common.FormatConfigSchema(c.ControllerKeys)
		fmt.Fprint(&builder, output)
	}

	if c.ModelKeys != nil {
		fmt.Fprint(&builder, "Model configuration keys (affecting the controller model):\n\n")
		output, _ := common.FormatConfigSchema(c.ModelKeys)
		fmt.Fprint(&builder, output)
	}

	return builder.String()
}

func (c *bootstrapCommand) SetFlags(f *gnuflag.FlagSet) {
	c.ModelCommandBase.SetFlags(f)
	f.Var(&c.ConstraintsStr, "constraints", "Set model constraints")
	f.Var(&c.BootstrapConstraintsStr, "bootstrap-constraints", "Specify bootstrap machine constraints")
	f.StringVar(&c.BootstrapBase, "bootstrap-base", "", "Specify the base of the bootstrap machine")
	f.StringVar(&c.BootstrapImage, "bootstrap-image", "", "Specify the image of the bootstrap machine (requires `--bootstrap-constraints` specifying architecture)")
	f.BoolVar(&c.BuildAgent, "build-agent", false, "Build local version of agent binary before bootstrapping")
	f.StringVar(&c.MetadataSource, "metadata-source", "", "Local path to use as agent and/or image metadata source")
	f.StringVar(&c.Placement, "to", "", "Placement directive indicating an instance to bootstrap")
	f.BoolVar(&c.KeepBrokenEnvironment, "keep-broken", false,
		"Do not destroy the provisioned controller instance if bootstrap fails")
	f.BoolVar(&c.AutoUpgrade, "auto-upgrade", false, "After bootstrap, upgrade to the latest patch release")
	f.StringVar(&c.AgentVersionParam, "agent-version", "", "Version of agent binaries to use for Juju agents")
	f.StringVar(&c.CredentialName, "credential", "", "Credentials to use when bootstrapping")
	f.Var(&c.config, "config",
		"Specify a controller configuration file, or one or more configuration options. Model config keys only affect the controller model.\n    (`--config config.yaml [--config key=value ...])`")
	f.Var(&c.modelDefaults, "model-default",
		"Specify a configuration file, or one or more configuration\n    options to be set for all models, unless otherwise specified\n    (`--model-default config.yaml [--model-default key=value ...])`")
	f.Var(&c.storagePool, "storage-pool",
		"Specify options for an initial storage pool\n    'name' and 'type' are required, plus any additional attributes\n    (`--storage-pool pool-config.yaml [--storage-pool key=value ...]`)")
	f.BoolVar(&c.showClouds, "clouds", false,
		"Print the available clouds which can be used to bootstrap a Juju environment")
	f.StringVar(&c.showRegionsForCloud, "regions", "", "Print the available regions for the specified cloud")
	f.BoolVar(&c.noSwitch, "no-switch", false, "Do not switch to the newly created controller")
	f.BoolVar(&c.Force, "force", false, "Allow the bypassing of checks such as supported base")
	f.StringVar(&c.ControllerCharmPath, "controller-charm-path", "", "Path to a locally built controller charm")
	f.StringVar(&c.ControllerCharmChannelStr, "controller-charm-channel",
		fmt.Sprintf("%d.%d/stable", jujuversion.Current.Major, jujuversion.Current.Minor),
		"The Charmhub channel to download the controller charm from (if not using a local charm)")
}

func (c *bootstrapCommand) Init(args []string) (err error) {
	// Validate the bootstrap base looks like a base.
	if c.BootstrapBase != "" {
		if _, err := corebase.ParseBaseFromString(c.BootstrapBase); err != nil {
			return errors.NotValidf("base %q", c.BootstrapBase)
		}
	}

	if c.ControllerCharmPath != "" {
		if refresher.IsLocalURL(c.ControllerCharmPath) {
			_, err := c.Filesystem().Stat(c.ControllerCharmPath)
			if err != nil {
				return errors.Annotatef(err, "problem with --controller-charm-path")
			}
			ch, err := charm.ReadCharmArchive(c.ControllerCharmPath)
			if err != nil {
				return errors.Annotatef(err, "--controller-charm-path %q is not a valid charm", c.ControllerCharmPath)
			}
			if ch.Meta().Name != bootstrap.ControllerCharmName {
				return errors.Errorf("--controller-charm-path %q is not a %q charm", c.ControllerCharmPath,
					bootstrap.ControllerCharmName)
			}
		}
	}

	c.ControllerCharmChannel, err = parseControllerCharmChannel(c.ControllerCharmChannelStr)
	if err != nil {
		return errors.NotValidf("controller charm channel %q", c.ControllerCharmChannelStr)
	}

	if c.showClouds && c.showRegionsForCloud != "" {
		return errors.New("--clouds and --regions can't be used together")
	}
	if c.showClouds {
		return cmd.CheckEmpty(args)
	}
	if c.showRegionsForCloud != "" {
		return cmd.CheckEmpty(args)
	}
	if c.AgentVersionParam != "" && c.BuildAgent {
		return errors.New("--agent-version and --build-agent can't be used together")
	}

	// Parse the placement directive. Bootstrap currently only
	// supports provider-specific placement directives.
	if c.Placement != "" {
		_, err = instance.ParsePlacement(c.Placement)
		if err != instance.ErrPlacementScopeMissing {
			// We only support unscoped placement directives for bootstrap.
			return errors.Errorf("unsupported bootstrap placement directive %q", c.Placement)
		}
	}
	if !c.AutoUpgrade {
		// With no auto upgrade chosen, we default to the version matching the bootstrap client.
		vers := jujuversion.Current
		c.AgentVersion = &vers
	}
	if c.AgentVersionParam != "" {
		if vers, err := semversion.ParseBinary(c.AgentVersionParam); err == nil {
			c.AgentVersion = &vers.Number
		} else if vers, err := semversion.Parse(c.AgentVersionParam); err == nil {
			c.AgentVersion = &vers
		} else {
			return err
		}
	}
	if c.AgentVersion != nil && (c.AgentVersion.Major != jujuversion.Current.Major || c.AgentVersion.Minor != jujuversion.Current.Minor) {
		return errors.Errorf("this client can only bootstrap %v.%v agents", jujuversion.Current.Major,
			jujuversion.Current.Minor)
	}

	switch len(args) {
	case 0:
		// no args or flags, go interactive.
		c.interactive = true
		return nil
	}
	c.Cloud = args[0]
	if i := strings.IndexRune(c.Cloud, '/'); i > 0 {
		c.Cloud, c.Region = c.Cloud[:i], c.Cloud[i+1:]
	}
	if ok := names.IsValidCloud(c.Cloud); !ok {
		return errors.NotValidf("cloud name %q", c.Cloud)
	}
	if len(args) > 1 {
		c.setControllerName(args[1])
		return cmd.CheckEmpty(args[2:])
	}
	return nil
}

// Run connects to the environment specified on the command line and bootstraps
// a juju in that environment if none already exists. If there is as yet no environments.yaml file,
// the user is informed how to create one.
func (c *bootstrapCommand) Run(ctx *cmd.Context) error {
	return c.provisionController(ctx)
}
