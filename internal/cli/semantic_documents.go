package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/requirement"
	"formal-gates/internal/settings"
	"formal-gates/internal/validate"
)

func runRequirement(args []string, streams IO) (int, error) {
	return runRequirementWithSource(args, streams, "")
}

func runRequirementWithSource(args []string, streams IO, requirementSource string) (int, error) {
	command := "show"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("requirement "+command, flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	root := fs.String("root", ".", "project root")
	fs.Int("run", 0, "select current formal run by published position")
	fs.String("run-id", "", "program integration: exact run id")
	path := fs.String("file", "", "new requirement document filename")
	documentPosition := fs.Int("document", 0, "published existing document position")
	item := fs.Int("item", 0, "published requirement position; 0 appends a new item")
	condition := fs.Int("condition", 0, "published acceptance position; 0 appends a new condition")
	title := fs.String("title", "", "new title prose")
	body := fs.String("requirement", "", "new requirement prose")
	acceptance := fs.String("acceptance", "", "new acceptance condition prose")
	source := fs.String("source", "", "new source prose")
	context := fs.String("context", "", "new background prose; omitted retains the original")
	solution := fs.String("solution", "", "new solution prose; omitted retains the original")
	removeCondition := fs.Bool("remove-condition", false, "remove the selected acceptance condition")
	if err := fs.Parse(args); err != nil {
		return 1, err
	}
	if fs.NArg() != 0 {
		return 1, fmt.Errorf("unexpected requirement arguments")
	}
	fullRoot, err := filepath.Abs(*root)
	if err != nil {
		return 1, err
	}
	if *path != "" && command != "append" {
		return 1, fmt.Errorf("--file is only for initial append; select an existing document by its published position")
	}
	if *path != "" && *documentPosition != 0 {
		return 1, fmt.Errorf("choose a new file or an existing document position")
	}
	if *documentPosition != 0 || *path == "" && requirementSource == "" {
		documents, err := requirementDocuments(fullRoot)
		if err != nil {
			return 1, err
		}
		if *documentPosition == 0 && command == "show" {
			return printJSON(streams.Stdout, documents)
		}
		if *documentPosition < 1 || *documentPosition > len(documents) {
			return 1, fmt.Errorf("select a published --document position, or use --file for a new document")
		}
		requirementSource = documents[*documentPosition-1].Path
	}
	target := requirementSource
	if *path != "" {
		target = *path
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(fullRoot, target)
	}
	relative, err := filepath.Rel(fullRoot, target)
	if err != nil || !filepath.IsLocal(relative) || relative == "." {
		return 1, fmt.Errorf("requirement document must be within the project")
	}
	if command == "show" {
		preview, err := facade.PrepareIntake(fullRoot, relative, "", nil, true)
		if err != nil {
			return 1, err
		}
		return printJSON(streams.Stdout, preview)
	}
	if command != "edit" && command != "append" && command != "remove" {
		return 1, fmt.Errorf("requirement command must be show, append, edit or remove")
	}
	data, err := os.ReadFile(target)
	if *path != "" && !os.IsNotExist(err) {
		return 1, fmt.Errorf("--file creates a new document; select the existing document by its published position")
	}
	if err != nil && !(command == "append" && os.IsNotExist(err)) {
		return 1, err
	}
	if command == "append" && *item != 0 {
		return 1, fmt.Errorf("append does not select an existing requirement")
	}
	change := requirement.SemanticChange{Item: *item, Condition: *condition, Remove: command == "remove", RemoveCondition: *removeCondition}
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "title":
			change.Title = title
		case "requirement":
			change.Requirement = body
		case "acceptance":
			change.Acceptance = acceptance
		case "source":
			change.Source = source
		case "context":
			change.Context = context
		case "solution":
			change.Solution = solution
		}
	})

	if command == "remove" && *item == 0 {
		return 1, fmt.Errorf("remove requires --item")
	}
	if command == "edit" && *item == 0 && change.Context == nil && change.Solution == nil {
		return 1, fmt.Errorf("edit requires --item or new --context/--solution prose")
	}
	if len(data) == 0 && change.Solution == nil {
		return 1, fmt.Errorf("initial requirement creation requires --solution prose")
	}
	rendered, err := requirement.RenderSemanticChange(relative, data, change)
	if err != nil {
		return 1, err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return 1, err
	}
	if err := os.WriteFile(target, rendered, 0644); err != nil {
		return 1, err
	}
	preview, err := facade.PrepareIntake(fullRoot, relative, "", nil, true)
	if err != nil {
		return 1, err
	}
	return printJSON(streams.Stdout, preview)
}

func runSettings(args []string, streams IO) (int, error) {
	command := "show"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("settings "+command, flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	root := fs.String("root", ".", "project root")
	home, err := os.UserHomeDir()
	if err != nil {
		return 1, err
	}
	userHome := fs.String("home", home, "user configuration home")
	scope := fs.String("scope", "project", "project or user overlay")
	field := fs.Int("field", 0, "published field position")
	value := fs.String("value", "", "new semantic value; stdin when omitted")
	if err := fs.Parse(args); err != nil {
		return 1, err
	}
	if fs.NArg() != 0 {
		return 1, fmt.Errorf("unexpected settings arguments")
	}
	var effective settings.Effective
	if command == "show" {
		effective, err = settings.Load(*root, *userHome)
	} else if command == "set" || command == "remove" {
		if command == "set" {
			supplied := false
			fs.Visit(func(f *flag.Flag) {
				if f.Name == "value" {
					supplied = true
				}
			})
			if !supplied {
				data, readErr := io.ReadAll(streams.Stdin)
				if readErr != nil {
					return 1, readErr
				}
				*value = string(data)
			}
		}
		effective, err = settings.EditField(*root, *userHome, *scope, *field, *value, command == "remove")
	} else {
		return 1, fmt.Errorf("settings command must be show, set or remove")
	}
	if err != nil {
		return 1, err
	}
	fields, err := settings.EditableFields(effective)
	if err != nil {
		return 1, err
	}
	return printJSON(streams.Stdout, struct {
		Effective settings.Effective       `json:"effective"`
		Fields    []settings.EditableField `json:"fields"`
	}{Effective: effective, Fields: fields})
}

type requirementDocumentChoice struct {
	Position int    `json:"position"`
	Path     string `json:"path"`
}

func repositoryMetadata(entry os.DirEntry) bool {
	return entry.Name() == ".git" || entry.IsDir() && entry.Name() == ".svn"
}

func requirementDocuments(root string) ([]requirementDocumentChoice, error) {
	files, err := validate.ProjectRuntimeFiles(root)
	if err != nil {
		return nil, err
	}
	documents := []requirementDocumentChoice{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if repositoryMetadata(entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if vcs.RuntimePath(relative) {
				return filepath.SkipDir
			}
			return nil
		}
		if files.ContainsFile(path) || !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if _, err := requirement.ParseRequirementProjection(relative, data); err == nil {
			documents = append(documents, requirementDocumentChoice{Position: len(documents) + 1, Path: filepath.ToSlash(relative)})
		}
		return nil
	})
	return documents, err
}

// Intake materials are the project's ordinary files, excluding repository and runtime metadata.
func intakeMaterials(root string) ([]requirementDocumentChoice, error) {
	files, err := validate.ProjectRuntimeFiles(root)
	if err != nil {
		return nil, err
	}
	materials := []requirementDocumentChoice{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if repositoryMetadata(entry) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if vcs.RuntimePath(relative) {
				return filepath.SkipDir
			}
			return nil
		}
		if files.ContainsFile(path) || !entry.Type().IsRegular() {
			return nil
		}
		materials = append(materials, requirementDocumentChoice{Position: len(materials) + 1, Path: filepath.ToSlash(relative)})
		return nil
	})
	return materials, err
}

func printIntakeMaterials(root string, out io.Writer) (int, error) {
	materials, err := intakeMaterials(root)
	return printValue(out, materials, err)
}

func selectIntakeDocuments(root string, requirementPath *string, document int, solutionPath *string, solution int, artifacts *stringListFlag, artifactPositions []int, out io.Writer) (bool, error) {
	if document != 0 && *requirementPath != "" {
		return false, fmt.Errorf("select a document position or exact requirement file")
	}
	if *requirementPath == "" {
		documents, err := requirementDocuments(root)
		if err != nil {
			return false, err
		}
		if document == 0 && len(documents) == 1 {
			document = 1
		}
		if document == 0 {
			_, err := printJSON(out, documents)
			return false, err
		}
		if document < 1 || document > len(documents) {
			return false, fmt.Errorf("select an original displayed requirement document")
		}
		*requirementPath = documents[document-1].Path
	}
	if solution != 0 && *solutionPath != "" || len(artifactPositions) != 0 && len(*artifacts) != 0 {
		return false, fmt.Errorf("select confirmation material positions or exact material files")
	}
	if solution == 0 && len(artifactPositions) == 0 {
		return true, nil
	}
	materials, err := intakeMaterials(root)
	if err != nil {
		return false, err
	}
	selectPath := func(position int) (string, error) {
		if position < 1 || position > len(materials) {
			return "", fmt.Errorf("select an original displayed confirmation material")
		}
		return materials[position-1].Path, nil
	}
	if solution != 0 {
		*solutionPath, err = selectPath(solution)
		if err != nil {
			return false, err
		}
	}
	for _, position := range artifactPositions {
		path, err := selectPath(position)
		if err != nil {
			return false, err
		}
		*artifacts = append(*artifacts, path)
	}
	return true, nil
}
