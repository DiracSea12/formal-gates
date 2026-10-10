package harness

// Native interaction decisions select original pending questions. The program
// saves their complete bindings before an explicit confirmation can deliver them.

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

func nativePendingPath(directory string) string {
	return filepath.Join(directory, "interaction-pending.json")
}
func nativeAnswerPath(directory string) string {
	return filepath.Join(directory, "interaction-answer.json")
}

func readNativePendings(directory string) ([]any, error) {
	value, err := readJSONFile(nativePendingPath(directory))
	if os.IsNotExist(err) {
		return []any{}, nil
	}
	if err != nil {
		return nil, err
	}
	records, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("native pending evidence must be an array")
	}
	return records, nil
}

type nativeQuestionResponse struct {
	Choices []int  `json:"choices,omitempty"`
	Text    string `json:"text,omitempty"`
}
type nativeSelection struct {
	Choice    int                      `json:"choice,omitempty"`
	Responses []nativeQuestionResponse `json:"responses,omitempty"`
	Values    []any                    `json:"values,omitempty"`
	Cancel    bool                     `json:"cancel,omitempty"`
}
type nativeAnswerCandidate struct {
	Directory string          `json:"directory"`
	Pending   map[string]any  `json:"pending"`
	Selection nativeSelection `json:"selection"`
}
type nativeHeldQuestion struct {
	Directory string
	Record    map[string]any
}

func nativePendingBinding(record map[string]any) map[string]any {
	original := map[string]any{}
	for key, value := range record {
		if key != "updated_at" {
			original[key] = value
		}
	}
	return original
}

func nativeOption(options any, position int) (map[string]any, error) {
	choices := listOf(options)
	if position < 1 || position > len(choices) {
		return nil, fmt.Errorf("select an original displayed option")
	}
	return objectOrEmpty(choices[position-1]), nil
}

func assembleNativeAnswer(record map[string]any, selection nativeSelection) (map[string]any, error) {
	answer := map[string]any{"request_id": record["request_id"], "cancel": selection.Cancel}
	if selection.Cancel {
		if selection.Choice != 0 || len(selection.Responses) != 0 || len(selection.Values) != 0 {
			return nil, fmt.Errorf("cancel has no option responses")
		}
		return answer, nil
	}
	questions := listOf(record["questions"])
	if record["method"] == zcodeUserInputMethod && selection.Choice == 0 && len(selection.Responses) == 1 && len(questions) == 1 && len(listOf(getOf(questions[0], "options"))) == 0 {
		response := selection.Responses[0]
		if response.Text == "" || len(response.Choices) != 0 || len(selection.Values) != 0 {
			return nil, fmt.Errorf("this question needs original free text")
		}
		answer["answers"] = map[string]any{"answer": response.Text}
		return answer, nil
	}
	if record["method"] == "item/tool/requestUserInput" {
		responses := selection.Responses
		if selection.Choice != 0 {
			if len(questions) != 1 || len(responses) != 0 {
				return nil, fmt.Errorf("a single choice answers one question")
			}
			responses = []nativeQuestionResponse{{Choices: []int{selection.Choice}}}
		}
		if len(responses) != len(questions) {
			return nil, fmt.Errorf("answer each displayed question in order")
		}
		answers := map[string]any{}
		for i, question := range questions {
			response := responses[i]
			values := []any{}
			for _, position := range response.Choices {
				option, err := nativeOption(getOf(question, "options"), position)
				if err != nil {
					return nil, err
				}
				values = append(values, option["label"])
			}
			if response.Text != "" {
				if getOf(question, "options") != nil && getOf(question, "isOther") != true {
					return nil, fmt.Errorf("this question does not offer free text")
				}
				values = append(values, response.Text)
			}
			if len(values) == 0 {
				return nil, fmt.Errorf("each question needs an option or free text")
			}
			answers[textOf(getOf(question, "id"))] = map[string]any{"answers": values}
		}
		answer["answers"] = answers
		return answer, nil
	}
	if len(selection.Responses) != 0 {
		return nil, fmt.Errorf("this interaction accepts a displayed option")
	}
	option, err := nativeOption(record["options"], selection.Choice)
	if err != nil {
		return nil, err
	}
	identity := option["optionId"]
	if identity == nil {
		identity = option["value"]
	}
	if identity == nil {
		return nil, fmt.Errorf("original option has no protocol identity")
	}
	answer["option"] = identity
	if record["method"] == "mcpServer/elicitation/request" && identity == "accept" && record["mode"] != "url" {
		names := nativeFormFields(record)
		if len(selection.Values) != len(names) {
			return nil, fmt.Errorf("supply the displayed form values in order")
		}
		content := map[string]any{}
		properties := objectOrEmpty(getOf(getOf(record, "requestedSchema"), "properties"))
		for i, name := range names {
			value := selection.Values[i]
			if choices := listOf(getOf(properties[name], "enum")); len(choices) != 0 {
				position := 0
				switch number := getOf(value, "choice").(type) {
				case int:
					position = number
				case float64:
					position = int(number)
				case json.Number:
					position = intOf(number)
				}
				if len(objectOrEmpty(value)) != 1 || !valuesEqual(getOf(value, "choice"), position) || position < 1 || position > len(choices) {
					return nil, fmt.Errorf("form enum selects a displayed numeric choice")
				}
				value = choices[position-1]
			}
			content[name] = value
		}
		answer["answers"] = content
	} else if len(selection.Values) != 0 {
		return nil, fmt.Errorf("this question has no form values")
	}
	return answer, nil
}

func nativeFormFields(record map[string]any) []string {
	names := []string{}
	for name := range objectOrEmpty(getOf(getOf(record, "requestedSchema"), "properties")) {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func nativeRunQuestions(run string) ([]nativeHeldQuestion, error) {
	info, err := os.Stat(run)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("run directory is unavailable")
	}
	files := []string{}
	for _, pattern := range []string{filepath.Join(run, "jobs", "*", "interaction-pending.json"), filepath.Join(run, "native", "*", "*", "interaction-pending.json")} {
		found, err := filepath.Glob(pattern)
		if err != nil {
			return nil, err
		}
		files = append(files, found...)
	}
	questions := []nativeHeldQuestion{}
	for _, path := range files {
		directory := resolveNativePath(filepath.Dir(path))
		records, err := readNativePendings(directory)
		if err != nil {
			return nil, err
		}
		for _, raw := range records {
			record := objectOrEmpty(raw)
			if record["status"] == "pending" {
				questions = append(questions, nativeHeldQuestion{directory, record})
			}
		}
	}
	return questions, nil
}

func prepareNativeAnswer(held nativeHeldQuestion, selection nativeSelection) (nativeAnswerCandidate, error) {
	if held.Record["status"] != "pending" {
		return nativeAnswerCandidate{}, fmt.Errorf("select a current pending question")
	}
	_, err := assembleNativeAnswer(held.Record, selection)
	if err != nil {
		return nativeAnswerCandidate{}, err
	}
	return nativeAnswerCandidate{resolveNativePath(held.Directory), nativePendingBinding(held.Record), selection}, nil
}

func submitNativeAnswer(candidate nativeAnswerCandidate) error {
	records, err := readNativePendings(candidate.Directory)
	if err != nil {
		return err
	}
	var original map[string]any
	for _, raw := range records {
		record := objectOrEmpty(raw)
		if valuesEqual(record["request_id"], candidate.Pending["request_id"]) && record["status"] == "pending" {
			original = record
		}
	}
	if original == nil || !valuesEqual(nativePendingBinding(original), candidate.Pending) {
		return fmt.Errorf("native question changed or is no longer pending")
	}
	answer, err := assembleNativeAnswer(original, candidate.Selection)
	if err != nil {
		return err
	}
	if _, err := os.Stat(nativeAnswerPath(candidate.Directory)); err == nil {
		return fmt.Errorf("the job has not consumed the previous answer yet")
	} else if !os.IsNotExist(err) {
		return err
	}
	answer["written_at"] = time.Now().UTC().Format(time.RFC3339)
	return writeAtomicJSON(nativeAnswerPath(candidate.Directory), answer)
}

func NativeRespond(args []string, errout io.Writer) int {
	flags := flag.NewFlagSet("native-respond", flag.ContinueOnError)
	flags.SetOutput(errout)
	run := flags.String("run", "", "original run directory")
	question := flags.Int("question", 1, "displayed pending question")
	choice := flags.Int("choice", 0, "displayed option")
	responses := flags.String("responses", "", "ordered numeric choices or free text per question")
	values := flags.String("values", "", "ordered authored form values")
	cancel := flags.Bool("cancel", false, "cancel the question")
	save := flags.String("save-decision", "", "save a complete decision candidate")
	confirmed := flags.String("decision-file", "", "confirm the original saved candidate")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return 2
	}
	if *confirmed != "" {
		if flags.NFlag() != 1 {
			fmt.Fprintln(errout, "confirm only the original --decision-file")
			return 2
		}
		body, err := os.ReadFile(*confirmed)
		if err == nil {
			var candidate nativeAnswerCandidate
			err = StrictDecode(body, &candidate)
			if err == nil {
				err = submitNativeAnswer(candidate)
			}
		}
		if err != nil {
			fmt.Fprintln(errout, err)
			return 1
		}
		return 0
	}
	if *run == "" || *save == "" {
		fmt.Fprintln(errout, "prepare with --run, a numeric choice or cancel, and --save-decision; confirm with --decision-file")
		return 2
	}
	selection := nativeSelection{Choice: *choice, Cancel: *cancel}
	if *responses != "" {
		if err := StrictDecode([]byte(*responses), &selection.Responses); err != nil {
			fmt.Fprintln(errout, err)
			return 2
		}
	}
	if *values != "" {
		if err := StrictDecode([]byte(*values), &selection.Values); err != nil {
			fmt.Fprintln(errout, err)
			return 2
		}
	}
	questions, err := nativeRunQuestions(*run)
	if err == nil && (*question < 1 || *question > len(questions)) {
		err = fmt.Errorf("select a current displayed native question")
	}
	if err == nil {
		var candidate nativeAnswerCandidate
		candidate, err = prepareNativeAnswer(questions[*question-1], selection)
		if err == nil {
			err = writeAtomicJSON(*save, candidate)
		}
	}
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	return 0
}

func nativeQuestionView(record map[string]any, position int) map[string]any {
	view := map[string]any{"question": position, "summary": record["summary"]}
	options := []any{}
	for i, option := range listOf(record["options"]) {
		label := getOf(option, "label")
		if label == nil {
			label = getOf(option, "name")
		}
		if label == nil {
			label = getOf(option, "decision")
		}
		if label == nil {
			label = getOf(option, "value")
		}
		if label == nil {
			label = getOf(option, "kind")
		}
		options = append(options, map[string]any{"choice": i + 1, "label": label})
	}
	view["options"] = options
	if record["method"] == "session/request_permission" {
		view["summary"] = "Permission request"
		tool := record["toolCall"]
		operation := map[string]any{}
		for _, key := range []string{"title", "kind", "rawInput"} {
			if value := getOf(tool, key); value != nil {
				operation[key] = value
			}
		}
		view["operation"] = operation
	} else {
		operation := map[string]any{}
		for _, key := range []string{"toolName", "input", "command", "cwd", "permissions", "message", "url"} {
			if value := record[key]; value != nil {
				operation[key] = value
			}
		}
		if len(operation) != 0 {
			view["operation"] = operation
		}
	}
	if questions := listOf(record["questions"]); len(questions) != 0 {
		view["summary"] = "Native input"
		rows := []any{}
		for i, raw := range questions {
			row := map[string]any{"question": i + 1, "text": getOf(raw, "question"), "freeText": getOf(raw, "options") == nil || getOf(raw, "isOther") == true}
			if record["method"] == zcodeUserInputMethod && len(listOf(getOf(raw, "options"))) == 0 {
				row["freeText"] = true
			}
			choices := []any{}
			for j, option := range listOf(getOf(raw, "options")) {
				choices = append(choices, map[string]any{"choice": j + 1, "label": getOf(option, "label"), "description": getOf(option, "description")})
			}
			row["options"] = choices
			rows = append(rows, row)
		}
		view["questions"] = rows
		view["options"] = []any{}
	}
	if record["method"] == "mcpServer/elicitation/request" && record["mode"] != "url" {
		fields := []any{}
		properties := objectOrEmpty(getOf(getOf(record, "requestedSchema"), "properties"))
		for i, name := range nativeFormFields(record) {
			property := objectOrEmpty(properties[name])
			label := getOrOf(property, "title", name)
			field := map[string]any{"field": i + 1, "label": label, "type": property["type"], "description": property["description"]}
			if choices := listOf(property["enum"]); len(choices) != 0 {
				options := []any{}
				for i, choice := range choices {
					options = append(options, map[string]any{"choice": i + 1, "label": choice})
				}
				field["options"] = options
			}
			fields = append(fields, field)
		}
		view["fields"] = fields
	}
	return view
}

func NativeShow(args []string, out, errout io.Writer) int {
	flags := flag.NewFlagSet("native-show", flag.ContinueOnError)
	flags.SetOutput(errout)
	run := flags.String("run", "", "original run directory")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || *run == "" {
		return 2
	}
	questions, err := nativeRunQuestions(*run)
	if err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	views := []any{}
	for i, question := range questions {
		views = append(views, nativeQuestionView(question.Record, i+1))
	}
	if err := json.NewEncoder(out).Encode(views); err != nil {
		fmt.Fprintln(errout, err)
		return 1
	}
	return 0
}
