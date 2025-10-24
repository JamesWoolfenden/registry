package auth

// Method represents the authentication method used
type Method string

const (
	// MethodGitHubAT - GitHub OAuth authentication (access token)
	MethodGitHubAT Method = "github-at"

	// MethodGitHubOIDC - GitHub Actions OIDC authentication
	MethodGitHubOIDC Method = "github-oidc"

	// MethodOIDC - Generic OIDC authentication
	MethodOIDC Method = "oidc"

	// MethodDNS - DNS-based public/private key authentication
	MethodDNS Method = "dns"

	// MethodHTTP - HTTP-based public/private key authentication
	MethodHTTP Method = "http"

	// MethodNone - No authentication - should only be used for local development and testing
	MethodNone Method = "none"
)
