package cloudflare

// Client is reserved for API fallback when no qualifying interactive Vast host
// is available. It intentionally contains no credentials or network behavior in pre-V0.
type Client struct{}
