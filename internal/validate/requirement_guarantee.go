package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"formal-gates/internal/engine/coverage"
)

const (
	guaranteeFrozen        = "frozen"
	guaranteeActive        = "active"
	guaranteeBlocked       = "blocked"
	guaranteeNotGuaranteed = "not-guaranteed"
	guaranteeWaived        = "waived/not-guaranteed"
	guaranteeSealSkipID    = "requirement-guarantee"
	masterMergeOwner       = "master-merge"
)

// RequirementGuarantee is the explicit, persisted activation envelope for the
// REQ/AC guarantee. A nil envelope means that this run never activated the new
// contract; callers must never infer activation from its route, artifact count,
// filename, or document shape.
type RequirementGuarantee struct {
	Activation          string                           `json:"activation"`
	ActivationSource    string                           `json:"activationSource"`
	Reason              string                           `json:"reason,omitempty"`
	RequirementRevision string                           `json:"requirementRevision"`
	SolutionRevision    string                           `json:"solutionRevision"`
	ManifestDigest      string                           `json:"manifestDigest"`
	Projection          *FrozenRequirementProjection     `json:"projection,omitempty"`
	ReviewsByMode       map[string]GuaranteeReviewRecord `json:"reviewsByMode"`
	Waiver              *RequirementGuaranteeWaiver      `json:"waiver,omitempty"`
	Report              RequirementGuaranteeReport       `json:"report"`
}

type FrozenRequirementProjection struct {
	Path          string              `json:"path"`
	ContentDigest string              `json:"contentDigest"`
	Requirements  []FrozenRequirement `json:"requirements"`
}

type FrozenRequirement struct {
	ID                   string                      `json:"id"`
	Title                string                      `json:"title"`
	Requirement          string                      `json:"requirement"`
	Source               string                      `json:"source"`
	AcceptanceConditions []FrozenAcceptanceCondition `json:"acceptanceConditions"`
}

type FrozenAcceptanceCondition struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Line int    `json:"line"`
}

type GuaranteeReviewRecord struct {
	Review    coverage.ReviewResult       `json:"review"`
	Whitelist *coverage.ApprovedWhitelist `json:"whitelist,omitempty"`
}

type RequirementGuaranteeWaiver struct {
	Origin     string   `json:"origin"`
	Reason     string   `json:"reason"`
	Snapshot   string   `json:"snapshot"`
	Unresolved []string `json:"unresolved"`
}

type RequirementGuaranteeReport struct {
	Status              string                                  `json:"status"`
	Reason              string                                  `json:"reason,omitempty"`
	RequirementRevision string                                  `json:"requirementRevision,omitempty"`
	SolutionRevision    string                                  `json:"solutionRevision,omitempty"`
	ManifestDigest      string                                  `json:"manifestDigest,omitempty"`
	RequirementCount    int                                     `json:"requirementCount"`
	AcceptanceCount     int                                     `json:"acceptanceCount"`
	Requirements        []RequirementGuaranteeRequirementReport `json:"requirements,omitempty"`
	Items               []RequirementGuaranteeItemReport        `json:"items,omitempty"`
}

// RequirementGuaranteeRequirementReport is the REQ-level completion
// projection. Owners and cases are unions of the AC rows below it; Status is
// PASS only when every owned AC has both review and current execution PASS.
type RequirementGuaranteeRequirementReport struct {
	RequirementID string   `json:"requirementId"`
	Owners        []string `json:"owners"`
	AcceptanceIDs []string `json:"acceptanceIds"`
	Cases         []string `json:"cases"`
	ReviewStatus  string   `json:"reviewStatus"`
	Execution     string   `json:"executionStatus"`
	Status        string   `json:"status"`
}

type RequirementGuaranteeItemReport struct {
	RequirementID string   `json:"requirementId"`
	AcceptanceID  string   `json:"acceptanceId"`
	Owner         string   `json:"owner"`
	Cases         []string `json:"cases"`
	ReviewStatus  string   `json:"reviewStatus"`
	Execution     string   `json:"executionStatus"`
}

type RequirementUpdateOptions struct {
	ActivateGuarantee bool
}

type QAReviewRecordOptions struct {
	SourceDecisions []string
	PointDecisions  []string
	CaseDecisions   []string
	UnboundSources  []string
	UnboundPoints   []string
	UnboundCases    []string
}

type SealOptions struct {
	GuaranteeWaiverReason string
}

type RequirementPrecheckIssue struct {
	Path        string `json:"path"`
	Line        int    `json:"line"`
	Requirement string `json:"requirementId,omitempty"`
	Rule        string `json:"rule"`
}

type RequirementPrecheckError struct{ Issues []RequirementPrecheckIssue }

func (e *RequirementPrecheckError) Error() string {
	parts := make([]string, 0, len(e.Issues))
	for _, issue := range e.Issues {
		prefix := fmt.Sprintf("%s:%d", issue.Path, issue.Line)
		if issue.Requirement != "" {
			prefix += " [" + issue.Requirement + "]"
		}
		parts = append(parts, prefix+": "+issue.Rule)
	}
	return "requirement structure precheck failed: " + strings.Join(parts, "; ")
}

var (
	requirementIDPattern       = regexp.MustCompile(`^(REQ-[0-9]{3,})：(.+)$`)
	requirementIdentityPattern = regexp.MustCompile(`^(REQ-[0-9]+)(?:：|$)`)
	acceptancePattern          = regexp.MustCompile(`^(AC-[0-9]{3,})：(.+)$`)
	acceptancePartsPattern     = regexp.MustCompile(`^AC-([0-9]+)：(.*)$`)
)

type parsedRequirement struct {
	requirement FrozenRequirement
	field       string
	fieldLines  map[string]int
	fieldText   map[string][]string
	fieldOrder  []string
}

// ParseRequirementProjection performs the deterministic, fence-aware precheck
// and returns the exact finite REQ/AC projection from the single 需求点 section.
// It intentionally makes no semantic judgment about the prose.
func ParseRequirementProjection(path string, data []byte) (FrozenRequirementProjection, error) {
	if !utf8.Valid(data) {
		return FrozenRequirementProjection{}, &RequirementPrecheckError{Issues: []RequirementPrecheckIssue{{Path: path, Line: 1, Rule: "document is not valid UTF-8 Markdown"}}}
	}
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	issues := []RequirementPrecheckIssue{}
	addIssue := func(line int, reqID, rule string) {
		if line < 1 {
			line = 1
		}
		issues = append(issues, RequirementPrecheckIssue{Path: path, Line: line, Requirement: reqID, Rule: rule})
	}

	var requirements []FrozenRequirement
	reqIDs, acIDs := map[string]int{}, map[string]int{}
	sectionCount, sectionLine := 0, 1
	inSection := false
	var current *parsedRequirement
	fenceChar := byte(0)
	fenceLength := 0

	finishRequirement := func(boundaryLine int) {
		if current == nil {
			return
		}
		reqID := current.requirement.ID
		expected := []string{"要求", "验收条件", "来源"}
		for _, field := range expected {
			if current.fieldLines[field] == 0 {
				addIssue(boundaryLine, reqID, "missing direct #### "+field+" field")
			}
		}
		if len(current.fieldOrder) == 3 {
			for i := range expected {
				if current.fieldOrder[i] != expected[i] {
					addIssue(current.fieldLines[current.fieldOrder[i]], reqID, "direct #### fields must appear exactly in 要求 → 验收条件 → 来源 order")
					break
				}
			}
		}
		requirementText := strings.TrimSpace(strings.Join(current.fieldText["要求"], "\n"))
		sourceText := strings.TrimSpace(strings.Join(current.fieldText["来源"], "\n"))
		if current.fieldLines["要求"] != 0 && requirementText == "" {
			addIssue(current.fieldLines["要求"], reqID, "要求 field must contain non-empty prose")
		}
		if current.fieldLines["来源"] != 0 && sourceText == "" {
			addIssue(current.fieldLines["来源"], reqID, "来源 field must contain non-empty prose")
		}
		if current.fieldLines["验收条件"] != 0 && len(current.requirement.AcceptanceConditions) == 0 {
			addIssue(current.fieldLines["验收条件"], reqID, "验收条件 must contain at least one direct - AC-nnn：... list item")
		}
		current.requirement.Requirement = requirementText
		current.requirement.Source = sourceText
		requirements = append(requirements, current.requirement)
		current = nil
	}

	for index, raw := range lines {
		lineNo := index + 1
		if marker, length, remainder, ok := markdownFence(raw); ok {
			if fenceChar == 0 {
				if marker != '`' || !strings.Contains(remainder, "`") {
					fenceChar, fenceLength = marker, length
				}
			} else if marker == fenceChar && length >= fenceLength && strings.TrimSpace(remainder) == "" {
				fenceChar, fenceLength = 0, 0
			}
			continue
		}
		if fenceChar != 0 {
			continue
		}
		level, heading, isHeading := markdownHeading(raw)
		if isHeading && level == 2 {
			if inSection {
				finishRequirement(lineNo)
			}
			inSection = heading == "需求点"
			if inSection {
				sectionCount++
				sectionLine = lineNo
				if sectionCount > 1 {
					addIssue(lineNo, "", "document must contain exactly one ## 需求点 section")
				}
			}
			continue
		}
		if isHeading && level == 3 {
			if !inSection {
				if strings.HasPrefix(heading, "REQ-") {
					addIssue(lineNo, "", "REQ heading appears outside ## 需求点")
				}
				continue
			}
			finishRequirement(lineNo)
			match := requirementIDPattern.FindStringSubmatch(heading)
			if match == nil || strings.TrimSpace(match[2]) == "" {
				reqID := ""
				if identity := requirementIdentityPattern.FindStringSubmatch(heading); identity != nil {
					reqID = identity[1]
				}
				addIssue(lineNo, reqID, "every direct ### heading in ## 需求点 must match REQ-[0-9]{3,}：non-empty title")
				continue
			}
			reqID := match[1]
			if prior := reqIDs[reqID]; prior != 0 {
				addIssue(lineNo, reqID, fmt.Sprintf("duplicate REQ ID (first declared at line %d)", prior))
			} else {
				reqIDs[reqID] = lineNo
			}
			current = &parsedRequirement{
				requirement: FrozenRequirement{ID: reqID, Title: strings.TrimSpace(match[2])},
				fieldLines:  map[string]int{},
				fieldText:   map[string][]string{},
			}
			continue
		}
		if isHeading && level == 4 && inSection {
			if current == nil {
				addIssue(lineNo, "", "direct #### field appears before a valid REQ heading")
				continue
			}
			if heading != "要求" && heading != "验收条件" && heading != "来源" {
				addIssue(lineNo, current.requirement.ID, "REQ blocks may contain only direct #### 要求, #### 验收条件, and #### 来源 fields")
				current.field = ""
				continue
			}
			if current.fieldLines[heading] != 0 {
				addIssue(lineNo, current.requirement.ID, "duplicate direct #### "+heading+" field")
			} else {
				current.fieldLines[heading] = lineNo
				current.fieldOrder = append(current.fieldOrder, heading)
			}
			current.field = heading
			continue
		}

		listText, directList := markdownDirectList(raw)
		if directList {
			looksLikeAC := strings.HasPrefix(strings.TrimSpace(listText), "AC-")
			if !inSection || current == nil || current.field != "验收条件" {
				if looksLikeAC {
					addIssue(lineNo, requirementID(current), "AC list item appears outside its REQ's direct #### 验收条件 field")
				} else if current != nil && current.field != "" {
					current.fieldText[current.field] = append(current.fieldText[current.field], raw)
				}
			} else {
				trimmedList := strings.TrimSpace(listText)
				match := acceptancePattern.FindStringSubmatch(trimmedList)
				if match == nil || strings.TrimSpace(match[2]) == "" {
					rule := "every direct list item in 验收条件 must match - AC-[0-9]{3,}：non-empty condition"
					if parts := acceptancePartsPattern.FindStringSubmatch(trimmedList); parts != nil {
						switch {
						case len(parts[1]) < 3:
							rule = "AC number must contain at least three digits"
						case strings.TrimSpace(parts[2]) == "":
							rule = "AC condition text must be non-empty"
						}
					}
					addIssue(lineNo, current.requirement.ID, rule)
				} else {
					acID := match[1]
					if prior := acIDs[acID]; prior != 0 {
						addIssue(lineNo, current.requirement.ID, fmt.Sprintf("duplicate AC ID %s (first declared at line %d)", acID, prior))
					} else {
						acIDs[acID] = lineNo
					}
					current.requirement.AcceptanceConditions = append(current.requirement.AcceptanceConditions, FrozenAcceptanceCondition{ID: acID, Text: strings.TrimSpace(match[2]), Line: lineNo})
				}
			}
			continue
		}
		if current != nil && current.field != "" && strings.TrimSpace(raw) != "" {
			current.fieldText[current.field] = append(current.fieldText[current.field], raw)
		}
	}
	finishRequirement(len(lines))
	if sectionCount == 0 {
		addIssue(1, "", "document must contain exactly one ## 需求点 section")
	}
	if sectionCount == 1 && len(requirements) == 0 {
		addIssue(sectionLine, "", "## 需求点 must contain at least one valid direct REQ heading")
	}
	if len(issues) != 0 {
		sort.SliceStable(issues, func(i, j int) bool { return issues[i].Line < issues[j].Line })
		return FrozenRequirementProjection{}, &RequirementPrecheckError{Issues: issues}
	}
	sum := sha256.Sum256([]byte(normalized))
	return FrozenRequirementProjection{Path: filepath.ToSlash(path), ContentDigest: hex.EncodeToString(sum[:]), Requirements: requirements}, nil
}

func requirementID(current *parsedRequirement) string {
	if current == nil {
		return ""
	}
	return current.requirement.ID
}

func markdownFence(line string) (byte, int, string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 3 {
		return 0, 0, "", false
	}
	marker := trimmed[0]
	if marker != '`' && marker != '~' {
		return 0, 0, "", false
	}
	length := 0
	for length < len(trimmed) && trimmed[length] == marker {
		length++
	}
	if length < 3 {
		return 0, 0, "", false
	}
	return marker, length, trimmed[length:], true
}

func markdownHeading(line string) (int, string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || trimmed == "" || trimmed[0] != '#' {
		return 0, "", false
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level > 6 || (level < len(trimmed) && trimmed[level] != ' ' && trimmed[level] != '\t') {
		return 0, "", false
	}
	text := strings.TrimSpace(trimmed[level:])
	if cut := strings.LastIndex(text, " #"); cut >= 0 && strings.Trim(text[cut+1:], "#") == "" {
		text = strings.TrimSpace(text[:cut])
	}
	return level, text, true
}

func markdownDirectList(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 || len(trimmed) < 2 || trimmed[0] != '-' || (trimmed[1] != ' ' && trimmed[1] != '\t') {
		return "", false
	}
	return strings.TrimSpace(trimmed[2:]), true
}

func frozenProjection(root, source string) (*FrozenRequirementProjection, error) {
	rel := normalizeArtifactPath(root, source)
	data, err := os.ReadFile(resolveFromRoot(cleanWorktree(root), rel))
	if err != nil {
		return nil, err
	}
	projection, err := ParseRequirementProjection(rel, data)
	if err != nil {
		return nil, err
	}
	return &projection, nil
}

func projectionDigest(projection *FrozenRequirementProjection) (string, error) {
	if projection == nil || len(projection.Requirements) == 0 {
		return "", fmt.Errorf("frozen requirement projection is missing")
	}
	return coverage.CanonicalDigest(projection)
}

func activateFrozenGuarantee(root string, state *RunState) error {
	if isLightweight(*state) {
		return fmt.Errorf("the lightweight route does not activate the REQ/AC coverage guarantee")
	}
	projection, err := frozenProjection(root, state.RequirementSource)
	if err != nil {
		return err
	}
	digest, err := projectionDigest(projection)
	if err != nil {
		return err
	}
	if projection.ContentDigest != state.RequirementRevision {
		return fmt.Errorf("the presented requirement revision %s does not match the current file digest %s", state.RequirementRevision, projection.ContentDigest)
	}
	state.RequirementGuarantee = &RequirementGuarantee{
		Activation:          guaranteeFrozen,
		ActivationSource:    "EXPLICIT_REQUIREMENT_CONFIRMATION",
		RequirementRevision: state.RequirementRevision,
		SolutionRevision:    state.RequirementRevision,
		ManifestDigest:      digest,
		Projection:          projection,
		ReviewsByMode:       map[string]GuaranteeReviewRecord{},
	}
	return nil
}

func validateGuaranteeEnvelope(state RunState) error {
	g := state.RequirementGuarantee
	if g == nil {
		return nil
	}
	if g.ActivationSource == "" || g.RequirementRevision == "" || g.SolutionRevision == "" || g.ManifestDigest == "" || g.ReviewsByMode == nil {
		return fmt.Errorf("requirement guarantee state is incomplete or damaged")
	}
	if g.RequirementRevision != state.RequirementRevision || g.SolutionRevision != state.RequirementRevision {
		return fmt.Errorf("requirement guarantee binding does not match the current confirmed revision")
	}
	if g.Projection == nil {
		return fmt.Errorf("requirement guarantee frozen projection is missing")
	}
	digest, err := projectionDigest(g.Projection)
	if err != nil || digest != g.ManifestDigest {
		return fmt.Errorf("requirement guarantee manifest digest does not match its frozen projection")
	}
	return nil
}

// requireExplicitGuaranteeForQASelection closes the only ambiguous route
// boundary introduced by the structured single-file requirement flow. A run
// with exactly one formal requirement artifact must have frozen that artifact
// explicitly before selecting any QA kind; otherwise nil would mean both
// "legacy/non-guaranteed" and "the operator forgot --activate-guarantee" and
// the latter could bypass the REQ/AC closure. Multi-artifact legacy runs stay
// outside this change's guarantee contract and retain their existing behavior.
func requireExplicitGuaranteeForQASelection(state RunState, selected []string) error {
	if len(state.RequirementArtifacts) != 1 {
		return nil
	}
	qaSelected := false
	for _, id := range selected {
		if isQAMode(id) {
			qaSelected = true
			break
		}
	}
	if !qaSelected || state.RequirementGuarantee != nil {
		return nil
	}
	return fmt.Errorf("a QA-enabled route with one formal requirement artifact requires explicit REQ/AC guarantee activation; run workflow requirement --confirmed --activate-guarantee before Product Review")
}

func requirementIndex(projection *FrozenRequirementProjection) (map[string]FrozenRequirement, map[string]struct {
	RequirementID string
	Condition     FrozenAcceptanceCondition
}) {
	requirements := map[string]FrozenRequirement{}
	conditions := map[string]struct {
		RequirementID string
		Condition     FrozenAcceptanceCondition
	}{}
	if projection == nil {
		return requirements, conditions
	}
	for _, req := range projection.Requirements {
		requirements[req.ID] = req
		for _, ac := range req.AcceptanceConditions {
			conditions[ac.ID] = struct {
				RequirementID string
				Condition     FrozenAcceptanceCondition
			}{req.ID, ac}
		}
	}
	return requirements, conditions
}

func guaranteeProductReviewKey(reqID string) string { return reqID + " obligation-to-AC completeness" }

func ensureGuaranteeProductReviewItems(state *RunState) {
	if state.RequirementGuarantee == nil || state.RequirementGuarantee.Projection == nil {
		return
	}
	if state.ReviewItemsByAction == nil {
		state.ReviewItemsByAction = map[string]map[string]ReviewItem{}
	}
	table := state.ReviewItemsByAction["product-review"]
	if table == nil {
		table = map[string]ReviewItem{}
	}
	for _, req := range state.RequirementGuarantee.Projection.Requirements {
		key := guaranteeProductReviewKey(req.ID)
		if _, exists := table[key]; !exists {
			table[key] = ReviewItem{Status: "PENDING"}
		}
	}
	state.ReviewItemsByAction["product-review"] = table
}

func requireGuaranteeProductReviewResult(state RunState, status string) error {
	if state.RequirementGuarantee == nil {
		return nil
	}
	if err := validateGuaranteeEnvelope(state); err != nil {
		return err
	}
	for _, req := range state.RequirementGuarantee.Projection.Requirements {
		item, ok := state.ReviewItemsByAction["product-review"][guaranteeProductReviewKey(req.ID)]
		if !ok || item.Status == "PENDING" {
			return fmt.Errorf("Product Review must decide the requirement-obligation-to-AC completeness of %s", req.ID)
		}
		if item.Status == "FAIL" && status == "PASS" {
			return fmt.Errorf("Product Review cannot PASS while %s has an uncovered mandatory obligation", req.ID)
		}
	}
	return nil
}

func updateGuaranteeForRoute(state *RunState) {
	g := state.RequirementGuarantee
	if g == nil || g.Activation == guaranteeWaived {
		return
	}
	if !isSelectedQA(*state) {
		g.Activation = guaranteeNotGuaranteed
		g.Reason = "the confirmed custom route selected no QA mode"
		return
	}
	if len(state.RequirementArtifacts) != 1 {
		g.Activation = guaranteeBlocked
		g.Reason = "QA-enabled requirement guarantee requires exactly one formal requirement artifact"
		return
	}
	g.Activation = guaranteeActive
	g.Reason = ""
}

func guaranteeReadyForQA(state RunState) error {
	if state.RequirementGuarantee == nil {
		return requireExplicitGuaranteeForQASelection(state, state.SelectedGates)
	}
	if err := validateGuaranteeEnvelope(state); err != nil {
		return err
	}
	if state.RequirementGuarantee.Activation != guaranteeActive {
		return fmt.Errorf("requirement guarantee is %s: %s", state.RequirementGuarantee.Activation, state.RequirementGuarantee.Reason)
	}
	if err := requireGuaranteeResponsibilityBinding(state); err != nil {
		return err
	}
	return nil
}

func requireGuaranteeResponsibilityBinding(state RunState) error {
	if state.RequirementGuarantee == nil {
		return nil
	}
	if state.SplitMasterRunID != "" {
		if state.Slicing == nil || state.Slicing.Decision != "split" {
			return fmt.Errorf("slice guarantee responsibility binding is missing after the requirement revision; record the inherited split topology again")
		}
	}
	if state.RetainedOverall {
		if state.Slicing == nil || state.Slicing.Decision != "split" {
			return fmt.Errorf("retained-master guarantee responsibility binding is missing after the requirement revision; record the split topology again")
		}
	}
	return nil
}

func invalidateGuaranteeRevision(state *RunState) {
	if state.RequirementGuarantee == nil {
		return
	}
	state.RequirementGuarantee.Activation = guaranteeFrozen
	state.RequirementGuarantee.Reason = "requirement revision changed and must be reconfirmed"
	state.RequirementGuarantee.RequirementRevision = state.RequirementRevision
	state.RequirementGuarantee.SolutionRevision = state.RequirementRevision
	state.RequirementGuarantee.ManifestDigest = ""
	state.RequirementGuarantee.Projection = nil
	state.RequirementGuarantee.ReviewsByMode = map[string]GuaranteeReviewRecord{}
	state.RequirementGuarantee.Waiver = nil
	state.RequirementConfirmed = false
	for mode, cases := range state.QACasesByMode {
		for i := range cases {
			cases[i].ReviewStatus = "PENDING"
			cases[i].ApprovedSource = ""
		}
		state.QACasesByMode[mode] = cases
	}
	state.QAReviewByMode = map[string]ActionResult{}
	state.QAExecutionByMode = map[string]QAExecutionResult{}
	state.PriorQAExecutionByMode = map[string]*QAExecutionResult{}
	// A split decision contains the revision-bound primary owner for every AC.
	// Discard the whole binding point so the same topology can be recorded again
	// with a complete owner map derived from the newly confirmed projection.
	if state.Slicing != nil && state.Slicing.Decision == "split" {
		state.Slicing = nil
	}
}

func inheritRequirementGuarantee(master RunState, slice *RunState) error {
	if master.RequirementGuarantee == nil {
		if slice.RequirementGuarantee != nil {
			return fmt.Errorf("slice cannot activate a requirement guarantee that its retained master does not hold")
		}
		return nil
	}
	if err := validateGuaranteeEnvelope(master); err != nil {
		return fmt.Errorf("master requirement guarantee is invalid: %w", err)
	}
	if slice.RequirementRevision != master.RequirementRevision {
		return fmt.Errorf("slice requirement revision does not match the master guarantee")
	}
	data, err := json.Marshal(master.RequirementGuarantee)
	if err != nil {
		return err
	}
	var inherited RequirementGuarantee
	if err := json.Unmarshal(data, &inherited); err != nil {
		return err
	}
	inherited.Activation = guaranteeFrozen
	inherited.ActivationSource = "INHERITED_FROM_MASTER:" + master.RunID
	inherited.Reason = "awaiting the slice route selection"
	inherited.ReviewsByMode = map[string]GuaranteeReviewRecord{}
	inherited.Waiver = nil
	inherited.Report = RequirementGuaranteeReport{}
	slice.RequirementGuarantee = &inherited
	return nil
}

func guaranteeAllACs(testCase QACase) []string {
	values := append([]string(nil), testCase.AcceptanceCriteria...)
	values = append(values, testCase.AdditionalAcceptanceCriteria...)
	return values
}

func validateGuaranteeCaseBindings(state RunState, testCase QACase) error {
	if state.RequirementGuarantee == nil {
		return nil
	}
	if err := validateGuaranteeEnvelope(state); err != nil {
		return err
	}
	if err := requireGuaranteeResponsibilityBinding(state); err != nil {
		return err
	}
	_, conditions := requirementIndex(state.RequirementGuarantee.Projection)
	if len(testCase.AcceptanceCriteria) == 0 {
		return fmt.Errorf("QA case %s requires at least one explicit --ac binding", testCase.ID)
	}
	seen := map[string]bool{}
	for _, acID := range guaranteeAllACs(testCase) {
		if _, ok := conditions[acID]; !ok {
			return fmt.Errorf("QA case %s references unknown acceptance condition %s", testCase.ID, acID)
		}
		if seen[acID] {
			return fmt.Errorf("QA case %s repeats acceptance condition %s", testCase.ID, acID)
		}
		seen[acID] = true
	}
	if state.SplitMasterRunID != "" && state.Slicing != nil {
		for _, acID := range testCase.AcceptanceCriteria {
			if state.Slicing.ACResponsibilities[acID] != state.RunID {
				return fmt.Errorf("slice %s cannot claim primary responsibility for %s (owner is %s)", state.RunID, acID, state.Slicing.ACResponsibilities[acID])
			}
		}
	}
	if state.RetainedOverall && state.Slicing != nil {
		for _, acID := range testCase.AcceptanceCriteria {
			if state.Slicing.ACResponsibilities[acID] != masterMergeOwner {
				return fmt.Errorf("master merge QA cannot claim primary responsibility for %s (owner is %s)", acID, state.Slicing.ACResponsibilities[acID])
			}
		}
	}
	return nil
}

func normalizeACIDs(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		id := strings.TrimSpace(raw)
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

func coverageKind(mode string, merge bool) coverage.ReviewKind {
	if merge || mode == mergeQAID {
		return coverage.ReviewMerge
	}
	if mode == whiteboxQAID {
		return coverage.ReviewWhitebox
	}
	return coverage.ReviewBlackbox
}

func coveragePointID(kind coverage.ReviewKind, acID string) string {
	return string(kind) + "::" + acID
}

func parseDecisionValues(values []string, label string) (map[string]coverage.DecisionStatus, error) {
	out := map[string]coverage.DecisionStatus{}
	for _, raw := range values {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return nil, fmt.Errorf("%s decision must use <id>=<PASS|FAIL|PENDING>", label)
		}
		id := strings.TrimSpace(parts[0])
		status := coverage.DecisionStatus(strings.ToUpper(strings.TrimSpace(parts[1])))
		if status != coverage.StatusPass && status != coverage.StatusFail && status != coverage.StatusPending {
			return nil, fmt.Errorf("%s decision for %s must be PASS, FAIL, or PENDING", label, id)
		}
		if _, duplicate := out[id]; duplicate {
			return nil, fmt.Errorf("duplicate %s decision for %s", label, id)
		}
		out[id] = status
	}
	return out, nil
}

func sortedDecisionMap(values map[string]coverage.DecisionStatus) []coverage.ReviewDecision {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]coverage.ReviewDecision, 0, len(ids))
	for _, id := range ids {
		out = append(out, coverage.ReviewDecision{ID: id, Status: values[id]})
	}
	return out
}

func allDecisionPass(values []coverage.ReviewDecision) bool {
	for _, decision := range values {
		if decision.Status != coverage.StatusPass {
			return false
		}
	}
	return true
}

func copyStringSlice(values []string) []string { return append([]string(nil), values...) }

func marshalWithoutDigest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

type guaranteeCaseEvidence struct {
	Kind      coverage.ReviewKind
	Case      QACase
	Review    GuaranteeReviewRecord
	HasReview bool
	Qualified string
}

type SliceGuaranteeRecord struct {
	RunID               string                           `json:"runId"`
	MasterRunID         string                           `json:"masterRunId"`
	Activation          string                           `json:"activation"`
	RequirementRevision string                           `json:"requirementRevision"`
	ManifestDigest      string                           `json:"manifestDigest"`
	Snapshot            string                           `json:"snapshot"`
	RouteMode           string                           `json:"routeMode"`
	CasesByMode         map[string][]QACase              `json:"casesByMode"`
	ReviewsByMode       map[string]GuaranteeReviewRecord `json:"reviewsByMode"`
	Digest              string                           `json:"digest"`
}

func sliceGuaranteeDir(root, masterRunID string) string {
	return filepath.Join(RunDir(root, masterRunID), "slice-guarantee")
}

func sliceGuaranteePath(root, masterRunID, sliceRunID string) string {
	return filepath.Join(sliceGuaranteeDir(root, masterRunID), sliceRunID+".json")
}

func saveSliceGuaranteeRecord(root string, state RunState) error {
	if state.SplitMasterRunID == "" || state.RequirementGuarantee == nil {
		return nil
	}
	record := SliceGuaranteeRecord{
		RunID:               state.RunID,
		MasterRunID:         state.SplitMasterRunID,
		Activation:          state.RequirementGuarantee.Activation,
		RequirementRevision: state.RequirementRevision,
		ManifestDigest:      state.RequirementGuarantee.ManifestDigest,
		Snapshot:            state.CurrentSnapshot,
		RouteMode:           state.RouteMode,
		CasesByMode:         state.QACasesByMode,
		ReviewsByMode:       state.RequirementGuarantee.ReviewsByMode,
	}
	digest, err := marshalWithoutDigest(record)
	if err != nil {
		return err
	}
	record.Digest = digest
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	path := sliceGuaranteePath(root, state.SplitMasterRunID, state.RunID)
	if existing, err := os.ReadFile(path); err == nil {
		var prior SliceGuaranteeRecord
		if json.Unmarshal(existing, &prior) == nil && prior.Digest == record.Digest {
			return nil
		}
		return fmt.Errorf("sealed slice guarantee record %s is immutable and already exists with different content", state.RunID)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'), 0o600)
}

func loadSliceGuaranteeRecord(root, masterRunID, sliceRunID string) (SliceGuaranteeRecord, error) {
	data, err := os.ReadFile(sliceGuaranteePath(root, masterRunID, sliceRunID))
	if err != nil {
		return SliceGuaranteeRecord{}, err
	}
	var record SliceGuaranteeRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return SliceGuaranteeRecord{}, err
	}
	digest := record.Digest
	record.Digest = ""
	want, err := marshalWithoutDigest(record)
	if err != nil {
		return SliceGuaranteeRecord{}, err
	}
	record.Digest = digest
	if digest == "" || digest != want || record.RunID != sliceRunID || record.MasterRunID != masterRunID {
		return SliceGuaranteeRecord{}, fmt.Errorf("slice guarantee record %s is damaged or mismatched", sliceRunID)
	}
	return record, nil
}

func guaranteeTargetACs(state RunState) map[string]bool {
	_, conditions := requirementIndex(state.RequirementGuarantee.Projection)
	targets := map[string]bool{}
	for acID := range conditions {
		targets[acID] = true
	}
	if state.SplitMasterRunID != "" && state.Slicing != nil {
		for acID := range targets {
			if state.Slicing.ACResponsibilities[acID] != state.RunID {
				delete(targets, acID)
			}
		}
	}
	return targets
}

func qualifiedGuaranteeCaseID(scope string, kind coverage.ReviewKind, caseID string) string {
	return scope + "::" + string(kind) + "::" + caseID
}

func collectLocalGuaranteeEvidence(state RunState, qualify bool, merge bool) []guaranteeCaseEvidence {
	var evidence []guaranteeCaseEvidence
	keys := make([]string, 0, len(state.QACasesByMode))
	for key := range state.QACasesByMode {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for _, testCase := range state.QACasesByMode[key] {
			kind := coverageKind(testCase.Mode, merge)
			qualified := testCase.ID
			if qualify {
				qualified = qualifiedGuaranteeCaseID(state.RunID, kind, testCase.ID)
			}
			review, ok := state.RequirementGuarantee.ReviewsByMode[key]
			evidence = append(evidence, guaranteeCaseEvidence{Kind: kind, Case: testCase, Review: review, HasReview: ok, Qualified: qualified})
		}
	}
	return evidence
}

func collectGuaranteeEvidence(root string, state RunState) ([]guaranteeCaseEvidence, bool, error) {
	if !state.RetainedOverall || state.Slicing == nil || state.Slicing.Decision != "split" {
		return collectLocalGuaranteeEvidence(state, false, false), false, nil
	}
	var evidence []guaranteeCaseEvidence
	sliceNotGuaranteed := false
	owners := map[string]bool{}
	for _, owner := range state.Slicing.ACResponsibilities {
		if owner != masterMergeOwner {
			owners[owner] = true
		}
	}
	ownerIDs := make([]string, 0, len(owners))
	for owner := range owners {
		ownerIDs = append(ownerIDs, owner)
	}
	sort.Strings(ownerIDs)
	for _, owner := range ownerIDs {
		record, err := loadSliceGuaranteeRecord(root, state.RunID, owner)
		if err != nil {
			return evidence, sliceNotGuaranteed, fmt.Errorf("sealed guarantee evidence for responsibility slice %s is missing: %w", owner, err)
		}
		if record.RequirementRevision != state.RequirementRevision || record.ManifestDigest != state.RequirementGuarantee.ManifestDigest {
			return evidence, sliceNotGuaranteed, fmt.Errorf("slice %s guarantee evidence is bound to a different requirement revision or manifest", owner)
		}
		if record.Activation == guaranteeNotGuaranteed || record.Activation == guaranteeWaived {
			sliceNotGuaranteed = true
		}
		keys := make([]string, 0, len(record.CasesByMode))
		for key := range record.CasesByMode {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			for _, testCase := range record.CasesByMode[key] {
				kind := coverageKind(testCase.Mode, false)
				review, ok := record.ReviewsByMode[key]
				evidence = append(evidence, guaranteeCaseEvidence{Kind: kind, Case: testCase, Review: review, HasReview: ok, Qualified: qualifiedGuaranteeCaseID(owner, kind, testCase.ID)})
			}
		}
	}
	evidence = append(evidence, collectLocalGuaranteeEvidence(state, true, true)...)
	return evidence, sliceNotGuaranteed, nil
}

func localGuaranteeManifest(state RunState, mode string, cases []QACase) (coverage.AcceptanceManifest, error) {
	if err := validateGuaranteeEnvelope(state); err != nil {
		return coverage.AcceptanceManifest{}, err
	}
	merge := state.RetainedOverall && state.Slicing != nil && state.Slicing.Decision == "split"
	kind := coverageKind(mode, merge)
	evidence := make([]guaranteeCaseEvidence, 0, len(cases))
	for _, testCase := range cases {
		if err := validateGuaranteeCaseBindings(state, testCase); err != nil {
			return coverage.AcceptanceManifest{}, err
		}
		evidence = append(evidence, guaranteeCaseEvidence{Kind: kind, Case: testCase, Qualified: testCase.ID})
	}
	return buildGuaranteeManifest(state, kind, evidence)
}

// buildGuaranteeManifest is the single REQ/AC/case projection used for both a
// local QA review and the retained master's aggregate evidence contract. The
// caller decides whether case identities are local or scope-qualified.
func buildGuaranteeManifest(state RunState, kind coverage.ReviewKind, evidence []guaranteeCaseEvidence) (coverage.AcceptanceManifest, error) {
	_, conditions := requirementIndex(state.RequirementGuarantee.Projection)
	pointMap := map[string]coverage.AcceptancePoint{}
	sourceSet := map[string]bool{}
	manifestCases := make([]coverage.AcceptanceCase, 0, len(evidence))
	var edges []coverage.CoverageEdge
	for _, item := range evidence {
		testCase := item.Case
		caseValue := coverage.AcceptanceCase{CaseID: item.Qualified, Oracle: testCase.Oracle}
		if kind == coverage.ReviewWhitebox {
			caseValue.Mode, caseValue.TestRef = coverage.CaseWhitebox, testCase.Test
		} else {
			caseValue.Mode = coverage.CaseBlackbox
			if kind == coverage.ReviewMerge {
				caseValue.Mode = coverage.CaseMerge
			}
			caseValue.PublicEntry = testCase.Description
			caseValue.Preconditions = "confirmed requirement projection"
			caseValue.Steps = testCase.Procedure
		}
		manifestCases = append(manifestCases, caseValue)
		for _, acID := range guaranteeAllACs(testCase) {
			entry, ok := conditions[acID]
			if !ok {
				return coverage.AcceptanceManifest{}, fmt.Errorf("case %s references unknown AC %s", item.Qualified, acID)
			}
			pointID := coveragePointID(kind, acID)
			pointMap[pointID] = coverage.AcceptancePoint{PointID: pointID, ObservableBehavior: entry.Condition.Text, Oracle: testCase.Oracle}
			sourceSet[entry.RequirementID] = true
			edges = append(edges, coverage.CoverageEdge{ReviewKind: kind, SourceID: entry.RequirementID, PointID: pointID, CaseID: item.Qualified})
		}
	}
	points := make([]coverage.AcceptancePoint, 0, len(pointMap))
	for _, point := range pointMap {
		points = append(points, point)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].PointID < points[j].PointID })
	sources := make([]string, 0, len(sourceSet))
	for source := range sourceSet {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	binding := coverage.SourceBinding{RunID: state.RunID, RequirementRevision: state.RequirementRevision, SolutionRevision: state.RequirementRevision}
	return coverage.AcceptanceManifest{
		Binding: coverage.ManifestBinding{SourceBinding: binding, ReviewKind: kind, RouteScope: state.RouteMode, TopologyScope: guaranteeTopology(state)},
		Sources: sources, Points: points, Cases: manifestCases, Edges: edges,
	}, nil
}

func guaranteeTopology(state RunState) string {
	if state.SplitMasterRunID != "" {
		return "slice:" + state.RunID
	}
	if state.RetainedOverall {
		return "retained-master"
	}
	return "no-split"
}

// guaranteeReviewDecisionIDs returns the finite REQ/AC projection represented
// by this QA mode's cases. Cross-mode completeness is enforced later by the
// union evidence contract, so one mode must not repeat decisions owned only by
// another selected mode.
func guaranteeReviewDecisionIDs(state RunState, kind coverage.ReviewKind, cases []QACase) ([]string, []string) {
	_, conditions := requirementIndex(state.RequirementGuarantee.Projection)
	targets := map[string]bool{}
	for _, testCase := range cases {
		for _, acID := range guaranteeAllACs(testCase) {
			targets[acID] = true
		}
	}
	sourceSet := map[string]bool{}
	points := make([]string, 0, len(targets))
	for acID := range targets {
		sourceSet[conditions[acID].RequirementID] = true
		points = append(points, coveragePointID(kind, acID))
	}
	sources := make([]string, 0, len(sourceSet))
	for sourceID := range sourceSet {
		sources = append(sources, sourceID)
	}
	sort.Strings(sources)
	sort.Strings(points)
	return sources, points
}

func decisionSubset(values map[string]coverage.DecisionStatus, ids []string) []coverage.ReviewDecision {
	subset := make(map[string]coverage.DecisionStatus, len(ids))
	for _, id := range ids {
		if status, ok := values[id]; ok {
			subset[id] = status
		}
	}
	return sortedDecisionMap(subset)
}

func exactDecisionSet(values map[string]coverage.DecisionStatus, expected []string, label string) error {
	want := map[string]bool{}
	for _, id := range expected {
		want[id] = true
	}
	for id := range values {
		if !want[id] {
			return fmt.Errorf("unknown %s decision %s", label, id)
		}
	}
	for _, id := range expected {
		if _, ok := values[id]; !ok {
			return fmt.Errorf("missing explicit %s decision for %s", label, id)
		}
	}
	return nil
}

func recordGuaranteeReview(state *RunState, mode string, cases []QACase, options QAReviewRecordOptions) error {
	if state.RequirementGuarantee == nil {
		return nil
	}
	manifest, err := localGuaranteeManifest(*state, mode, cases)
	if err != nil {
		return err
	}
	sourceValues, err := parseDecisionValues(options.SourceDecisions, "source")
	if err != nil {
		return err
	}
	pointInput, err := parseDecisionValues(options.PointDecisions, "point")
	if err != nil {
		return err
	}
	kind := manifest.Binding.ReviewKind
	pointValues := map[string]coverage.DecisionStatus{}
	for acID, status := range pointInput {
		pointValues[coveragePointID(kind, acID)] = status
	}
	if len(cases) == 0 && isMergeVerification(*state) {
		return nil
	}
	expectedSources, expectedPoints := guaranteeReviewDecisionIDs(*state, kind, cases)
	if err := exactDecisionSet(sourceValues, expectedSources, "source"); err != nil {
		return err
	}
	if err := exactDecisionSet(pointValues, expectedPoints, "point"); err != nil {
		return err
	}
	caseValues, err := parseDecisionValues(options.CaseDecisions, "case")
	if err != nil {
		return err
	}
	caseIDs := make([]string, len(manifest.Cases))
	for i, testCase := range manifest.Cases {
		caseIDs[i] = testCase.CaseID
	}
	if err := exactDecisionSet(caseValues, caseIDs, "case"); err != nil {
		return err
	}
	for _, testCase := range cases {
		if caseValues[testCase.ID] != coverage.DecisionStatus(testCase.ReviewStatus) {
			return fmt.Errorf("explicit case decision for %s does not match its QA Review outcome", testCase.ID)
		}
	}
	unboundPoints := make([]string, 0, len(options.UnboundPoints))
	for _, acID := range options.UnboundPoints {
		unboundPoints = append(unboundPoints, coveragePointID(kind, strings.TrimSpace(acID)))
	}
	review := coverage.ReviewResult{
		Binding: manifest.Binding, Scope: coverage.ScopeFull,
		SourceDecisions: sortedDecisionMap(sourceValues), PointDecisions: sortedDecisionMap(pointValues), CaseDecisions: sortedDecisionMap(caseValues),
		UnboundSources: copyStringSlice(options.UnboundSources), UnboundPoints: unboundPoints, UnboundCases: copyStringSlice(options.UnboundCases),
	}
	if allDecisionPass(review.SourceDecisions) && allDecisionPass(review.PointDecisions) && allDecisionPass(review.CaseDecisions) && len(review.UnboundSources)+len(review.UnboundPoints)+len(review.UnboundCases) == 0 {
		review.SetStatus = coverage.StatusPass
	}
	required := make([]coverage.RequiredSource, 0, len(manifest.Sources))
	for _, sourceID := range manifest.Sources {
		required = append(required, coverage.RequiredSource{SourceID: sourceID, Category: coverage.ProductRequirement, Applicability: coverage.QAApplicability})
	}
	contract := coverage.CoverageContract{RequiredSources: coverage.RequiredSources{Binding: manifest.Binding.SourceBinding, Sources: required}, SelectedKinds: []coverage.ReviewKind{kind}, Manifests: []coverage.AcceptanceManifest{manifest}}
	record := GuaranteeReviewRecord{Review: review}
	if review.SetStatus == coverage.StatusPass {
		// The existing coverage contract represents only source/point/case triples
		// with an edge. The adapter retains the complete explicit REQ/AC review
		// above, then passes only this manifest's represented subset to the existing
		// whitelist projection; final guarantee derivation still rejects any AC with
		// no case in the selected-mode union.
		contractReview := review
		pointIDs := make([]string, len(manifest.Points))
		for i, point := range manifest.Points {
			pointIDs[i] = point.PointID
		}
		contractReview.SourceDecisions = decisionSubset(sourceValues, manifest.Sources)
		contractReview.PointDecisions = decisionSubset(pointValues, pointIDs)
		contractReview.CaseDecisions = decisionSubset(caseValues, caseIDs)
		whitelist, err := coverage.ProjectWhitelist(contract, []coverage.ReviewResult{contractReview})
		if err != nil {
			return err
		}
		record.Whitelist = &whitelist
	}
	state.RequirementGuarantee.ReviewsByMode[mode] = record
	return nil
}

func guaranteeEvidenceContract(state RunState, evidence []guaranteeCaseEvidence) (coverage.CoverageContract, map[string]guaranteeCaseEvidence, error) {
	_, conditions := requirementIndex(state.RequirementGuarantee.Projection)
	targets := guaranteeTargetACs(state)
	coveredPrimary := map[string]bool{}
	byKind := map[coverage.ReviewKind][]guaranteeCaseEvidence{}
	byID := map[string]guaranteeCaseEvidence{}
	for _, item := range evidence {
		if len(item.Case.AcceptanceCriteria) == 0 {
			return coverage.CoverageContract{}, nil, fmt.Errorf("case %s has no primary AC binding", item.Qualified)
		}
		for _, acID := range item.Case.AcceptanceCriteria {
			if targets[acID] {
				coveredPrimary[acID] = true
			}
		}
		if _, duplicate := byID[item.Qualified]; duplicate {
			return coverage.CoverageContract{}, nil, fmt.Errorf("duplicate qualified QA case identity %s", item.Qualified)
		}
		byID[item.Qualified] = item
		byKind[item.Kind] = append(byKind[item.Kind], item)
	}
	for acID := range targets {
		if !coveredPrimary[acID] {
			return coverage.CoverageContract{}, byID, fmt.Errorf("acceptance condition %s has no QA case with primary responsibility coverage", acID)
		}
	}
	requiredSet := map[string]bool{}
	for acID := range targets {
		requiredSet[conditions[acID].RequirementID] = true
	}
	kinds := make([]coverage.ReviewKind, 0, len(byKind))
	for kind := range byKind {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	binding := coverage.SourceBinding{RunID: state.RunID, RequirementRevision: state.RequirementRevision, SolutionRevision: state.RequirementRevision}
	contract := coverage.CoverageContract{RequiredSources: coverage.RequiredSources{Binding: binding}}
	for _, kind := range kinds {
		manifest, err := buildGuaranteeManifest(state, kind, byKind[kind])
		if err != nil {
			return coverage.CoverageContract{}, byID, err
		}
		for _, sourceID := range manifest.Sources {
			requiredSet[sourceID] = true
		}
		contract.SelectedKinds = append(contract.SelectedKinds, kind)
		contract.Manifests = append(contract.Manifests, manifest)
	}
	required := make([]coverage.RequiredSource, 0, len(requiredSet))
	for sourceID := range requiredSet {
		required = append(required, coverage.RequiredSource{SourceID: sourceID, Category: coverage.ProductRequirement, Applicability: coverage.QAApplicability})
	}
	sort.Slice(required, func(i, j int) bool { return required[i].SourceID < required[j].SourceID })
	contract.RequiredSources.Sources = required
	if err := contract.Validate(); err != nil {
		return coverage.CoverageContract{}, byID, err
	}
	return contract, byID, nil
}

func guaranteeEvidenceReviews(contract coverage.CoverageContract, evidence []guaranteeCaseEvidence) ([]coverage.ReviewResult, error) {
	var reviews []coverage.ReviewResult
	for _, manifest := range contract.Manifests {
		manifestSources := map[string]bool{}
		for _, sourceID := range manifest.Sources {
			manifestSources[sourceID] = true
		}
		manifestPoints := map[string]bool{}
		for _, point := range manifest.Points {
			manifestPoints[point.PointID] = true
		}
		sourceValues := map[string]coverage.DecisionStatus{}
		pointValues := map[string]coverage.DecisionStatus{}
		caseValues := map[string]coverage.DecisionStatus{}
		for _, item := range evidence {
			if item.Kind != manifest.Binding.ReviewKind || !item.HasReview {
				continue
			}
			if item.Review.Review.SetStatus != coverage.StatusPass || item.Review.Whitelist == nil {
				return nil, fmt.Errorf("QA case %s does not have a complete explicit PASS review", item.Qualified)
			}
			for _, decision := range item.Review.Review.SourceDecisions {
				if !manifestSources[decision.ID] {
					continue
				}
				if prior, ok := sourceValues[decision.ID]; ok && prior != decision.Status {
					return nil, fmt.Errorf("conflicting explicit source review decisions for %s", decision.ID)
				}
				sourceValues[decision.ID] = decision.Status
			}
			for _, decision := range item.Review.Review.PointDecisions {
				if !manifestPoints[decision.ID] {
					continue
				}
				if prior, ok := pointValues[decision.ID]; ok && prior != decision.Status {
					return nil, fmt.Errorf("conflicting explicit point review decisions for %s", decision.ID)
				}
				pointValues[decision.ID] = decision.Status
			}
			for _, decision := range item.Review.Review.CaseDecisions {
				if decision.ID == item.Case.ID {
					caseValues[item.Qualified] = decision.Status
				}
			}
		}
		if err := exactDecisionSet(sourceValues, manifest.Sources, "source"); err != nil {
			return nil, err
		}
		pointIDs := make([]string, len(manifest.Points))
		for i := range manifest.Points {
			pointIDs[i] = manifest.Points[i].PointID
		}
		if err := exactDecisionSet(pointValues, pointIDs, "point"); err != nil {
			return nil, err
		}
		caseIDs := make([]string, len(manifest.Cases))
		for i := range manifest.Cases {
			caseIDs[i] = manifest.Cases[i].CaseID
		}
		if err := exactDecisionSet(caseValues, caseIDs, "case"); err != nil {
			return nil, err
		}
		reviews = append(reviews, coverage.ReviewResult{Binding: manifest.Binding, Scope: coverage.ScopeFull, SourceDecisions: sortedDecisionMap(sourceValues), PointDecisions: sortedDecisionMap(pointValues), CaseDecisions: sortedDecisionMap(caseValues), SetStatus: coverage.StatusPass})
	}
	return reviews, nil
}

func guaranteeExecutionReport(state RunState, whitelist coverage.ApprovedWhitelist, evidenceByID map[string]guaranteeCaseEvidence) (coverage.ExecutionReport, error) {
	digests := coverage.ExecutionBinding{RequiredSourcesDigest: whitelist.RequiredSourcesDigest, ManifestDigest: whitelist.ManifestDigest, MapDigest: whitelist.MapDigest, WhitelistDigest: whitelist.Digest, Candidate: coverage.ValidationCandidate{Identity: state.CurrentSnapshot, Digest: state.CurrentSnapshot}, Scope: coverage.ScopeFull}
	caseSet := map[string]bool{}
	for _, entry := range whitelist.Entries {
		caseSet[entry.CaseID] = true
	}
	for id := range caseSet {
		digests.ExpectedCaseIDs = append(digests.ExpectedCaseIDs, id)
		digests.ActualCaseIDs = append(digests.ActualCaseIDs, id)
	}
	sort.Strings(digests.ExpectedCaseIDs)
	sort.Strings(digests.ActualCaseIDs)
	results := map[string]QAResultRecord{}
	if state.RetainedOverall && state.Slicing != nil && state.Slicing.Decision == "split" {
		result := state.qaExecution("")
		if result.Snapshot != state.CurrentSnapshot {
			return coverage.ExecutionReport{}, fmt.Errorf("final master QA execution is not bound to the current merged candidate")
		}
		for _, record := range result.Cases {
			results[record.CaseID] = record
		}
	} else {
		for id, evidence := range evidenceByID {
			result := qaModeResult(state, evidence.Case.Mode)
			if result.Snapshot != state.CurrentSnapshot {
				continue
			}
			for _, record := range result.Cases {
				if record.CaseID == evidence.Case.ID {
					results[id] = record
				}
			}
		}
	}
	report := coverage.ExecutionReport{Binding: digests}
	for _, entry := range whitelist.Entries {
		record, ok := results[entry.CaseID]
		if !ok {
			return coverage.ExecutionReport{}, fmt.Errorf("approved QA case %s has no result on the current candidate", entry.CaseID)
		}
		result := coverage.ExecutedFail
		if record.Outcome == "PASS" {
			result = coverage.ExecutedPass
		}
		provenance := coverage.ProvenanceExecuted
		if record.Origin == "inherited" {
			provenance = coverage.ProvenanceInherited
		}
		report.Records = append(report.Records, coverage.ExecutionRecord{ReviewKind: entry.ReviewKind, SourceID: entry.SourceID, PointID: entry.PointID, CaseID: entry.CaseID, Result: result, Provenance: provenance})
	}
	return report, nil
}

func deriveRequirementGuarantee(root string, state RunState) RequirementGuaranteeReport {
	g := state.RequirementGuarantee
	if g == nil {
		return RequirementGuaranteeReport{}
	}
	report := RequirementGuaranteeReport{Status: g.Activation, Reason: g.Reason, RequirementRevision: g.RequirementRevision, SolutionRevision: g.SolutionRevision, ManifestDigest: g.ManifestDigest}
	if g.Projection == nil {
		return report
	}
	evidence, sliceNotGuaranteed, evidenceErr := collectGuaranteeEvidence(root, state)
	setRequirementGuaranteeItems(&report, state, evidence, nil)
	if g.Activation == guaranteeWaived || g.Activation == guaranteeNotGuaranteed || g.Activation == guaranteeFrozen || g.Activation == guaranteeBlocked {
		return report
	}
	if err := validateGuaranteeEnvelope(state); err != nil {
		report.Status, report.Reason = guaranteeBlocked, err.Error()
		return report
	}
	if err := requireGuaranteeResponsibilityBinding(state); err != nil {
		report.Status, report.Reason = "incomplete", err.Error()
		return report
	}
	if sliceNotGuaranteed {
		report.Status, report.Reason = guaranteeNotGuaranteed, "at least one AC-responsibility slice selected custom without QA or waived its guarantee"
		return report
	}
	if evidenceErr != nil {
		report.Status, report.Reason = "incomplete", evidenceErr.Error()
		return report
	}
	contract, byID, err := guaranteeEvidenceContract(state, evidence)
	if err != nil {
		report.Status, report.Reason = "incomplete", err.Error()
		return report
	}
	reviews, err := guaranteeEvidenceReviews(contract, evidence)
	if err != nil {
		report.Status, report.Reason = "incomplete", err.Error()
		return report
	}
	whitelist, err := coverage.ProjectWhitelist(contract, reviews)
	if err != nil {
		report.Status, report.Reason = "incomplete", err.Error()
		return report
	}
	execution, err := guaranteeExecutionReport(state, whitelist, byID)
	if err != nil {
		report.Status, report.Reason = "incomplete", err.Error()
		return report
	}
	setRequirementGuaranteeItems(&report, state, evidence, &execution)
	contract.Candidate = &coverage.ValidationCandidate{Identity: state.CurrentSnapshot, Digest: state.CurrentSnapshot}
	if err := coverage.ValidateExecution(contract, whitelist, execution); err != nil {
		report.Status, report.Reason = "incomplete", err.Error()
		return report
	}
	report.Status = "pass"
	report.Reason = "all confirmed REQ/AC items have approved case coverage and FULL execution PASS on the current candidate"
	return report
}

func guaranteeItemReports(state RunState, evidence []guaranteeCaseEvidence, execution *coverage.ExecutionReport) []RequirementGuaranteeItemReport {
	targets := guaranteeTargetACs(state)
	var items []RequirementGuaranteeItemReport
	for _, req := range state.RequirementGuarantee.Projection.Requirements {
		for _, ac := range req.AcceptanceConditions {
			if !targets[ac.ID] {
				continue
			}
			owner := state.RunID
			if state.Slicing != nil && state.Slicing.ACResponsibilities[ac.ID] != "" {
				owner = state.Slicing.ACResponsibilities[ac.ID]
			}
			item := RequirementGuaranteeItemReport{RequirementID: req.ID, AcceptanceID: ac.ID, Owner: owner, Cases: []string{}, ReviewStatus: "PENDING", Execution: "PENDING"}
			reviewStates, executionStates := []string{}, []string{}
			seenCases := map[string]bool{}
			for _, evidenceItem := range evidence {
				for _, bound := range guaranteeAllACs(evidenceItem.Case) {
					if bound != ac.ID || seenCases[evidenceItem.Qualified] {
						continue
					}
					seenCases[evidenceItem.Qualified] = true
					item.Cases = append(item.Cases, evidenceItem.Qualified)
					reviewStates = append(reviewStates, guaranteeCaseReviewStatus(evidenceItem, req.ID, ac.ID))
					executionStates = append(executionStates, guaranteeCaseExecutionStatus(state, evidenceItem, execution))
				}
			}
			sort.Strings(item.Cases)
			item.ReviewStatus = aggregateGuaranteeStatus(reviewStates)
			item.Execution = aggregateGuaranteeStatus(executionStates)
			items = append(items, item)
		}
	}
	return items
}

func setRequirementGuaranteeItems(report *RequirementGuaranteeReport, state RunState, evidence []guaranteeCaseEvidence, execution *coverage.ExecutionReport) {
	report.Items = guaranteeItemReports(state, evidence, execution)
	report.Requirements = guaranteeRequirementReports(report.Items)
	report.AcceptanceCount = len(report.Items)
	report.RequirementCount = len(report.Requirements)
}

func guaranteeCaseReviewStatus(item guaranteeCaseEvidence, requirementID, acceptanceID string) string {
	if !item.HasReview {
		return "PENDING"
	}
	review := item.Review.Review
	if containsExactString(review.UnboundSources, requirementID) || containsExactString(review.UnboundPoints, coveragePointID(item.Kind, acceptanceID)) || containsExactString(review.UnboundCases, item.Case.ID) {
		return "FAIL"
	}
	statuses := []coverage.DecisionStatus{
		reviewDecisionStatus(review.SourceDecisions, requirementID),
		reviewDecisionStatus(review.PointDecisions, coveragePointID(item.Kind, acceptanceID)),
		reviewDecisionStatus(review.CaseDecisions, item.Case.ID),
	}
	for _, status := range statuses {
		if status == coverage.StatusFail {
			return "FAIL"
		}
	}
	for _, status := range statuses {
		if status != coverage.StatusPass {
			return "PENDING"
		}
	}
	return "PASS"
}

func reviewDecisionStatus(decisions []coverage.ReviewDecision, id string) coverage.DecisionStatus {
	for _, decision := range decisions {
		if decision.ID == id {
			return decision.Status
		}
	}
	return coverage.StatusPending
}

func guaranteeCaseExecutionStatus(state RunState, item guaranteeCaseEvidence, execution *coverage.ExecutionReport) string {
	if execution != nil {
		statuses := []string{}
		for _, record := range execution.Records {
			if record.CaseID != item.Qualified {
				continue
			}
			if record.Result == coverage.ExecutedFail {
				statuses = append(statuses, "FAIL")
			} else if record.Result == coverage.ExecutedPass && record.Provenance == coverage.ProvenanceExecuted {
				statuses = append(statuses, "PASS")
			} else {
				statuses = append(statuses, "PENDING")
			}
		}
		return aggregateGuaranteeStatus(statuses)
	}

	result, caseID := qaModeResult(state, item.Case.Mode), item.Case.ID
	if state.RetainedOverall && state.Slicing != nil && state.Slicing.Decision == "split" {
		result, caseID = state.qaExecution(""), item.Qualified
	}
	if result.Snapshot == "" || result.Snapshot != state.CurrentSnapshot {
		return "PENDING"
	}
	for _, record := range result.Cases {
		if record.CaseID != caseID {
			continue
		}
		if record.Outcome == "FAIL" {
			return "FAIL"
		}
		if record.Outcome == "PASS" && record.Origin != "inherited" {
			return "PASS"
		}
	}
	return "PENDING"
}

func aggregateGuaranteeStatus(statuses []string) string {
	if len(statuses) == 0 {
		return "PENDING"
	}
	allPass := true
	for _, status := range statuses {
		if status == "FAIL" {
			return "FAIL"
		}
		if status != "PASS" {
			allPass = false
		}
	}
	if allPass {
		return "PASS"
	}
	return "PENDING"
}

func guaranteeRequirementReports(items []RequirementGuaranteeItemReport) []RequirementGuaranteeRequirementReport {
	var reports []RequirementGuaranteeRequirementReport
	indexes := map[string]int{}
	ownerSets, caseSets := map[string]map[string]bool{}, map[string]map[string]bool{}
	for _, item := range items {
		index, ok := indexes[item.RequirementID]
		if !ok {
			index = len(reports)
			indexes[item.RequirementID] = index
			ownerSets[item.RequirementID] = map[string]bool{}
			caseSets[item.RequirementID] = map[string]bool{}
			reports = append(reports, RequirementGuaranteeRequirementReport{RequirementID: item.RequirementID, Owners: []string{}, AcceptanceIDs: []string{}, Cases: []string{}, ReviewStatus: "PASS", Execution: "PASS", Status: "PASS"})
		}
		report := &reports[index]
		report.AcceptanceIDs = append(report.AcceptanceIDs, item.AcceptanceID)
		ownerSets[item.RequirementID][item.Owner] = true
		for _, caseID := range item.Cases {
			caseSets[item.RequirementID][caseID] = true
		}
		report.ReviewStatus = mergeGuaranteeStatus(report.ReviewStatus, item.ReviewStatus)
		report.Execution = mergeGuaranteeStatus(report.Execution, item.Execution)
		if report.ReviewStatus != "PASS" || report.Execution != "PASS" {
			report.Status = "INCOMPLETE"
		}
	}
	for index := range reports {
		report := &reports[index]
		for owner := range ownerSets[report.RequirementID] {
			report.Owners = append(report.Owners, owner)
		}
		for caseID := range caseSets[report.RequirementID] {
			report.Cases = append(report.Cases, caseID)
		}
		sort.Strings(report.Owners)
		sort.Strings(report.Cases)
	}
	return reports
}

func mergeGuaranteeStatus(current, next string) string {
	if current == "FAIL" || next == "FAIL" {
		return "FAIL"
	}
	if current != "PASS" || next != "PASS" {
		return "PENDING"
	}
	return "PASS"
}

func refreshRequirementGuarantee(root string, state *RunState) {
	if state.RequirementGuarantee == nil {
		return
	}
	state.RequirementGuarantee.Report = deriveRequirementGuarantee(root, *state)
}

func LoadRunStateForShow(root, runID string) (RunState, error) {
	state, err := LoadRunState(root, runID)
	if err != nil {
		return RunState{}, err
	}
	refreshRequirementGuarantee(root, &state)
	return state, nil
}

func requireRequirementGuaranteeComplete(root string, state *RunState) error {
	if state.RequirementGuarantee == nil {
		return requireExplicitGuaranteeForQASelection(*state, state.SelectedGates)
	}
	refreshRequirementGuarantee(root, state)
	status := state.RequirementGuarantee.Report.Status
	if status == "pass" || status == guaranteeNotGuaranteed || status == guaranteeWaived {
		return nil
	}
	gaps := requirementGuaranteeGapDetails(state.RequirementGuarantee.Report)
	if len(gaps) == 0 {
		gaps = append(gaps, state.RequirementGuarantee.Report.Reason)
	}
	return fmt.Errorf("requirement guarantee is not complete: %s", strings.Join(gaps, "; "))
}

func requirementGuaranteeGapDetails(report RequirementGuaranteeReport) []string {
	var unresolved []string
	for _, item := range report.Items {
		if item.ReviewStatus == "PASS" && item.Execution == "PASS" {
			continue
		}
		identifier := item.RequirementID + "/" + item.AcceptanceID
		if len(item.Cases) != 0 {
			identifier += " cases=" + strings.Join(item.Cases, ",")
		}
		unresolved = append(unresolved, identifier+" review="+item.ReviewStatus+" execution="+item.Execution)
	}
	return unresolved
}

func authorizeRequirementGuaranteeWaiver(root string, state *RunState, reason string) error {
	if state.RequirementGuarantee == nil {
		return fmt.Errorf("this run did not explicitly activate the requirement guarantee")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("waiving the requirement guarantee requires --guarantee-waiver-reason")
	}
	refreshRequirementGuarantee(root, state)
	var unresolved []string
	for _, item := range state.RequirementGuarantee.Report.Items {
		if item.ReviewStatus != "PASS" || item.Execution != "PASS" {
			unresolved = append(unresolved, item.RequirementID+"/"+item.AcceptanceID+" review="+item.ReviewStatus+" execution="+item.Execution)
		}
	}
	if len(unresolved) == 0 && state.RequirementGuarantee.Report.Reason != "" {
		unresolved = append(unresolved, state.RequirementGuarantee.Report.Reason)
	}
	state.RequirementGuarantee.Waiver = &RequirementGuaranteeWaiver{Origin: "SEAL-USER", Reason: reason, Snapshot: state.CurrentSnapshot, Unresolved: unresolved}
	state.RequirementGuarantee.Activation = guaranteeWaived
	state.RequirementGuarantee.Reason = reason
	refreshRequirementGuarantee(root, state)
	return nil
}

func masterFinalGuaranteeCases(root string, state RunState) ([]QACase, error) {
	evidence, notGuaranteed, err := collectGuaranteeEvidence(root, state)
	if err != nil {
		return nil, err
	}
	if notGuaranteed {
		return nil, fmt.Errorf("the retained master is not guaranteed because a responsibility slice selected no QA")
	}
	contract, _, err := guaranteeEvidenceContract(state, evidence)
	if err != nil {
		return nil, err
	}
	reviews, err := guaranteeEvidenceReviews(contract, evidence)
	if err != nil {
		return nil, err
	}
	whitelist, err := coverage.ProjectWhitelist(contract, reviews)
	if err != nil {
		return nil, err
	}
	approved := map[string]bool{}
	for _, entry := range whitelist.Entries {
		approved[entry.CaseID] = true
	}
	cases := make([]QACase, 0, len(evidence))
	for _, item := range evidence {
		if !approved[item.Qualified] {
			continue
		}
		copyCase := item.Case
		copyCase.ID = item.Qualified
		copyCase.ReviewStatus = "PASS"
		cases = append(cases, copyCase)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	return cases, nil
}

// approvedSliceBlackboxCases returns only the blackbox definitions contributed
// by immutable slice guarantee records. The retained master's own cases remain
// owned by its normal QACasesByMode table and are materialized separately, so
// merge-interaction cases keep their established representation while slice
// case IDs are scope-qualified and cannot collide.
func approvedSliceBlackboxCases(root string, state RunState) ([]QACase, error) {
	evidence, _, err := collectGuaranteeEvidence(root, state)
	if err != nil {
		return nil, err
	}
	approved := make([]QACase, 0, len(evidence))
	seen := map[string]bool{}
	for _, item := range evidence {
		if item.Kind != coverage.ReviewBlackbox || !item.HasReview || item.Case.ReviewStatus != "PASS" || item.Review.Review.SetStatus != coverage.StatusPass || item.Review.Whitelist == nil {
			continue
		}
		whitelisted := false
		for _, entry := range item.Review.Whitelist.Entries {
			if entry.CaseID == item.Case.ID {
				whitelisted = true
				break
			}
		}
		if !whitelisted {
			continue
		}
		if seen[item.Qualified] {
			return nil, fmt.Errorf("duplicate qualified QA case identity %s", item.Qualified)
		}
		seen[item.Qualified] = true
		copyCase := item.Case
		copyCase.ID = item.Qualified
		approved = append(approved, copyCase)
	}
	sort.Slice(approved, func(i, j int) bool { return approved[i].ID < approved[j].ID })
	return approved, nil
}

func copyStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func validateACResponsibilities(state RunState, slices, inputs []string) (map[string]string, error) {
	if state.RequirementGuarantee == nil {
		if len(inputs) != 0 {
			return nil, fmt.Errorf("--ac-owner is valid only for an explicitly activated REQ/AC guarantee")
		}
		return nil, nil
	}
	if err := validateGuaranteeEnvelope(state); err != nil {
		return nil, err
	}
	allowedOwners := map[string]bool{masterMergeOwner: true}
	for _, raw := range slices {
		owner := strings.TrimSpace(raw)
		if !promptIDPattern.MatchString(owner) {
			return nil, fmt.Errorf("guaranteed split slice %q must be a concrete run id", raw)
		}
		allowedOwners[owner] = true
	}
	_, conditions := requirementIndex(state.RequirementGuarantee.Projection)
	owners := map[string]string{}
	for _, raw := range inputs {
		parts := strings.SplitN(raw, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("AC responsibility must use <AC-ID>=<slice-run-id|master-merge>")
		}
		acID, owner := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if _, ok := conditions[acID]; !ok {
			return nil, fmt.Errorf("AC responsibility references unknown acceptance condition %s", acID)
		}
		if !allowedOwners[owner] {
			return nil, fmt.Errorf("AC responsibility for %s references unknown slice scope %s", acID, owner)
		}
		if prior, duplicate := owners[acID]; duplicate {
			return nil, fmt.Errorf("acceptance condition %s has duplicate primary responsibility (%s and %s)", acID, prior, owner)
		}
		owners[acID] = owner
	}
	for acID := range conditions {
		if owners[acID] == "" {
			return nil, fmt.Errorf("acceptance condition %s is missing its one primary responsibility scope", acID)
		}
	}
	return owners, nil
}
