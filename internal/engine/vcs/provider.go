package vcs

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// WorkspaceHandle names the native resources owned by one registered workspace.
// It is comparable so the original intent and resource registry bind exact values.
type WorkspaceHandle struct {
	URL    string `json:"url,omitempty"`
	Client string `json:"client,omitempty"`
	Branch string `json:"branch,omitempty"`
	Stream string `json:"stream,omitempty"`
	Depot  string `json:"depot,omitempty"`
	Source string `json:"source,omitempty"`
}

type nativeIdentity struct {
	Provider   string          `json:"provider"`
	Repository string          `json:"repository"`
	Scope      string          `json:"scope"`
	Revision   int             `json:"revision"`
	Address    string          `json:"address,omitempty"`
	User       string          `json:"user,omitempty"`
	Client     string          `json:"client,omitempty"`
	View       []string        `json:"view,omitempty"`
	ActualView []string        `json:"actualView,omitempty"`
	LineEnd    string          `json:"lineEnd,omitempty"`
	Charset    string          `json:"charset,omitempty"`
	Stream     string          `json:"stream,omitempty"`
	StreamSpec string          `json:"streamSpec,omitempty"`
	Depot      string          `json:"depot,omitempty"`
	Files      []nativeAddress `json:"files,omitempty"`
}

type nativeAddress struct{ Depot, Path, Origin string }

func NewNative(root, name string, runner Runner) *Provider {
	p := New(root, runner)
	if name != "" {
		p.name = strings.ToLower(name)
	}
	return p
}
func (p *Provider) Name() string { return p.name }

// BindIdentity turns a numeric revision into a scoped immutable identity once,
// at Start. Reopen and all later operations consume the bound identity.
func (p *Provider) BindIdentity(identity string) (string, error) {
	if p.name == "git" {
		return strings.ToLower(strings.TrimSpace(identity)), p.Verify(identity)
	}
	if strings.HasPrefix(identity, "native:") {
		return identity, p.Verify(identity)
	}
	if _, err := strconv.Atoi(identity); err != nil {
		return "", fmt.Errorf("%s requires a revision or scoped native identity", p.name)
	}
	return p.nativeResolve(identity)
}
func encodeNative(id nativeIdentity) string {
	b, _ := json.Marshal(id)
	return "native:" + base64.RawURLEncoding.EncodeToString(b)
}
func decodeNative(identity string) (nativeIdentity, error) {
	var id nativeIdentity
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(identity, "native:"))
	if err != nil {
		return id, err
	}
	if err = json.Unmarshal(b, &id); err != nil {
		return id, err
	}
	if id.Provider != "svn" && id.Provider != "p4" || id.Repository == "" || id.Scope == "" || id.Revision < 1 {
		return id, fmt.Errorf("incomplete native snapshot identity")
	}
	return id, nil
}

// PlanWorkspace derives native names before any write. Resource IDs already
// provide ownership and uniqueness; no second resource index is needed.
func PlanWorkspace(name, source, run, id string) WorkspaceHandle {
	handle := WorkspaceHandle{Source: source}
	identity, err := decodeNative(source)
	if err != nil {
		return handle
	}
	switch name {
	case "svn":
		handle.URL = identity.Address + "/formal-gates-" + run + "-" + id
	case "p4":
		handle.Client = "fg-" + run + "-" + id
		handle.Branch = handle.Client
		if identity.Stream != "" {
			depot, _, _ := strings.Cut(strings.TrimPrefix(identity.Stream, "//"), "/")
			handle.Stream = "//" + depot + "/" + handle.Client
			handle.Depot = handle.Stream
		} else {
			for _, row := range identity.ActualView {
				fields := viewFields(row)
				if len(fields) == 2 {
					depot, _, _ := strings.Cut(strings.TrimLeft(fields[0], "-+&/"), "/")
					handle.Depot = "//" + depot + "/.formal-gates/" + run + "/" + id
					break
				}
			}
		}
	}
	return handle
}
func (p *Provider) nativeResolve(revision string) (string, error) {
	if p.name == "svn" {
		return p.svnResolve(revision)
	}
	if p.name == "p4" {
		return p.p4Resolve(revision)
	}
	return "", fmt.Errorf("unsupported VCS %q", p.name)
}
func (p *Provider) nativeRoot() error { _, err := p.nativeResolve(""); return err }
func (p *Provider) nativeVerify(identity string) error {
	id, err := decodeNative(identity)
	if err != nil {
		return err
	}
	if id.Provider != p.name {
		return fmt.Errorf("snapshot provider differs from %s", p.name)
	}
	current, err := p.nativeResolve("")
	if err != nil {
		return err
	}
	bound, err := decodeNative(current)
	if err != nil {
		return err
	}
	if bound.Repository != id.Repository {
		return fmt.Errorf("snapshot repository differs from bound workspace")
	}
	_, err = p.nativeEntries(identity)
	return err
}
func (p *Provider) nativeStatus() (string, error) {
	if p.name == "svn" {
		return p.svnStatus()
	}
	return p.p4Status()
}
func (p *Provider) nativeChangedPaths() ([]string, error) {
	if p.name == "svn" {
		return p.svnChangedPaths()
	}
	return p.p4ChangedPaths()
}
func (p *Provider) nativeTrack(paths []string) error {
	if p.name == "svn" {
		return p.svnTrack(paths)
	}
	return p.p4Track(paths)
}
func (p *Provider) nativeTracked() (bool, error) {
	paths, err := p.nativeChangedPaths()
	return len(paths) > 0, err
}
func (p *Provider) nativeCommit(message string) error {
	if p.name == "svn" {
		return p.svnCommit(message)
	}
	return p.p4Commit(message)
}
func (p *Provider) nativeEntries(identity string) (map[string]string, error) {
	if identity == "HEAD" {
		var err error
		identity, err = p.Resolve()
		if err != nil {
			return nil, err
		}
	}
	id, err := decodeNative(identity)
	if err != nil {
		return nil, err
	}
	if id.Provider != p.name {
		return nil, fmt.Errorf("snapshot provider mismatch")
	}
	if p.name == "svn" {
		return p.svnEntries(id)
	}
	return p.p4Entries(id)
}
func (p *Provider) nativeMessage(identity string) (string, error) {
	id, err := decodeNative(identity)
	if err != nil {
		return "", err
	}
	if p.name == "svn" {
		return p.svnMessage(id)
	}
	return p.p4Message(id)
}
func (p *Provider) nativeAncestor(base, head string) error {
	a, err := decodeNative(base)
	if err != nil {
		return err
	}
	b, err := decodeNative(head)
	if err != nil {
		return err
	}
	if a.Provider != p.name || b.Provider != p.name || a.Repository != b.Repository {
		return fmt.Errorf("native lineage repository mismatch")
	}
	if base == head {
		return nil
	}
	if p.name == "svn" {
		return p.svnAncestor(a, b)
	}
	return p.p4Ancestor(a, b)
}
func (p *Provider) nativeCount(base, head string) (int, error) {
	a, err := decodeNative(base)
	if err != nil {
		return 0, err
	}
	b, err := decodeNative(head)
	if err != nil {
		return 0, err
	}
	if p.name == "svn" {
		return p.svnCount(a, b)
	}
	return p.p4Count(a, b)
}
func (p *Provider) nativeApply(identity string, paths []string) error {
	id, err := decodeNative(identity)
	if err != nil {
		return err
	}
	if p.name == "svn" {
		return p.svnApply(id, paths)
	}
	return p.p4Apply(id, paths)
}
func (p *Provider) nativeIndex() ([]IndexEntry, error) {
	if p.name == "svn" {
		return p.svnIndex()
	}
	return p.p4Index()
}
func (p *Provider) nativeMerge(incoming string) error {
	id, err := decodeNative(incoming)
	if err != nil {
		return err
	}
	if p.name == "svn" {
		return p.svnMerge(id)
	}
	return p.p4Merge(id)
}
