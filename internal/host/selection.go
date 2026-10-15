package host

type ModelSelection struct {
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

type ModelOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	// nil means unreported; an empty non-nil slice means unsupported.
	Efforts       []string `json:"efforts"`
	DefaultEffort string   `json:"defaultEffort,omitempty"`
}

type SelectionCatalogue struct {
	Models  []ModelOption  `json:"models"`
	Current ModelSelection `json:"current"`
	Default ModelSelection `json:"default"`
	Source  string         `json:"source"`
}

// SelectionAdapter queries native metadata in the admitted runtime that owns its assets.
type SelectionAdapter interface {
	Adapter
	SelectionOptions(root, runtimeTarget string, native map[string]string, model string) (SelectionCatalogue, error)
}
