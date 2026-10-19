package config

// GCPProvider holds the Google Cloud Platform deploy settings and per-stage
// configuration. GCP uses ADC (Application Default Credentials) + gcloud CLI for
// auth — no profile field. Project is the GCP project ID every resource belongs
// to; Region is a required GCP region.
type GCPProvider struct {
	// Project is the GCP project ID the stack deploys into (e.g. "my-app-prod").
	// It scopes the Cloud Run service, the Artifact Registry repository, the GCS
	// buckets, and the image push URL (<region>-docker.pkg.dev/<project>/...).
	Project string
	// Region is the GCP region (e.g. "southamerica-east1"). Required.
	Region string
	// Stages holds the per-stage configuration, keyed by stage name.
	Stages map[string]GCPStage
	// CDN tunes how Google Cloud CDN / load-balancer caching behaves for the
	// dynamic (Cloud Run) requests. Its zero value mirrors the AWS default: all
	// query params, no cookies, no headers. See CDNConfig (config/cdn.go).
	CDN CDNConfig
}

// GCPStage mirrors AWSProvider.Stage for GCP: custom domain and ENV values.
//
// CustomDomain is a source-aware EnvValue (built with gothic.Env /
// gothic.SSMParam / gothic.SecretsManager) exactly like ENV entries, so the
// domain can be pulled from SSM Parameter Store or Secrets Manager at deploy
// time instead of being committed in plain text. Use gothic.Env("literal") for
// a plain value.
//
// GCP has no HostedZoneId / CertificateArn / WafArn equivalents to wire here:
// a Cloud Run domain mapping provisions the Google-managed TLS certificate
// automatically (after the domain is verified with gcloud), and Cloud Armor
// security policies are attached via user infra/ files.
type GCPStage struct {
	CustomDomain EnvValue
	ENV          map[string]EnvValue
}
