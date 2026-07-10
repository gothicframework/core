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

// DeployConfig holds the deployment settings and per-stage configuration.
type DeployConfig struct {
	ServerMemory  int
	ServerTimeout int
	Region        string
	Profile       string
	CustomDomain  bool
	Stages        map[string]Stage
}

// Stage is the per-stage configuration declared inside Deploy.Stages.
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
