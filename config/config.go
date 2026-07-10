package config

// Config is the user-facing configuration type declared in a project's
// gothic.config.go file. It mirrors the internal cli.Config that the AST parser
// produces, but lives here so user projects can reference it via the public
// gothicframework/core/config package without importing CLI internals.
//
// GoModuleName is intentionally absent: it is read from go.mod at runtime.
type Config struct {
	ProjectName    string
	TofuBinaryPath string
	DockerfilePath string
	WasmBinary     string
	TailwindBinary string
	OptimizeImages OptimizeImagesConfig
	// Runtime is the routing/caching configuration the generated main.go hands to
	// gothicServer.Middleware. It is read at runtime (not by the CLI's config
	// parser), and its zero value is the default behavior, so it may be omitted.
	Runtime RuntimeConfig
	Deploy  *DeployConfig
}

// OptimizeImagesConfig controls image optimization defaults.
type OptimizeImagesConfig struct {
	LowResolutionRate int
}

// Provider selects which cloud the stack deploys to. v3 ships AWS only; GCP and
// Azure are reserved for later without changing the config shape.
type Provider int

const (
	// AWS is the only provider supported in v3. It is the zero value, so a
	// DeployConfig that omits Provider deploys to AWS.
	AWS Provider = iota
)

// DeployConfig selects a deploy provider and holds the per-provider settings.
// The AWS-specific settings (memory, timeout, region, profile, stages) now live
// under Providers.AWS so additional providers can be added under Providers later
// without reshaping the top-level Deploy block.
type DeployConfig struct {
	// Provider chooses which cloud to deploy to. Defaults to AWS.
	Provider Provider
	// Providers holds the per-provider deploy settings.
	Providers Providers
}

// Providers groups the per-provider deploy settings. Only AWS exists in v3;
// future providers (e.g. GCP, Azure) are added here as additional value fields.
type Providers struct {
	AWS AWSProvider
}

// AWSProvider holds the AWS-specific deploy settings and per-stage configuration.
type AWSProvider struct {
	ServerMemory  int
	ServerTimeout int
	Region        string
	Profile       string
	Stages        map[string]Stage
}

// Stage is the per-stage configuration declared inside Deploy.Providers.AWS.Stages.
//
// HostedZoneId, CustomDomain, CertificateArn and WafArn are source-aware EnvValues
// (built with gothic.Env / gothic.SSMParam / gothic.SecretsManager) exactly like
// ENV entries, so a domain or an ARN can be pulled from SSM Parameter Store or
// Secrets Manager at deploy time instead of being committed in plain text. Use
// gothic.Env("literal") for a plain value.
type Stage struct {
	HostedZoneId   EnvValue
	CustomDomain   EnvValue
	CertificateArn EnvValue
	WafArn         EnvValue
	ENV            map[string]EnvValue
}
