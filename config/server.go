package config

// ContextKey is a custom type for context keys to avoid collisions (SA1029).
type ContextKey string

type ServerRoot struct {
	Server ServerConfig `yaml:"server"`
}

type ServerConfig struct {
	Hostname           string                   `yaml:"hostname"`
	Port               int                      `yaml:"port"`
	Certs              Certs                    `yaml:"certs"`
	Repo               Repo                     `yaml:"repo"`
	ListPageSize       int                      `yaml:"listPageSize"` // When >0, list endpoint paginates with ?page=N (spec 4.1). When 0, pagination disabled.
	Publish            PublishConfig            `yaml:"publish"`
	Auth               AuthConfig               `yaml:"auth"`
	TlsEnabled         bool                     `yaml:"tlsEnabled"`
	PackageCollections PackageCollectionsConfig `yaml:"packageCollections"`
}

type Certs struct {
	CertFile string `yaml:"cert"`
	KeyFile  string `yaml:"key"`
}

type PublishConfig struct {
	MaxSize int64 `yaml:"maxSize"`
}

type Repo struct {
	Path  string      `yaml:"path"`
	Type  string      `yaml:"type"`
	Maven MavenConfig `yaml:"maven"`
	S3    S3Config    `yaml:"s3"`
}

type MavenConfig struct {
	BaseURL       string `yaml:"baseURL"`
	GroupIdPrefix string `yaml:"groupIdPrefix"`
	AuthMode      string `yaml:"authMode"`
	Username      string `yaml:"username"`
	Password      string `yaml:"password"`
	Timeout       int    `yaml:"timeout"`
}

type S3Config struct {
	Bucket string `yaml:"bucket"`
	Region string `yaml:"region"`
	// Prefix is prepended to every object key, allowing multiple registries to share a bucket.
	Prefix string `yaml:"prefix"`
	// Profile selects a named AWS profile (e.g. SSO) from the shared AWS config/credentials files.
	// Leave empty to use the default credential chain (env vars, instance role, default profile, ...).
	Profile string `yaml:"profile"`
	// Endpoint overrides the default AWS endpoint, for S3-compatible services (MinIO, LocalStack, ...).
	Endpoint string `yaml:"endpoint"`
	// UsePathStyle selects path-style addressing (bucket in the path instead of the host),
	// required by most S3-compatible services when Endpoint is set.
	UsePathStyle bool `yaml:"usePathStyle"`
}

type AuthConfig struct {
	Name         string `yaml:"name"`
	Type         string `yaml:"type"`
	Enabled      bool   `yaml:"enabled"`
	ClientId     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
	Issuer       string `yaml:"issuer"`
	GrantType    string `yaml:"grant_type"`
	Users        []User `yaml:"users"`
}

type User struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type PackageCollectionsConfig struct {
	Enabled            bool `yaml:"enabled"`
	RequirePackageJson bool `yaml:"requirePackageJson"`
	// PublicRead allows unauthenticated GET /collection and /collection/{scope}.
	// Required for swift package-collection add, which fetches the URL without credentials.
	PublicRead bool `yaml:"publicRead"`
	// AllowAuthQueryParam allows ?auth=<base64(Authorization)> on collection paths only, for clients
	// that cannot send headers (e.g. swift package-collection add). Off by default to avoid credential
	// leakage via logs, referrers, and proxies. When true, decoded value must start with "Basic " or "Bearer ".
	AllowAuthQueryParam bool `yaml:"allowAuthQueryParam"`
}

const (
	// AuthHeaderContextKey is the context key for the Authorization header (passthrough auth).
	AuthHeaderContextKey ContextKey = "Authorization"
)
