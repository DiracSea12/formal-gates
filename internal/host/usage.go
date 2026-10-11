package host

type TaskTiming struct {
	StartedAt      string `json:"startedAt"`
	CompletedAt    string `json:"completedAt"`
	WallTimeMillis int64  `json:"wallTimeMillis"`
}

// Usage is the exact metered token usage parsed from one host transcript,
// normalized to the pricing categories shared by the supported hosts.
type Usage struct {
	InputCacheHitTokens  int64
	InputCacheMissTokens int64
	OutputTokens         int64
}
