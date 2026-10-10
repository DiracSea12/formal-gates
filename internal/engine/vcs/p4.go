package vcs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Tagged JSON keeps native field names, filetypes, View order and resolve data.
func (p *Provider) p4Args(args ...string) []string {
	prefix := []string{}
	if p.handle.Source != "" {
		if id, err := decodeNative(p.handle.Source); err == nil {
			prefix = append(prefix, "-p", id.Address, "-u", id.User)
			if id.Charset != "" {
				prefix = append(prefix, "-C", id.Charset)
			}
		}
	}
	if p.handle.Client != "" {
		prefix = append(prefix, "-c", p.handle.Client)
	}
	return append(prefix, args...)
}
func (p *Provider) p4Raw(input []byte, args ...string) ([]byte, error) {
	return p.runner.RunRaw(p.configRoot, "p4", input, p.p4Args(args...)...)
}
func (p *Provider) p4JSON(args ...string) ([]map[string]string, error) {
	body, commandErr := p.p4Raw(nil, append([]string{"-ztag", "-Mj"}, args...)...)
	if commandErr != nil && len(body) == 0 {
		return nil, commandErr
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	var records []map[string]string
	empty := false
	for {
		var raw map[string]any
		if err := decoder.Decode(&raw); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		record := map[string]string{}
		for key, value := range raw {
			record[key] = fmt.Sprint(value)
		}
		if record["code"] == "error" || record["severity"] == "3" || record["severityformat"] == "error" {
			if record["generic"] == "17" {
				empty = true
				continue
			}
			return nil, fmt.Errorf("p4: %s", record["data"])
		}
		if record["code"] != "info" {
			records = append(records, record)
		}
	}
	if empty && len(records) == 0 {
		return nil, nil
	}
	return records, commandErr
}
func p4List(record map[string]string, prefix string) []string {
	var values []string
	for i := 0; ; i++ {
		value, ok := record[prefix+strconv.Itoa(i)]
		if !ok {
			break
		}
		values = append(values, value)
	}
	return values
}
func viewFields(row string) []string {
	var fields []string
	var value strings.Builder
	quoted := false
	for _, r := range row {
		if r == '"' {
			quoted = !quoted
			continue
		}
		if (r == ' ' || r == '\t') && !quoted {
			if value.Len() > 0 {
				fields = append(fields, value.String())
				value.Reset()
			}
		} else {
			value.WriteRune(r)
		}
	}
	if value.Len() > 0 {
		fields = append(fields, value.String())
	}
	return fields
}
func viewRow(left, right string) string { return strconv.Quote(left) + " " + strconv.Quote(right) }
func normalizeP4View(view []string, client string, h WorkspaceHandle) []string {
	var result []string
	for _, row := range view {
		fields := viewFields(row)
		if len(fields) != 2 {
			result = append(result, row)
			continue
		}
		left := fields[0]
		if h.Depot != "" {
			marker := ""
			if strings.ContainsAny(left[:1], "-+&") {
				marker, left = left[:1], left[1:]
			}
			if strings.HasPrefix(left, h.Depot+"/") {
				origin := "//"
				if h.Stream != "" {
					source, _ := decodeNative(h.Source)
					origin = source.Stream + "/"
				}
				left = origin + strings.TrimPrefix(left, h.Depot+"/")
			}
			left = marker + left
		}
		right := strings.Replace(fields[1], "//"+client+"/", "//project/", 1)
		result = append(result, viewRow(left, right))
	}
	return result
}
func (p *Provider) p4Spec() (map[string]string, error) {
	client := p.handle.Client
	if client == "" {
		records, err := p.p4JSON("info")
		if err != nil {
			return nil, err
		}
		if len(records) != 1 {
			return nil, fmt.Errorf("P4 info is not unique")
		}
		client = records[0]["clientName"]
	}
	records, err := p.p4JSON("client", "-o", client)
	if err != nil {
		return nil, err
	}
	if len(records) != 1 {
		return nil, fmt.Errorf("P4 client spec is not unique")
	}
	return records[0], nil
}
func (p *Provider) p4Resolve(revision string) (string, error) {
	return p.p4ResolveAt(revision, p.root)
}

func (p *Provider) p4ResolveAt(revision, workspace string) (string, error) {
	info, err := p.p4JSON("info")
	if err != nil {
		return "", err
	}
	if len(info) != 1 {
		return "", fmt.Errorf("P4 server identity is not unique")
	}
	spec, err := p.p4Spec()
	if err != nil {
		return "", err
	}
	if !SamePath(spec["Root"], workspace) {
		return "", fmt.Errorf("P4 client root differs from bound workspace")
	}
	client := spec["Client"]
	view := p4List(spec, "View")
	canonical := normalizeP4View(view, client, p.handle)
	if p.handle.Source != "" {
		source, err := decodeNative(p.handle.Source)
		if err != nil {
			return "", err
		}
		if spec["Stream"] != "" {
			for i, row := range canonical {
				fields := viewFields(row)
				if len(fields) == 2 {
					fields[0] = strings.Replace(fields[0], spec["Stream"]+"/", source.Stream+"/", 1)
					canonical[i] = viewRow(fields[0], fields[1])
				}
			}
		}
	}
	rev := 0
	if revision != "" {
		rev, err = strconv.Atoi(revision)
		if err != nil {
			return "", err
		}
	} else {
		// cstat orders native changes and includes deleted revisions. Only the
		// have prefix selects complete material, not a historical sync cursor:
		// absent paths can make later removals have with equivalent material.
		changes, err := p.p4JSON("cstat", "//"+client+"/...")
		if err != nil {
			return "", err
		}
		for _, change := range changes {
			if change["status"] != "have" {
				break
			}
			synced, err := strconv.Atoi(change["change"])
			if err != nil {
				return "", err
			}
			rev = max(rev, synced)
		}
	}
	if rev < 1 {
		return "", fmt.Errorf("P4 workspace has no submitted snapshot")
	}
	repository := info[0]["serverID"]
	if repository == "" {
		repository = info[0]["serverAddress"]
	}
	identity := nativeIdentity{Provider: "p4", Repository: repository, Scope: hashValue(view), Revision: rev, Address: info[0]["serverAddress"], User: info[0]["userName"], Client: client, View: canonical, ActualView: view, LineEnd: spec["LineEnd"], Charset: info[0]["clientCharset"], Stream: spec["Stream"], Depot: p.handle.Depot}
	if identity.Stream != "" {
		stream, err := p.p4JSON("stream", "-o", identity.Stream)
		if err != nil {
			return "", err
		}
		if len(stream) != 1 {
			return "", fmt.Errorf("P4 stream spec is not unique")
		}
		semantic := map[string]any{}
		for _, field := range []string{"ParentView", "Paths", "Remapped", "Ignored"} {
			semantic[field] = p4List(stream[0], field)
			if stream[0][field] != "" {
				semantic[field] = stream[0][field]
			}
		}
		encoded, _ := json.Marshal(semantic)
		identity.StreamSpec = string(encoded)
	}
	files, err := p.p4JSON("fstat", "-Om", "-Op", "-Ol", "//"+client+"/...@"+strconv.Itoa(rev))
	if err != nil {
		return "", err
	}
	for _, file := range files {
		if file["depotFile"] == "" {
			continue
		}
		logical := strings.TrimPrefix(file["clientFile"], "//"+client+"/")
		if logical == file["clientFile"] {
			relative, err := filepath.Rel(p.root, file["clientFile"])
			if err != nil {
				return "", err
			}
			logical = filepath.ToSlash(relative)
		}
		origin := file["depotFile"]
		if p.handle.Depot != "" && strings.HasPrefix(origin, p.handle.Depot+"/") {
			if p.handle.Stream != "" {
				source, _ := decodeNative(p.handle.Source)
				origin = source.Stream + "/" + strings.TrimPrefix(origin, p.handle.Depot+"/")
			} else {
				origin = "//" + strings.TrimPrefix(origin, p.handle.Depot+"/")
			}
		}
		identity.Files = append(identity.Files, nativeAddress{Depot: file["depotFile"], Path: logical, Origin: origin})
	}
	slices.SortFunc(identity.Files, func(a, b nativeAddress) int { return strings.Compare(a.Depot, b.Depot) })
	return encodeNative(identity), nil
}
func (p *Provider) p4IdentityJSON(id nativeIdentity, args ...string) ([]map[string]string, error) {
	bound := *p
	bound.handle.Source = encodeNative(id)
	return bound.p4JSON(args...)
}
func (p *Provider) p4Entries(id nativeIdentity) (map[string]string, error) {
	entries := map[string]string{".": entryValue(nativeEntry{Kind: "view", View: id.View, LineEnd: id.LineEnd, Charset: id.Charset, StreamSpec: id.StreamSpec})}
	for _, address := range id.Files {
		records, err := p.p4IdentityJSON(id, "files", address.Depot+"@"+strconv.Itoa(id.Revision))
		if err != nil {
			return nil, err
		}
		if len(records) != 1 {
			return nil, fmt.Errorf("P4 immutable file address is not unique")
		}
		file := records[0]
		if file["action"] == "delete" || file["action"] == "move/delete" {
			continue
		}
		bound := *p
		bound.handle.Source = encodeNative(id)
		body, err := bound.p4Raw(nil, "print", "-q", "-K", address.Depot+"#"+file["rev"])
		if err != nil {
			return nil, err
		}

		value := nativeEntry{Kind: "file", Content: body, Filetype: file["type"], Origin: address.Origin}
		if prior, exists := entries[address.Path]; exists {
			var aggregate nativeEntry
			if err = json.Unmarshal([]byte(prior), &aggregate); err != nil {
				return nil, err
			}
			if len(aggregate.Layers) == 0 {
				aggregate = nativeEntry{Kind: "file", Layers: []nativeEntry{aggregate}}
			}
			aggregate.Layers = append(aggregate.Layers, value)
			entries[address.Path] = entryValue(aggregate)
		} else {
			entries[address.Path] = entryValue(value)
		}

	}
	return entries, nil
}
func (p *Provider) p4ChangedPaths() ([]string, error) {
	records, err := p.p4JSON("reconcile", "-n", "//"+p.p4Client()+"/...")
	if err != nil {
		return nil, err
	}
	opened, err := p.p4JSON("opened", "//"+p.p4Client()+"/...")
	if err != nil {
		return nil, err
	}
	records = append(records, opened...)
	var paths []string
	for _, record := range records {
		native := record["clientFile"]
		if native == "" {
			continue
		}
		logical := strings.TrimPrefix(native, "//"+p.p4Client()+"/")
		if filepath.IsAbs(native) && !strings.HasPrefix(native, "//") {
			relative, e := filepath.Rel(p.root, native)
			if e != nil {
				return nil, e
			}
			logical = filepath.ToSlash(relative)
		}
		if !p.isRuntimeStatusPath(logical) || record["change"] != "" {
			paths = append(paths, logical)
		}
	}
	slices.Sort(paths)
	return slices.Compact(paths), nil
}
func (p *Provider) p4Client() string {
	if p.handle.Client != "" {
		return p.handle.Client
	}
	spec, err := p.p4Spec()
	if err != nil {
		return ""
	}
	return spec["Client"]
}
func (p *Provider) p4Status() (string, error) {
	paths, err := p.p4ChangedPaths()
	return strings.Join(paths, "\n"), err
}
func (p *Provider) p4Track(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	args := []string{"reconcile"}
	for _, path := range paths {
		args = append(args, materialPath(p.root, path))
	}
	_, err := p.p4Raw(nil, args...)
	return err
}
func (p *Provider) p4Commit(message string) error {
	pending, err := p.p4JSON("changes", "-l", "-s", "pending", "-c", p.p4Client())
	if err != nil {
		return err
	}
	submitted := false
	for _, change := range pending {
		if strings.TrimSuffix(change["desc"], "\n") == message {
			if _, err = p.p4Raw(nil, "submit", "-c", change["change"]); err != nil {
				return err
			}
			submitted = true
			break
		}
	}
	if !submitted {
		if _, err = p.p4Raw(nil, "submit", "-d", message); err != nil {
			return err
		}
	}
	changes, err := p.p4JSON("changes", "-l", "-s", "submitted", "-c", p.p4Client(), "//"+p.p4Client()+"/...")
	if err != nil {
		return err
	}
	for _, change := range changes {
		if strings.TrimSuffix(change["desc"], "\n") == message {
			_, err = p.p4Raw(nil, "sync", "-f", "//"+p.p4Client()+"/...@"+change["change"])
			return err
		}
	}
	return fmt.Errorf("P4 submit has no exact operation change")
}
func (p *Provider) p4Message(id nativeIdentity) (string, error) {
	records, err := p.p4IdentityJSON(id, "describe", "-s", strconv.Itoa(id.Revision))
	if err != nil {
		return "", err
	}
	if len(records) != 1 || records[0]["status"] != "submitted" {
		return "", fmt.Errorf("P4 identity is not a submitted change")
	}
	return strings.TrimSuffix(records[0]["desc"], "\n"), nil
}
func (p *Provider) p4Ancestor(a, b nativeIdentity) error {
	if a.Revision > b.Revision {
		return fmt.Errorf("P4 source revision is beyond descendant")
	}
	for _, address := range a.Files {
		sources, err := p.p4IdentityJSON(a, "files", address.Depot+"@"+strconv.Itoa(a.Revision))
		if err != nil {
			return err
		}
		if len(sources) != 1 {
			return fmt.Errorf("P4 ancestry source file is not unique")
		}
		sourceRevision, err := strconv.Atoi(sources[0]["rev"])
		if err != nil {
			return err
		}
		found := false
		for _, target := range b.Files {
			if address.Origin != target.Origin {
				continue
			}
			records, err := p.p4IdentityJSON(b, "filelog", "-i", target.Depot+"@"+strconv.Itoa(b.Revision))
			if err != nil {
				return err
			}
			for _, record := range records {
				if record["depotFile"] == address.Depot {
					for key, value := range record {
						if strings.HasPrefix(key, "rev") {
							revision, _ := strconv.Atoi(value)
							found = found || revision >= sourceRevision
						}
					}
				}
				for key, value := range record {
					if strings.HasPrefix(key, "file") && value == address.Depot {
						end, err := strconv.Atoi(strings.TrimPrefix(record["erev"+strings.TrimPrefix(key, "file")], "#"))
						if err == nil && end >= sourceRevision {
							found = true
						}
					}
				}
			}
		}
		if !found {
			return fmt.Errorf("P4 snapshot lacks required native file lineage: %s", address.Path)
		}
	}
	if len(a.Files) == 0 && a.Scope != b.Scope {
		return fmt.Errorf("P4 empty snapshots have no unique cross-scope lineage")
	}
	return nil
}
func (p *Provider) p4Count(a, b nativeIdentity) (int, error) {
	if a.Scope != b.Scope {
		return 0, fmt.Errorf("P4 count requires one View scope")
	}
	records, err := p.p4IdentityJSON(b, "changes", "-s", "submitted", "//"+b.Client+"/...@"+strconv.Itoa(a.Revision+1)+","+strconv.Itoa(b.Revision))
	return len(records), err
}
func formFields(record map[string]string, keys []string) string {
	var form strings.Builder
	for _, key := range keys {
		if value := record[key]; value != "" {
			form.WriteString(key + ": " + value + "\n")
		}
	}
	return form.String()
}
func (p *Provider) p4WorkspaceExists(workspace string, h WorkspaceHandle) (bool, error) {
	records, err := p.p4JSON("clients", "-e", h.Client)
	if err != nil {
		return false, err
	}
	if !slices.ContainsFunc(records, func(r map[string]string) bool { return r["client"] == h.Client }) {
		return false, nil
	}
	spec, err := p.At(workspace, h).p4Spec()
	if err != nil {
		return false, err
	}
	if !SamePath(spec["Root"], workspace) {
		return false, fmt.Errorf("P4 registered client root differs")
	}
	if _, err := os.Stat(workspace); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}
func (p *Provider) p4WorkspaceRevision(h WorkspaceHandle, message string) (int, error) {
	changes, err := p.p4JSON("changes", "-l", "-s", "submitted", h.Depot+"/...")
	if err != nil {
		return 0, err
	}
	revision := 0
	for _, change := range changes {
		if strings.TrimSuffix(change["desc"], "\n") == message {
			if revision != 0 {
				return 0, fmt.Errorf("multiple P4 changes match one workspace operation")
			}
			revision, err = strconv.Atoi(change["change"])
			if err != nil {
				return 0, err
			}
		}
	}
	return revision, nil
}

func (p *Provider) p4CreateWorkspace(workspace string, id nativeIdentity, message string, h WorkspaceHandle) error {
	bound := p.At(workspace, h)
	original, err := p.p4IdentityJSON(id, "client", "-o", id.Client)
	if err != nil {
		return err
	}
	if len(original) != 1 {
		return fmt.Errorf("P4 source client spec is not unique")
	}
	sourceSpec := map[string]string{"Client": h.Client, "Owner": id.User, "Root": workspace, "LineEnd": id.LineEnd, "Options": original[0]["Options"], "SubmitOptions": original[0]["SubmitOptions"]}
	// Workers edit files directly; reconcile opens only the declared delivery.
	// Native allwrite keeps synced files editable without changing their type.
	sourceSpec["Options"] = strings.Replace(sourceSpec["Options"], "noallwrite", "allwrite", 1)
	var view, branchView []string
	if h.Stream != "" {
		template, err := p.p4IdentityJSON(id, "stream", "-o", id.Stream)
		if err != nil {
			return err
		}
		if len(template) != 1 {
			return fmt.Errorf("P4 parent stream spec is not unique")
		}
		spec := template[0]
		form := "Stream: " + h.Stream + "\nOwner: " + id.User + "\nName: " + h.Client + "\nParent: " + id.Stream + "\nType: task\nParentView: " + spec["ParentView"] + "\n"
		for _, key := range []string{"Paths", "Remapped", "Ignored"} {
			values := p4List(spec, key)
			if len(values) > 0 {
				form += key + ":\n"
				for _, line := range values {
					form += "\t" + line + "\n"
				}
			}
		}
		if _, err = bound.p4Raw([]byte(form), "stream", "-i"); err != nil {
			return err
		}
		sourceSpec["Stream"] = h.Stream
	} else {
		for viewIndex, row := range id.ActualView {
			fields := viewFields(row)
			if len(fields) != 2 {
				return fmt.Errorf("P4 View row is incomplete")
			}
			marker := ""
			left := fields[0]
			if strings.ContainsAny(left[:1], "-+&") {
				marker, left = left[:1], left[1:]
			}
			canonical := viewFields(id.View[viewIndex])
			if len(canonical) != 2 {
				return fmt.Errorf("P4 canonical View row is incomplete")
			}
			canonicalLeft := strings.TrimLeft(canonical[0], "-+&")
			target := h.Depot + "/" + strings.TrimPrefix(canonicalLeft, "//")
			view = append(view, viewRow(marker+target, strings.Replace(fields[1], "//"+id.Client+"/", "//"+h.Client+"/", 1)))
			branchMarker := marker
			if marker == "+" || marker == "&" {
				branchMarker = ""
			}
			branchView = append(branchView, viewRow(branchMarker+left, target))
		}
		form := "Branch: " + h.Branch + "\nOwner: " + id.User + "\nView:\n"
		for _, row := range branchView {
			form += "\t" + row + "\n"
		}
		if _, err := bound.p4Raw([]byte(form), "branch", "-i"); err != nil {
			return err
		}
	}
	form := formFields(sourceSpec, []string{"Client", "Owner", "Root", "LineEnd", "Options", "SubmitOptions", "Stream"})
	if len(view) > 0 {
		form += "View:\n"
		for _, row := range view {
			form += "\t" + row + "\n"
		}
	}
	if _, err = bound.p4Raw([]byte(form), "client", "-i"); err != nil {
		return err
	}
	created, err := bound.p4WorkspaceRevision(h, message)
	if err != nil {
		return err
	}
	if created == 0 {
		args := []string{"populate", "-d", message}
		if h.Stream != "" {
			args = append(args, "-r", "-S", h.Stream, h.Depot+"/...@"+strconv.Itoa(id.Revision))
		} else {
			args = append(args, "-b", h.Branch, h.Depot+"/...@"+strconv.Itoa(id.Revision))
		}
		if _, err = bound.p4Raw(nil, args...); err != nil {
			return err
		}
		changes, err := bound.p4JSON("changes", "-s", "submitted", "-m", "1", h.Depot+"/...")
		if err != nil {
			return err
		}
		if len(changes) != 1 {
			return fmt.Errorf("P4 populated change is not unique")
		}
		created, _ = strconv.Atoi(changes[0]["change"])
	}
	if err = os.MkdirAll(workspace, 0755); err != nil {
		return err
	}
	_, err = bound.p4Raw(nil, "sync", "//"+h.Client+"/...@"+strconv.Itoa(created))
	return err
}
func (p *Provider) p4WorkspaceAbsent(workspace string, h WorkspaceHandle) (bool, error) {
	clients, err := p.p4JSON("clients", "-e", h.Client)
	if err != nil {
		return false, err
	}
	if slices.ContainsFunc(clients, func(r map[string]string) bool { return r["client"] == h.Client }) {
		return false, nil
	}
	branches, err := p.p4JSON("branches", "-e", h.Branch)
	if err != nil {
		return false, err
	}
	if slices.ContainsFunc(branches, func(r map[string]string) bool { return r["branch"] == h.Branch }) {
		return false, nil
	}
	if h.Stream != "" {
		streams, err := p.p4JSON("streams", h.Stream)
		if err != nil {
			return false, err
		}
		if len(streams) != 0 {
			return false, nil
		}
	}
	_, err = os.Stat(workspace)
	return os.IsNotExist(err), nil
}
func (p *Provider) p4CleanupWorkspace(workspace, message string, h WorkspaceHandle) error {
	bound := p.At(workspace, h)
	clients, err := bound.p4JSON("clients", "-e", h.Client)
	if err != nil {
		return err
	}
	exists := slices.ContainsFunc(clients, func(r map[string]string) bool { return r["client"] == h.Client })
	if exists {
		spec, err := bound.p4Spec()
		if err != nil {
			return err
		}
		if !SamePath(spec["Root"], workspace) {
			return fmt.Errorf("cleanup client differs from registered root")
		}
		if _, err = bound.p4Raw(nil, "revert", "//"+h.Client+"/..."); err != nil {
			return err
		}
		pending, err := bound.p4JSON("changes", "-s", "pending", "-c", h.Client)
		if err != nil {
			return err
		}
		for _, change := range pending {
			_, err = bound.p4Raw(nil, "shelve", "-d", "-c", change["change"])
			if err != nil && !strings.Contains(err.Error(), "No shelved files") {
				return err
			}
			if _, err = bound.p4Raw(nil, "change", "-d", change["change"]); err != nil {
				return err
			}
		}
		files, err := bound.p4JSON("files", h.Depot+"/...")
		if err != nil {
			return err
		}
		active := false
		for _, file := range files {
			active = active || file["action"] != "delete" && file["action"] != "move/delete"
		}
		if active {
			if _, err = bound.p4Raw(nil, "delete", h.Depot+"/..."); err != nil {
				return err
			}
			if err = bound.p4Commit(message); err != nil {
				return err
			}
		}
		if _, err = bound.p4Raw(nil, "sync", "//"+h.Client+"/...#none"); err != nil {
			return err
		}
		if _, err = bound.p4Raw(nil, "client", "-d", h.Client); err != nil {
			return err
		}
	}
	if h.Stream != "" {
		streams, err := bound.p4JSON("streams", h.Stream)
		if err != nil {
			return err
		}
		if len(streams) > 0 {
			if _, err = bound.p4Raw(nil, "stream", "-d", h.Stream); err != nil {
				return err
			}
		}
	} else {
		branches, err := bound.p4JSON("branches", "-e", h.Branch)
		if err != nil {
			return err
		}
		if len(branches) > 0 {
			if _, err = bound.p4Raw(nil, "branch", "-d", h.Branch); err != nil {
				return err
			}
		}
	}
	return os.RemoveAll(workspace)
}
func (p *Provider) p4Apply(id nativeIdentity, paths []string) error {
	entries, err := p.p4Entries(id)
	if err != nil {
		return err
	}
	currentIdentity, err := p.Resolve()
	if err != nil {
		return err
	}
	current, err := decodeNative(currentIdentity)
	if err != nil {
		return err
	}
	for _, key := range paths {
		if key == "." {
			if hashValue(id.View) != hashValue(current.View) || id.LineEnd != current.LineEnd || id.Charset != current.Charset || id.StreamSpec != current.StreamSpec {
				return fmt.Errorf("P4 promotion changes the bound View or translation")
			}
			continue
		}
		var value nativeEntry
		if encoded := entries[key]; encoded != "" {
			if err = json.Unmarshal([]byte(encoded), &value); err != nil {
				return err
			}
		}
		wanted := value.Layers
		if len(wanted) == 0 && value.Kind != "" {
			wanted = []nativeEntry{value}
		}
		for _, layer := range wanted {
			position := slices.IndexFunc(id.Files, func(a nativeAddress) bool { return a.Path == key && a.Origin == layer.Origin })
			if position < 0 {
				return fmt.Errorf("P4 artifact layer has no immutable native address")
			}
			source := id.Files[position]
			target := p.p4TargetAddress(source.Origin)
			if _, err = p.p4Raw(nil, "copy", "-v", source.Depot+"@"+strconv.Itoa(id.Revision), target); err != nil {
				return err
			}
		}
		for _, address := range current.Files {
			if address.Path != key {
				continue
			}
			if !slices.ContainsFunc(wanted, func(e nativeEntry) bool { return e.Origin == address.Origin }) {
				if _, err = p.p4Raw(nil, "delete", "-v", address.Depot); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func (p *Provider) p4TargetAddress(origin string) string {
	if p.handle.Depot == "" {
		return origin
	}
	if p.handle.Stream != "" {
		source, err := decodeNative(p.handle.Source)
		if err == nil {
			return p.handle.Depot + "/" + strings.TrimPrefix(origin, source.Stream+"/")
		}
	}
	return p.handle.Depot + "/" + strings.TrimPrefix(origin, "//")
}

func (p *Provider) p4Index() ([]IndexEntry, error) {
	identity, err := p.Resolve()
	if err != nil {
		return nil, err
	}
	entries, err := p.TreeEntries(identity)
	if err != nil {
		return nil, err
	}
	opened, err := p.p4JSON("fstat", "-Or", "-Op", "//"+p.p4Client()+"/...")
	if err != nil {
		return nil, err
	}
	conflicts := map[string]string{}
	for _, file := range opened {
		logical := strings.TrimPrefix(file["clientFile"], "//"+p.p4Client()+"/")
		if file["action"] == "delete" || file["action"] == "move/delete" {
			delete(entries, logical)
			continue
		}
		if file["action"] != "" {
			body, err := workingBytes(materialPath(p.root, logical))
			if err != nil && !os.IsNotExist(err) {
				return nil, err
			}
			entries[logical] = entryValue(nativeEntry{Kind: "file", Content: body, Filetype: file["type"], Origin: p.p4Origin(file["depotFile"])})
		}
		var resolve map[string]string = map[string]string{}
		for key, value := range file {
			if strings.HasPrefix(key, "resolve") {
				resolve[key] = value
			}
		}
		if file["action"] != "" {
			var value nativeEntry
			_ = json.Unmarshal([]byte(entries[logical]), &value)
			value.Resolve = resolve
			value.Action = file["action"]
			entries[logical] = entryValue(value)
		}
		if _, unresolved := file["unresolved"]; unresolved {
			conflicts[logical] = hashValue(resolve)
		}
	}
	var result []IndexEntry
	for key, value := range entries {
		stage := 0
		if conflicts[key] != "" {
			stage = 1
		}
		result = append(result, IndexEntry{Path: key, Mode: entryMode(value), Object: entryObject(value), Metadata: value, Stage: stage, Conflict: conflicts[key]})
	}
	slices.SortFunc(result, func(a, b IndexEntry) int { return strings.Compare(a.Path, b.Path) })
	return result, nil
}
func (p *Provider) p4Pending(base, expected string) (Pending, error) {
	paths, err := p.p4ChangedPaths()
	if err != nil {
		return Pending{}, err
	}
	entries, err := p.p4Index()
	if err != nil {
		return Pending{}, err
	}
	result := Pending{Fingerprint: hashValue(entries), Clean: len(paths) == 0, Tracked: len(paths) > 0}
	baseEntries, err := p.TreeEntries(base)
	if err != nil {
		return result, err
	}
	current := map[string]string{}
	for _, entry := range entries {
		current[entry.Path] = entry.Metadata
	}
	for _, change := range entryChanges(baseEntries, current) {
		if slices.Contains(paths, change.Path) {
			result.Changes = append(result.Changes, change)
		}
	}

	if expected != "" {
		id, err := decodeNative(expected)
		if err != nil {
			return result, err
		}
		want, err := p.TreeEntries(expected)
		if err != nil {
			return result, err
		}
		expectedChanges := entryChanges(baseEntries, want)
		opened, err := p.p4JSON("fstat", "-Or", "-Op", "//"+p.p4Client()+"/...")
		if err != nil {
			return result, err
		}
		equal := true
		prefix := true
		for _, change := range expectedChanges {
			if change.Path == "." {
				currentIdentity, err := p.Resolve()
				if err != nil {
					return result, err
				}
				currentID, _ := decodeNative(currentIdentity)
				equal = equal && hashValue(currentID.View) == hashValue(id.View) && currentID.LineEnd == id.LineEnd && currentID.Charset == id.Charset && currentID.StreamSpec == id.StreamSpec
				continue
			}
			var value nativeEntry
			if change.Metadata != "" {
				_ = json.Unmarshal([]byte(change.Metadata), &value)
			}
			layers := value.Layers
			if len(layers) == 0 && value.Kind != "" {
				layers = []nativeEntry{value}
			}
			for _, layer := range layers {
				position := slices.IndexFunc(id.Files, func(a nativeAddress) bool { return a.Path == change.Path && a.Origin == layer.Origin })
				if position < 0 {
					return result, fmt.Errorf("P4 expected layer has no native source")
				}
				source := id.Files[position]
				target := p.p4TargetAddress(source.Origin)
				recordPosition := slices.IndexFunc(opened, func(r map[string]string) bool { return r["depotFile"] == target && r["action"] != "" })
				if recordPosition < 0 {
					equal = false
					continue
				}
				record := opened[recordPosition]
				equal = equal && record["type"] == layer.Filetype
				proven := false
				for key, address := range record {
					if strings.HasPrefix(key, "resolveFromFile") && address == source.Depot {
						end, _ := strconv.Atoi(record["resolveEndFromRev"+strings.TrimPrefix(key, "resolveFromFile")])
						files, err := p.p4IdentityJSON(id, "files", source.Depot+"@"+strconv.Itoa(id.Revision))
						if err != nil {
							return result, err
						}
						if len(files) == 1 {
							revision, _ := strconv.Atoi(files[0]["rev"])
							action := record["resolveAction"+strings.TrimPrefix(key, "resolveFromFile")]
							proven = proven || end == revision && (action == "copy from" || action == "branch from")
						}
					}
				}
				equal = equal && proven
			}
			if change.Mode == "000000" {
				equal = equal && slices.ContainsFunc(opened, func(r map[string]string) bool {
					return strings.TrimPrefix(r["clientFile"], "//"+p.p4Client()+"/") == change.Path && (r["action"] == "delete" || r["action"] == "move/delete")
				})
			}
		}
		for _, change := range result.Changes {
			if !slices.ContainsFunc(expectedChanges, func(c Change) bool { return c.Path == change.Path }) {
				equal = false
			}
		}
		for _, record := range opened {
			if record["action"] == "" {
				continue
			}
			logical := strings.TrimPrefix(record["clientFile"], "//"+p.p4Client()+"/")
			if !slices.ContainsFunc(expectedChanges, func(c Change) bool { return c.Path == logical }) {
				prefix = false
			}
		}
		result.Equal = equal
		result.Prefix = prefix
	}

	return result, nil
}
func (p *Provider) p4Merge(incoming nativeIdentity) error {
	spec, err := p.p4Spec()
	if err != nil {
		return err
	}
	if incoming.Stream != "" && spec["Stream"] != "" {
		if _, err = p.p4JSON("integrate", "-S", incoming.Stream, "//"+p.p4Client()+"/...@"+strconv.Itoa(incoming.Revision)); err != nil {
			return err
		}
	} else {
		base, err := p.Resolve()
		if err != nil {
			return err
		}
		target, _ := decodeNative(base)
		for _, address := range incoming.Files {
			position := slices.IndexFunc(target.Files, func(a nativeAddress) bool { return a.Origin == address.Origin })
			targetPath := materialPath(p.root, address.Path)
			if position >= 0 {
				targetPath = target.Files[position].Depot
			}
			if _, err = p.p4JSON("integrate", address.Depot+"@"+strconv.Itoa(incoming.Revision), targetPath); err != nil {
				return err
			}
		}
	}
	_, err = p.p4JSON("resolve", "-am")
	return err
}
func (p *Provider) p4ObserveIntegration(base, incoming string) (IntegrationObservation, error) {
	id, err := decodeNative(incoming)
	if err != nil {
		return IntegrationObservation{}, err
	}
	entries, err := p.p4Index()
	if err != nil {
		return IntegrationObservation{}, err
	}
	records, err := p.p4JSON("fstat", "-Or", "//"+p.p4Client()+"/...")
	if err != nil {
		return IntegrationObservation{}, err
	}
	started := false
	for _, record := range records {
		for key, value := range record {
			if strings.HasPrefix(key, "resolveFromFile") {
				started = started || slices.ContainsFunc(id.Files, func(a nativeAddress) bool { return a.Depot == value })
			}
		}

	}
	return IntegrationObservation{Started: started, Lineage: hashValue(records), Entries: entries}, nil
}
func (p *Provider) p4ResolveConflicts(paths []string) error {
	for _, path := range paths {
		// A semantic edit must remain an edited integration result. Native edit
		// preserves the pending source records and accepts the workspace bytes.
		if _, err := p.p4Raw(nil, "edit", materialPath(p.root, path)); err != nil {
			return err
		}
		if _, err := p.p4Raw(nil, "resolve", "-e", "-ay", materialPath(p.root, path)); err != nil {
			return err
		}
	}
	return nil
}

func (p *Provider) p4Origin(address string) string {
	if p.handle.Depot == "" || !strings.HasPrefix(address, p.handle.Depot+"/") {
		return address
	}
	if p.handle.Stream != "" {
		source, err := decodeNative(p.handle.Source)
		if err == nil {
			return source.Stream + "/" + strings.TrimPrefix(address, p.handle.Depot+"/")
		}
	}
	return "//" + strings.TrimPrefix(address, p.handle.Depot+"/")
}
