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

type SelectionAdapter interface {
	Adapter
	SelectionOptions(root string, native map[string]string, model string) (SelectionCatalogue, error)
}
