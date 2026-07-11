package config

// CDNConfig configures how the CloudFront distribution treats the dynamic
// (server) requests it forwards to your app: which query params, cookies, and
// request headers are forwarded to the origin AND folded into the cache key (so
// two requests that differ only in an allowed value cache separately).
//
// Each field is set with the gothic.Allow* builders — you never construct an
// AllowRule directly:
//
//	CDN: gothic.CDNConfig{
//		QueryParams: gothic.AllowAll(),                       // the default
//		Cookies:     gothic.Allow("session", "cart"),         // only these
//		Headers:     gothic.Allow("CloudFront-Viewer-Country"),
//	}
//
// The zero value of a field applies Gothic's per-field default: AllowAll for
// QueryParams, AllowNone for Cookies and Headers. So an omitted CDN block keeps
// every query param in the cache key and forwards no cookies or headers.
type CDNConfig struct {
	// QueryParams controls query-string participation. Default: AllowAll — every
	// query param is forwarded and part of the cache key, so ?lang=PT and ?lang=ENG
	// render and cache independently.
	QueryParams AllowRule
	// Cookies controls cookie participation. Default: AllowNone.
	Cookies AllowRule
	// Headers controls request-header participation. Default: AllowNone.
	//
	// Headers accept only AllowNone or Allow (a whitelist) — AllowAll / AllowAllExcept
	// are rejected at deploy time (a CloudFront cache-policy limit). Never Allow the
	// "Host" or "Authorization" header for the server behavior: CloudFront signs the
	// Lambda Function URL (SigV4, via Origin Access Control) against the function
	// URL's own host, so forwarding either header breaks the signature (HTTP 403).
	Headers AllowRule
}

// AllowRule is an opaque rule for one class of request values (query params,
// cookies, or headers) in a CDNConfig. Build it only with AllowAll / AllowNone /
// Allow / AllowAllExcept; its zero value means "unset" (use the field default).
type AllowRule struct {
	mode  allowMode
	names []string
}

// allowMode is the internal behavior selector. Users never see it — they call the
// Allow* builders, and the deploy generator reads AllowRule via Behavior/Items.
type allowMode int

const (
	allowUnset     allowMode = iota // zero value: use the field's default
	allowAllMode                    // every value
	allowNoneMode                   // no value
	allowListMode                   // only the named values (whitelist)
	allowExceptMode                 // every value except the named ones
)

// AllowAll includes every value of the class in the cache key and forwards them
// to the origin.
func AllowAll() AllowRule { return AllowRule{mode: allowAllMode} }

// AllowNone excludes the class entirely — nothing is forwarded or cached on.
func AllowNone() AllowRule { return AllowRule{mode: allowNoneMode} }

// Allow includes only the named values (a whitelist). Pass at least one name.
func Allow(names ...string) AllowRule {
	return AllowRule{mode: allowListMode, names: names}
}

// AllowAllExcept includes every value except the named ones. Pass at least one
// name. Not valid for headers.
func AllowAllExcept(names ...string) AllowRule {
	return AllowRule{mode: allowExceptMode, names: names}
}

// Behavior returns the CloudFront cache-policy behavior string for this rule
// ("all", "none", "whitelist", or "allExcept"), or "" when the rule is unset (the
// caller should substitute the field's default). It exists for the deploy
// generator; user code never needs it.
func (r AllowRule) Behavior() string {
	switch r.mode {
	case allowAllMode:
		return "all"
	case allowNoneMode:
		return "none"
	case allowListMode:
		return "whitelist"
	case allowExceptMode:
		return "allExcept"
	default: // allowUnset
		return ""
	}
}

// Items returns the names attached to a whitelist / all-except rule (nil for
// all / none / unset). For the deploy generator.
func (r AllowRule) Items() []string { return r.names }
