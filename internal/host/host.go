// Package host owns platform descriptions and native evidence interpretation.
// It has no dependency on the workflow engine, settings loader, or cost ledger.
// Consumers resolve one descriptor instead of maintaining separate platform lists.
package host

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const (
	Claude    = "claude-code"
	Codex     = "codex"
	Cursor    = "cursor"
	DeepSeek  = "deepseek-harness"
	ZCode     = "zcode"
	WorkBuddy = "workbuddy"
	Default   = "default"
)

type HookKind string

const (
	HookNested HookKind = "nested"
	HookFlat   HookKind = "flat"
	HookZCode  HookKind = "zcode"
	HookDSH    HookKind = "dsh"
)

type HookProtocol string

const (
	ProtocolGeneric HookProtocol = "generic"
	ProtocolCodex   HookProtocol = "codex"
	ProtocolZCode   HookProtocol = "zcode"
)

type PathLayout struct {
	GlobalBase             string
	GlobalHookConfig       string
	GlobalManaged          string
	ProjectBase            string
	ProjectHookConfig      string
	ProjectManaged         string
	RemoveManagedWhenEmpty bool
}

type HookSpec struct {
	Response         func(HookDecision) any `json:"-"`
	ExitCode         func(HookDecision) int `json:"-"`
	Kind             HookKind
	Protocol         HookProtocol
	GateEvent        string
	GateMatcher      string
	GateTimeout      bool
	LifecycleMatcher string
	LifecycleTimeout bool
}

type Descriptor struct {
	Lifecycle         *Lifecycle                                  `json:"-"`
	Configuration     []ConfigField                               `json:"-"`
	ReadUsage         func(io.Reader) (Usage, error)              `json:"-"`
	CheckIdentity     func(string, string) error                  `json:"-"`
	ReadTiming        func(string, time.Time) (TaskTiming, error) `json:"-"`
	ObserveRecovery   func(RecoveryObservation) string            `json:"-"`
	ID                string
	InstallName       string
	ManifestName      string
	Aliases           []string
	Installable       bool
	Paths             PathLayout
	Hook              HookSpec
	LifecycleEvents   []string
	LifecycleRequired bool
	CostProvider      string
}

// Adapter keeps the lower-level descriptor independent of an engine transport.
// A runtime adapter may implement additional, engine-owned interfaces on this
// same registered object. Installation-only hosts need only a Descriptor.
type Adapter interface{ HostDescriptor() *Descriptor }

func (d *Descriptor) HostDescriptor() *Descriptor { return d }

var registryMu sync.RWMutex
var adapters = []Adapter{workbuddyDescriptor, claudeDescriptor, codexDescriptor, cursorDescriptor, dshDescriptor, zcodeDescriptor, defaultDescriptor}

func names(d *Descriptor) []string { return append([]string{d.ID}, d.Aliases...) }
func normalize(s string) string    { return strings.ToLower(strings.TrimSpace(s)) }

// Register installs one platform description and its optional runtime together.
// An existing installation descriptor can be extended by its runtime once,
// without copying or changing that descriptor. Conflicting definitions fail.
func Register(adapter Adapter) error {
	if adapter == nil || adapter.HostDescriptor() == nil || normalize(adapter.HostDescriptor().ID) == "" {
		return fmt.Errorf("platform descriptor requires an ID")
	}
	d := adapter.HostDescriptor()
	registryMu.Lock()
	defer registryMu.Unlock()
	for i, current := range adapters {
		old := current.HostDescriptor()
		if old.ID == d.ID {
			if current == old && d == old && adapter != current {
				adapters[i] = adapter
				return nil
			}
			return fmt.Errorf("platform %q is already registered", d.ID)
		}
		for _, left := range names(old) {
			for _, right := range names(d) {
				if normalize(left) == normalize(right) {
					return fmt.Errorf("platform alias %q is already registered", right)
				}
			}
		}
		for _, left := range old.Configuration {
			for _, right := range d.Configuration {
				if left.Name == right.Name {
					return fmt.Errorf("platform configuration field %q is already registered", right.Name)
				}
			}
		}
	}
	adapters = append(adapters, adapter)
	return nil
}

func LookupAdapter(value string) (Adapter, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	for _, a := range adapters {
		for _, name := range names(a.HostDescriptor()) {
			if normalize(name) == normalize(value) {
				return a, nil
			}
		}
	}
	return nil, fmt.Errorf("unsupported host %q", value)
}

func Lookup(value string) (Descriptor, error) {
	a, err := LookupAdapter(value)
	if err != nil {
		return Descriptor{}, err
	}
	return *a.HostDescriptor(), nil
}

// All returns registry order, shared by installation, settings and execution.
func All() []Descriptor {
	registryMu.RLock()
	defer registryMu.RUnlock()
	result := make([]Descriptor, 0, len(adapters))
	for _, a := range adapters {
		result = append(result, *a.HostDescriptor())
	}
	return result
}

func InstallableNames() []string {
	result := []string{}
	for _, d := range All() {
		if d.Installable && d.InstallName != "" {
			result = append(result, d.InstallName)
		}
	}
	return result
}
func InstallableIDs() []string {
	result := []string{}
	for _, d := range All() {
		if d.Installable {
			result = append(result, d.ID)
		}
	}
	return result
}

// ConfigField is a platform-owned editable string option. Defaults and validation
// live with its adapter, not in the common settings struct or loader.
type ConfigField struct {
	Name     string
	Default  string
	Validate func(string) error
}

func StringConfig(name, value string) ConfigField {
	return ConfigField{Name: name, Default: value, Validate: func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s: use a nonempty command/path or remove the field", name)
		}
		return nil
	}}
}

func Configuration() map[string]ConfigField {
	fields := map[string]ConfigField{}
	for _, d := range All() {
		for _, f := range d.Configuration {
			fields[f.Name] = f
		}
	}
	return fields
}

type HookDecision struct {
	Decision                 string `json:"decision"`
	Reason                   string `json:"reason"`
	Permission               string `json:"permission"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}
