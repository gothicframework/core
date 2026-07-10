// Package config defines the shared, user-facing types used inside a project's
// gothic.config.go file: the typed environment-value builders (Env, SSMParam,
// SecretsManager) and the GothicContext passed to lifecycle hooks.
package config

// EnvSource identifies where a runtime env var value comes from.
type EnvSource int

const (
	// RawEnv is a plain string value baked into the Lambda environment.
	RawEnv EnvSource = iota
	// SSMParamEnv is an AWS SSM Parameter Store path resolved at deploy time.
	SSMParamEnv
	// SecretsManagerEnv is an AWS Secrets Manager path/ARN resolved at deploy time.
	SecretsManagerEnv
)

// EnvValue is an opaque typed wrapper around a runtime env var. Users construct
// these via the builder functions below; the AST parser identifies each builder
// by its call-expression function name.
type EnvValue struct {
	Source EnvSource
	Value  string
	// JSONKey, when non-empty, selects a single field out of a JSON-encoded
	// secret/parameter at deploy time (jsondecode(...)["JSONKey"]). Set via .Get.
	JSONKey string
}

// Env declares a plain string environment value.
func Env(v string) EnvValue { return EnvValue{Source: RawEnv, Value: v} }

// SSMParam declares an environment value sourced from an SSM Parameter Store path.
func SSMParam(path string) EnvValue { return EnvValue{Source: SSMParamEnv, Value: path} }

// SecretsManager declares an environment value sourced from a Secrets Manager path/ARN.
func SecretsManager(path string) EnvValue { return EnvValue{Source: SecretsManagerEnv, Value: path} }

// Get selects a single field from a JSON-encoded secret or parameter. AWS Secrets
// Manager secrets are commonly stored as a JSON object (e.g. {"secret-key":"..."});
// gothic.SecretsManager("/myapp/dev/api-key").Get("secret-key") pulls just that key
// at deploy time via Terraform's jsondecode, so the env var receives the value
// rather than the whole JSON blob. It also works on JSON-valued SSMParam entries.
func (v EnvValue) Get(jsonKey string) EnvValue {
	v.JSONKey = jsonKey
	return v
}

// GothicContext is injected into BeforeDeploy / AfterDeploy lifecycle hooks.
type GothicContext struct {
	Stage       string
	ProjectName string
	Suffix      string
	Region      string
	Env         map[string]EnvValue
	Outputs     map[string]string
}
