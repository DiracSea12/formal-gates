package validate

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"gopkg.in/yaml.v3"
)

// DeepSeek Harness (DSH) 没有内置的 hooks.json 协议。安装器改用一个最小 Cordis
// 插件：插件在 tools/pre-execute 上调用原生 `formal-gates hook decide`，并把
// subagent/start、subagent/end 转发给 `formal-gates lifecycle capture`。
// 插件文件写在已安装的 skill 目录内，home 级 `cordis.patch.yml` 用绝对 file URL
// 引用它。DSH 的 home patch 会以所选 profile 目录为解析基准，相对路径会错误地解析
// 成 `$DSH_HOME/profiles/<profile>/skills/...`，因此不能使用相对 specifier。
//
// DSH 的 cordis.patch.yml 是机器级（home 级）补丁层，项目目录下的
// cordis.patch.yml 不会被 DSH 自动加载。因此 `--host dsh --scope project`
// 只安装 skill 与宿主指令文件，不写 hook 补丁；需要 hook 集成时安装 global。

const (
	dshPluginEntryID = "formal-gates-dsh"
	dshPluginFileRel = "plugin/formal-gates.mjs"
	dshPatchFileName = "cordis.patch.yml"
)

//go:embed dsh_plugin.mjs
var dshPluginSource string

// dshInstallTargetPaths owns every DSH path decision for one platform here, so
// host-specific switches in install.go stay small: global resolves $DSH_HOME
// (else ~/.dsh), project uses <project>/.dsh. Project scope has no hook config
// because DSH only auto-loads the home-level cordis.patch.yml.
func dshInstallTargetPaths(home, project, scope string) (base, hookConfig, managedRulePath string, err error) {
	if scope == "global" {
		dshHome, err := dshInstallHome(home)
		if err != nil {
			return "", "", "", err
		}
		return filepath.Join(dshHome, "skills"), filepath.Join(dshHome, dshPatchFileName), filepath.Join(dshHome, "AGENTS.md"), nil
	}
	return filepath.Join(project, ".dsh", "skills"), "", filepath.Join(project, "AGENTS.md"), nil
}

// dshInstallHome resolves the DeepSeek Harness home the same way DSH does:
// explicit $DSH_HOME wins, otherwise ~/.dsh.
func dshInstallHome(fallbackHome string) (string, error) {
	if value := strings.TrimSpace(os.Getenv("DSH_HOME")); value != "" {
		abs, err := filepath.Abs(value)
		if err != nil {
			return "", err
		}
		return filepath.Clean(abs), nil
	}
	return filepath.Clean(filepath.Join(fallbackHome, ".dsh")), nil
}

func dshPluginPath(target installTarget) string {
	return filepath.Join(target.targetPath, filepath.FromSlash(dshPluginFileRel))
}

// dshPluginSpecifier returns the loader specifier DSH resolves from any profile
// directory. Windows uses file:///C:/..., POSIX uses file:///home/....
func dshPluginSpecifier(target installTarget) string {
	path := filepath.ToSlash(dshPluginPath(target))
	if runtime.GOOS == "windows" {
		return "file:///" + strings.TrimPrefix(path, "/")
	}
	return "file://" + path
}

func configureDshHook(target installTarget) error {
	if strings.TrimSpace(target.hookConfig) == "" {
		return nil
	}
	binary := filepath.Join(target.targetPath, filepath.FromSlash("bin"), nativeBinaryName())
	if !isFile(binary) {
		return fmt.Errorf("formal-gates native binary is missing at %s", binary)
	}
	if err := os.MkdirAll(filepath.Dir(dshPluginPath(target)), 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(dshPluginPath(target), []byte(dshPluginSource), 0o600); err != nil {
		return err
	}
	doc, err := readDshPatch(target.hookConfig)
	if err != nil {
		return err
	}
	seq := doc.Content[0]
	// DSH 的 home cordis.patch.yml 是 PatchOptions 列表：新插件行必须包在
	// `- insert:` 下，否则会被当作"找不到目标 id 的 patch"跳过。这里同时收敛
	// 旧版/重复条目，并保留用户已有的其他 patch。id 是安装器自有行，重复安装
	// 直接替换该行（含旧版相对路径 specifier 的迁移）。
	specifier := dshPluginSpecifier(target)
	dshHome := dshHomeFromHookConfig(target)
	kept := make([]*yaml.Node, 0, len(seq.Content)+1)
	wrote := false
	for _, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			kept = append(kept, item)
			continue
		}
		if insert := dshMappingNode(item, "insert"); insert != nil && insert.Kind == yaml.SequenceNode {
			rows := make([]*yaml.Node, 0, len(insert.Content)+1)
			for _, row := range insert.Content {
				if id, _ := dshPluginRowIdentity(row); id == dshPluginEntryID {
					if wrote {
						continue
					}
					rows = append(rows, newDshPluginEntry(binary, dshHome, specifier))
					wrote = true
					continue
				}
				rows = append(rows, row)
			}
			if len(rows) == 0 {
				continue
			}
			insert.Content = rows
			kept = append(kept, item)
			continue
		}
		// 迁移裸插件行（旧格式）为 insert patch。
		if id, _ := dshPluginRowIdentity(item); id == dshPluginEntryID {
			if wrote {
				continue
			}
			kept = append(kept, newDshInsertPatch(binary, dshHome, specifier))
			wrote = true
			continue
		}
		kept = append(kept, item)
	}
	if !wrote {
		kept = append(kept, newDshInsertPatch(binary, dshHome, specifier))
	}
	seq.Content = kept
	return writeDshPatch(target.hookConfig, doc)
}

func removeDshHook(target installTarget) error {
	if strings.TrimSpace(target.hookConfig) == "" {
		return nil
	}
	doc, err := readDshPatch(target.hookConfig)
	if err != nil {
		return err
	}
	if !isFile(target.hookConfig) {
		return nil
	}
	seq := doc.Content[0]
	kept := make([]*yaml.Node, 0, len(seq.Content))
	removed := false
	for _, item := range seq.Content {
		if item.Kind != yaml.MappingNode {
			kept = append(kept, item)
			continue
		}
		if insert := dshMappingNode(item, "insert"); insert != nil && insert.Kind == yaml.SequenceNode {
			rows := make([]*yaml.Node, 0, len(insert.Content))
			for _, row := range insert.Content {
				if id, _ := dshPluginRowIdentity(row); id == dshPluginEntryID {
					removed = true
					continue
				}
				rows = append(rows, row)
			}
			if len(rows) == 0 {
				continue
			}
			insert.Content = rows
			kept = append(kept, item)
			continue
		}
		if id, _ := dshPluginRowIdentity(item); id == dshPluginEntryID {
			removed = true
			continue
		}
		kept = append(kept, item)
	}
	if !removed {
		return nil
	}
	seq.Content = kept
	return writeDshPatch(target.hookConfig, doc)
}

func readDshPatch(path string) (*yaml.Node, error) {
	empty := &yaml.Node{
		Kind: yaml.DocumentNode,
		Content: []*yaml.Node{{
			Kind: yaml.SequenceNode,
			Tag:  "!!seq",
		}},
	}
	if !isFile(path) {
		return empty, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return empty, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("existing DSH patch config is not valid YAML; refusing to touch it: %s", path)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("existing DSH patch config must be a top-level YAML sequence; refusing to touch it: %s", path)
	}
	return &doc, nil
}

func writeDshPatch(path string, doc *yaml.Node) error {
	seq := doc.Content[0]
	if len(seq.Content) == 0 {
		// DSH 把空文件/纯注释文件视为解析失败；不存在才是合法的"无补丁"状态。
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if isFile(path) {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path+".bak", data, 0o600); err != nil {
			return err
		}
	}
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(doc); err != nil {
		return err
	}
	if err := encoder.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}

func dshMappingValue(mapping *yaml.Node, key string) string {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return strings.TrimSpace(mapping.Content[i+1].Value)
		}
	}
	return ""
}

func dshMappingNode(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

func dshPluginRowIdentity(row *yaml.Node) (id, name string) {
	if row == nil || row.Kind != yaml.MappingNode {
		return "", ""
	}
	return dshMappingValue(row, "id"), dshMappingValue(row, "name")
}

func newDshPluginEntry(binary, dshHome, specifier string) *yaml.Node {
	config := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	config.Content = yamlPairs("binary", binary, "provider", "deepseek-harness", "dshHome", dshHome)
	config.Content = append(config.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "timeoutMs"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: "30000"})
	entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	entry.Content = yamlPairs("id", dshPluginEntryID, "name", specifier)
	entry.Content = append(entry.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "config"}, config)
	return entry
}

func newDshInsertPatch(binary, dshHome, specifier string) *yaml.Node {
	insert := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	insert.Content = []*yaml.Node{newDshPluginEntry(binary, dshHome, specifier)}
	patch := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	patch.Content = append(patch.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "insert"}, insert)
	return patch
}

func dshHomeFromHookConfig(target installTarget) string {
	return filepath.Dir(target.hookConfig)
}

func yamlPairs(pairs ...string) []*yaml.Node {
	content := make([]*yaml.Node, 0, len(pairs))
	for _, value := range pairs {
		content = append(content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
	}
	return content
}
