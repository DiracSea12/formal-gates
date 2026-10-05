// Package facade exposes the candidate engine surface.  It is kept
// separate from the stable validate package so legacy workflow runs continue
// to use their existing writer and state namespace.
package facade

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"formal-gates/internal/coordination"
	"formal-gates/internal/cost"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
)

const (
	EngineNamespace           = ".gates/engine"
	startIntentSuffix         = ".start.intent.json"
	terminalIntentName        = "terminal-summary.intent.json"
	DefaultDefinitionSource   = "definitions/workflow.json"
	RuntimeEngine             = "engine"
	RuntimeLegacy             = "legacy"
	DefaultProvider           = "engine"
	UnregisteredInstall       = "UNREGISTERED_INSTALL"
	UnsupportedEngineEntry    = "UNSUPPORTED_ENGINE_ENTRY"
	InvalidIntakeConfirmation = "INVALID_INTAKE_CONFIRMATION"
)

// IntakeArtifact and IntakeConfirmationReceipt are aliases for the typed
// protocol contracts.  Aliases make the façade API convenient without
// creating a second representation of the binding.
type IntakeArtifact = protocol.IntakeArtifact
type IntakeAuthority = protocol.IntakeAuthority
type IntakeConfirmationReceipt = protocol.IntakeConfirmationReceipt
type IntakeTransport = protocol.IntakeTransport
type IntakeReceipt = protocol.IntakeReceipt

const (
	DefaultIntakeSource    = "stable-driver"
	DefaultIntakeAuthority = protocol.IntakeAuthorityStableDriver
	DefaultIntakeTransport = protocol.IntakeTransportStableLauncher
)

// StartRequest is the candidate bootstrap request.  Runtime is not a caller
// selectable switch in normal use; an admitted target identity/package digest
// selects the candidate engine.  Runtime is retained as a read-only projection
// for diagnostics and is always set to "engine" by Start. MasterRunID and
// SliceID bind Controller-created children; public Start creates top-level runs.
type StartRequest struct {
	VCS                       string                    `json:"vcs,omitempty"`
	NativeHandle              vcs.WorkspaceHandle       `json:"nativeHandle,omitempty"`
	BaseSnapshot              string                    `json:"baseSnapshot,omitempty"`
	RunID                     string                    `json:"runId"`
	Route                     string                    `json:"route"`
	Provider                  string                    `json:"provider,omitempty"`
	PackageDigest             string                    `json:"packageDigest"`
	InstalledTargetIdentity   string                    `json:"installedTargetIdentity"`
	AdmissionRegistryPath     string                    `json:"admissionRegistryPath"`
	AdmissionTarget           string                    `json:"admissionTarget"`
	AdmissionRoot             string                    `json:"admissionRoot"`
	AdmissionGeneration       uint64                    `json:"admissionGeneration"`
	AdmissionLease            string                    `json:"admissionLease"`
	AdmissionToken            string                    `json:"admissionToken"`
	DefinitionSource          string                    `json:"definitionSource"`
	DefinitionDigest          string                    `json:"definitionDigest"`
	IntakeConfirmationReceipt IntakeConfirmationReceipt `json:"intakeConfirmationReceipt"`
	MasterRunID               string                    `json:"masterRunId,omitempty"`
	SliceID                   string                    `json:"sliceId,omitempty"`
}

// Envelope is the exact six-field identity projected by the candidate state.
// The persistence store additionally records writer/revision/content digest.
type Envelope = persistence.Envelope

// Run is the read-only façade projection returned by show/status/next.
type Run struct {
	MasterRunID          string                               `json:"masterRunId,omitempty"`
	ParentRequirement    *protocol.ParentRequirementAuthority `json:"parentRequirement,omitempty"`
	RequirementGuarantee *protocol.GuaranteeActivation        `json:"requirementGuarantee,omitempty"`
	RequirementChildren  []protocol.ChildRequirementDecision  `json:"requirementChildren,omitempty"`
	RecoveryRecords      []protocol.RecoveryRecord            `json:"recoveryRecords,omitempty"`
	lastEvent            *protocol.EventRecord
	Termination          *protocol.State                      `json:"termination,omitempty"`
	ValidationDecisions  map[string]protocol.RecordedDecision `json:"validationDecisions,omitempty"`
	ValidationRequests   map[string]protocol.PendingAsk       `json:"validationRequests,omitempty"`
	NotGuaranteed        bool                                 `json:"notGuaranteed,omitempty"`
	ValidationInputs     map[string]protocol.ValidationInput  `json:"validationInputs,omitempty"`
	Attempts             map[runtime.TaskKey]protocol.Attempt `json:"attempts,omitempty"`
	PendingActions       map[string]protocol.PendingAction    `json:"pendingActions,omitempty"`
	RejectedResults      map[string]protocol.RejectedResult   `json:"rejectedResults,omitempty"`
	Validation           *protocol.Validation                 `json:"validation,omitempty"`
	DispatchBindings     map[string]protocol.WorkBinding      `json:"dispatchBindings,omitempty"`
	AdapterIntents       map[string]protocol.HostActionIntent `json:"adapterIntents,omitempty"`
	EndedAttempts        map[runtime.TaskKey]protocol.Attempt `json:"endedAttempts,omitempty"`
	RunID                string                               `json:"runId"`
	Runtime              string                               `json:"runtime"`
	Route                string                               `json:"route"`
	Status               string                               `json:"status"`
	Revision             uint64                               `json:"revision"`
	Envelope             Envelope                             `json:"envelope"`
	// BaseSnapshot 是 regular 路线经 Git provider 校验过的不可变基线；lightweight
	// 恒空（无 VCS 语义）。
	BaseSnapshot string `json:"baseSnapshot,omitempty"`
	// PromotedSnapshot 是终态晋升绑定的候选快照（validation 完成后经
	// PrepareCandidate 落账）。
	PromotedSnapshot string `json:"promotedSnapshot,omitempty"`
	// QAWorktree projects the current blackbox design resource; NativeResources
	// retains its cleanup ownership after this role is retired.
	QAWorktree                string                     `json:"qaWorktree,omitempty"`
	IntakeConfirmationReceipt *IntakeConfirmationReceipt `json:"intakeConfirmationReceipt,omitempty"`
	IntakeReceipt             *IntakeReceipt             `json:"intakeReceipt,omitempty"`
	Next                      decision.NextResult        `json:"next"`
	AvailableActions          []string                   `json:"availableActions,omitempty"`
	// Review 是开发前审查的只读投影面（总需求 §7.3.5）：分动作的轮次/容量/
	// effective 状态与拓扑、路线绑定。不改 Next 的六类边界形态。
	Review ReviewFace `json:"review"`
	// RequirementWork 是需求变更需逐项决定的当前工作身份，由协议层派生。
	RequirementWork []protocol.RequirementWork `json:"requirementWork"`
	// PendingRequirementAsk 是修订屏障在位时未决需求变化 Ask 的公开面：
	// 其存在即 run 的当前外部边界（Next 投影为 ask 边界，等待用户而非
	// 错误）。
	PendingRequirementAsk *PendingRequirementAskFace `json:"pendingRequirementAsk,omitempty"`
	// PendingAsks 是一切未决受限控制 Ask 的泛化公开面（requestID/control/
	// options/token；需求变化 Ask 另有携包特化面 PendingRequirementAsk）
	//——完成路径与可见性对称：grant/topology/route/finding-decision 同样
	// 可见。
	PendingAsks []PendingAskFace `json:"pendingAsks,omitempty"`
	// Cost 是本 run 的 cost 权威投影（dispatch 逐条＋run 合计）；owner 项缺源
	// 时缺席。engine run 的 cost 只来自引擎权威 State。
	Cost          *cost.RunCost                    `json:"cost,omitempty"`
	SliceReceipt  *protocol.SliceReceipt           `json:"sliceReceipt,omitempty"`
	SliceChildren map[string]string                `json:"sliceChildren,omitempty"`
	SliceReceipts map[string]protocol.SliceReceipt `json:"sliceReceipts,omitempty"`
	// GuardBudget 是 dispatch 额度投影（额度数字、basis digest 与已用事实）；
	// 受理前（包络未就位）为 nil。
	GuardBudget *protocol.GuardBudget `json:"guardBudget,omitempty"`
	SummaryPath string                `json:"summaryPath,omitempty"`
	Unverified  bool                  `json:"unverified,omitempty"`
}

// PendingRequirementAskFace 是未决需求变化 Ask 的公开面：请求身份、选项、
// Ask 落账的确认包与创建时的权威基线 revision。内容与授权事实以 state 的
// PendingAsk 为唯一权威，本面只读投影。
type PendingRequirementAskFace struct {
	RequestID    string                               `json:"requestId"`
	Options      []protocol.AskOption                 `json:"options"`
	Package      *protocol.RequirementDecisionPackage `json:"package"`
	BaseRevision string                               `json:"baseRevision"`
	// Token 是投影时按当前 revision 即时求值的 freshness token（stale 后
	// 经 Freshness 重取再提交）。
	Token string `json:"token"`
}

// PendingAskFace 是任意未决受限控制 Ask 的公开面：请求身份、控制类型、
// 选项与投影时按当前 revision 即时求值的 freshness token。PendingAsk 为
// 唯一权威，本面只读投影。
type PendingAskFace struct {
	Validation *protocol.ValidationRequest  `json:"validation,omitempty"`
	RoundGrant *protocol.ReviewGrantRequest `json:"roundGrant,omitempty"`
	RequestID  string                       `json:"requestId"`
	Control    protocol.ControlKind         `json:"control"`
	Options    []protocol.AskOption         `json:"options"`
	// RouteGates 是 SELECT_ROUTE Ask 钉住的候选门全集的只读投影（其余
	// 控制为空）：用户据此所见全集提交决定。
	RouteGates []string `json:"routeGates,omitempty"`
	Token      string   `json:"token"`
}

// ReviewGrantFace 是一条额外语义轮授权的公开面：身份、理由与占用态（该授
// 权是否已被有效候选占用）——show/status/next 的授权来源。
type ReviewGrantFace struct {
	ID       string `json:"id"`
	Reason   string `json:"reason"`
	Occupied bool   `json:"occupied"`
}

// ReviewCandidateFace 是单条候选的裁决身份公开面（含整轮已排除者，以
// Excluded 标记）：仅凭本面即可构造精确绑定的 CorrectionInput——派发、
// 候选摘要、需求 revision、finding 与下游 CAS 代数全部在审查面内。
type ReviewCandidateFace struct {
	ResultDigest        string              `json:"resultDigest"`
	DispatchID          string              `json:"dispatchId"`
	RequirementRevision string              `json:"requirementRevision"`
	RawStatus           string              `json:"rawStatus"`
	Excluded            bool                `json:"excluded"`
	Findings            []ReviewFindingFace `json:"findings"`
}

// ReviewFindingFace 是候选内一条 finding 的公开面：ID 与 digest 是绑定
// 裁决的稳定身份，severity 为有效严重度；EffectiveDecisionID 是后续纠正
// 必须 supersede 的当前决定身份，空值表示未裁决。
type ReviewFindingFace struct {
	ID                  string `json:"id"`
	Digest              string `json:"digest"`
	Severity            string `json:"severity"`
	Message             string `json:"message"`
	EffectiveDecision   string `json:"effectiveDecision,omitempty"`
	EffectiveDecisionID string `json:"effectiveDecisionId,omitempty"`
}

// ReviewActionFace 是单个审查动作的账本公开面：completed/base/extra(授
// 权数)/effective(有效上限)/remaining、逐条授权来源（id/reason/占用态）、
// raw/adjudication/correction 计数与 effective 状态及来源、下一轮准入形
// 态、逐条候选的裁决身份面与投影代数。
type ReviewActionFace struct {
	Action               string                `json:"action"`
	Completed            int                   `json:"completed"`
	BaseLimit            int                   `json:"baseLimit"`
	ExtraAuthorized      int                   `json:"extraAuthorized"`
	EffectiveLimit       int                   `json:"effectiveLimit"`
	Remaining            int                   `json:"remaining"`
	NextRoundRequirement string                `json:"nextRoundRequirement"`
	Grants               []ReviewGrantFace     `json:"grants"`
	RawResults           int                   `json:"rawResults"`
	Adjudications        int                   `json:"adjudications"`
	Corrections          int                   `json:"corrections"`
	EffectiveStatus      string                `json:"effectiveStatus"`
	EffectiveSource      string                `json:"effectiveSource"`
	Candidates           []ReviewCandidateFace `json:"candidates"`
	EffectiveRevision    int                   `json:"effectiveRevision"`
}

// RequirementChangeFace 是最近一次修订确认的公开面：确认后的 revision、
// 用户声明的含义分类与来源、勾选的重走范围。只读投影，内容与授权事实
// 以 state 的确认账本为唯一权威。
type RequirementChangeFace struct {
	Revision       string                             `json:"revision"`
	Classification string                             `json:"classification"`
	Scope          []string                           `json:"scope"`
	Source         string                             `json:"source"`
	WorkDecisions  []protocol.RequirementWorkDecision `json:"workDecisions"`
}

// ReviewFace 是 run 的审查投影：两条 series 与拓扑/路线绑定，以及当前
// 权威需求 revision 与最近一次修订确认。
type ReviewFace struct {
	Actions       []ReviewActionFace        `json:"actions"`
	Topology      *protocol.TopologyBinding `json:"topology,omitempty"`
	RouteMode     string                    `json:"routeMode,omitempty"`
	SelectedGates []string                  `json:"selectedGates,omitempty"`
	// RequirementRevision 是当前权威需求 revision（确认账本末条）。
	RequirementRevision string `json:"requirementRevision"`
	// LastRequirementChange 是最近一次修订确认；从未修订时为空。
	LastRequirementChange *RequirementChangeFace `json:"lastRequirementChange,omitempty"`
}

// reviewFace 从权威 state 派生只读审查投影（纯函数）。
func reviewFace(state *protocol.State) ReviewFace {
	return ReviewFace{
		Actions: []ReviewActionFace{
			reviewActionFace(state.ReviewSeries.ProductReview),
			reviewActionFace(state.ReviewSeries.StartReadiness),
		},
		Topology:              state.Topology,
		RouteMode:             state.RouteMode,
		SelectedGates:         state.SelectedGates,
		RequirementRevision:   state.RequirementRevision(),
		LastRequirementChange: requirementChangeFace(state),
	}
}

// requirementChangeFace 投影账本中最近一条非初始的确认事实；只有受理
// 初始账目（或账本为空）时返回 nil。
func requirementChangeFace(state *protocol.State) *RequirementChangeFace {
	for index := len(state.RequirementRevisions) - 1; index >= 0; index-- {
		entry := state.RequirementRevisions[index]
		if entry.Classification == protocol.RequirementClassificationIntake {
			return nil
		}
		scope := append([]string{}, entry.DecidedScope...)
		if scope == nil {
			scope = []string{}
		}
		return &RequirementChangeFace{
			Revision:       entry.Revision,
			Classification: entry.Classification,
			Scope:          scope,
			Source:         entry.Source,
			WorkDecisions:  append([]protocol.RequirementWorkDecision{}, entry.WorkDecisions...),
		}
	}
	return nil
}

func reviewActionFace(series review.Series) ReviewActionFace {
	occupied := review.OccupiedAuthorizationIDs(series)
	grants := make([]ReviewGrantFace, 0, len(series.Authorizations))
	for _, authorization := range series.Authorizations {
		grants = append(grants, ReviewGrantFace{
			ID:       authorization.ID,
			Reason:   authorization.Reason,
			Occupied: occupied[authorization.ID],
		})
	}
	candidates := make([]ReviewCandidateFace, 0, len(series.RawCandidates))
	for _, candidate := range series.RawCandidates {
		findings := make([]ReviewFindingFace, 0, len(candidate.Findings))
		for _, finding := range candidate.Findings {
			decision, severity, decisionID := review.EffectiveFindingDecision(series, candidate.ResultDigest, finding)
			findings = append(findings, ReviewFindingFace{
				ID:                  finding.ID,
				Digest:              finding.Digest,
				Severity:            severity,
				Message:             finding.Message,
				EffectiveDecision:   decision,
				EffectiveDecisionID: decisionID,
			})
		}
		candidates = append(candidates, ReviewCandidateFace{
			ResultDigest:        candidate.ResultDigest,
			DispatchID:          candidate.DispatchID,
			RequirementRevision: candidate.RequirementRevision,
			RawStatus:           candidate.RawStatus,
			Excluded:            review.ResultExcluded(series, candidate.ResultDigest),
			Findings:            findings,
		})
	}
	return ReviewActionFace{
		Action:               string(series.Action),
		Completed:            series.Completed,
		BaseLimit:            series.AutomaticLimit,
		ExtraAuthorized:      series.ExtraAuthorized,
		EffectiveLimit:       series.CurrentLimit,
		Remaining:            series.RemainingCapacity,
		NextRoundRequirement: series.NextRoundRequirement,
		Grants:               grants,
		RawResults:           len(series.RawCandidates),
		Adjudications:        len(series.Adjudications),
		Corrections:          len(series.Corrections),
		EffectiveStatus:      series.Effective.Status,
		EffectiveSource:      series.Effective.Source,
		Candidates:           candidates,
		EffectiveRevision:    series.Effective.Revision,
	}
}

// TerminalSummary is immutable and supports terminal replay without reopening
// the active writer. An aborted run retains its original State here after cleanup.
type TerminalMaterial struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}
type TerminalSummary struct {
	SliceGuarantee      *TerminalMaterial                    `json:"sliceGuarantee,omitempty"`
	LastEvent           *protocol.EventRecord                `json:"lastEvent,omitempty"`
	Termination         *protocol.State                      `json:"termination,omitempty"`
	ValidationDecisions map[string]protocol.RecordedDecision `json:"validationDecisions,omitempty"`
	ValidationRequests  map[string]protocol.PendingAsk       `json:"validationRequests,omitempty"`
	NotGuaranteed       bool                                 `json:"notGuaranteed,omitempty"`
	RunID               string                               `json:"runId"`
	Runtime             string                               `json:"runtime"`
	Route               string                               `json:"route"`
	Status              string                               `json:"status"`
	Revision            uint64                               `json:"revision"`
	Envelope            Envelope                             `json:"envelope"`
	IntakeReceipt       *IntakeReceipt                       `json:"intakeReceipt,omitempty"`
	Next                decision.NextResult                  `json:"next"`
	Cost                *cost.RunCost                        `json:"cost,omitempty"`
	GuardBudget         *protocol.GuardBudget                `json:"guardBudget,omitempty"`
	Unverified          bool                                 `json:"unverified,omitempty"`
	CompletedAt         string                               `json:"completedAt"`
	// Owner 区间端点是跨代协调的终态载荷：legacy 终态只读重扫据此判定与
	// engine run 的区间相交（本 run 状态目录随终态清理，摘要是留存的区间
	// 事实）。endedAt 在终态 owner 协调锁段内与量取同时点盖下。
	OwnerTranscript string `json:"ownerTranscript,omitempty"`
	OwnerStartedAt  string `json:"ownerStartedAt,omitempty"`
	OwnerEndedAt    string `json:"ownerEndedAt,omitempty"`
}

type startIntent struct {
	RunID            string    `json:"runId"`
	Admission        Admission `json:"admission"`
	DefinitionSource string    `json:"definitionSource"`
	DefinitionDigest string    `json:"definitionDigest"`
}

type StartOptions struct {
	Root    string
	Request StartRequest
	// PackageRoot is used by the host/launcher to document which installed
	// target was admitted.  The façade never resolves a runtime from a caller
	// supplied identity; the launcher must provide Admission instead.
	PackageRoot string
	// ArtifactRoot is the candidate test-project root containing injected
	// requirement/solution artifacts. When set, receipt bindings are
	// recomputed before the first state write.
	ArtifactRoot string
	Admission    *Admission
	Definition   *compiler.CompiledDefinition
	Registry     *compiler.Registry
	Capacity     int
	// Owner 是主代理（owner）计量来源声明（transcript＋provider，两者同
	// 时非空才承诺 owner 计量；REQ-012）。
	Owner OwnerBinding
	// VCSRunner 注入 regular 路线快照校验用的 Git 命令 runner（测试替身；
	// nil＝生产 exec）。lightweight 不消费它。
	VCSRunner vcs.Runner
}

// Admission is a launcher-derived runtime binding.  It is intentionally
// separate from StartRequest so user input cannot select a package digest or
// installed identity.
type Admission struct {
	RegistryPath            string `json:"registryPath"`
	Target                  string `json:"target"`
	Root                    string `json:"root"`
	Provider                string `json:"provider"`
	PackageDigest           string `json:"packageDigest"`
	InstalledTargetIdentity string `json:"installedTargetIdentity"`
	Generation              uint64 `json:"generation"`
	Lease                   string `json:"lease"`
	Token                   string `json:"token"`
}

type Facade struct {
	admission  Admission
	root       string
	runID      string
	stateDir   string
	metadata   string
	store      *persistence.Store
	engine     *protocol.Engine
	envelope   Envelope
	definition *compiler.CompiledDefinition
	capacity   int
	// vcsRunner 是注入的 Git 命令 runner（测试替身）；vcs 是绑定 run 根的
	// provider，regular 路线启动校验、终态快照边界与登记制共用。lightweight
	// 不引入 VCS 依赖，两者为 nil。
	vcsRunner vcs.Runner
	vcs       *vcs.Provider
}

// Start creates an engine candidate run after validating the complete typed
// intake receipt.  Validation happens before the first state directory write.
func Start(options StartOptions) (*Facade, Run, error) {
	if options.Request.MasterRunID != "" || options.Request.SliceID != "" {
		return nil, Run{}, fmt.Errorf("engine start: children are created by the master controller")
	}
	return startRun(options, nil, "")
}

func startRun(options StartOptions, sliceStart *protocol.SliceRunStart, nativeRoot string) (*Facade, Run, error) {
	if strings.TrimSpace(options.Root) == "" {
		return nil, Run{}, fmt.Errorf("engine start: root is required")
	}
	root, err := filepath.Abs(strings.TrimSpace(options.Root))
	if err != nil {
		return nil, Run{}, err
	}
	req := options.Request
	if strings.TrimSpace(req.RunID) == "" {
		return nil, Run{}, fmt.Errorf("engine start: run id is required")
	}
	if !validRunID(req.RunID) {
		return nil, Run{}, fmt.Errorf("engine start: run id %q is invalid", req.RunID)
	}
	runUnlock, err := coordination.AcquireRun(root, req.RunID)
	if err != nil {
		return nil, Run{}, err
	}
	defer runUnlock()
	if err := recoverStartIntent(root, req.RunID); err != nil {
		return nil, Run{}, err
	}
	// Legacy and candidate runtimes share the run-id namespace. Refuse a
	// candidate writer before creating any engine files when the legacy run
	// already exists.
	legacyDir := filepath.Join(root, ".gates", "tmp", req.RunID)
	if _, statErr := os.Stat(legacyDir); statErr == nil {
		return nil, Run{}, fmt.Errorf("run %q already exists in legacy runtime", req.RunID)
	} else if !os.IsNotExist(statErr) {
		return nil, Run{}, statErr
	}
	if _, statErr := os.Stat(filepath.Join(root, ".gates", "results", req.RunID+".json")); statErr == nil {
		return nil, Run{}, fmt.Errorf("run %q already has a retained legacy result", req.RunID)
	} else if !os.IsNotExist(statErr) {
		return nil, Run{}, statErr
	}
	if strings.TrimSpace(req.Route) == "" {
		req.Route = "lightweight"
	}
	if req.Route != "lightweight" && req.Route != "regular" {
		return nil, Run{}, fmt.Errorf("engine start: unsupported route %q", req.Route)
	}
	if req.Route == "regular" && strings.TrimSpace(req.BaseSnapshot) == "" {
		return nil, Run{}, fmt.Errorf("engine start: regular route requires an immutable native base snapshot")
	}
	// regular 路线把 BaseSnapshot 从字符串变成事实（镜像 legacy workflow Start
	// 的快照接线：base 必须可复现且是原生 HEAD 的祖先或相等）。lightweight 不
	// 引入 VCS 依赖。
	var vcsProvider *vcs.Provider
	var initialSnapshot string
	if nativeRoot == "" {
		nativeRoot = root
	}
	if req.Route == "regular" && sliceStart == nil {
		vcsProvider = vcs.NewNative(root, req.VCS, options.VCSRunner).At(nativeRoot, req.NativeHandle)
		current, err := vcsProvider.Resolve()
		if err != nil {
			return nil, Run{}, err
		}
		boundBase, err := vcsProvider.BindIdentity(req.BaseSnapshot)
		if err != nil {
			return nil, Run{}, err
		}
		req.BaseSnapshot = boundBase
		if err := vcsProvider.IsAncestorOrEqual(req.BaseSnapshot, current); err != nil {
			return nil, Run{}, fmt.Errorf("base snapshot %s: %w", req.BaseSnapshot, err)
		}
		initialSnapshot = current
	}
	if options.Admission == nil {
		return nil, Run{}, fmt.Errorf("UNREGISTERED_INSTALL: candidate start requires launcher admission")
	}
	admission := *options.Admission
	admission.Root = root
	if strings.TrimSpace(req.Provider) == "" {
		req.Provider = admission.Provider
	}
	if req.Provider != admission.Provider {
		return nil, Run{}, fmt.Errorf("UNREGISTERED_INSTALL: provider does not match admitted target")
	}
	if err := verifyAdmissionSnapshot(admission); err != nil {
		return nil, Run{}, err
	}
	if strings.TrimSpace(req.PackageDigest) != "" && req.PackageDigest != options.Admission.PackageDigest {
		return nil, Run{}, fmt.Errorf("UNREGISTERED_INSTALL: package digest does not match admitted target")
	}
	if strings.TrimSpace(req.InstalledTargetIdentity) != "" && req.InstalledTargetIdentity != options.Admission.InstalledTargetIdentity {
		return nil, Run{}, fmt.Errorf("UNREGISTERED_INSTALL: installed target identity does not match admitted target")
	}
	req.AdmissionRegistryPath = admission.RegistryPath
	req.AdmissionTarget = admission.Target
	req.AdmissionRoot = admission.Root
	req.PackageDigest = admission.PackageDigest
	req.InstalledTargetIdentity = options.Admission.InstalledTargetIdentity
	req.AdmissionGeneration = options.Admission.Generation
	req.AdmissionLease = options.Admission.Lease
	req.AdmissionToken = options.Admission.Token
	if strings.TrimSpace(req.PackageDigest) == "" {
		return nil, Run{}, fmt.Errorf("UNREGISTERED_INSTALL: package digest is required")
	}
	if strings.TrimSpace(req.InstalledTargetIdentity) == "" {
		return nil, Run{}, fmt.Errorf("UNREGISTERED_INSTALL: installed target identity is required")
	}
	if strings.TrimSpace(req.DefinitionSource) == "" {
		req.DefinitionSource = DefaultDefinitionSource
	}
	if strings.TrimSpace(req.DefinitionDigest) == "" {
		req.DefinitionDigest = definition.WorkflowDefinitionDigest
	}
	req.IntakeConfirmationReceipt = canonicalizeIntakeReceipt(req.IntakeConfirmationReceipt)
	if err := validateConfirmation(req.IntakeConfirmationReceipt, req.DefinitionSource); err != nil {
		return nil, Run{}, err
	}
	if strings.TrimSpace(options.ArtifactRoot) != "" {
		if err := validateArtifactBindings(options.ArtifactRoot, req.IntakeConfirmationReceipt); err != nil {
			return nil, Run{}, err
		}
	}
	digest, err := IntakeDigest(req.IntakeConfirmationReceipt)
	if err != nil {
		return nil, Run{}, err
	}
	req.IntakeConfirmationReceipt.Digest = digest
	if strings.TrimSpace(options.Request.IntakeConfirmationReceipt.Digest) != "" && options.Request.IntakeConfirmationReceipt.Digest != digest {
		return nil, Run{}, fmt.Errorf("%s: intake confirmation digest mismatch", InvalidIntakeConfirmation)
	}
	if options.Capacity < 0 {
		return nil, Run{}, fmt.Errorf("engine start: capacity must be non-negative")
	}
	// 项目级 token cap 配置在首个状态写入前读取：非法配置立即明确报错，缺
	// 文件＝不可用（request/attempt 硬止损继续生效）。
	tokenCap, err := readTokenCap(root)
	if err != nil {
		return nil, Run{}, err
	}
	if strings.TrimSpace(options.Owner.Transcript) == "" && strings.TrimSpace(options.Owner.Provider) != "" ||
		strings.TrimSpace(options.Owner.Transcript) != "" && strings.TrimSpace(options.Owner.Provider) == "" {
		return nil, Run{}, fmt.Errorf("engine start: owner binding requires both transcript and provider")
	}
	cd := options.Definition
	reg := options.Registry
	if cd == nil {
		cd, err = compiler.Compile(definition.Workflow(), definition.Registry())
		if err != nil {
			return nil, Run{}, err
		}
	}
	if reg == nil {
		reg = definition.Registry()
	}
	if req.DefinitionDigest != definition.WorkflowDefinitionDigest {
		return nil, Run{}, unsupported("definitionDigest", definition.WorkflowDefinitionDigest, req.DefinitionDigest)
	}
	if req.DefinitionSource != DefaultDefinitionSource {
		return nil, Run{}, unsupported("definitionSource", DefaultDefinitionSource, req.DefinitionSource)
	}
	stateDir := filepath.Join(root, filepath.FromSlash(EngineNamespace), req.RunID)
	// already-exists 与恢复共用同一成立判据：活动 state 或终态证据任一在
	// 即拒——已完成 run 不可重启，终态证据不动。
	established, statErr := runDirectoryEstablished(stateDir)
	if statErr != nil {
		return nil, Run{}, statErr
	}
	if established {
		return nil, Run{}, fmt.Errorf("engine start: run %q already exists", req.RunID)
	}
	intentPath := startIntentPath(root, req.RunID)
	// The admitted prefix is one bounded local write window. The intent carries
	// its own binding so recovery and failed-start cleanup cannot adopt a caller.
	if err := admission.write(func() error {
		if err := writeJSONAtomic(intentPath, startIntent{RunID: req.RunID, Admission: admission, DefinitionSource: req.DefinitionSource, DefinitionDigest: req.DefinitionDigest}); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(stateDir), 0o700); err != nil {
			return err
		}
		if err := os.Mkdir(stateDir, 0o700); err != nil {
			if errors.Is(err, fs.ErrExist) {
				return fmt.Errorf("engine start: run %q already exists", req.RunID)
			}
			return err
		}
		lock, err := os.OpenFile(filepath.Join(stateDir, "promotion.lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := lock.WriteAt([]byte("0"), 0)
		closeErr := lock.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return err
		}
		return writeJSONAtomic(filepath.Join(stateDir, "request.json"), req)
	}); err != nil {
		return nil, Run{}, err
	}
	// Any failure after the prefix retains its evidence if installation moved.
	// Established state and side effects stay available for normal reconciliation.
	cleanupFailedStart := func(cause error) error {
		if sliceStart != nil {
			return cause
		}
		if err := admission.write(func() error {
			if err := os.RemoveAll(filepath.Join(root, ".gates", "tmp", req.RunID)); err != nil {
				return err
			}
			if err := os.RemoveAll(stateDir); err != nil {
				return err
			}
			return os.Remove(intentPath)
		}); err != nil {
			return errors.Join(cause, err)
		}
		return cause
	}
	env := Envelope{
		Writer: persistence.Writer, StateSchemaVersion: encoder.StateSchemaVersion,
		WorkflowDefinitionVersion: definition.WorkflowDefinitionVersion,
		DefinitionSource:          req.DefinitionSource, DefinitionDigest: req.DefinitionDigest,
		PackageDigest: req.PackageDigest, InstalledTargetIdentity: req.InstalledTargetIdentity,
	}
	store, err := persistence.NewStore(stateDir, persistence.Config{
		PackageDigest: req.PackageDigest, DefinitionSource: req.DefinitionSource,
		InstalledTargetIdentity: req.InstalledTargetIdentity, AcquireAdmission: admission.acquire,
	})
	if err != nil {
		return nil, Run{}, cleanupFailedStart(err)
	}
	activation, err := initialGuarantee(root, req.Route, req.IntakeConfirmationReceipt)
	if err != nil {
		return nil, Run{}, err
	}
	eng, err := protocol.New(store, protocol.Config{VCSName: req.VCS, NativeHandle: req.NativeHandle, RequirementGuarantee: activation, NativeRoot: nativeRoot, Definition: cd, Registry: reg, Capacity: options.Capacity, CostSource: CostSourceBridge(root, req.RunID), ClaimDispatch: dispatchClaimBridge(root, req.RunID, admission)}, nil)
	if err != nil {
		return nil, Run{}, cleanupFailedStart(err)
	}
	if sliceStart != nil {
		bootstrap := &Facade{admission: admission, stateDir: stateDir}
		unlock, err := bootstrap.acquireNativeWindow()
		if err != nil {
			return nil, Run{}, err
		}
		err = initializeRun(eng, root, req, initialSnapshot, sliceStart, options.Owner, tokenCap, admission, cd)
		unlock()
		if err != nil {
			return nil, Run{}, err
		}
		vcsProvider = vcs.NewNative(root, req.VCS, options.VCSRunner).At(nativeRoot, req.NativeHandle)
	} else if err := initializeRun(eng, root, req, initialSnapshot, nil, options.Owner, tokenCap, admission, cd); err != nil {
		return nil, Run{}, cleanupFailedStart(err)
	}
	f := &Facade{admission: admission, root: root, runID: req.RunID, stateDir: stateDir, metadata: filepath.Join(stateDir, "request.json"), store: store, engine: eng, envelope: env, definition: cd, capacity: options.Capacity, vcsRunner: options.VCSRunner, vcs: vcsProvider}
	run, err := f.project()
	if err != nil {
		return nil, Run{}, cleanupFailedStart(err)
	}
	if err := admission.write(func() error { return os.Remove(intentPath) }); err != nil {
		return nil, Run{}, err
	}
	return f, run, nil
}

// initializeRun owns the initial workflow, cost envelope and lifecycle binding.
// The configured engine carries the native root and definition; initial cost
// and lifecycle facts are bound before any workflow dispatch.
func initializeRun(eng *protocol.Engine, root string, req StartRequest, initialSnapshot string, sliceStart *protocol.SliceRunStart, owner OwnerBinding, tokenCap *int64, admission Admission, cd *compiler.CompiledDefinition) error {
	// A child stays PREPARING until the master accepts every native proof.
	phase := runtime.PhaseIntakeRegistered
	if sliceStart != nil {
		phase = runtime.PhasePreparing
	}
	view, err := decision.NewState(cd.Version, phase)
	if err != nil {
		return err
	}
	if err := eng.InitWithMetadata(view, req.Provider, emptyFingerprint(), req.RunID, req.Route, &req.IntakeConfirmationReceipt, req.BaseSnapshot, initialSnapshot, sliceStart); err != nil {
		return err
	}
	// owner 区间操作（扫描＋自身标记＋peer 标记）与 token cap 绑定同在
	// lockOwnerCost（与 legacy 同路径＝两代共同的区间协调能力）内执行：锁内
	// 的绑定提交使后到者的扫描必见先到者的 owner 来源，并行启动的区间判定因
	// 此闭合。锁序＝协调锁→run store 锁（与 legacy mutateRun 同序）。无 owner
	// 来源时无区间操作，不取锁。
	bindOwnerCost := func() error {
		if strings.TrimSpace(owner.Transcript) == "" || strings.TrimSpace(owner.Provider) == "" {
			return eng.BindCostSources(captureOwnerBinding(root, req.RunID, owner), tokenCap, emptyFingerprint())
		}
		release, err := lockOwnerCost(root)
		if err != nil {
			return err
		}
		defer release()
		binding := captureOwnerBinding(root, req.RunID, owner)
		if err := eng.BindCostSources(binding, tokenCap, emptyFingerprint()); err != nil {
			return err
		}
		// 区间重叠事实双边持久：把重叠补写进每个共享 owner transcript 的活动
		// engine peer。被安装 fence 的 peer 不再写；自身相交事实照常保守
		// 报告 unavailable，其他写失败上抛。
		return markOverlappingEnginePeers(root, req.RunID, binding.Transcript)
	}
	if err := bindOwnerCost(); err != nil {
		return err
	}
	if err := admission.write(func() error { return lifecycle.BeginRun(root, req.RunID) }); err != nil {
		return err
	}
	return nil
}

// Open loads an existing engine run and validates all six identity fields
// before exposing a writer.  No fallback to the legacy namespace occurs.
func Open(root, runID string) (*Facade, error) {
	root, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return nil, err
	}
	if !validRunID(runID) {
		return nil, fmt.Errorf("engine open: run id %q is invalid", runID)
	}
	stateDir := filepath.Join(root, filepath.FromSlash(EngineNamespace), runID)
	var req StartRequest
	data, err := os.ReadFile(filepath.Join(stateDir, "request.json"))
	if err != nil {
		return nil, fmt.Errorf("%s: intake receipt is unavailable", InvalidIntakeConfirmation)
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("engine open: request JSON is invalid: %w", err)
	}
	if req.InstalledTargetIdentity == "" || req.PackageDigest == "" {
		return nil, unsupported("installedTargetIdentity", "non-empty", req.InstalledTargetIdentity)
	}
	if req.DefinitionSource != DefaultDefinitionSource {
		return nil, unsupported("definitionSource", DefaultDefinitionSource, req.DefinitionSource)
	}
	if req.DefinitionDigest != definition.WorkflowDefinitionDigest {
		return nil, unsupported("definitionDigest", definition.WorkflowDefinitionDigest, req.DefinitionDigest)
	}
	cd, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		return nil, err
	}
	admission := admissionFromRequest(req)
	store, err := persistence.NewStore(stateDir, persistence.Config{PackageDigest: req.PackageDigest, DefinitionSource: req.DefinitionSource, InstalledTargetIdentity: req.InstalledTargetIdentity, AcquireAdmission: admission.acquire})
	if err != nil {
		return nil, err
	}
	nativeRoot := ""
	if stored, err := store.Load(); err == nil {
		var state protocol.State
		if err := json.Unmarshal(stored.Content, &state); err != nil {
			return nil, err
		}
		nativeRoot = state.NativeRoot
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	} else if errors.Is(err, fs.ErrNotExist) {
		// A completed run may have had its active state cleaned while retaining
		// the immutable terminal summary. During a crash window the summary is
		// still represented by a durable terminal intent; Drive will reconcile
		// cleanup and publish that summary before returning Complete.
		for _, name := range []string{"terminal-summary.json", terminalIntentName} {
			data, summaryErr := os.ReadFile(filepath.Join(stateDir, name))
			if summaryErr != nil {
				continue
			}
			var summary TerminalSummary
			if summaryErr := json.Unmarshal(data, &summary); summaryErr != nil {
				return nil, summaryErr
			}
			if summaryErr := persistence.ValidateEnvelopeWithIdentity(summary.Envelope, req.PackageDigest, req.DefinitionSource, req.InstalledTargetIdentity); summaryErr != nil {
				return nil, summaryErr
			}
			if summary.Termination != nil {
				nativeRoot = summary.Termination.NativeRoot
			}
			break
		}
		if _, summaryErr := os.Stat(filepath.Join(stateDir, "terminal-summary.json")); summaryErr != nil {
			if _, intentErr := os.Stat(filepath.Join(stateDir, terminalIntentName)); intentErr != nil {
				return nil, err
			}
		}
	}
	if err := admission.validate(); err != nil {
		return nil, err
	}
	if !admission.matchesRoot(root) {
		return nil, fmt.Errorf("UNREGISTERED_INSTALL: run root does not match its admitted root")
	}
	eng, err := protocol.New(store, protocol.Config{VCSName: req.VCS, NativeHandle: req.NativeHandle, NativeRoot: nativeRoot, Definition: cd, Registry: definition.Registry(), Capacity: 16, CostSource: CostSourceBridge(root, runID), ClaimDispatch: dispatchClaimBridge(root, runID, admission)}, nil)
	if err != nil {
		return nil, err
	}
	env := Envelope{Writer: persistence.Writer, StateSchemaVersion: encoder.StateSchemaVersion, WorkflowDefinitionVersion: definition.WorkflowDefinitionVersion, DefinitionSource: req.DefinitionSource, DefinitionDigest: req.DefinitionDigest, PackageDigest: req.PackageDigest, InstalledTargetIdentity: req.InstalledTargetIdentity}
	return &Facade{admission: admission, root: root, runID: runID, stateDir: stateDir, metadata: filepath.Join(stateDir, "request.json"), store: store, engine: eng, envelope: env, definition: cd, capacity: 16, vcsRunner: nil, vcs: reopenNativeProvider(root, nativeRoot, req)}, nil
}

// Drive performs Observe → Decide → SelectIssued.  Lightweight runs record
// the intake receipt on the first call, settle deterministic steps, and retain
// an immutable Complete summary.
func (f *Facade) Drive() (Run, error) {
	if f == nil || f.engine == nil {
		return Run{}, fmt.Errorf("engine drive: nil façade")
	}
	unlock, err := f.acquireWriteWindow()
	if err != nil {
		return Run{}, err
	}
	run, err := f.driveInWindow()
	unlock()
	if err == nil && run.SliceReceipt != nil {
		err = f.handoffSliceReceipt(*run.SliceReceipt)
	}
	return run, err
}

// recordIntakeReceipt persists this run's original confirmation through the
// first-drive handler. Parent controls may arrive before the child's first drive.
func (f *Facade) recordIntakeReceipt(s *protocol.State) error {
	if s.IntakeReceipt != nil {
		return nil
	}
	confirmation := s.IntakeConfirmationReceipt
	if confirmation == nil {
		return fmt.Errorf("%s: start confirmation is missing", InvalidIntakeConfirmation)
	}
	digest, err := IntakeDigest(*confirmation)
	if err != nil {
		return err
	}
	_, err = f.engine.RecordIntakeReceipt(protocol.IntakeReceipt{Confirmation: *confirmation, IntakeDigest: digest}, emptyFingerprint())
	return err
}

// driveInWindow consumes the caller's existing native window without reentry.
func (f *Facade) driveInWindow() (Run, error) {
	if err := f.syncResetRequirement(); err != nil {
		return Run{}, err
	}
	// Recovery is a write-side reconciliation step: it may create/remove the
	// protocol lock and orphan temp files.  Keep it out of Open/Show/Status/Next
	// so those read-only paths cannot mutate directory metadata.
	if err := f.recoverTerminalIntent(); err != nil {
		return Run{}, err
	}
	if _, statErr := os.Stat(filepath.Join(f.stateDir, "state.json")); statErr == nil {
		if _, recoverErr := f.store.Recover(); recoverErr != nil {
			return Run{}, recoverErr
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return Run{}, statErr
	}
	snap, err := f.engine.Load()
	if err != nil {
		if !isMissingEngineState(err) {
			return Run{}, err
		}
		if summary, sumErr := f.readSummary(); sumErr == nil {
			if err := f.endLifecycle(); err != nil {
				return Run{}, err
			}
			return runFromSummary(summary), nil
		}
		return Run{}, err
	}
	if _, aborted := snap.State.AbortDecision(); snap.State.Phase == runtime.PhaseSliceReady && !aborted && !snap.State.ResetPending() {
		if err := f.publishSliceReceipt(snap.State.SliceReceipt); err != nil {
			return Run{}, err
		}
		return f.project()
	}
	terminalReady := snap.State.Phase == runtime.PhaseTerminal || snap.State.DeliveryComplete(f.definition) && snap.State.ResourcesCleaned()
	if !terminalReady {
		if err := f.discoverSliceReceipts(snap.State); err != nil {
			return Run{}, err
		}
	}
	if err := f.recordIntakeReceipt(snap.State); err != nil {
		return Run{}, err
	}
	var plan *decision.Plan
	if terminalReady {
		plan, _, err = f.engine.Plan()
		if err != nil {
			return Run{}, err
		}
	} else if snap.State.Route == "lightweight" {
		if _, err := f.engine.CompleteAll(emptyFingerprint()); err != nil {
			return Run{}, err
		}
	} else {
		for {
			if err := f.syncReleasedChildren(); err != nil {
				return Run{}, err
			}
			if err := f.driveChildren(); err != nil {
				return Run{}, err
			}
			plan, _, err = f.engine.Drive(emptyFingerprint())
			if err != nil {
				break
			}
			progress, nativeErr := f.driveNative()
			if nativeErr != nil {
				return Run{}, nativeErr
			}
			if progress {
				continue
			}
			current, loadErr := f.engine.Load()
			if loadErr != nil {
				return Run{}, loadErr
			}
			if current.State.NativeRoot != "" && !current.State.DeliveryComplete(f.definition) && current.State.FinalValidationComplete(f.definition) {
				if _, promoteErr := f.prepareCandidate("formal-gates delivery " + f.runID); promoteErr != nil {
					return Run{}, promoteErr
				}
				continue
			}
			if current.State.ChildCreation == nil && !current.State.ResetPending() && !current.State.ResourcesCleaned() && current.State.CleanupEligible(f.definition) {
				if err := f.engine.BeginCleanup(emptyFingerprint()); err != nil {
					return Run{}, err
				}
				continue
			}
			break
		}
		if err != nil {
			// 修订屏障冻结签发＝等待用户决定，不是错误：照常投影 ask 边界。
			var rejected *protocol.RejectedError
			if !errors.As(err, &rejected) || rejected.Code != protocol.CodeChangeBarrierActive {
				return Run{}, err
			}
		}
	}
	// Native delivery has one verifier. A child freezes a recoverable checkpoint;
	// an ordinary regular run seals. An abort consumes its existing decision.
	if plan != nil && plan.Next.Kind == decision.KindComplete {
		current, loadErr := f.engine.Load()
		if loadErr != nil {
			return Run{}, loadErr
		}
		if _, aborted := current.State.AbortDecision(); !aborted {
			var deliveryErr error
			if current.State.SliceChild != nil {
				deliveryErr = f.checkpointDeliveredInWindow()
			} else {
				deliveryErr = f.sealDelivered()
			}
			if deliveryErr != nil {
				return Run{}, deliveryErr
			}
		}
	}
	run, err := f.project()
	if err != nil {
		return Run{}, err
	}
	if run.Status == "COMPLETE" || run.Status == "ABORTED" {
		// 终态发布前在 owner 协调锁内重扫区间并量取 owner 终态差值（幂等；
		// 只作报告），区间端点随终态摘要留存。摘要不可变：重入的 Drive 不改写
		// 已落盘字节。
		interval, err := f.finishOwnerCost()
		if err != nil {
			return Run{}, err
		}
		if run.Status == "ABORTED" {
			if err := f.engine.FreezeAbortedSlice(interval.endedAt, emptyFingerprint()); err != nil {
				return Run{}, err
			}
		}
		if run, err = f.project(); err != nil {
			return Run{}, err
		}
		// 终态快照边界（regular COMPLETE）：发布段内原生身份恒等（发布前后
		// 双 resolve，镜像 legacy Seal 的发布窗口核验）。
		snapshotGuard, err := f.terminalSnapshotGuard(run)
		if err != nil {
			return Run{}, err
		}
		if run.Route == "lightweight" || run.Status == "ABORTED" || run.Status == "COMPLETE" && run.Termination.NativeRoot != "" {
			if err := f.finalizeTerminal(run, interval); err != nil {
				return Run{}, err
			}
		} else if err := f.writeSummaryValue(summaryForRun(run, interval)); err != nil {
			return Run{}, err
		}
		if err := snapshotGuard(); err != nil {
			return Run{}, err
		}
		// 终态收口：移除 lifecycle active 标记（幂等）——终态后晚到的宿主
		// 事件归属无 run，与终态语义一致。
		if err := f.endLifecycle(); err != nil {
			return Run{}, err
		}
	}
	_ = plan
	return run, nil
}

// sealDelivered sends the shared native delivery proof to ordinary run sealing.
func (f *Facade) sealDelivered() error {
	identity, _, err := f.verifyDelivered()
	if err != nil {
		return err
	}
	snapshot, err := f.engine.Load()
	if err != nil {
		return err
	}
	if snapshot.State.Phase == runtime.PhaseTerminal {
		return nil
	}
	summary, err := f.readTerminalIntent()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err != nil || !f.terminalEvidenceComplete(summary) || summary.Revision != snapshot.Revision+1 {
		interval, err := f.finishOwnerCost()
		if err != nil {
			return err
		}
		snapshot, err = f.engine.Load()
		if err != nil {
			return err
		}
		run, err := f.project()
		if err != nil {
			return err
		}
		data, err := json.Marshal(snapshot.State)
		if err != nil {
			return err
		}
		var terminal protocol.State
		if err := json.Unmarshal(data, &terminal); err != nil {
			return err
		}
		terminal.Phase = runtime.PhaseTerminal
		run.Termination = &terminal
		run.Status = "COMPLETE"
		run.Revision++
		run.Next = decision.NextResult{Kind: decision.KindComplete}
		summary = summaryForRun(run, interval)
		summary.SliceGuarantee, err = f.materializeSliceGuarantee(snapshot.State)
		if err != nil {
			return err
		}
		if !f.terminalEvidenceComplete(summary) {
			return fmt.Errorf("terminal material requires concluded delivery and cleanup")
		}
		if err := f.admission.write(func() error { return writeJSONAtomic(terminalIntentPath(f.stateDir), summary) }); err != nil {
			return err
		}
	}
	// Complete material precedes retirement of active child evidence. Reentry
	// consumes the same intent without rebuilding a child inventory.
	if err := f.writeSummaryValue(summary); err != nil {
		return err
	}
	if err := f.cleanupCheckpoints(summary); err != nil {
		return err
	}
	if err := f.endLifecycle(); err != nil {
		return err
	}
	if _, _, err := f.verifyDelivered(); err != nil {
		return err
	}
	return f.engine.SealTerminal(identity, emptyFingerprint())
}

// verifyDelivered observes the promoted native snapshot and tree, the clean
// worktree and its baseline ancestry. Both seal and child checkpoint use it.
func (f *Facade) verifyDelivered() (string, string, error) {
	if f.vcs == nil {
		return "", "", fmt.Errorf("engine delivery: regular run %q has no native VCS provider for delivery verification", f.runID)
	}
	snap, err := f.engine.Load()
	if err != nil {
		return "", "", err
	}
	if !snap.State.DeliveryComplete(f.definition) {
		return "", "", fmt.Errorf("engine delivery: current final validation and exact promotion mapping are incomplete")
	}
	head, err := f.vcs.Resolve()
	if err != nil {
		return "", "", err
	}
	if head != snap.State.PromotedSnapshot {
		return "", "", fmt.Errorf("engine delivery: native VCS identity %s does not match the promoted snapshot %s", head, snap.State.PromotedSnapshot)
	}
	if err := f.vcs.VerifyReady(); err != nil {
		return "", "", err
	}
	if err := f.vcs.IsAncestorOrEqual(snap.State.BaseSnapshot, head); err != nil {
		return "", "", err
	}
	if f.vcs.Name() == "git" && snap.State.SliceChild == nil {
		count, err := f.vcs.CommitCount(snap.State.BaseSnapshot, head)
		if err != nil {
			return "", "", err
		}
		if count > 1 {
			return "", "", fmt.Errorf("engine delivery: native delivery requires at most one commit")
		}
	}
	tree, err := f.vcs.TreeDigest(head)
	if err != nil {
		return "", "", err
	}
	if tree != snap.State.PromotedDigest {
		return "", "", fmt.Errorf("engine delivery: native tree %s does not match the verified promoted tree %s", tree, snap.State.PromotedDigest)
	}
	return head, tree, nil
}

// checkpointDelivered holds the promotion window against decisions that revoke
// admission, then freezes owner measurement and its endpoint under their owner
// coordination lock. A committed receipt is replayed without another measurement.
func (f *Facade) checkpointDelivered() error {
	unlock, err := f.acquireNativeWindow()
	if err != nil {
		return err
	}
	defer unlock()
	return f.checkpointDeliveredInWindow()
}
func (f *Facade) checkpointDeliveredInWindow() error {
	snap, err := f.engine.Load()
	if err != nil {
		return err
	}
	if snap.State.SliceReceipt == nil {
		if err := func() error {
			release, err := lockOwnerCost(f.root)
			if err != nil {
				return err
			}
			defer release()
			head, tree, err := f.verifyDelivered()
			if err != nil {
				return err
			}
			interval, owner, err := f.observeOwnerCost()
			if err != nil {
				return err
			}
			return f.engine.CheckpointSlice(head, tree, owner, interval.endedAt, emptyFingerprint())
		}(); err != nil {
			return err
		}
		if snap, err = f.engine.Load(); err != nil {
			return err
		}
	}
	return f.publishSliceReceipt(snap.State.SliceReceipt)
}

// publishSliceReceipt materializes evidence from the state-owned frozen value.
// Recovery verifies the immutable object, so later legitimate HEAD movement does
// not redefine a completed checkpoint or its owner interval.
func (f *Facade) publishSliceReceipt(receipt *protocol.SliceReceipt) error {
	if receipt == nil || receipt.Digest != receipt.Receipt.CanonicalDigest() {
		return fmt.Errorf("engine slice checkpoint: frozen receipt is unavailable or inconsistent")
	}
	if f.vcs == nil {
		return fmt.Errorf("engine slice checkpoint: Git provider is required")
	}
	if err := f.vcs.Verify(receipt.Receipt.Snapshot); err != nil {
		return err
	}
	tree, err := f.vcs.TreeDigest(receipt.Receipt.Snapshot)
	if err != nil {
		return err
	}
	if tree != receipt.Receipt.Tree {
		return fmt.Errorf("engine slice checkpoint: frozen snapshot tree differs from its receipt")
	}
	path := filepath.Join(f.stateDir, "slice-receipt.json")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var published protocol.SliceReceipt
	if err != nil || json.Unmarshal(data, &published) != nil || published.Digest != receipt.Digest || published.Receipt.CanonicalDigest() != receipt.Digest {
		if err := f.admission.write(func() error { return writeJSONAtomic(path, receipt) }); err != nil {
			return err
		}
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &published); err != nil {
		return err
	}
	if published.Digest != receipt.Digest || published.Receipt.CanonicalDigest() != receipt.Digest {
		return fmt.Errorf("engine slice checkpoint: published receipt does not match the frozen value")
	}
	return nil
}

// Submit serializes new typed events with native execution. Exact accepted
// replay stays read-only; waiting callers consult the current ledger again.
func (f *Facade) Submit(event protocol.Event) (protocol.Acceptance, Run, error) {
	if f == nil || f.engine == nil {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("engine submit: nil façade")
	}
	accepted, recorded, err := f.recordedEvent(event)
	if err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	if recorded {
		run, err := f.projectOrSummary()
		return accepted, run, err
	}
	unlock, err := f.acquireWriteWindow()
	if err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	defer unlock()
	accepted, recorded, err = f.recordedEvent(event)
	if err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	if recorded {
		run, err := f.projectOrSummary()
		return accepted, run, err
	}
	if event.Request != nil && event.Request.Control == protocol.ControlReset {
		snap, err := f.engine.Load()
		if err != nil {
			return protocol.Acceptance{}, Run{}, err
		}
		preview := snap.State.ResetPreview()
		preview.Work, err = f.engine.RequirementWork()
		if err != nil {
			return protocol.Acceptance{}, Run{}, err
		}
		req, err := f.request()
		if err != nil {
			return protocol.Acceptance{}, Run{}, err
		}
		if req.IntakeConfirmationReceipt.RequirementSource != "" {
			revision, err := requirementArtifactRevision(filepath.Join(f.root, req.IntakeConfirmationReceipt.RequirementSource))
			if err != nil {
				return protocol.Acceptance{}, Run{}, err
			}
			if strings.HasPrefix(snap.State.RequirementRevision(), "sha256:") {
				revision = "sha256:" + revision
			}
			preview.RequirementRevision = revision
		}
		if err := f.resetChildrenPreview(&preview); err != nil {
			return protocol.Acceptance{}, Run{}, err
		}
		preview.Digest = preview.CanonicalDigest()
		event = event.WithResetPreview(preview)
	}
	if event.Request != nil && event.Request.Package != nil {
		if err := f.validateChildRequirementPackage(event.Request.Package); err != nil {
			return protocol.Acceptance{}, Run{}, err
		}
	}
	if event.Decide != nil && event.Decide.Reset != nil {
		if err := f.validateResetChildren(string(event.Decide.Request)); err != nil {
			return protocol.Acceptance{}, Run{}, err
		}
	}
	if event.Decide != nil && event.Decide.Requirement != nil {
		pkg := event.Decide.Requirement
		if len(pkg.Children) > 0 {
			view := protocol.RequirementDecisionPackage{Children: pkg.Children, Impact: pkg.Impact, RedoTopology: pkg.RedoTopology}
			if err := f.validateChildRequirementPackage(&view, true); err != nil {
				return protocol.Acceptance{}, Run{}, err
			}
		}
	}
	acceptance, err := f.engine.Submit(event, emptyFingerprint())
	if err != nil {
		return protocol.Acceptance{}, Run{}, err
	}
	if err := f.syncResetRequirement(); err != nil {
		return acceptance, Run{}, err
	}
	if err := f.syncReleasedChildren(); err != nil {
		return acceptance, Run{}, err
	}
	if err := f.recordChildReset(); err != nil {
		return acceptance, Run{}, err
	}
	run, err := f.projectOrSummary()
	return acceptance, run, err
}

func (f *Facade) recordedEvent(event protocol.Event) (protocol.Acceptance, bool, error) {
	accepted, recorded, err := f.engine.RecordedEvent(event)
	if isMissingEngineState(err) {
		accepted, err = f.replayTerminalEvent(event)
		recorded = err == nil
	}
	return accepted, recorded, err
}

// replayTerminalEvent reads accepted receipts from the retained State while
// cleanup is pending or complete. LastEvent also supports summaries without
// the State event ledger. Only Drive may finish the terminal intent.
func (f *Facade) replayTerminalEvent(event protocol.Event) (protocol.Acceptance, error) {
	summary, err := f.readTerminalIntent()
	if errors.Is(err, fs.ErrNotExist) {
		summary, err = f.readSummary()
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return protocol.Acceptance{}, &protocol.RejectedError{Code: protocol.CodeNotInitialized, Detail: "engine state and terminal receipt do not exist"}
		}
		return protocol.Acceptance{}, err
	}
	record := summary.LastEvent
	if summary.Termination != nil && summary.Termination.Events != nil {
		accepted, ok := summary.Termination.Events[string(event.ID)]
		if !ok {
			return protocol.Acceptance{}, &protocol.RejectedError{Code: protocol.CodeNotInitialized, Detail: "event is not recorded in the terminal run"}
		}
		record = &accepted
	}
	if record == nil || record.Acceptance.EventID != string(event.ID) {
		return protocol.Acceptance{}, &protocol.RejectedError{Code: protocol.CodeNotInitialized, Detail: "event is not the final recorded terminal event"}
	}
	return record.Replay(event)
}

func (f *Facade) Next() (decision.NextResult, error) {
	run, err := f.projectOrSummary()
	if err != nil {
		return decision.NextResult{}, err
	}
	return run.Next, nil
}

func (f *Facade) Show() (Run, error)   { return f.projectOrSummary() }
func (f *Facade) Status() (Run, error) { return f.projectOrSummary() }

func (f *Facade) projectOrSummary() (Run, error) {
	run, err := f.project()
	if err == nil && run.Status != "COMPLETE" && run.Status != "ABORTED" {
		return run, nil
	}
	if err != nil && !isMissingEngineState(err) {
		return Run{}, err
	}
	summary, summaryErr := f.readSummary()
	if summaryErr == nil {
		run = runFromSummary(summary)
		run.SummaryPath = terminalSummaryPath(f.stateDir)
		return run, nil
	}
	if !errors.Is(summaryErr, fs.ErrNotExist) {
		return Run{}, summaryErr
	}
	if err != nil {
		intent, intentErr := f.readTerminalIntent()
		if intentErr != nil {
			return Run{}, intentErr
		}
		if !f.terminalEvidenceComplete(intent) {
			return Run{}, fmt.Errorf("engine state is missing with unfinished terminal evidence")
		}
		run = runFromSummary(intent)
	}
	run.Status = "FINALIZING_CLEANUP"
	run.Next = decision.NextResult{Kind: decision.KindWait, Wait: &decision.WaitPayload{Reason: decision.WaitEngineInternal}}
	return run, nil
}

func isMissingEngineState(err error) bool {
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	var rejected *protocol.RejectedError
	return errors.As(err, &rejected) && rejected.Code == protocol.CodeNotInitialized
}

func (f *Facade) project() (Run, error) {
	snap, err := f.engine.Load()
	if err != nil {
		return Run{}, err
	}
	plan, _, err := f.engine.Plan()
	if err != nil {
		return Run{}, err
	}
	work, err := f.engine.RequirementWork()
	if err != nil {
		return Run{}, err
	}
	// COMPLETE 的单一权威是引擎 phase TERMINAL：regular 的 decide 边界在
	// SealTerminal 相变前保持 ACTIVE（交付核验失败可恢复），lightweight 的
	// CompleteAll 已在引擎内相变。
	status := "ACTIVE"
	if snap.State.Phase == runtime.PhaseFinalizingCleanup && snap.State.CleanupEligible(f.definition) {
		status = "FINALIZING_CLEANUP"
	}
	if snap.State.Phase == runtime.PhaseSliceReady {
		status = "SLICE_READY"
	}
	if snap.State.Phase == runtime.PhaseTerminal {
		status = "COMPLETE"
	}
	if _, aborted := snap.State.AbortDecision(); aborted {
		status = "ABORTING"
		if snap.State.Phase == runtime.PhaseTerminal {
			status = "ABORTED"
		}
	}

	run := Run{

		ValidationInputs: snap.State.ValidationInputs(f.definition),
		Attempts:         snap.State.Attempts,
		PendingActions:   snap.State.PendingActions,
		RejectedResults:  snap.State.RejectedResults,
		RecoveryRecords:  snap.State.RecoveryRecords,
		Validation:       snap.State.Validation,
		DispatchBindings: snap.State.DispatchBindings,
		AdapterIntents:   snap.State.ExecutableHostActions(),
		EndedAttempts:    snap.State.EndedAttempts,
		RunID:            f.runID,
		Runtime:          RuntimeEngine,
		Route:            snap.State.Route,
		Status:           status,
		Revision:         snap.Revision,
		Envelope:         f.envelope,
		BaseSnapshot:     snap.State.BaseSnapshot,
		PromotedSnapshot: snap.State.PromotedSnapshot,
		QAWorktree:       snap.State.QAWorktree,
		Next:             plan.Next,
		Review:           reviewFace(snap.State),
		Cost:             snap.State.Cost,
		SliceReceipt:     snap.State.SliceReceipt,
		SliceChildren:    snap.State.SliceChildren,
		SliceReceipts:    snap.State.SliceReceipts,
		GuardBudget:      snap.State.Guard,
		SummaryPath:      filepath.Join(f.stateDir, "terminal-summary.json"),
		Unverified:       snap.State.Route == "lightweight",
	}
	if status == "SLICE_READY" {
		run.SummaryPath = ""
	}

	for _, event := range snap.State.Events {
		if event.Acceptance.Kind == string(protocol.KindHostActionReceipt) || event.Acceptance.Kind == string(protocol.KindLifecycleEvent) || event.Acceptance.Kind == string(protocol.KindSpawnReceipt) {
			continue
		}
		if run.lastEvent == nil || event.Acceptance.Revision > run.lastEvent.Acceptance.Revision {
			record := event
			run.lastEvent = &record
		}
	}
	if record, aborted := snap.State.AbortEvent(); aborted {
		run.lastEvent = &record
		run.Termination = snap.State
	} else if snap.State.Phase == runtime.PhaseTerminal {
		run.Termination = snap.State
	}
	run.ValidationDecisions = map[string]protocol.RecordedDecision{}
	run.ValidationRequests = map[string]protocol.PendingAsk{}
	for id, d := range snap.State.Decisions {
		if d.Control == protocol.ControlValidation {
			run.ValidationDecisions[id] = d
			run.ValidationRequests[id] = snap.State.PendingAsks[id]
		}
	}
	run.NotGuaranteed = snap.State.ValidationNotGuaranteed()
	run.RequirementGuarantee = snap.State.RequirementGuarantee
	if snap.State.SliceChild != nil {
		run.MasterRunID = snap.State.SliceChild.MasterRunID
		run.ParentRequirement = snap.State.SliceChild.ParentChange
		parent, err := Open(f.root, run.MasterRunID)
		if err != nil {
			return Run{}, err
		}
		current, err := parent.engine.Load()
		if err != nil {
			return Run{}, err
		}
		for id, ask := range current.State.PendingAsks {
			if ask.Control == protocol.ControlRequestRequirementChange && !ask.Resolved {
				run.Next = decision.NextResult{Kind: decision.KindAsk, Ask: &decision.AskPayload{Requests: []string{"master:" + run.MasterRunID + ":" + id}}}
				break
			}
		}
	}
	run.RequirementWork = work
	if snap.State.Phase != runtime.PhaseTerminal {
		run.RequirementChildren, err = f.requirementChildren()
		if err != nil {
			return Run{}, err
		}
	}
	run.IntakeConfirmationReceipt = snap.State.IntakeConfirmationReceipt
	run.IntakeReceipt = snap.State.IntakeReceipt
	// The protocol plan owns the boundary; the facade adds the bound package
	// and freshness token for the pending requirement confirmation.
	if ask := f.pendingRequirementAskFace(snap); ask != nil {
		run.PendingRequirementAsk = ask
	}
	if faces := f.pendingAskFaces(snap); len(faces) > 0 {
		run.PendingAsks = faces
	}
	if plan.Next.Kind == decision.KindAsk {
		run.AvailableActions = []string{"submit"}
	}
	return run, nil
}

// pendingRequirementAskFace 投影未决的需求变化 Ask；不存在（屏障不在位）
// 时返回 nil。
func (f *Facade) pendingRequirementAskFace(snap protocol.Snapshot) *PendingRequirementAskFace {
	for id, ask := range snap.State.PendingAsks {
		if ask.Control != protocol.ControlRequestRequirementChange || ask.Resolved {
			continue
		}
		token, err := snap.Freshness(protocol.RequestID(id))
		if err != nil {
			token = ""
		}
		return &PendingRequirementAskFace{
			RequestID:    id,
			Options:      append([]protocol.AskOption{}, ask.Options...),
			Package:      ask.Package,
			BaseRevision: ask.BaseRevision,
			Token:        token,
		}
	}
	return nil
}

// pendingAskFaces 投影一切未决受限控制 Ask（按 requestID 排序，确定性输
// 出）；无未决 Ask 时返回 nil。
func (f *Facade) pendingAskFaces(snap protocol.Snapshot) []PendingAskFace {
	ids := make([]string, 0, len(snap.State.PendingAsks))
	for id, ask := range snap.State.PendingAsks {
		if ask.Resolved {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return nil
	}
	faces := make([]PendingAskFace, 0, len(ids))
	for _, id := range ids {
		ask := snap.State.PendingAsks[id]
		token, err := snap.Freshness(protocol.RequestID(id))
		if err != nil {
			token = ""
		}
		faces = append(faces, PendingAskFace{
			Validation: ask.Validation,
			RequestID:  id,
			RoundGrant: ask.RoundGrant,
			Control:    ask.Control,
			Options:    append([]protocol.AskOption{}, ask.Options...),
			RouteGates: ask.RouteGates,
			Token:      token,
		})
	}
	return faces
}

// Freshness 返回未决 Ask 的当前 freshness token：token 随每次提交失效，
// stale 后经本入口重取再提交决定。
func (f *Facade) Freshness(requestID string) (string, error) {
	if f == nil || f.engine == nil {
		return "", fmt.Errorf("facade freshness: nil façade")
	}
	return f.engine.Freshness(protocol.RequestID(requestID))
}

func terminalSummaryPath(stateDir string) string {
	return filepath.Join(stateDir, "terminal-summary.json")
}

// reopenVCSProvider 是重开 run 的 provider 构造：regular 路线用生产 exec
// runner（重开没有注入替身的通道，exec 是唯一合法生产形态）；lightweight 无
// VCS 语义，返回 nil。
func reopenVCSProvider(root, route string, names ...string) *vcs.Provider {
	if route != "regular" {
		return nil
	}
	name := "git"
	if len(names) != 0 {
		name = names[0]
	}
	return vcs.NewNative(root, name, nil)
}

func terminalIntentPath(stateDir string) string {
	return filepath.Join(stateDir, terminalIntentName)
}

func summaryForRun(run Run, interval ownerInterval) TerminalSummary {
	return TerminalSummary{LastEvent: run.lastEvent, RunID: run.RunID, Runtime: run.Runtime, Route: run.Route, Status: run.Status, Termination: run.Termination, ValidationDecisions: run.ValidationDecisions, ValidationRequests: run.ValidationRequests, NotGuaranteed: run.NotGuaranteed, Revision: run.Revision, Envelope: run.Envelope, IntakeReceipt: run.IntakeReceipt, Next: run.Next, Cost: run.Cost, GuardBudget: run.GuardBudget, Unverified: run.Unverified, CompletedAt: time.Now().UTC().Format(time.RFC3339Nano), OwnerTranscript: interval.transcript, OwnerStartedAt: interval.startedAt, OwnerEndedAt: interval.endedAt}
}

func (f *Facade) writeSummaryValue(summary TerminalSummary) error {
	path := terminalSummaryPath(f.stateDir)
	if data, err := os.ReadFile(path); err == nil {
		var existing TerminalSummary
		if decodeErr := json.Unmarshal(data, &existing); decodeErr != nil {
			return fmt.Errorf("engine terminal summary is invalid: %w", decodeErr)
		}
		// A terminal summary is immutable. Repeated drive/replay calls return
		// the already committed bytes and never refresh CompletedAt.
		if existing.RunID == summary.RunID && existing.Status == summary.Status && existing.Revision == summary.Revision && existing.Envelope == summary.Envelope && existing.Unverified == summary.Unverified {
			return nil
		}
		return fmt.Errorf("engine terminal summary conflicts with completed run")
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return f.admission.write(func() error { return writeJSONAtomic(path, summary) })
}

func (f *Facade) readTerminalIntent() (TerminalSummary, error) {
	data, err := os.ReadFile(terminalIntentPath(f.stateDir))
	if err != nil {
		return TerminalSummary{}, err
	}
	var summary TerminalSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return TerminalSummary{}, err
	}
	if (summary.Status != "COMPLETE" && summary.Status != "ABORTED") || summary.RunID != f.runID {
		return TerminalSummary{}, fmt.Errorf("engine terminal intent is not a complete summary")
	}
	if err := persistence.ValidateEnvelopeWithIdentity(summary.Envelope, f.envelope.PackageDigest, f.envelope.DefinitionSource, f.envelope.InstalledTargetIdentity); err != nil {
		return TerminalSummary{}, err
	}
	return summary, nil
}

// recoverTerminalIntent finishes a terminal cleanup whose process response
// was lost. It is called only by Drive, keeping Show/Status/Next read-only.
func (f *Facade) terminalEvidenceComplete(summary TerminalSummary) bool {
	s := summary.Termination
	if s == nil || s.Phase != runtime.PhaseTerminal || !s.ResourcesCleaned() || len(s.PendingActions) != 0 || len(s.PendingHostActions) != 0 {
		return false
	}
	for _, ask := range s.PendingAsks {
		if !ask.Resolved {
			return false
		}
	}
	for _, attempt := range s.EndedAttempts {
		if attempt.AwaitingResult {
			return false
		}
	}
	if _, abort := s.AbortDecision(); abort {
		record, ok := s.AbortEvent()
		return ok && summary.Status == "ABORTED" && summary.LastEvent != nil && record.Digest == summary.LastEvent.Digest && reflect.DeepEqual(record.Acceptance, summary.LastEvent.Acceptance)
	}
	return summary.Status == "COMPLETE" && (s.Route == "lightweight" || s.DeliveryComplete(f.definition))
}
func (f *Facade) recoverTerminalIntent() error {
	summary, err := f.readTerminalIntent()
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := f.engine.Load(); err == nil {
		return nil
	} else if !isMissingEngineState(err) {
		return err
	}
	if !f.terminalEvidenceComplete(summary) {
		return fmt.Errorf("engine state is missing with unfinished terminal evidence")
	}
	return f.commitTerminalCleanup(summary)
}

func (f *Facade) commitTerminalCleanup(summary TerminalSummary) error {
	if err := f.cleanupCheckpoints(summary); err != nil {
		return err
	}
	if err := f.store.CleanupTerminal(); err != nil {
		return err
	}
	if err := f.writeSummaryValue(summary); err != nil {
		return err
	}
	return f.admission.write(func() error {
		if err := os.Remove(terminalIntentPath(f.stateDir)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	})
}

// cleanupCheckpoints consumes the master's durable terminal authority. Frozen
// receipt evidence remains in that authority while active child files retire.
func (f *Facade) cleanupCheckpoints(summary TerminalSummary) error {
	s := summary.Termination
	if summary.Status != "COMPLETE" || s == nil || s.ChildCreation == nil {
		return nil
	}
	for _, plan := range s.ChildCreation.Children {
		child := &Facade{admission: f.admission, root: f.root, runID: plan.RunID, stateDir: filepath.Join(f.root, EngineNamespace, plan.RunID)}
		store, err := persistence.NewStore(child.stateDir, persistence.Config{PackageDigest: f.envelope.PackageDigest, DefinitionSource: f.envelope.DefinitionSource, InstalledTargetIdentity: f.envelope.InstalledTargetIdentity, AcquireAdmission: f.admission.acquire})
		if err != nil {
			return err
		}
		unlock, err := child.acquireNativeWindow()
		if err != nil {
			return err
		}
		err = child.endLifecycle()
		if err == nil {
			err = store.CleanupTerminal()
		}
		unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

// freezeCleanupSummary preserves the intended terminal evidence before native
// resource deletion. The State registry remains authoritative; typed cleanup
// evidence is attached when that intent is finalized.
func (f *Facade) freezeCleanupSummary(s *protocol.State) error {
	if _, err := f.readTerminalIntent(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	interval, err := f.finishOwnerCost()
	if err != nil {
		return err
	}
	run, err := f.project()
	if err != nil {
		return err
	}
	run.Termination = s
	run.Status = "COMPLETE"
	if _, aborted := s.AbortDecision(); aborted {
		run.Status = "ABORTED"
	}
	run.Next = decision.NextResult{Kind: decision.KindComplete}
	summary := summaryForRun(run, interval)
	if !s.RequirementBarrierActive() && !s.ResetPending() {
		summary.SliceGuarantee, err = f.materializeSliceGuarantee(s)
		if err != nil {
			return err
		}
	}
	return f.admission.write(func() error { return writeJSONAtomic(terminalIntentPath(f.stateDir), summary) })
}

func (f *Facade) finalizeTerminal(run Run, interval ownerInterval) error {
	summary := summaryForRun(run, interval)
	existing, err := f.readTerminalIntent()
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err == nil && f.terminalEvidenceComplete(existing) {
		summary = existing
	} else {
		if err == nil {
			summary.SliceGuarantee = existing.SliceGuarantee
		}
		if !f.terminalEvidenceComplete(summary) {
			return fmt.Errorf("terminal publication requires the actual completed State and authority")
		}
		if err := f.admission.write(func() error { return writeJSONAtomic(terminalIntentPath(f.stateDir), summary) }); err != nil {
			return err
		}
	}
	return f.commitTerminalCleanup(summary)
}

func (f *Facade) readSummary() (TerminalSummary, error) {
	data, err := os.ReadFile(filepath.Join(f.stateDir, "terminal-summary.json"))
	if err != nil {
		return TerminalSummary{}, err
	}
	var summary TerminalSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return TerminalSummary{}, err
	}
	if err := persistence.ValidateEnvelopeWithIdentity(summary.Envelope, f.envelope.PackageDigest, f.envelope.DefinitionSource, f.envelope.InstalledTargetIdentity); err != nil {
		return TerminalSummary{}, err
	}
	return summary, nil
}

func runFromSummary(summary TerminalSummary) Run {
	run := Run{lastEvent: summary.LastEvent, Termination: summary.Termination, ValidationDecisions: summary.ValidationDecisions, ValidationRequests: summary.ValidationRequests, NotGuaranteed: summary.NotGuaranteed, RunID: summary.RunID, Runtime: summary.Runtime, Route: summary.Route, Status: summary.Status, Revision: summary.Revision, Envelope: summary.Envelope, IntakeReceipt: summary.IntakeReceipt, Next: summary.Next, Cost: summary.Cost, GuardBudget: summary.GuardBudget, Unverified: summary.Unverified, SummaryPath: ""}
	if s := summary.Termination; s != nil {
		run.SliceReceipt = s.SliceReceipt
		run.BaseSnapshot = s.BaseSnapshot
		run.PromotedSnapshot = s.PromotedSnapshot
		run.Validation = s.Validation
		run.DispatchBindings = s.DispatchBindings
		run.RecoveryRecords = s.RecoveryRecords
	}
	return run
}

// Diagnose is deliberately raw and read-only.  It reports malformed/old
// envelopes without opening the normal writer.
type DiagnosticReport struct {
	Path             string         `json:"path"`
	JSONReadable     bool           `json:"jsonReadable"`
	DetectedVersions map[string]any `json:"detectedVersions,omitempty"`
	Supported        Envelope       `json:"supported"`
	Integrity        string         `json:"integrity"`
	Recommendation   string         `json:"recommendation,omitempty"`
	Summary          map[string]any `json:"summary,omitempty"`
}

func Diagnose(path string, packageDigest, targetIdentity string) (DiagnosticReport, error) {
	// When invoked with only a run path, recover the owning identity from the
	// immutable start request. This remains read-only and lets diagnose report a
	// non-empty-but-mismatched package/target binding, not just missing fields.
	if strings.TrimSpace(packageDigest) == "" || strings.TrimSpace(targetIdentity) == "" {
		if requestData, requestErr := os.ReadFile(filepath.Join(filepath.Dir(path), "request.json")); requestErr == nil {
			var request StartRequest
			if json.Unmarshal(requestData, &request) == nil {
				if strings.TrimSpace(packageDigest) == "" {
					packageDigest = request.PackageDigest
				}
				if strings.TrimSpace(targetIdentity) == "" {
					targetIdentity = request.InstalledTargetIdentity
				}
			}
		}
	}
	report := DiagnosticReport{Path: filepath.Clean(path), Integrity: "unknown", Supported: Envelope{Writer: persistence.Writer, StateSchemaVersion: encoder.StateSchemaVersion, WorkflowDefinitionVersion: definition.WorkflowDefinitionVersion, DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, PackageDigest: packageDigest, InstalledTargetIdentity: targetIdentity}}
	data, err := os.ReadFile(path)
	if err != nil {
		return report, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		report.Recommendation = "jsonReadable:false; rebuild with the owning writer"
		return report, nil
	}
	report.JSONReadable = true
	report.Integrity = "readable"
	report.DetectedVersions = map[string]any{}
	observedRaw := raw
	if nested, ok := raw["envelope"].(map[string]any); ok {
		observedRaw = nested
		report.Summary = raw
	}
	for _, key := range []string{"writer", "stateSchemaVersion", "workflowDefinitionVersion", "definitionSource", "definitionDigest", "packageDigest", "installedTargetIdentity"} {
		if v, ok := observedRaw[key]; ok {
			report.DetectedVersions[key] = v
		}
	}
	var observed Envelope
	if value, ok := raw["envelope"].(map[string]any); ok {
		observed = decodeEnvelope(value)
	} else {
		observed = decodeEnvelope(raw)
	}
	if strings.TrimSpace(packageDigest) != "" && strings.TrimSpace(targetIdentity) != "" {
		if err := persistence.ValidateEnvelopeWithIdentity(observed, packageDigest, report.Supported.DefinitionSource, targetIdentity); err != nil {
			report.Integrity = "unsupported"
			report.Recommendation = err.Error() + "; rebuild with the owning writer"
		}
	} else {
		for _, field := range []string{observed.Writer, observed.StateSchemaVersion, observed.WorkflowDefinitionVersion, observed.DefinitionSource, observed.DefinitionDigest, observed.PackageDigest, observed.InstalledTargetIdentity} {
			if strings.TrimSpace(field) == "" {
				report.Integrity = "unsupported"
				report.Recommendation = persistence.UnsupportedRunVersionCode + ": state envelope is incomplete; rebuild with the owning writer"
				break
			}
		}
	}
	return report, nil
}

func decodeEnvelope(raw map[string]any) Envelope {
	str := func(k string) string { v, _ := raw[k].(string); return v }
	return Envelope{Writer: str("writer"), StateSchemaVersion: str("stateSchemaVersion"), WorkflowDefinitionVersion: str("workflowDefinitionVersion"), DefinitionSource: str("definitionSource"), DefinitionDigest: str("definitionDigest"), PackageDigest: str("packageDigest"), InstalledTargetIdentity: str("installedTargetIdentity")}
}

func IntakeDigest(receipt IntakeConfirmationReceipt) (string, error) {
	receipt = canonicalizeIntakeReceipt(receipt)
	// Timestamps and the digest field are transport metadata, not intake
	// bindings. Excluding them keeps the digest stable when a receipt is
	// replayed and makes ConfirmedAt/ExpiresAt non-authoritative as required.
	canonical := struct {
		Coverage            *protocol.CoverageInventory `json:"coverage,omitempty"`
		Source              string                      `json:"source"`
		Authority           IntakeAuthority             `json:"authority"`
		Transport           IntakeTransport             `json:"transport"`
		RequirementSource   string                      `json:"requirementSource"`
		RequirementRevision string                      `json:"requirementRevision"`
		Artifacts           []IntakeArtifact            `json:"artifacts"`
		SolutionRevision    string                      `json:"solutionRevision"`
		SolutionDigest      string                      `json:"solutionDigest"`
	}{receipt.Coverage, receipt.Source, receipt.Authority, receipt.Transport, receipt.RequirementSource, receipt.RequirementRevision, receipt.Artifacts, receipt.SolutionRevision, receipt.SolutionDigest}
	data, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateConfirmation(receipt IntakeConfirmationReceipt, definitionSource string) error {
	if receipt.Source != DefaultIntakeSource || receipt.Authority != DefaultIntakeAuthority || receipt.Transport != DefaultIntakeTransport {
		return fmt.Errorf("%s: source, authority or transport is not the fixed stable launcher binding", InvalidIntakeConfirmation)
	}
	if strings.TrimSpace(receipt.RequirementSource) == "" || strings.TrimSpace(receipt.RequirementRevision) == "" || strings.TrimSpace(receipt.SolutionRevision) == "" || strings.TrimSpace(receipt.SolutionDigest) == "" {
		return fmt.Errorf("%s: requirement and solution bindings are required", InvalidIntakeConfirmation)
	}
	_ = definitionSource // requirement source is a caller-owned artifact path.
	seen := map[string]bool{}
	if len(receipt.Artifacts) == 0 {
		return fmt.Errorf("%s: complete artifact set is required", InvalidIntakeConfirmation)
	}
	for _, artifact := range receipt.Artifacts {
		if strings.TrimSpace(artifact.Path) == "" || strings.TrimSpace(artifact.Revision) == "" {
			return fmt.Errorf("%s: artifact path and revision are required", InvalidIntakeConfirmation)
		}
		cleanPath := filepath.ToSlash(filepath.Clean(artifact.Path))
		if filepath.IsAbs(filepath.FromSlash(cleanPath)) || cleanPath == "." || cleanPath == ".." || strings.HasPrefix(cleanPath, "../") {
			return fmt.Errorf("%s: artifact path %q must be relative to the candidate project", InvalidIntakeConfirmation, artifact.Path)
		}
		if seen[cleanPath] {
			return fmt.Errorf("%s: duplicate artifact path %q", InvalidIntakeConfirmation, artifact.Path)
		}
		seen[cleanPath] = true
	}
	// Receipt freshness is bound to the canonical requirement/solution fields
	// and their digest.  No wall-clock expiry is imposed because the confirmed
	// revision itself is the stable acceptance boundary.
	return nil
}

// validateArtifactBindings recomputes the injected artifact revisions using
// the same deterministic content hash used by the stable intake writer. It is
// intentionally scoped to normal repository files beneath ArtifactRoot;
// callers that run the protocol entirely in memory may omit ArtifactRoot.
func validateArtifactBindings(root string, receipt IntakeConfirmationReceipt) error {
	rootAbs, err := filepath.Abs(strings.TrimSpace(root))
	if err != nil {
		return fmt.Errorf("%s: resolve artifact root: %w", InvalidIntakeConfirmation, err)
	}
	solutionDigest := ""
	for _, artifact := range receipt.Artifacts {
		path := filepath.FromSlash(artifact.Path)
		if !filepath.IsAbs(path) {
			path = filepath.Join(rootAbs, path)
		}
		path = filepath.Clean(path)
		rel, relErr := filepath.Rel(rootAbs, path)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s: artifact path %q escapes candidate root", InvalidIntakeConfirmation, artifact.Path)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("%s: artifact %s is unavailable: %w", InvalidIntakeConfirmation, artifact.Path, readErr)
		}
		normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
		sum := sha256.Sum256([]byte(normalized))
		observed := hex.EncodeToString(sum[:])
		if observed != artifact.Revision {
			return fmt.Errorf("%s: artifact %s revision mismatch (receipt %s, current %s)", InvalidIntakeConfirmation, artifact.Path, artifact.Revision, observed)
		}
		if artifact.Revision == receipt.SolutionRevision {
			solutionDigest = "sha256:" + observed
		}
	}
	requirementPath := filepath.Clean(receipt.RequirementSource)
	if filepath.IsAbs(requirementPath) {
		if rel, relErr := filepath.Rel(rootAbs, requirementPath); relErr == nil {
			requirementPath = rel
		}
	}
	requirementPath = filepath.ToSlash(requirementPath)
	matched := false
	solutionMatched := false
	for _, artifact := range receipt.Artifacts {
		if filepath.ToSlash(filepath.Clean(artifact.Path)) == requirementPath {
			matched = true
			if artifact.Revision != receipt.RequirementRevision {
				return fmt.Errorf("%s: requirement revision does not match artifact %s", InvalidIntakeConfirmation, artifact.Path)
			}
		}
		if artifact.Revision == receipt.SolutionRevision {
			solutionMatched = true
		}
	}
	if !matched {
		return fmt.Errorf("%s: requirement source %q is not in the complete artifact set", InvalidIntakeConfirmation, receipt.RequirementSource)
	}
	if !solutionMatched {
		return fmt.Errorf("%s: solution revision is not in the complete artifact set", InvalidIntakeConfirmation)
	}
	if solutionDigest == "" || solutionDigest != receipt.SolutionDigest {
		return fmt.Errorf("%s: solution digest does not match the normalized solution artifact", InvalidIntakeConfirmation)
	}
	return nil
}

// canonicalizeIntakeReceipt returns the exact persisted representation for a
// receipt. Relative paths use slash separators and clean components, and the
// complete artifact set is sorted by canonical path then revision. Keeping the
// same value in StartRequest, state, intake receipt, digest and terminal
// summary prevents input order from becoming observable state.
func canonicalizeIntakeReceipt(receipt IntakeConfirmationReceipt) IntakeConfirmationReceipt {
	receipt.RequirementSource = filepath.ToSlash(filepath.Clean(strings.TrimSpace(receipt.RequirementSource)))
	receipt.Artifacts = append([]IntakeArtifact(nil), receipt.Artifacts...)
	for i := range receipt.Artifacts {
		receipt.Artifacts[i].Path = filepath.ToSlash(filepath.Clean(strings.TrimSpace(receipt.Artifacts[i].Path)))
	}
	sort.Slice(receipt.Artifacts, func(i, j int) bool {
		if receipt.Artifacts[i].Path != receipt.Artifacts[j].Path {
			return receipt.Artifacts[i].Path < receipt.Artifacts[j].Path
		}
		return receipt.Artifacts[i].Revision < receipt.Artifacts[j].Revision
	})
	return receipt
}

func validRunID(id string) bool {
	if id == "" {
		return false
	}
	for _, r := range id {
		if !(r == '-' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func startIntentPath(root, runID string) string {
	return filepath.Join(root, filepath.FromSlash(EngineNamespace), runID+startIntentSuffix)
}

// runDirectoryEstablished 报告 run 目录是否承载已成立的 run 事实：活动
// state.json，或终态清理后保留的不可变终态摘要（terminal-summary.json 及
// 其发布窗 intent）。无 active state ≠ 无 run——半启动判别必须把终态证据
// 当作已成立（Open 的终态回落读同一组文件）。
func runDirectoryEstablished(stateDir string) (bool, error) {
	for _, name := range []string{"state.json", "terminal-summary.json", terminalIntentName} {
		if _, err := os.Stat(filepath.Join(stateDir, name)); err == nil {
			return true, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
	}
	return false, nil
}

// recoverStartIntent 清理未完成启动的残留：run 已成立（活动 state 或终态
// 证据）时只清过期 intent，目录不动；三者皆无而 intent 在＝真半启动残留
// （request.json 只是 StartRequest 的纯投影——无 state 即无 run，「request
// 先于 state」的不变量不受影响），stateDir 内容与 intent 一并清除，让同
// 一 bootstrap 路径重建。
func recoverStartIntent(root, runID string) error {
	path := startIntentPath(root, runID)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var intent startIntent
	if err := json.Unmarshal(data, &intent); err != nil {
		return fmt.Errorf("engine start intent is invalid: %w", err)
	}
	if intent.RunID != runID {
		return fmt.Errorf("engine start intent run id mismatch")
	}
	stateDir := filepath.Join(root, filepath.FromSlash(EngineNamespace), runID)
	established, err := runDirectoryEstablished(stateDir)
	if err != nil {
		return err
	}
	if !intent.Admission.matchesRoot(root) {
		return fmt.Errorf("UNREGISTERED_INSTALL: start intent root does not match its admitted root")
	}
	return intent.Admission.write(func() error {
		if !established {
			if err := os.RemoveAll(stateDir); err != nil {
				return err
			}
		}
		return os.Remove(path)
	})
}

var errStaleFencingToken = errors.New("STALE_FENCING_TOKEN")

func admissionFromRequest(req StartRequest) Admission {
	return Admission{RegistryPath: req.AdmissionRegistryPath, Target: req.AdmissionTarget, Root: req.AdmissionRoot,
		Provider: req.Provider, PackageDigest: req.PackageDigest, InstalledTargetIdentity: req.InstalledTargetIdentity,
		Generation: req.AdmissionGeneration, Lease: req.AdmissionLease, Token: req.AdmissionToken}
}

func (admission Admission) validate() error {
	if !filepath.IsAbs(admission.RegistryPath) || !filepath.IsAbs(admission.Target) || !filepath.IsAbs(admission.Root) ||
		strings.TrimSpace(admission.Provider) == "" || strings.TrimSpace(admission.PackageDigest) == "" ||
		strings.TrimSpace(admission.InstalledTargetIdentity) == "" || admission.Generation == 0 ||
		strings.TrimSpace(admission.Lease) == "" || strings.TrimSpace(admission.Token) == "" {
		return fmt.Errorf("UNREGISTERED_INSTALL: candidate writer requires a complete immutable installation binding")
	}
	return nil
}

func (admission Admission) matchesRoot(root string) bool {
	return vcs.SamePath(admission.Root, root)
}

// acquire owns each bounded installation write window. Business coordination
// locks are acquired before it; Store process/state locks are acquired after it.
// Callers release it before invoking another Store or facade writer, acquiring
// a business lock, or waiting for host work. The request carries the original
// namespace/token; registry records alone determine whether that token can write.
func (admission Admission) acquire() (func(), error) {
	if err := admission.validate(); err != nil {
		return nil, err
	}
	// Missing registries cannot create a namespace merely by rejecting a writer.
	if _, err := os.Stat(admission.RegistryPath); err != nil {
		return nil, fmt.Errorf("UNREGISTERED_INSTALL: admission registry is unavailable: %w", err)
	}
	if err := verifyAdmissionSnapshot(admission); err != nil {
		return nil, err
	}
	release, err := coordination.AcquireExistingPathWaiting(admission.RegistryPath+".lock", "admission")
	if err != nil {
		return nil, err
	}
	if err := verifyAdmissionSnapshot(admission); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func (admission Admission) write(write func() error) error {
	release, err := admission.acquire()
	if err != nil {
		return err
	}
	defer release()
	return write()
}

// verifyAdmissionSnapshot checks the exact owning binding without resolving a
// replacement token or hashing the installed tree. Public launcher resolution
// owns the package verification; the per-operation check owns registry fencing.
func verifyAdmissionSnapshot(admission Admission) error {
	if err := admission.validate(); err != nil {
		return err
	}
	data, err := os.ReadFile(admission.RegistryPath)
	if err != nil {
		return fmt.Errorf("UNREGISTERED_INSTALL: admission registry cannot be read: %w", err)
	}
	var doc struct {
		Records []struct {
			ID              string `json:"id"`
			Status          string `json:"status"`
			Target          string `json:"target"`
			Host            string `json:"host"`
			Scope           string `json:"scope"`
			ProjectRoot     string `json:"projectRoot"`
			PackageDigest   string `json:"packageDigest"`
			InstalledDigest string `json:"installedDigest"`
			Generation      uint64 `json:"generation"`
			Lease           string `json:"lease"`
			Token           string `json:"token"`
		} `json:"records"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("UNREGISTERED_INSTALL: admission registry is invalid: %w", err)
	}
	for _, record := range doc.Records {
		if record.ID != admission.InstalledTargetIdentity {
			continue
		}
		if record.Generation != admission.Generation || record.Lease != admission.Lease || record.Token != admission.Token || !strings.EqualFold(record.Status, "active") {
			return fmt.Errorf("%w: owning installation is no longer active", errStaleFencingToken)
		}
		packageDigest := record.PackageDigest
		if packageDigest == "" {
			packageDigest = record.InstalledDigest
		}
		if packageDigest != admission.PackageDigest || filepath.Clean(record.Target) != filepath.Clean(admission.Target) || !admissionProviderMatches(record.Host, admission.Provider) ||
			(record.Scope != "project" && record.Scope != "global") {
			return fmt.Errorf("UNREGISTERED_INSTALL: owning installation binding does not match registry record")
		}
		if record.Scope == "project" {
			root, rootErr := filepath.EvalSymlinks(admission.Root)
			project, projectErr := filepath.EvalSymlinks(record.ProjectRoot)
			if rootErr != nil || projectErr != nil {
				return fmt.Errorf("UNREGISTERED_INSTALL: project binding cannot be resolved: %w", errors.Join(rootErr, projectErr))
			}
			if root != project {
				return fmt.Errorf("UNREGISTERED_INSTALL: registry project root does not match owning run")
			}
		}
		return nil
	}
	return fmt.Errorf("UNREGISTERED_INSTALL: candidate admission record %q is missing", admission.InstalledTargetIdentity)
}

func admissionProviderMatches(registered, provider string) bool {
	if registered == provider {
		return true
	}
	descriptor, err := host.Lookup(registered)
	return err == nil && descriptor.ID == provider
}

func (f *Facade) endLifecycle() error {
	path := filepath.Join(f.root, ".gates", "tmp", f.runID, "lifecycle", "active")
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return f.admission.write(func() error { return lifecycle.EndRun(f.root, f.runID) })
}

func emptyFingerprint() string {
	digest, _ := (decision.Observation{Facts: []decision.Fact{}}).Digest()
	return digest
}

func unsupported(field, want, got string) error {
	return &persistence.UnsupportedRunVersionError{Field: field, Expected: want, Observed: got}
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data, 0o600)
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".engine-*-.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// DevelopmentPlan projects the exact proposal and bindings for route confirmation.
func (f *Facade) DevelopmentPlan(route protocol.RouteDecision) (*protocol.DevelopmentPlanPreview, error) {
	return f.engine.DevelopmentPlan(route)
}

func reopenNativeProvider(configRoot, root string, req StartRequest) *vcs.Provider {
	p := reopenVCSProvider(configRoot, req.Route, req.VCS)
	if p == nil {
		return nil
	}
	return p.At(root, req.NativeHandle)
}
