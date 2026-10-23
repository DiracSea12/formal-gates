"use strict";
const BOOT = (typeof window !== "undefined" && window.__PANEL__) || { window: "", mode: "library", intakePath: "", token: "" };
const DOM = typeof document !== "undefined";
const $ = s => document.querySelector(s);
const el = (tag, cls, text) => { const e = document.createElement(tag); if (cls) e.className = cls; if (text !== undefined) e.textContent = text; return e; };

const api = {
	async get(path) {
		const r = await fetch(path);
		const j = await r.json().catch(() => ({}));
		if (!r.ok) throw new Error(j.error || ("HTTP " + r.status));
		return j;
	},
	async post(path, body) {
		const r = await fetch(path, {
			method: "POST",
			headers: { "Content-Type": "application/json", "X-Formal-Gates-Token": BOOT.token },
			body: JSON.stringify(body),
		});
		const j = await r.json().catch(() => ({}));
		if (!r.ok) throw new Error(j.error || ("HTTP " + r.status));
		return j;
	},
};

// Seven configurable responsibilities; every repeated batch/slice/merge node
// maps back to one of these keys. round is the persisted roundLimits key.
const RESP = [
	{ id: "product-review", t: "产品审查", round: "product-review", rh: "跑满后每加一轮需你授权" },
	{ id: "start-readiness", t: "开发前技术审查", round: "tech-review", rh: "跑满后每加一轮需你授权" },
	{ id: "development", t: "初始开发实现" },
	{ id: "qa-blackbox", t: "黑盒 QA", qa: "blackbox", round: "qa-blackbox", rh: "设计↔审查返工连续 FAIL 的上限", warning: "黑盒 QA 涉及实际操作与验证，工作量、耗时和 token 消耗可能较大。" },
	{ id: "qa-whitebox", t: "白盒 QA", qa: "whitebox", round: "qa-whitebox", rh: "设计↔审查返工连续 FAIL 的上限" },
	{ id: "gate", t: "质量门", d: "已选普通门 + 合并门" },
	{ id: "repair", t: "业务返修", round: "repair", rh: "验证轮与返修轮合计达到上限后需授权", d: "已成立问题的返修" },
];
const LABEL = Object.fromEntries(RESP.map(n => [n.id, n.t]));
const BUILTIN = "默认";
// Node box width in px; must match .node width in style.css.
const NODE_W = 220;
const SVG_NS = "http://www.w3.org/2000/svg";

let state = null;          // last /api/state payload
let winInfo = null;        // last /api/window payload
let editing = null;        // configuration name being edited
let editingScope = "project";
let work = null;           // browser-only draft
let dirty = false;
let baselineRev = null;    // overlay revisions the draft was read against
let sel = null;            // selected responsibility (a RESP id, shared by repeated graph nodes)
let selKey = null;         // clicked graph node key; distinct instances of one resp differ
let saveMode = false;
let runID = null;          // selected run; "" means library view
let closing = false;
let optSeq = 0;            // stale async guard for execution options
let activity = false;      // real user activity since last heartbeat
let gone = false;
let downSince = 0;         // first failure of the current poll-failure streak; a successful poll clears it
let pendingDo = null;      // continuation to run after a successful dirty-guard save
let inspSig = "";          // inspector content signature; rebuilds only on change
let inspGen = 0;           // bumped on each inspector rebuild so stale async work bails
const boundOpen = {};      // configuration name -> bound-run list expanded

if (DOM) ["click", "keydown", "wheel", "pointerdown", "touchstart", "dragstart"].forEach(ev =>
	document.addEventListener(ev, () => { activity = true; }, { capture: true, passive: true }));

// ---------- configuration semantics (mirroring settings.Configuration) ----------
function dis(work, id) { return !!(work.flowSelection && (work.flowSelection.disabled || []).includes(id)); }
function setSel(arr, v, on) { arr = (arr || []).slice(); const i = arr.indexOf(v); if (on && i < 0) arr.push(v); if (!on && i >= 0) arr.splice(i, 1); return arr; }
function nodeEnabled(work, id) {
	const meta = RESP.find(n => n.id === id);
	if (meta && meta.qa) return !dis(work, "qa") && (work.qaModes || []).includes(meta.qa);
	return !dis(work, id);
}
function setNodeEnabled(work, id, on) {
	const meta = RESP.find(n => n.id === id);
	if (!work.flowSelection) work.flowSelection = { disabled: [] };
	if (meta && meta.qa) {
		if (dis(work, "qa")) work.qaModes = null;
		work.qaModes = setSel(work.qaModes, meta.qa, on);
		work.flowSelection.disabled = setSel(work.flowSelection.disabled, "qa", (work.qaModes || []).length === 0);
		return;
	}
	work.flowSelection.disabled = setSel(work.flowSelection.disabled, id, !on);
}
function roundKey(id) { const m = RESP.find(n => n.id === id); return m ? m.round : null; }
function roundOf(work, id) { const k = roundKey(id); return (work.roundLimits && work.roundLimits[k]) || 3; }
function setRound(work, id, v) { const k = roundKey(id); if (!work.roundLimits) work.roundLimits = {}; work.roundLimits[k] = v; }
function execOf(work, id) { return (work.execution && work.execution[id]) || {}; }
function setExec(work, id, patch) {
	if (!work.execution) work.execution = {};
	const cur = work.execution[id] || {};
	work.execution[id] = {
		provider: patch.provider !== undefined ? patch.provider : cur.provider || "",
		model: patch.model !== undefined ? patch.model : cur.model || "",
		reasoningEffort: patch.reasoningEffort !== undefined ? patch.reasoningEffort : cur.reasoningEffort || "",
	};
}
// Gates: undefined/null = all discovered ordinary gates; array = explicit list.
function gatesAll(work) { return work.gates === undefined || work.gates === null; }
// ---------- server state ----------
async function loadState() {
	state = await api.get("/api/state");
	if (!baselineRev) baselineRev = state.revisions;
	return state;
}
// The selected run: runID picks one; "" means library view; no selection falls
// back to the first live run. An intake window is always a fresh admission: it
// never inherits an existing run's locks, plan or binding.
function selectedRun() {
	if (BOOT.mode === "intake") return null;
	const runs = (state && state.runs) || [];
	if (runID === "" ) return null;
	if (runID) return runs.find(r => r.runId === runID) || null;
	const live = runs.filter(r => r.status !== "COMPLETE" && r.status !== "ABORTED");
	return live[0] || null;
}
function option(name) { return (state.configurations || []).find(o => o.name === name); }
function boundRuns(name) { return (state.bindings && state.bindings[name]) || []; }
// Runs are identified to users by their requirement document name, start
// time and status; a run without a requirement source falls back to a short
// id.  The full run id stays on tooltips and API values.
function shortRunId(id) { return (id || "").replace(/^run-/, "").slice(0, 12) || "?"; }
function runLabel(r) {
	const t = r && r.startedAt ? new Date(r.startedAt) : null;
	const when = t && !isNaN(t) ? `${t.getMonth() + 1}-${t.getDate()} ${String(t.getHours()).padStart(2, "0")}:${String(t.getMinutes()).padStart(2, "0")}` : "";
	const st = !r || r.status === "COMPLETE" || r.status === "ABORTED" ? "已结束" : r.status;
	const name = (r && r.requirement) || "run " + shortRunId(r && r.runId);
	return `${name}${when ? " · " + when : ""} · ${st}`;
}
function runRef(id) { const r = (state.runs || []).find(x => x.runId === id); return r ? runLabel(r) : "run " + shortRunId(id); }
// The currently-effective configuration: the selected run's binding in run
// mode, else the enable pointer target.
function currentName() { const run = selectedRun(); return run ? run.configuration : state.active; }
// Node obligations belong to the selected run only when editing its bound
// configuration; other edits are library drafts without a lock story.
function nodeState(id) {
	const run = selectedRun();
	if (!run || editing !== run.configuration) return null;
	return (run.nodes || []).find(n => n.node === id) || null;
}
function selectedRunTerminal() {
	const r = ((state && state.runs) || []).find(x => x.runId === runID);
	return !!r && (r.status === "COMPLETE" || r.status === "ABORTED");
}

// ---------- revision / external change ----------
function layersChanged(next) {
	const out = [];
	for (const k of ["user", "project"]) if ((baselineRev || {})[k] !== (next.revisions || {})[k]) out.push(k);
	return out;
}
function layerName(k) { return k === "user" ? "用户层" : "项目层"; }
let bannerTimer = null;
function banner(html, buttons, info) {
	const b = $("#banner");
	b.className = "banner show" + (info ? " info" : "");
	b.textContent = "";
	const span = el("span", "", html); b.appendChild(span);
	for (const btn of buttons || []) b.appendChild(btn);
	if (bannerTimer) clearTimeout(bannerTimer);
	if (info) bannerTimer = setTimeout(() => b.classList.remove("show"), 5000);
}
let lastLoadError = "";
// conflictActive tracks the unresolved dirty-vs-external-edit banner so
// transient toasts cannot clobber its resolution buttons.
let conflictActive = false;
async function onRevisionsChanged(layers) {
	const label = layers.map(layerName).join("、");
	if (!dirty) {
		await refreshEditing();
		baselineRev = state.revisions;
		banner(label + "配置在别处被修改，已重新加载", null, true);
		return;
	}
	conflictActive = true;
	banner(`有未保存修改，但${label}配置在别处被修改`, [
		Object.assign(el("button", "", "重新加载（放弃草稿）"), {
			onclick: async () => { await refreshEditing(); baselineRev = state.revisions; dirty = false; conflictActive = false; $("#banner").classList.remove("show"); render(); },
		}),
		Object.assign(el("button", "", "用我的覆盖"), {
			onclick: async () => {
				await loadState(); baselineRev = state.revisions;
				try {
					const o = option(editing);
					await save(o ? o.scope || "project" : "project", editing, !!o);
					conflictActive = false;
					$("#banner").classList.remove("show");
				} catch (e) { toast(e.message); }
			},
		}),
	]);
}
async function refreshEditing() {
	const opt = option(editing);
	if (opt) work = structuredClone(opt.configuration);
	render();
}

// ---------- library operations ----------
async function save(scope, name, overwrite) {
	const expected = baselineRev || state.revisions;
	await api.post("/api/configurations", {
		operation: "save", scope, name, candidate: work, overwrite: !!overwrite,
		expected, runId: selectedRun() ? selectedRun().runId : "",
	});
	const keepSel = sel;
	// Work and baseline reload together from one state read, so a foreign write
	// landing after our save is reflected verbatim instead of mixing stale
	// draft bytes with a newer baseline.
	await loadState(); baselineRev = state.revisions;
	editing = name;
	const stored = option(editing);
	work = structuredClone(stored ? stored.configuration : work);
	editingScope = (stored || {}).scope || scope;
	dirty = false; saveMode = false; conflictActive = false;
	sel = keepSel;
	render();
	savedTick();
	const next = pendingDo; pendingDo = null;
	if (next) await next();
}
function savedTick() {
	const m = $("#msg"); const ok = el("span", "ok", "✓ 已保存"); m.prepend(ok);
	setTimeout(() => ok.remove(), 2000);
}
function toast(text) {
	// While the conflict banner holds resolution buttons, transient errors go
	// to the footer message area instead of replacing it.
	if (conflictActive) {
		const m = $("#msg"); const t = el("span", "ok", text); m.prepend(t);
		setTimeout(() => t.remove(), 4000);
		return;
	}
	banner(text, null, true);
}

// Reload the library after a mutation without touching the browser draft,
// unless the configuration being edited disappeared; then fall back to the
// preferred name or the active pointer target.
async function reloadLibrary(prefer) {
	await loadState(); baselineRev = state.revisions;
	const cur = option(editing);
	if (cur) {
		editingScope = cur.scope || "project";
		if (!dirty) work = structuredClone(cur.configuration);
	} else {
		editing = (prefer && option(prefer) ? prefer : null) || state.active || ((state.configurations || [])[0] || {}).name || BUILTIN;
		const o = option(editing);
		work = structuredClone(o ? o.configuration : {});
		editingScope = (o && o.scope) || "project";
		dirty = false;
	}
	render();
}

// guardDirtyThen routes every draft-losing navigation through the required
// Save / Discard / Cancel choice; Save enters save mode and continues once a
// named save succeeds.
async function guardDirtyThen(next) {
	if (!dirty) { await next(); return; }
	const r = await dlg({ title: "未保存的修改", msg: "当前草稿有未保存修改。", ok: "保存", cancel: "放弃修改", extra: "取消" });
	if (r === "extra" || r === undefined) return;
	if (r === true) { pendingDo = next; openSaveMode(); return; }
	dirty = false; conflictActive = false;
	await next();
}

// ---------- graph ----------
// buildGraph draws the real structure per §3.1.1: reviews → dependency-ordered
// development batches and their gates → parallel slices (each own dev + slice
// gates, no QA inside) → mainline merge → merge QA + merge gate →
// verification QA and ordinary gates in parallel → repair → verification.
// Before a plan is confirmed the canvas is an explicitly marked schematic.
//
// Every repeated development node names its role in the label (开发批次 /
// 开发分片 / 开发分片 · 分片门) because those boxes are the only ones whose
// count is run-specific; the fixed review, merge and verification nodes keep
// their own names so a lane of batches never reads as one more review stage.
function buildGraph() {
	const run = selectedRun();
	const nodes = [], edges = [], groups = [];
	// X and Y are the column and row pitches in px. X stays wider than one node
	// box (NODE_W = 220, mirrored by .node width in style.css) so horizontal
	// neighbours never overlap and the r→l flow link keeps a channel of its
	// own. SLICE_DX is the batch pitch inside one slice lane, counted in X
	// units; 0.95 * X = 237.5px, still wider than NODE_W.
	const X = 250, Y = 118, SLICE_DX = 0.95;
	// put() places a node on the col/row lattice: x = 40 + col*X,
	// y = 50 + row*Y. op marks a non-configurable operation node — it carries
	// no resp, so nodeState()/execOf() never see it, it renders no status pill
	// and it cannot be selected. terminal marks a display-only end node that
	// closes the flow (the seal). Both flags describe the graph, not runtime
	// state.
	const put = (key, resp, label, sub, col, row, op, terminal, gateStatus) => {
		nodes.push({ key, resp, label, sub, op: !!op, terminal: !!terminal, gateStatus, x: 40 + col * X, y: 50 + row * Y });
	};
	const link = (a, b, cls) => edges.push({ a, b, cls });
	const qaMergeResp = () => nodeEnabled(work, "qa-whitebox") ? "qa-whitebox" : nodeEnabled(work, "qa-blackbox") ? "qa-blackbox" : null;
	const verifyTail = col => {
		let col2 = col;
		const qaOn = nodeEnabled(work, "qa-blackbox") || nodeEnabled(work, "qa-whitebox");
		put("v-qa-b", "qa-blackbox", "验证·黑盒 QA", "执行验收", col2, 0.4);
		put("v-qa-w", "qa-whitebox", "验证·白盒 QA", "执行验收", col2, 1.4);
		put("v-gate", "gate", "验证·质量门", "已选普通门", col2, 2.4);
		// The repair column sits a stride and a half right of the verification
		// column (1.45 * X = 362.5px): the channel left between those columns
		// carries the return manifold that drawRepairLoops runs from the repair
		// node's bottom into each verification node's bottom.
		put("v-repair", "repair", "业务返修", "", col2 + 1.45, 1.4);
		// Seal closes the flow as a display-only terminal operation: its resp is
		// empty, so no nodeState()/execOf() lookup and no backend read or write
		// can name it; only the repair edge into it plus the op/terminal render
		// express it. It sits 1.2 * X = 300px right of repair so the manifold
		// channel stays clear.
		put("v-seal", "", "封板 Seal", "终态操作 · 不可配置", col2 + 2.65, 1.4, true, true);
		link("v-repair", "v-seal");
		const heads = [];
		if (nodeEnabled(work, "qa-blackbox")) heads.push("v-qa-b");
		if (nodeEnabled(work, "qa-whitebox")) heads.push("v-qa-w");
		heads.push("v-gate");
		for (const h of heads) link(h, "v-repair");
		for (const h of heads) link("v-repair", h, "loop"); // 返修后回到验证
		return heads;
	};
	const mergeTail = (fromKeys, col) => {
		put("op-merge", "", "主线合并", "不可配置操作", col, 1, true);
		for (const f of fromKeys) link(f, "op-merge");
		const mqa = qaMergeResp();
		put("m-gate", "gate", "合并门", "", col + 1, mqa ? 1.5 : 1);
		if (mqa) put("m-qa", mqa, "合并 QA", mqa === "qa-whitebox" ? "跟随白盒配置" : "跟随黑盒配置", col + 1, 0.5);
		link("op-merge", "m-gate");
		if (mqa) link("op-merge", "m-qa");
		const heads = verifyTail(col + 2);
		for (const h of heads) {
			if (h === "v-gate") link("m-gate", h);
			else if (mqa) link("m-qa", h);
		}
	};

	const flow = run && run.flow;
	// Master dev chain draws only batches the engine requires of this run;
	// slice-owned batches are rendered inside their slice. An omitted required
	// flag counts as required for this filter, while required:false marks the
	// batches the engine does not own under this run's merge ownership — the
	// owning slice child run supplies their progress.
	const batches = ((flow && flow.developmentBatches) || []).filter(b => b.required !== false);
	const slices = (run && run.slices) || [];
	// Any slice switches the mainline to the split shape: slice batches and
	// their gate render inside the slice lane, the slice gates feed the merge
	// operation, and only the master's own batches stay in the dev chain.
	const split = slices.length > 0;
	// planReady is the one prerequisite for drawing the real flow: an intake
	// window, a run without flow, or a run whose development plan is still
	// unconfirmed all render the schematic below instead.
	const planReady = !!(flow && flow.planConfirmed);

	if (!run || !planReady) {
		// Schematic: two serial batches with ordinary gates, then two parallel
		// slices. The actual counts come from topology confirmation.
		put("n-product-review", "product-review", "产品审查", "", 0, 1);
		put("n-start-readiness", "start-readiness", "开发前技术审查", "", 1, 1);
		put("n-dev-0", "development", "开发批次 1", "", 2, 1);
		put("n-gate-0", "gate", "批次 1 · 质量门", "已选普通门", 3, 1);
		put("n-dev-1", "development", "开发批次 2", "批次串行", 4, 1);
		put("n-gate-1", "gate", "批次 2 · 质量门", "已选普通门", 5, 1);
		put("n-qa-blackbox", "qa-blackbox", "黑盒 QA 设计/审查", "与开发并行", 2.6, 0);
		put("n-qa-whitebox", "qa-whitebox", "白盒 QA 设计/审查", "与开发并行", 2.6, 2.1);
		link("n-product-review", "n-start-readiness"); link("n-start-readiness", "n-dev-0");
		link("n-dev-0", "n-gate-0"); link("n-gate-0", "n-dev-1"); link("n-dev-1", "n-gate-1");
		link("n-start-readiness", "n-qa-blackbox"); link("n-start-readiness", "n-qa-whitebox");
		put("s0-dev", "development", "开发分片 A", "", 6, 0.3);
		put("s0-gate", "gate", "开发分片 A · 分片门", "", 6.95, 0.3);
		put("s1-dev", "development", "开发分片 B", "", 6, 1.7);
		put("s1-gate", "gate", "开发分片 B · 分片门", "", 6.95, 1.7);
		link("n-gate-1", "s0-dev"); link("n-gate-1", "s1-dev"); link("s0-dev", "s0-gate"); link("s1-dev", "s1-gate");
		mergeTail(["s0-gate", "s1-gate"], 7.95);
		groups.push({ x: 16, y: 0, w: 900, h: 26, label: "示意图 · 受理阶段的批次与分片数量以拓扑确认为准；分片内无 QA" });
		return { nodes, edges, groups, schematic: true };
	}

	// Actual dependencies leave each batch through its own projected gate.
	put("n-product-review", "product-review", "产品审查", "", 0, 1);
	put("n-start-readiness", "start-readiness", "开发前技术审查", "", 1, 1);
	link("n-product-review", "n-start-readiness");
	const batchKey = {};
	const batchGateKey = {};
	batches.forEach((b, i) => {
		const key = "n-dev-" + b.id;
		batchKey[b.id] = key;
		batchGateKey[b.id] = "n-gate-" + b.id;
		const statusText = { QUEUED: "排队", ISSUED: "已签发", RUNNING: "运行中", VALIDATING: "校验中", TERMINAL: "已完成" }[b.status] || b.status;
		put(key, "development", "开发批次 " + b.id, [b.owner || "", statusText].filter(Boolean).join(" · "), 2 + i * 2, split ? 0.15 : 0.7);
		put(batchGateKey[b.id], "gate", "批次 " + b.id + " · 质量门", "已选普通门", 3 + i * 2, split ? 0.15 : 0.7, false, false, b.gate && b.gate.status);
		link(key, batchGateKey[b.id]);
	});
	for (const b of batches) {
		const deps = (b.dependsOn || []).filter(d => batchKey[d]);
		for (const d of deps) link(batchGateKey[d], batchKey[b.id]);
		if (!deps.length) link("n-start-readiness", batchKey[b.id]);
	}
	if (!batches.length) {
		put("n-dev", "development", "初始开发实现", "", 2, split ? 0.6 : 1);
		link("n-start-readiness", "n-dev");
	}
	const devHeads = batches.length ? batches.filter(b => !batches.some(next => (next.dependsOn || []).includes(b.id))).map(b => batchGateKey[b.id]) : ["n-dev"];
	put("n-qa-blackbox", "qa-blackbox", "黑盒 QA 设计/审查", "与开发并行", 2, 1.6); link("n-start-readiness", "n-qa-blackbox");
	put("n-qa-whitebox", "qa-whitebox", "白盒 QA 设计/审查", "与开发并行", 2, 2.4); link("n-start-readiness", "n-qa-whitebox");

	if (split) {
		const sliceCol = 2 + Math.max(batches.length * 2, 1) + 0.6;
		const sliceGateCol = len => sliceCol + Math.max(len * 2 * SLICE_DX, SLICE_DX) + 0.15;
		const sliceGates = [];
		// Owner-plan batches map batch IDs to their owning slice; child-owned
		// ones carry no master status, the child run supplies its own.
		const ownerOf = {};
		for (const b of flow.developmentBatches || []) if (b.owner) ownerOf[b.id] = b.owner;
		let widest = 1;
		// Pass 1: create every slice's nodes and maps so a batch in one slice
		// can depend on a batch the sorted list has not reached yet.
		const sliceInfo = slices.map((s, i) => {
			const row = 0.2 + i * 1.35;
			// A slice that entered the mainline runs under the master
			// configuration, so its nodes say so instead of reporting the child
			// run's own status.
			const entered = s.enteredMainline;
			const childBatches = (s.flow && s.flow.developmentBatches) || [];
			if (childBatches.length > widest) widest = childBatches.length;
			const localKey = {};
			const localGateKey = {};
			childBatches.forEach((b, j) => {
				const key = "sl-dev-" + s.id + "-" + b.id;
				localKey[b.id] = key;
				localGateKey[b.id] = "sl-batch-gate-" + s.id + "-" + b.id;
				const st = { QUEUED: "待执行", ISSUED: "已派发", RUNNING: "执行中", VALIDATING: "验证中", TERMINAL: "已结束" }[b.status] || "";
				put(key, "development", "开发分片 " + s.id + " · 批次 " + b.id,
					(entered ? "已进主线，随主线配置" : st || (s.status || "")), sliceCol + j * 2 * SLICE_DX, row);
				put(localGateKey[b.id], "gate", "分片 " + s.id + " · 批次 " + b.id + " 质量门", "已选普通门", sliceCol + (j * 2 + 1) * SLICE_DX, row, false, false, b.gate && b.gate.status);
				link(key, localGateKey[b.id]);
			});
			const gt = "sl-gate-" + s.id;
			put(gt, "gate", "开发分片 " + s.id + " · 分片门", entered ? "已进主线" : "", sliceGateCol(childBatches.length), row);
			if (!childBatches.length) {
				const dv = "sl-dev-" + s.id;
				put(dv, "development", "开发分片 " + s.id, (entered ? "已进主线，随主线配置" : "运行中 " + (s.status || "")), sliceCol, row);
				localKey[""] = dv;
			}
			sliceGates.push(gt);
			// Lane strip: one header band per slice row, starting 20px left of
			// the first batch column and spanning the batch pitches plus the
			// gate. h = 26 is exactly one 16px text line + 8px padding + 2px
			// border (style.css .group under border-box), and y = 22 + row*Y
			// puts its bottom 2px above the row's node top edge
			// (y = 50 + row*Y), so the strip never covers a node box. The label
			// is whitespace-nowrap, so it has to stay short enough for that
			// single fixed line instead of growing the strip.
			groups.push({ x: 20 + sliceCol * X, y: 22 + row * Y, w: X * Math.max(2, childBatches.length * 2 * SLICE_DX + 1.15), h: 26, label: "开发分片 " + s.id + (entered ? " · 已进主线，随主线配置" : "") });
			return { s, localKey, localGateKey, childBatches, gt };
		});
		// Pass 2: dependency edges can now resolve cross-slice sources.
		for (const info of sliceInfo) {
			if (!info.childBatches.length) { for (const d of devHeads) link(d, info.localKey[""]); link(info.localKey[""], info.gt); continue; }
			for (const b of info.childBatches) {
				let incoming = false;
				for (const d of b.dependsOn || []) {
					if (info.localKey[d]) { link(info.localGateKey[d], info.localKey[b.id]); incoming = true; continue; }
					const src = ownerOf[d] && ownerOf[d] !== info.s.id ? "sl-batch-gate-" + ownerOf[d] + "-" + d : batchGateKey[d];
					if (src && nodes.some(n => n.key === src)) { link(src, info.localKey[b.id]); incoming = true; }
				}
				if (!incoming) for (const d of devHeads) link(d, info.localKey[b.id]);
			}
			for (const b of info.childBatches.filter(b => !info.childBatches.some(next => (next.dependsOn || []).includes(b.id)))) link(info.localGateKey[b.id], info.gt);
		}
		mergeTail(sliceGates, sliceCol + widest * 2 * SLICE_DX + 1.2);
	} else {
		verifyTail(2 + Math.max(batches.length * 2, 1) + 0.8);
		for (const d of devHeads) link(d, "v-gate");
	}
	// QA design/review feeds its verification-execution counterpart.
	if (nodeEnabled(work, "qa-blackbox") && nodes.some(n => n.key === "v-qa-b")) link("n-qa-blackbox", "v-qa-b");
	if (nodeEnabled(work, "qa-whitebox") && nodes.some(n => n.key === "v-qa-w")) link("n-qa-whitebox", "v-qa-w");
	return { nodes, edges, groups, schematic: false };
}

// ---------- view / pan / zoom ----------
// Auto-fit never shrinks below FIT_MIN: a wider graph overflows and pans
// instead of rendering node text illegibly small. The wheel, the zoom control
// and the whole-graph overview reach down to ZMIN; ZMAX caps every explicit
// zoom.
const FIT_MIN = 0.9, ZMIN = 0.4, ZMAX = 1.8;
// VIEW_PAD is the gap the pan boundary keeps between the content and the
// visible canvas edge. contentBox already adds 40px of breathing space around
// the outermost nodes, so the pad is a second, deliberate margin; it must stay
// >= fitContent's own margin m = 30, otherwise applyView would re-clamp a
// freshly fitted view and shift it off the fit position.
const VIEW_PAD = 70;
const view = { x: 0, y: 0, z: 1 };
// viewAdjusted separates a view the user chose from one a panel placed. Pan
// (mouse/touch), arrow keys, wheel, the double-click reset and every zoombar
// button set it; fitContent clears it whenever an automatic fit runs (initial
// load, library open, node pick, configuration switch). closeDrawer reads it to
// decide between refitting and clamping.
let viewAdjusted = false;
let world = null, canvas = null, zoomPct = null;
// Measured node height per node key, written by renderNodes; the || fallbacks
// cover the first frame, before any node has been laid out.
const NODE_H = {};
// Insets of the two overlay panels, measured rather than assumed because both
// animate open and closed. The inspector takes canvas width only on desktop —
// below the 760px breakpoint style.css turns it into a bottom sheet — while the
// drawer always overlays the canvas from the left.
function inspectorInset() { return DOM && window.innerWidth > 760 && $("#insp").classList.contains("open") ? 320 : 0; }
function drawerInset() { return DOM && $("#drawer").classList.contains("open") ? $("#drawer").offsetWidth : 0; }
// contentBox is the graph's bounding box in world coordinates: node extents
// plus 40px on each side, so the outer cards keep breathing space inside the
// framed area.
function contentBox() {
	const g = graph;
	return {
		x0: Math.min(...g.nodes.map(n => n.x)) - 40, x1: Math.max(...g.nodes.map(n => n.x + NODE_W)) + 40,
		y0: Math.min(...g.nodes.map(n => n.y)) - 40, y1: Math.max(...g.nodes.map(n => n.y + (NODE_H[n.key] || 96))) + 40,
	};
}
// rawView writes the view as it stands, without the boundary check. fitAll
// needs that: the overview deliberately frames content wider than the band,
// which applyView would re-centre and thereby undo.
function rawView() {
	world.style.transform = `translate(${view.x}px,${view.y}px) scale(${view.z})`;
	if (zoomPct) zoomPct.textContent = Math.round(view.z * 100) + "%";
}
// applyView keeps the content inside the visible canvas band [L, L+W]: the
// drawer overlays the canvas from the left, so the band origin is its width,
// and the inspector overlays the right edge, so W already subtracts it. With
// the content spanning [cx0*z, cx1*z] canvas px and a translation view.x, the
// two pad constraints are
//   view.x <= L + W - VIEW_PAD - cx1*z   right edge one pad short of L+W
//   view.x >= L + VIEW_PAD - cx0*z       left edge one pad inside L
// i.e. lx = [right-most, left-most] placement. lx[0] > lx[1] means the content
// is narrower than the band, no placement satisfies both, and that axis is
// centred instead; otherwise view.x is clamped into [lx[0], lx[1]] so a wider
// axis can never be panned out of the band. view.y repeats the derivation
// without insets.
function applyView() {
	if (!graph.nodes.length) return;
	const L = drawerInset(), W = canvas.clientWidth - inspectorInset() - L, H = canvas.clientHeight;
	const { x0: cx0, x1: cx1, y0: cy0, y1: cy1 } = contentBox();
	const z = view.z;
	const lx = [L + W - VIEW_PAD - cx1 * z, L + VIEW_PAD - cx0 * z], ly = [H - VIEW_PAD - cy1 * z, VIEW_PAD - cy0 * z];
	view.x = lx[0] > lx[1] ? L + (W - (cx0 + cx1) * z) / 2 : Math.min(lx[1], Math.max(lx[0], view.x));
	view.y = ly[0] > ly[1] ? (H - (cy0 + cy1) * z) / 2 : Math.min(ly[1], Math.max(ly[0], view.y));
	rawView();
}
// fitContent places the graph for an automatic fit. inset is the inspector
// width the caller wants excluded from the band; the drawer inset is measured
// live because the drawer animates.
function fitContent(inset) {
	const L = drawerInset(), W = canvas.clientWidth - inset - L, H = canvas.clientHeight, m = 30, g = graph;
	if (!g.nodes.length) return;
	viewAdjusted = false;
	const { x0: cx0, x1: cx1, y0: cy0, y1: cy1 } = contentBox();
	// Fit never zooms in past 1 nor drops below the readability floor. When
	// the graph still overflows at that floor the clicked node stays in
	// view; otherwise each overflowing axis pins its start edge to the
	// margin so reading order begins at the first column.
	const nz = Math.max(FIT_MIN, Math.min(1, (W - 2 * m) / (cx1 - cx0), (H - 2 * m) / (cy1 - cy0)));
	const sx0 = view.x + cx0 * view.z, sx1 = view.x + cx1 * view.z, sy0 = view.y + cy0 * view.z, sy1 = view.y + cy1 * view.z;
	// Already inside the margins at the wanted scale: leave the view untouched,
	// so re-picking a node or re-opening a panel does not re-animate and jump a
	// view that is still valid.
	if (sx0 >= m && sx1 <= W - m && sy0 >= m && sy1 <= H - m && Math.abs(nz - view.z) < 0.01) return;
	view.z = nz;
	const clicked = selKey && g.nodes.find(n => n.key === selKey);
	if (clicked && ((cx1 - cx0) * nz > W - 2 * m || (cy1 - cy0) * nz > H - 2 * m)) {
		// Overflowing even at the floor: centre on the clicked instance rather
		// than an edge, so the box a repeated responsibility was picked from
		// stays on screen.
		view.x = L + W / 2 - (clicked.x + NODE_W / 2) * nz;
		view.y = H / 2 - (clicked.y + (NODE_H[clicked.key] || 96) / 2) * nz;
	} else {
		view.x = (cx1 - cx0) * nz > W - 2 * m ? L + m - cx0 * nz : L + (W - (cx0 + cx1) * nz) / 2;
		view.y = (cy1 - cy0) * nz > H - 2 * m ? m - cy0 * nz : (H - (cy0 + cy1) * nz) / 2;
	}
	world.classList.add("anim"); applyView(); setTimeout(() => world.classList.remove("anim"), 300);
}
// Zoom keeps the point at the centre of the visible canvas band fixed. For a
// canvas point p the world point under it is (p - view.x) / view.z, so holding
// p still across the scale change gives view.x' = p - (p - view.x) * z' / z.
// p is the band centre L + W/2 horizontally — not the canvas centre once a
// side panel is open — and H/2 vertically. z is clamped first so a press at the
// limit is a no-op instead of a rounding drift.
function zoomTo(z) {
	const L = drawerInset(), W = canvas.clientWidth - inspectorInset() - L, H = canvas.clientHeight;
	const mx = L + W / 2, my = H / 2;
	z = Math.min(ZMAX, Math.max(ZMIN, z));
	view.x = mx - (mx - view.x) * z / view.z;
	view.y = my - (my - view.y) * z / view.z;
	view.z = z;
	applyView();
}
// The overview frames the whole graph and may go below FIT_MIN; the ordinary
// fit (fitContent) keeps its readability floor. 80px is the total slack (40 per
// side), the scale is clamped to [ZMIN, 1], and the result goes through
// rawView: showing content wider than the band is the point, so applyView's pad
// boundary must not pull it back. The zoombar marks this as a user view, so
// closing the drawer later only clamps it instead of refitting.
function fitAll() {
	const g = graph;
	if (!g.nodes.length) return;
	const L = drawerInset(), W = canvas.clientWidth - inspectorInset() - L, H = canvas.clientHeight;
	const { x0, x1, y0, y1 } = contentBox();
	const z = Math.max(ZMIN, Math.min(1, (W - 80) / (x1 - x0), (H - 80) / (y1 - y0)));
	view.z = z;
	view.x = L + (W - (x0 + x1) * z) / 2;
	view.y = (H - (y0 + y1) * z) / 2;
	world.classList.add("anim"); rawView(); setTimeout(() => world.classList.remove("anim"), 300);
}
// − / percentage / ＋ / 适应, laid over the canvas; style.css shifts it left
// while the inspector is open. Every button marks the view as user-chosen. −
// and ＋ step the scale by 1/1.15 and 1.15 around the band centre (zoomTo), the
// percentage returns to 1, and 适应 switches to the whole-graph overview, which
// has no single anchor point.
function buildZoomBar() {
	const bar = el("div", "zoombar");
	const button = (act, cls, text, title) => {
		const b = el("button", cls, text);
		b.type = "button"; b.dataset.z = act; b.title = title;
		b.onclick = () => { viewAdjusted = true; act === "fit" ? fitAll() : zoomTo(act === "pct" ? 1 : act === "in" ? view.z * 1.15 : view.z / 1.15); };
		return b;
	};
	bar.appendChild(button("out", "", "−", "缩小"));
	zoomPct = button("pct", "pct", "100%", "回到 100%");
	bar.appendChild(zoomPct);
	bar.appendChild(button("in", "", "＋", "放大"));
	bar.appendChild(button("fit", "", "适应", "全览整张流程图"));
	canvas.appendChild(bar);
	zoomPct.textContent = Math.round(view.z * 100) + "%";
}
let panning = false, px = 0, py = 0, moved = 0;
if (DOM) {
world = $("#world"); canvas = $("#canvas");
buildZoomBar();
// Overlay chrome (node cards, legend, zoom control) handles its own pointer
// events, so a pan, a zoom or the blank-click clear starts only on the canvas
// background.
const overChrome = t => !!(t.closest && t.closest(".node,.legend,.zoombar"));
// Panning accumulates pointer deltas into the view and ends every move in
// applyView, so the boundary applies continuously: a large drag stops at it
// instead of banking invisible translation. moved distinguishes a drag from a
// click, which clears the selection.
canvas.addEventListener("mousedown", e => { if (overChrome(e.target)) return; panning = true; moved = 0; px = e.clientX; py = e.clientY; canvas.classList.add("panning"); });
window.addEventListener("mousemove", e => { if (!panning) return; view.x += e.clientX - px; view.y += e.clientY - py; moved += Math.abs(e.clientX - px) + Math.abs(e.clientY - py); px = e.clientX; py = e.clientY; viewAdjusted = true; applyView(); });
window.addEventListener("mouseup", () => { panning = false; canvas.classList.remove("panning"); });
canvas.addEventListener("touchstart", e => { if (overChrome(e.target)) return; const t = e.touches[0]; panning = true; moved = 0; px = t.clientX; py = t.clientY; }, { passive: true });
canvas.addEventListener("touchmove", e => { if (!panning) return; const t = e.touches[0]; view.x += t.clientX - px; view.y += t.clientY - py; moved += Math.abs(t.clientX - px) + Math.abs(t.clientY - py); px = t.clientX; py = t.clientY; viewAdjusted = true; applyView(); }, { passive: true });
canvas.addEventListener("touchend", () => { panning = false; });
canvas.addEventListener("click", e => { if (moved > 4) return; if (overChrome(e.target)) return; if (!sel) return; sel = null; selKey = null; render(); });
canvas.addEventListener("dblclick", e => { if (overChrome(e.target)) return; view.x = view.y = 0; view.z = 1; viewAdjusted = true; world.classList.add("anim"); applyView(); setTimeout(() => world.classList.remove("anim"), 300); });
// Wheel zoom keeps the pointer as its fixed point: mx/my are the cursor in
// canvas coordinates and the same p - (p - x) * z'/z transform leaves the world
// point under the cursor standing still while the scale steps by 1.12.
canvas.addEventListener("wheel", e => { if (overChrome(e.target)) return; e.preventDefault(); const r = canvas.getBoundingClientRect(), mx = e.clientX - r.left, my = e.clientY - r.top; const nz = Math.min(ZMAX, Math.max(ZMIN, view.z * (e.deltaY < 0 ? 1.12 : 1 / 1.12))); view.x = mx - (mx - view.x) * nz / view.z; view.y = my - (my - view.y) * nz / view.z; view.z = nz; viewAdjusted = true; applyView(); }, { passive: false });
document.addEventListener("keydown", e => {
	if (e.key === "Escape") {
		if ($("#drawer").classList.contains("open")) { closeDrawer(); return; }
		if ($("#banner").classList.contains("show")) { $("#banner").classList.remove("show"); return; }
		if (sel) { sel = null; selKey = null; render(); } return;
	}
	if (e.target.closest("input,select,textarea")) return;
	// Arrow keys pan by 40px steps. The delta is expressed as content movement,
	// so left and up (which reveal content from those sides) carry positive
	// deltas.
	const k = { ArrowLeft: [40, 0], ArrowRight: [-40, 0], ArrowUp: [0, 40], ArrowDown: [0, -40] }[e.key];
	if (!k) return; e.preventDefault(); view.x += k[0]; view.y += k[1]; viewAdjusted = true; applyView();
});
}

// ---------- node rendering ----------
let graph = { nodes: [], edges: [], groups: [] };
function nodeStatusClass(resp, gateStatus) {
	if (gateStatus !== undefined) return gateStatus === "RUNNING" ? " running" : gateStatus === "PASS" || gateStatus === "NOT_APPLICABLE" ? " done" : gateStatus === "FAIL" ? " failed" : "";
	const st = nodeState(resp);
	if (!st) return "";
	if (st.running) return " running";
	if (st.completed || st.bypassed) return " done";
	return "";
}
function nodeStatusText(resp, gateStatus) {
	if (gateStatus !== undefined) return { DISABLED: "未启用", NOT_APPLICABLE: "不适用", QUEUED: "待验证", RUNNING: "运行中", PASS: "已通过", FAIL: "未通过" }[gateStatus] || "";
	const st = nodeState(resp);
	if (!st) return null;
	if (st.running) return "运行中";
	if (st.closing) return "关闭中";
	if (st.completed) return "已完成";
	if (st.bypassed) return "已越过";
	return "";
}
// The graph DOM rebuilds only when its rendered content changes, so the flow
// animation, hover and focus survive the 2s poll untouched. The signature must
// therefore name every input renderNodes() reads: node identity, label,
// subtitle, op/terminal flags and position, the lane groups, the edge list, the
// selection, and per configurable node its status class, status text, enabled
// flag and execution choice. An input missing here renders once and then sticks
// with stale content until some other field happens to change.
let nodesSig = "";
function nodesSignature() {
	return JSON.stringify([
		graph.nodes.map(n => [n.key, n.resp, n.label, n.sub, n.op, n.terminal, n.gateStatus, n.x, n.y]),
		graph.groups, graph.edges, sel, selKey,
		graph.nodes.map(n => n.op ? null : [nodeStatusClass(n.resp, n.gateStatus), nodeStatusText(n.resp, n.gateStatus), nodeEnabled(work, n.resp), execOf(work, n.resp)]),
	]);
}
// renderNodes rebuilds the graph model on every render and compares only the
// rendered-content signature; prev is the per-responsibility snapshot taken by
// the last render (lastSigs), used to flash a node whose enabled flag or
// execution choice changed.
function renderNodes(prev) {
	graph = buildGraph();
	const sig = nodesSignature();
	if (sig === nodesSig) return;
	nodesSig = sig;
	world.querySelectorAll(".node,.group").forEach(n => n.remove());
	for (const grp of graph.groups || []) {
		const g = el("div", "group", grp.label); Object.assign(g.style, { left: grp.x + "px", top: grp.y + "px", width: grp.w + "px", height: grp.h + "px" });
		world.appendChild(g);
	}
	for (const n of graph.nodes) {
		// Class order mirrors style.css .node rules. op/terminal come from the
		// graph node itself; a configurable node adds its runtime status
		// (running/done) and the disabled mirror of nodeEnabled. sel is keyed by
		// responsibility, so every repeated instance of the selected responsibility
		// highlights. An op node never takes disabled: it has no configuration to
		// switch off, and its resp is empty.
		const e = el("div", "node" + (n.op ? " op" : "") + (n.terminal ? " terminal" : "") + (n.op ? "" : nodeStatusClass(n.resp, n.gateStatus))
			+ (n.resp && !nodeEnabled(work, n.resp) ? " disabled" : "") + (sel === n.resp && RESP.find(r => r.id === n.resp) ? " sel" : ""));
		e.style.left = n.x + "px"; e.style.top = n.y + "px"; e.dataset.key = n.key; if (n.resp) e.dataset.resp = n.resp; e.tabIndex = n.op ? -1 : 0;
		const meta = RESP.find(r => r.id === n.resp);
		if (meta && meta.warning) e.title = meta.warning;
		// Title and status share one header row (.head > .t + .st): a long label
		// wraps inside its own column instead of running under the status pill.
		// Configurable nodes show selection unless a runtime status applies.
		const status = n.op ? "" : nodeStatusText(n.resp, n.gateStatus) || (nodeEnabled(work, n.resp) ? "已选择" : "未选择");
		const head = el("div", "head");
		head.appendChild(el("div", "t", n.label));
		if (status) head.appendChild(el("div", "st", status));
		e.appendChild(head);
		const ex = n.resp ? execOf(work, n.resp) : {};
		if (n.sub) e.appendChild(el("div", "d", n.sub));
		if (n.key.startsWith("sl-dev-") || n.key.startsWith("sl-gate-") || n.key.startsWith("sl-batch-gate-") || n.key.startsWith("n-gate-") || n.key.startsWith("n-dev-") && graph.nodes.filter(x => x.resp === "development").length > 1) {
			e.appendChild(el("div", "shared", "共享「" + (LABEL[n.resp] || "") + "」配置"));
		}
		if (n.key.startsWith("m-qa")) e.appendChild(el("div", "shared", "随「" + LABEL[n.resp] + "」配置"));
		if (n.resp && nodeEnabled(work, n.resp)) {
			const chips = el("div", "exec");
			if (ex.provider) chips.appendChild(el("span", "chip p", ex.provider));
			if (ex.model) chips.appendChild(el("span", "chip", ex.model));
			if (ex.reasoningEffort) chips.appendChild(el("span", "chip", ex.reasoningEffort));
			if (chips.childNodes.length) e.appendChild(chips);
		}
		if (!n.op) {
			// render() opens the inspector for the new selection, so the fit that
			// follows measures the inset already in effect; swap replays the
			// slide-in when the panel was open before the click.
			const pick = ev => { ev.stopPropagation(); const was = $("#insp").classList.contains("open"); sel = n.resp; selKey = n.key; render(); fitContent(inspectorInset()); if (was) { $("#insp").classList.remove("swap"); void $("#insp").offsetWidth; $("#insp").classList.add("swap"); } };
			e.onclick = pick;
			e.onkeydown = ev => { if (ev.key === "Enter" || ev.key === " ") pick(ev); };
		}
		if (n.resp && prev && prev[n.resp] && JSON.stringify(prev[n.resp]) !== JSON.stringify(nodeSig(n.resp))) e.classList.add("flash");
		world.appendChild(e);
		NODE_H[n.key] = e.offsetHeight;
	}
	drawEdges();
}
function nodeSig(resp) { return { on: nodeEnabled(work, resp), ex: execOf(work, resp) }; }
// anchor returns the attach point of one node box side in world coordinates:
// r/l sit at mid-height, t/b at mid-width. The height is the measured NODE_H
// from the last render, with a placeholder before the first layout.
function anchor(key, side) {
	const n = graph.nodes.find(n => n.key === key);
	const h = NODE_H[key] || 90;
	return side === "r" ? [n.x + NODE_W, n.y + h / 2] : side === "l" ? [n.x, n.y + h / 2] : side === "b" ? [n.x + NODE_W / 2, n.y + h] : [n.x + NODE_W / 2, n.y];
}
// Every connection is an animated dashed line layered by meaning: ordinary
// flow, dimmed closed/bypass links, the amber repair cycle (.loop, drawn by
// drawRepairLoops), the teal seal link, and links touching a running (green,
// fast) or selected (blue) node.
// The base layer is decided from the edge itself: seal-edge when it enters the
// seal node, off when either endpoint is a switched-off responsibility, flow
// otherwise. The hot layer is read back from the rendered node DOM (.running /
// .sel classes joined through data-key), so the links always agree with the
// cards; hot() emits at most one of the two and tests running first, so a link
// touching both kinds of node keeps the running highlight.
function drawEdges() {
	const svg = $("#edges"); svg.querySelectorAll("path,.loop-label").forEach(p => p.remove());
	const add = (d, cls, marker) => {
		const p = document.createElementNS(SVG_NS, "path");
		p.setAttribute("d", d); p.setAttribute("class", "edge " + cls);
		p.setAttribute("marker-end", marker);
		svg.appendChild(p);
		return p;
	};
	const running = new Set([...world.querySelectorAll(".node.running")].map(n => n.dataset.key));
	const selected = new Set([...world.querySelectorAll(".node.sel")].map(n => n.dataset.key));
	const hot = (a, b) => running.has(a) || running.has(b) ? " hot-run" : selected.has(a) || selected.has(b) ? " hot-sel" : "";
	const loops = [];
	for (const e of graph.edges) {
		const na = graph.nodes.find(n => n.key === e.a), nb = graph.nodes.find(n => n.key === e.b);
		if (!na || !nb) continue;
		if (e.cls === "loop") { loops.push(e); continue; }
		const [x1, y1] = anchor(e.a, "r"), [x2, y2] = anchor(e.b, "l");
		const mx = (x1 + x2) / 2;
		const off = (na.resp && !nodeEnabled(work, na.resp)) || (nb.resp && !nodeEnabled(work, nb.resp));
		const seal = e.b === "v-seal";
		add(`M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`,
			(seal ? "seal-edge" : off ? "off" : "flow") + hot(e.a, e.b), seal ? "url(#arrSeal)" : "url(#arr)");
	}
	for (const n of graph.nodes) {
		if (!n.resp || nodeEnabled(work, n.resp)) continue;
		// A switched-off responsibility is stepped over, not disconnected: one
		// dimmed bypass arc joins each of its predecessors to each of its
		// successors, arcing above the card (control points at n.y - 70) so the
		// chain still reads left to right.
		const ins = graph.edges.filter(e => e.b === n.key).map(e => e.a);
		const outs = graph.edges.filter(e => e.a === n.key).map(e => e.b);
		for (const a of ins) for (const b of outs) {
			const [x1, y1] = anchor(a, "r"), [x2, y2] = anchor(b, "l");
			add(`M${x1},${y1} C${x1 + 70},${n.y - 70} ${x2 - 70},${n.y - 70} ${x2},${y2}`, "bypass" + hot(a, b), "url(#arrB)");
		}
	}
	drawRepairLoops(svg, loops, add);
}
// A repair run reruns verification. Drawing that as one return manifold under
// the verification column keeps the loop out of the node boxes: the line
// leaves the repair node's bottom, runs below every node, then enters each
// verification node's bottom with an upward arrow.
//
// Geometry in world px, per verification head (heads sorted top to bottom):
//   sy      repair's bottom edge: the single shared departure line
//   bottom  the lowest bottom edge of repair and heads; every horizontal run
//           stays below it, so no run can cross a card
//   trackY  bottom + 56 + (n-1-i) * 18: one track per head, the topmost head
//           taking the deepest one, so the horizontal runs nest instead of
//           coinciding on one line
//   sx      repair.x + min(150, 54 + i * 36): staggered departures along the
//           repair bottom keep the vertical drops apart
//   railX   head.x + NODE_W + 26 + i * 16: the vertical rail just right of the
//           head card, one 16px further per index so the rails nest as well;
//           with the three heads this graph has, the 26 + 2*16 worst case stays
//           left of the repair column, which is 1.45 * X = 362.5px away
//   cr = 10 corner radius: each segment stops cr short of a corner and a
//           quadratic with the corner as control point rounds it, so the
//           manifold reads as one line instead of overlapping right angles
//   drop = 16 the final approach: the line passes 16px under the head, turns
//           up and enters the head's bottom edge with its arrow pointing into it
//
// These loops cannot use the ordinary r→l anchors: a return edge from repair,
// which sits right of the verification column, would cross the card it targets
// and the column between them, whereas the manifold stays outside every box —
// the canvas probe samples the whole path length to hold that.
function drawRepairLoops(svg, loops, add) {
	const repair = graph.nodes.find(n => n.key === "v-repair");
	if (!repair || !loops.length) return;
	const heads = loops.map(e => graph.nodes.find(n => n.key === e.b)).filter(Boolean).sort((a, b) => a.y - b.y);
	if (!heads.length) return;
	const h = n => NODE_H[n.key] || 96;
	const sy = repair.y + h(repair);
	const bottom = Math.max(sy, ...heads.map(n => n.y + h(n)));
	const cr = 10, drop = 16;
	heads.forEach((head, i) => {
		const sx = repair.x + Math.min(150, 54 + i * 36);
		const railX = head.x + NODE_W + 26 + i * 16;
		const hx = head.x + NODE_W / 2, hb = head.y + h(head);
		const trackY = bottom + 56 + (heads.length - 1 - i) * 18;
		// One path per head: down from the repair bottom, round the corner onto
		// its own track toward the verification column, up the rail, left to the
		// head's centre line and straight up into its underside.
		const d = [
			`M ${sx} ${sy}`,
			`V ${trackY - cr}`,
			`Q ${sx} ${trackY} ${sx - cr} ${trackY}`,
			`H ${railX + cr}`,
			`Q ${railX} ${trackY} ${railX} ${trackY - cr}`,
			`V ${hb + drop + cr}`,
			`Q ${railX} ${hb + drop} ${railX - cr} ${hb + drop}`,
			`H ${hx + cr}`,
			`Q ${hx} ${hb + drop} ${hx} ${hb + drop - cr}`,
			`V ${hb}`,
		].join(" ");
		const p = add(d, "loop", "url(#arrLoop)");
		const title = document.createElementNS(SVG_NS, "title");
		title.textContent = "返修后重新验证：" + head.label;
		p.appendChild(title);
	});
	const middle = heads[Math.floor(heads.length / 2)];
	// The caption sits under the middle head, 20px below the deepest track, so it
	// touches neither a rail nor a node box.
	const label = document.createElementNS(SVG_NS, "text");
	label.setAttribute("class", "loop-label");
	label.setAttribute("text-anchor", "middle");
	label.setAttribute("x", String(middle.x + NODE_W / 2));
	label.setAttribute("y", String(bottom + 56 + (heads.length - 1) * 18 + 20));
	label.textContent = "返修后重跑验证";
	svg.appendChild(label);
}

// ---------- inspector ----------
// The inspector rebuilds only when its inputs' governing signature changes,
// so an in-progress edit keeps its focus and caret across polls.
function inspectorSignature() {
	const st = nodeState(sel);
	const run = selectedRun();
	return JSON.stringify({
		sel, editing, dirty,
		enabled: sel ? nodeEnabled(work, sel) : null,
		locked: !!st && !st.editable,
		st: st ? [st.completed, st.running, st.closing, st.bypassed, st.enabled] : null,
		bound: !!(run && editing === run.configuration),
		round: sel ? roundOf(work, sel) : null,
		exec: sel ? execOf(work, sel) : null,
		gates: sel === "gate" ? (gatesAll(work) ? "all" : work.gates) : null,
		optKey: optCache[sigOptKey()] ? (optCache[sigOptKey()].error ? "err" : "ok") : "none",
	});
}
// The built-in configuration supplies run defaults without node overrides.
// Native catalogues are shared across nodes and configurations; overlay
// revisions keep a changed native configuration from reusing an old answer.
function optPrefix() {
	return JSON.stringify([(state && state.revisions) || {}, execOf(work, sel).provider || ""]).slice(0, -1) + ",";
}
function sigOptKey() {
	return optPrefix() + JSON.stringify(execOf(work, sel).model || "") + "]";
}
function effectiveChoice(options, ex) {
	let value = { ...((options && options.effective) || {}) };
	if (ex.provider && ex.provider !== value.provider) value = { provider: ex.provider };
	if (ex.model) value.model = ex.model;
	if (ex.reasoningEffort) value.reasoningEffort = ex.reasoningEffort;
	return value;
}
// Poll-driven rebuilds keep a focused draft control untouched; a committed
// control change calls with force so dependent fields refresh immediately.
async function renderInspector(force) {
	const i = $("#insp");
	if (!sel || !RESP.find(n => n.id === sel)) { i.classList.remove("open"); inspSig = ""; return; }
	const sig = inspectorSignature();
	const focused = i.contains(document.activeElement) && document.activeElement.matches("input,select,textarea");
	if (i.classList.contains("open") && sig === inspSig && !force) return;
	if (focused && !force) {
		// Keep the in-progress edit and caret; still reflect the newest lock
		// status on the controls already rendered.
		const fst = nodeState(sel);
		const flocked = !!fst && !fst.editable;
		const gatesUnavailable = !i.querySelector('input[data-f^="gate:"]');
		i.querySelectorAll("input,select,textarea,button").forEach(c => {
			if (!c.classList.contains("iconbtn")) c.disabled = flocked || c.dataset.f === "gates:all" && gatesUnavailable;
		});
		return;
	}
	// The focused field's identity is captured so the rebuilt form can put
	// focus back on the matching control.
	const focusKey = i.contains(document.activeElement) && document.activeElement.dataset ? (document.activeElement.dataset.f || null) : null;
	inspSig = sig;
	const gen = ++inspGen;
	i.classList.add("open");
	const meta = RESP.find(n => n.id === sel);
	const st = nodeState(sel);
	const locked = !!st && !st.editable;
	const run = selectedRun();
	const bound = !!(run && editing === run.configuration);
	i.classList.toggle("locked", locked);
	i.textContent = "";
	const head = el("div", ""); head.style.cssText = "display:flex;align-items:flex-start;justify-content:space-between";
	const hd = el("div"); hd.appendChild(el("h3", "", meta.t));
	if (meta.d) hd.appendChild(el("div", "sub", meta.d));
	const x = el("button", "iconbtn", "✕"); x.style.padding = "4px 9px"; x.title = "收起面板（Esc）";
	x.onclick = () => { sel = null; selKey = null; render(); };
	head.appendChild(hd); head.appendChild(x); i.appendChild(head);
	if (st && (st.completed || st.bypassed)) {
		const lm = el("div", "lockmsg"); lm.innerHTML = "<b>这个节点已经结束，不能在面板里修改。</b><br>如需回退，请在对话里向代理提出（reset 或需求变更需确认）。";
		i.appendChild(lm);
	} else if (st && st.running) {
		const lm = el("div", "lockmsg"); lm.innerHTML = "这个节点正在执行。<b>当前调用按原配置跑完</b>；保存后的修改从下一次派发开始生效。";
		i.appendChild(lm);
	} else if (st && st.closing) {
		i.appendChild(el("div", "lockmsg", "这个节点正在关闭：已领取的调用按原配置跑完后才真正关闭。"));
	}
	if (!bound && run) {
		i.appendChild(el("div", "lockmsg", `正在编辑「${editing}」，与选中 run 绑定的「${run.configuration}」不同；保存不影响该 run。`));
	}
	if (meta.warning) i.appendChild(el("div", "lockmsg warning", meta.warning));
	const sw = el("div", "switch"); sw.appendChild(el("span", "", "启用这个节点"));
	const tg = el("button", "tg" + (nodeEnabled(work, sel) ? " on" : ""));
	tg.disabled = locked; tg.dataset.f = "enabled";
	tg.onclick = () => { const prev = snapshot(); setNodeEnabled(work, sel, !nodeEnabled(work, sel)); change(prev); renderInspector(true); };
	sw.appendChild(tg); i.appendChild(sw);
	const selAtStart = sel, editingAtStart = editing;
	await renderExecFields(i, meta, locked, editingAtStart, gen);
	if (gen !== inspGen || sel !== selAtStart || editing !== editingAtStart) return; // a newer rebuild owns the form now
	if (meta.round) {
		const f = el("div", "field");
		f.appendChild(el("label", "", `自动轮数上限（${meta.rh}，1–64）`));
		const inp = document.createElement("input"); inp.type = "number"; inp.min = "1"; inp.max = "64"; inp.value = roundOf(work, sel); inp.disabled = locked;
		inp.dataset.f = "round";
		inp.onchange = () => { const prev = snapshot(); const v = Math.max(1, Math.min(64, parseInt(inp.value) || 3)); inp.value = v; setRound(work, sel, v); change(prev); renderInspector(true); };
		f.appendChild(inp); i.appendChild(f);
	}
	if (sel === "gate") {
		await renderGateField(i, locked, editingAtStart, gen);
		if (gen !== inspGen || sel !== "gate" || editing !== editingAtStart) return;
	}
	i.appendChild(el("div", "hint", locked ? "此节点由当前 run 锁定。" : "修改先存为草稿；保存后在下一次派发评估生效。"));
	if (focusKey) {
		const again = [...i.querySelectorAll("[data-f]")].find(c => c.dataset.f === focusKey);
		if (again && !again.disabled) again.focus();
	}
}

// Late replies remain in their own revision/provider/model cache entry;
// inspector generations decide whether the reply still owns the rendered form.
const optCache = {};
const optInflight = {};
async function fetchOptions(provider, model) {
	const q = new URLSearchParams({ configuration: BUILTIN, node: "development", platform: provider || "", model: model || "", effort: "" });
	return api.get("/api/execution-options?" + q);
}
// Model lists are provider-wide; only the requested model's effort list is
// guaranteed to be present in a native catalogue answer.
function providerOptions() {
	const prefix = optPrefix();
	for (const key in optCache) {
		const value = optCache[key];
		if (key.startsWith(prefix) && value && (value.catalogue || value.unavailable)) return value;
	}
	return undefined;
}
async function optionsFor(force) {
	const key = sigOptKey();
	if (force) {
		const revisionPrefix = JSON.stringify([(state && state.revisions) || {}]).slice(0, -1) + ",";
		for (const cache of [optCache, optInflight]) {
			for (const cachedKey in cache) if (cachedKey.startsWith(revisionPrefix)) delete cache[cachedKey];
		}
	}
	if (optCache[key] !== undefined) return optCache[key];
	if (optInflight[key]) return optInflight[key];
	const seq = ++optSeq;
	const editingName = editing, node = sel;
	const ex = execOf(work, sel);
	const pending = (async () => {
		let result;
		try { result = await fetchOptions(ex.provider || "", ex.model || ""); }
		catch (e) { result = { error: e.message }; }
		if (optInflight[key] === pending) {
			optCache[key] = result;
			delete optInflight[key];
		}
		return result;
	})();
	optInflight[key] = pending;
	const result = await pending;
	if (seq !== optSeq || sel !== node || editing !== editingName || sigOptKey() !== key) return optCache[sigOptKey()];
	return result;
}
// The empty effort value follows defaults without persisting a native value.
function fillEfforts(efforts, options, ex, locked, pending) {
	const cat = (options && options.catalogue) || { models: [] };
	const nativeDefault = cat["default"] || {};
	const eff = effectiveChoice(options, { ...ex, reasoningEffort: "" });
	const effModel = ex.model || eff.model || nativeDefault.model || "";
	const modelEntry = (cat.models || []).find(m => m.value === effModel);
	const effList = (modelEntry && modelEntry.efforts) || [];
	const effDefault = eff.reasoningEffort || nativeDefault.effort || (modelEntry && modelEntry.defaultEffort) || "";
	const choices = [["", "跟随默认" + (effDefault ? "（" + effDefault + "）" : "")], ...effList.map(e => [e, e])];
	for (const [value, label] of choices) {
		const b = el("button", value === (ex.reasoningEffort || "") ? "on" : "", label);
		b.dataset.e = value; b.dataset.f = "effort"; b.type = "button"; b.disabled = locked;
		b.onclick = () => {
			const prev = snapshot(); setExec(work, sel, { reasoningEffort: value }); change(prev, ["dirty", "exec", "optKey"]);
			efforts.querySelectorAll("button[data-e]").forEach(button => button.classList.toggle("on", button.dataset.e === value));
		};
		efforts.appendChild(b);
	}
	if (pending) efforts.appendChild(el("span", "hint", "正在获取强度档…"));
	else if (!effList.length) efforts.appendChild(el("span", "hint", "该平台未报告强度档"));
}
// platformOption renders one platform entry; a platform whose executable the
// backend could not resolve on PATH is marked 未安装 and cannot be picked.
function platformOption(options, p, selected) {
	const uninstalled = options.installed && options.installed[p] === false;
	const o = new Option(uninstalled ? p + "（未安装）" : p, p, false, selected);
	if (uninstalled) { o.disabled = true; o.title = "可执行文件不在 PATH（未安装或未加环境变量）"; }
	return o;
}
// Backend option errors carry internal framing: a RuntimeBlocked wrapper and
// a leading UPPER_SNAKE code. The display reason translates the code by its
// suffix class so no platform is special-cased; unknown shapes fall back.
function optionsErrorText(error) {
	const s = String(error || "").replace(/^RuntimeBlocked:\s*/, "");
	const code = ((/^([A-Z][A-Z0-9_]+):/).exec(s) || [])[1] || "";
	if (code === "UNREGISTERED_INSTALL") return "运行时未登记安装";
	if (code === "UNSUPPORTED_NATIVE_HOST") return "该平台不支持原生执行通道";
	if (code.endsWith("_UNAVAILABLE")) return "平台可执行文件或脚本不可用";
	if (code.endsWith("_CAPABILITY")) return "平台能力不满足适配要求";
	if (code.endsWith("_PROTOCOL")) return "平台协议不匹配";
	if (code.endsWith("_BINDING")) return "会话绑定校验失败";
	if (code.endsWith("_CONFIG")) return "平台配置未生效";
	return "平台选项查询失败";
}
// The raw error text stays on the block's title for diagnosis; the body
// shows only the translated reason plus the manual-entry hint.
function optionsErrorEl(error) {
	const lm = el("div", "lockmsg");
	lm.title = error;
	lm.appendChild(el("b", "", "暂取不到平台选项：" + optionsErrorText(error)));
	lm.appendChild(el("div", "", "可手工填写平台/模型/强度（原生值）。"));
	return lm;
}
async function renderExecFields(i, meta, locked, editingName, gen, update) {
	if (gen !== inspGen || sel !== meta.id || editing !== editingName) return;
	const key = sigOptKey();
	const controls = el("div", "switch");
	controls.appendChild(el("span", "", "执行选项"));
	const refresh = (update && i.querySelector('[data-f="refresh-options"]')) || el("button", "iconbtn", "刷新平台/模型");
	refresh.type = "button"; refresh.dataset.f = "refresh-options";
	const syncRefresh = () => {
		refresh.disabled = !!optInflight[sigOptKey()];
		refresh.textContent = refresh.disabled ? "正在获取…" : "刷新平台/模型";
	};
	syncRefresh();
	refresh.onclick = async () => {
		if (refresh.disabled) return;
		const focused = i.contains(document.activeElement) && document.activeElement.dataset.f;
		const refreshKey = sigOptKey();
		const request = optionsFor(true);
		syncRefresh();
		await request;
		if (gen !== inspGen || sel !== meta.id || editing !== editingName || sigOptKey() !== refreshKey) return;
		await renderInspector(true);
		const again = focused && [...i.querySelectorAll("[data-f]")].find(c => c.dataset.f === focused);
		if (again && !again.disabled) again.focus();
	};
	if (!update) { controls.appendChild(refresh); i.appendChild(controls); }
	const wrap = el("div");
	const target = update ? update.wrap : wrap;
	if (!update) i.appendChild(wrap);
	const current = () => gen === inspGen && sel === meta.id && editing === editingName && sigOptKey() === key && target.isConnected;
	const currentLocked = () => { const st = nodeState(meta.id); return !!st && !st.editable; };
	const attach = () => {
		if (!current()) return;
		locked = currentLocked();
		if (update) {
			// Upstream fields stay mounted while only their dependencies move.
			const previousDefault = target.querySelector('[data-exec="provider"] option[value=""]');
			const nextDefault = wrap.querySelector('[data-exec="provider"] option[value=""]');
			if (previousDefault && nextDefault && options && !options.unavailable && !options.error) previousDefault.textContent = nextDefault.textContent;
			const keep = element => element.dataset.exec === "provider" || update.from === "effort" && element.dataset.exec === "model";
			[...target.children].filter(element => !keep(element)).forEach(element => element.remove());
			[...wrap.children].filter(element => !keep(element)).forEach(element => target.appendChild(element));
		}
		target.querySelectorAll("input,select,button").forEach(c => { c.disabled = locked; });
		syncInspectorSignature();
	};
	const commit = patch => {
		const from = patch.provider !== undefined ? "model" : patch.model !== undefined ? "effort" : "";
		if (from === "model") { patch.model = ""; patch.reasoningEffort = ""; }
		else if (from === "effort") patch.reasoningEffort = "";
		const prev = snapshot(); setExec(work, sel, patch); change(prev, ["dirty", "exec", "optKey"]);
		if (from) renderExecFields(i, meta, currentLocked(), editingName, gen, { wrap: target, from });
	};
	// The catalogue query can take seconds; a cached answer renders at once.
	// The placeholder only marks a provider/model cache miss without a
	// same-provider answer, while a sibling model's
	// shared catalogue renders the form and only the effort row waits for the
	// current model's own answer.
	let options = optCache[key];
	let pending = false;
	if (options === undefined) {
		options = providerOptions();
		pending = options !== undefined;
	}
	if (options === undefined) {
		wrap.appendChild(el("div", "lockmsg load", "正在获取平台/模型选项…"));
		attach();
		const request = optionsFor(); syncRefresh();
		options = await request;
		syncRefresh();
		if (!current()) return;
		locked = currentLocked();
		if (!options) return; // a stale fetch lost to a newer one; the placeholder stays
	} else if (pending) {
		const request = optionsFor(); syncRefresh();
		request.then(landed => {
			if (!current()) return;
			syncRefresh();
			const live = target.querySelector(".efforts");
			if (!live) return;
			live.textContent = "";
			// The current model's tuple answers the row; anything else (error,
			// unavailable) replaces the sibling-rendered form with its own view.
			if (landed && landed.catalogue) { fillEfforts(live, landed, execOf(work, sel), currentLocked(), false); syncInspectorSignature(); return; }
			renderExecFields(i, meta, currentLocked(), editingName, gen, { wrap: target, from: update ? update.from : "model" });
		});
	}
	wrap.textContent = "";
	if (options.unavailable) {
		const ex = execOf(work, sel);
		const pname = options.platform || ex.provider || (options.effective || {}).provider || "?";
		const lm = el("div", "lockmsg");
		if (options.host) {
			lm.appendChild(el("b", "", `宿主（主代理）「${pname}」`));
			lm.appendChild(el("div", "", "宿主负责编排与派发，本身不执行节点；请为节点选择一个执行平台。"));
		} else {
			lm.appendChild(el("b", "", `平台「${pname}」不可用`));
			lm.appendChild(el("div", "", "该平台不提供执行选择，不能作为执行平台；改选其他平台后保存生效。"));
		}
		wrap.appendChild(lm);
		const pf = el("div", "field"); pf.dataset.exec = "provider"; pf.appendChild(el("label", "", "平台"));
		const selP = document.createElement("select"); selP.disabled = locked; selP.dataset.f = "provider";
		// The resolved value is not an execution platform, so the select holds
		// an explicit unset entry and lists only adaptered platforms.
		selP.appendChild(new Option("未选定执行平台", "", false, true));
		for (const p of options.platforms || []) selP.appendChild(platformOption(options, p, false));
		selP.onchange = () => commit({ provider: selP.value });
		pf.appendChild(selP); wrap.appendChild(pf);
		for (const [key, label] of [["model", "模型"], ["reasoningEffort", "推理强度"]]) {
			const f = el("div", "field"); f.dataset.exec = key === "provider" ? "provider" : key === "model" ? "model" : "effort"; f.appendChild(el("label", "", label));
			const inp = document.createElement("input"); inp.type = "text"; inp.value = ex[key] || ""; inp.disabled = locked; inp.dataset.f = "exec:" + key;
			inp.onchange = () => commit({ [key]: inp.value });
			f.appendChild(inp); wrap.appendChild(f);
		}
		attach(); return;
	}
	if (options.error) {
		wrap.appendChild(optionsErrorEl(options.error));
		const ex = execOf(work, sel);
		for (const [key, label] of [["provider", "平台"], ["model", "模型"], ["reasoningEffort", "推理强度"]]) {
			const f = el("div", "field"); f.dataset.exec = key === "provider" ? "provider" : key === "model" ? "model" : "effort"; f.appendChild(el("label", "", label));
			const inp = document.createElement("input"); inp.type = "text"; inp.value = ex[key] || ""; inp.disabled = locked; inp.dataset.f = "exec:" + key;
			inp.onchange = () => commit({ [key]: inp.value });
			f.appendChild(inp); wrap.appendChild(f);
		}
		attach(); return;
	}
	const ex = execOf(work, sel);
	const eff = effectiveChoice(options, { provider: ex.provider });
	const pf = el("div", "field"); pf.dataset.exec = "provider"; pf.appendChild(el("label", "", "平台"));
	const selP = document.createElement("select"); selP.disabled = locked; selP.dataset.f = "provider";
	const effProvider = (options.effective || {}).provider || "?";
	const effMark = options.installed && options.installed[effProvider] === false ? "·未安装" : "";
	selP.appendChild(new Option("跟随 run 默认（" + effProvider + effMark + "）", "", false, !ex.provider));
	for (const p of options.platforms || []) selP.appendChild(platformOption(options, p, p === ex.provider));
	if (ex.provider && !(options.platforms || []).includes(ex.provider)) selP.appendChild(platformOption(options, ex.provider, true));
	pf.appendChild(selP); wrap.appendChild(pf);
	const cat = options.catalogue || { models: [] };
	const defaultModel = eff.model || (cat["default"] || {}).model || (cat.current || {}).model || "";
	const defaultEntry = (cat.models || []).find(m => m.value === defaultModel);
	const defaultLabel = (defaultEntry && defaultEntry.label) || defaultModel || "未知";
	const mf = el("div", "field"); mf.dataset.exec = "model"; mf.appendChild(el("label", "", "模型（按平台实时获取）"));
	const selM = document.createElement("select"); selM.disabled = locked; selM.dataset.f = "model";
	selM.appendChild(new Option("跟随默认（" + defaultLabel + "）", "", false, !ex.model));
	for (const m of cat.models || []) selM.appendChild(new Option(m.label || m.value, m.value, false, m.value === ex.model));
	if (ex.model && !(cat.models || []).some(m => m.value === ex.model)) selM.appendChild(new Option(ex.model + "（目录未报告）", ex.model, false, true));
	mf.appendChild(selM); wrap.appendChild(mf);
	const ef = el("div", "field"); ef.dataset.exec = "effort"; ef.appendChild(el("label", "", "推理强度"));
	const efforts = el("div", "efforts");
	fillEfforts(efforts, options, ex, locked, pending);
	ef.appendChild(efforts); wrap.appendChild(ef);
	selP.onchange = () => {
		commit({ provider: selP.value });
	};
	selM.onchange = () => {
		commit({ model: selM.value });
	};
	attach();
}
async function renderGateField(i, locked, editingName, gen) {
	const f = el("div", "field"); f.appendChild(el("label", "", "普通门（合并门、分片门随质量门节点开关，不并入普通列表）"));
	const list = el("div", "checks gate-picker");
	const all = el("label", "gate-summary"), allCb = document.createElement("input");
	allCb.type = "checkbox"; allCb.disabled = true; allCb.dataset.f = "gates:all";
	all.appendChild(allCb); all.appendChild(document.createTextNode(" 全部门")); list.appendChild(all);
	let gates = [], loaded = true;
	try { gates = (await api.get("/api/gates")).gates || []; } catch (e) { loaded = false; }
	const available = gates.map(gate => gate.id);
	if (gen !== inspGen || sel !== "gate" || editing !== editingName) return;
	const currentLocked = () => { const st = nodeState("gate"); return !!st && !st.editable; };
	const sync = () => {
		const selected = gate => gatesAll(work) || work.gates.includes(gate);
		const count = available.filter(selected).length;
		allCb.checked = available.length > 0 && count === available.length;
		allCb.indeterminate = count > 0 && count < available.length;
		allCb.disabled = currentLocked() || !available.length;
		list.querySelectorAll('input[data-f^="gate:"]').forEach(cb => {
			cb.checked = selected(cb.dataset.f.slice(5)); cb.disabled = currentLocked();
		});
	};
	const commit = edit => {
		if (!currentLocked() && available.length) {
			const prev = snapshot(); edit(); change(prev, ["dirty", "gates"]);
		}
		sync();
	};
	allCb.onchange = () => commit(() => { work.gates = allCb.checked ? undefined : []; });
	const options = el("div", "gate-options"); list.appendChild(options);
	for (const gate of gates) {
		const label = el("label", "gate-option"), cb = document.createElement("input");
		cb.type = "checkbox"; cb.dataset.f = "gate:" + gate.id;
		cb.onchange = () => commit(() => { work.gates = setSel(gatesAll(work) ? available : work.gates, gate.id, cb.checked); });
		const text = el("span", "gate-copy"); text.appendChild(el("span", "gate-name", gate.id));
		if (gate.description && gate.description.trim()) text.appendChild(el("span", "gate-description", gate.description));
		label.appendChild(cb); label.appendChild(text); options.appendChild(label);
	}
	if (!available.length) list.appendChild(el("div", "hint", loaded ? "暂无普通门；保留当前选择。" : "未能获取门列表；保留当前选择。"));
	if (!nodeEnabled(work, "gate")) list.appendChild(el("div", "hint", "质量门节点已关闭：普通门、分片门、合并门均不派发。"));
	sync(); f.appendChild(list); i.appendChild(f);
}

function snapshot() { const o = {}; for (const n of RESP) o[n.id] = nodeSig(n.id); return o; }
function syncInspectorSignature(keys = ["dirty", "exec", "optKey"]) {
	if (!inspSig) return;
	const previous = JSON.parse(inspSig), current = JSON.parse(inspectorSignature());
	for (const key of keys) previous[key] = current[key];
	inspSig = JSON.stringify(previous);
}
function change(_, signatureKeys) { dirty = true; if (signatureKeys) syncInspectorSignature(signatureKeys); render(); }

// ---------- drawer ----------
// The library overlays the canvas from the left, so opening it fits the graph
// again over the narrower band; fitContent also clears viewAdjusted.
function openDrawer() { $("#drawer").classList.add("open"); $("#scrim").classList.add("show"); fitContent(0); }
// Closing the library has two branches. Without a user-adjusted view the canvas
// is refitted over the freed width. With one, the view is kept: applyView only
// clamps it to the widened band, so the content moves by at most the freed
// drawer width and never returns to the fit position. The inspector's ✕ and a
// blank-canvas click close that panel without calling either — the band only
// gains width there, so the view stays exactly where the user left it.
function closeDrawer() {
	$("#drawer").classList.remove("open"); $("#scrim").classList.remove("show"); saveMode = false; pendingDo = null; renderSaveBar();
	if (!viewAdjusted) fitContent(0); else applyView();
}
if (DOM) {
$("#scrim").onclick = () => closeDrawer();
$("#libBtn").onclick = () => { $("#drawer").classList.contains("open") ? closeDrawer() : openDrawer(); };
}

function isBuiltin(o) { return !!o && o.source === "default"; }
function scopeLabel(o) { return isBuiltin(o) ? "内置" : o.scope === "user" ? "用户层" : o.scope === "project" ? "项目层" : "未知层"; }

// In save mode a list click is a save target per §3.4.2; otherwise it picks
// the configuration to edit (through the dirty guard).
async function listClick(o) {
	if (saveMode) { saveTarget(o); return; }
	await guardDirtyThen(() => pickConfiguration(o.name));
}
async function saveTarget(o) {
	if (isBuiltin(o)) { toast("内置配置不能作为覆盖目标"); return; }
	if (o.name === editing && option(editing)) { await doSave(o.scope || "project", o.name, true); return; }
	const ok = await dlg({ title: "覆盖确认", msg: `用当前草稿覆盖「${o.name}」（${scopeLabel(o)}）？`, ok: "覆盖", danger: true });
	if (ok) await doSave(o.scope || "project", o.name, true);
}
function renderDrawer() {
	const list = $("#cfgList"); list.textContent = "";
	for (const o of state.configurations || []) {
		const item = el("div", "cfg" + (o.name === editing ? " on" : "") + (saveMode && isBuiltin(o) ? " skip" : ""));
		const n = el("div", "n");
		n.appendChild(el("span", "", o.name));
		if (o.name === currentName()) n.appendChild(el("span", "curmark", "（当前）"));
		if (isBuiltin(o)) n.appendChild(el("span", "badge end", "内置"));
		else n.appendChild(el("span", "badge end", o.scope === "user" ? "用户" : "项目"));
		const bound = boundRuns(o.name);
		// Up to two bound runs name themselves inline; more collapse to a count
		// that expands into a scrollable list inside the card.
		if (bound.length && bound.length <= 2) {
			const badge = el("span", "badge run", `绑定 ${bound.map(runRef).join("、")}`);
			badge.title = bound.join("、");
			n.appendChild(badge);
		}
		if ((o.shadowed || []).length) n.appendChild(el("span", "badge shadow", "有同名遮蔽项被忽略"));
		item.appendChild(n);
		const m = el("div", "m");
		const qs = (o.configuration.qaModes || []).join("+") || "无 QA";
		m.textContent = `${scopeLabel(o)} · ${qs}`;
		item.appendChild(m);
		if (bound.length > 2) {
			const det = el("details", "boundruns");
			det.open = !!boundOpen[o.name];
			const sum = el("summary", "badge run", `绑定 ${bound.length} 个 run`);
			sum.title = bound.join("、");
			det.appendChild(sum);
			for (const id of bound) { const line = el("div", "", runRef(id)); line.title = id; det.appendChild(line); }
			det.onclick = ev => ev.stopPropagation();
			det.addEventListener("toggle", () => { boundOpen[o.name] = det.open; });
			item.appendChild(det);
		}
		const acts = el("div", "acts");
		const builtin = isBuiltin(o);
		const canRD = !builtin && !bound.length;
		if (canRD) {
			const rn = el("button", "", "重命名");
			rn.onclick = async ev => {
				ev.stopPropagation();
				const name = await dlg({ title: "重命名配置", input: o.name, ok: "重命名" });
				if (!name || name === o.name) return;
				await guardDirtyThen(async () => {
					try {
						await api.post("/api/configurations", { operation: "rename", scope: o.scope || "project", name: o.name, newName: name, expected: baselineRev || state.revisions });
						if (editing === o.name) editing = name; // identity update; draft preserved
						await reloadLibrary(name);
					} catch (e) { toast(e.message); }
				});
			};
			acts.appendChild(rn);
		}
		const cp = el("button", "", "复制");
		cp.onclick = async ev => {
			ev.stopPropagation();
			await guardDirtyThen(async () => {
				try { const r = await api.post("/api/configurations", { operation: "copy", scope: o.scope || "project", source: o.name, expected: baselineRev || state.revisions }); await reloadLibrary(r.name || editing); }
				catch (e) { toast(e.message); }
			});
		};
		acts.appendChild(cp);
		if (canRD) {
			const del = el("button", "del", "删除");
			del.onclick = async ev => {
				ev.stopPropagation();
				const ok = await dlg({ title: "删除配置", msg: `删除「${o.name}」？此操作不可撤销。`, ok: "删除", danger: true });
				if (!ok) return;
				await guardDirtyThen(async () => {
					try { await api.post("/api/configurations", { operation: "delete", scope: o.scope || "project", name: o.name, expected: baselineRev || state.revisions }); await reloadLibrary(editing); }
					catch (e) { toast(e.message); }
				});
			};
			acts.appendChild(del);
		}
		if (!o.active) {
			const use = el("button", "", "设为默认");
			use.onclick = async ev => {
				ev.stopPropagation();
				await guardDirtyThen(async () => {
					try { await api.post("/api/configurations", { operation: "use", name: o.name, expected: baselineRev || state.revisions }); await reloadLibrary(editing); }
					catch (e) { toast(e.message); }
				});
			};
			acts.appendChild(use);
		}
		item.appendChild(acts);
		item.onclick = () => listClick(o);
		list.appendChild(item);
	}
	const ft = $("#drawerFt");
	if (state.error) ft.textContent = "⚠ " + state.error;
	else ft.textContent = `${(state.configurations || []).length} 项配置`;
	renderSaveBar();
	renderSettingsRow();
}
async function pickConfiguration(name) {
	if (name === editing) return;
	const o = option(name);
	editing = name; editingScope = o ? (o.scope || "project") : "project";
	work = structuredClone(o ? o.configuration : {});
	dirty = false; sel = null; selKey = null;
	render(); fitContent(0);
}
if (DOM) $("#newCfg").onclick = async () => {
	await guardDirtyThen(async () => {
		for (;;) {
			const name = await dlg({ title: "新建配置", input: uniqueName("新配置"), ok: "创建" });
			if (!name) return;
			if (option(name)) { toast(`「${name}」已存在，请换一个名字`); continue; }
			editing = name; editingScope = "project";
			work = { qaModes: [], flowSelection: undefined, gates: undefined, execution: {}, roundLimits: {} };
			dirty = true; sel = null; selKey = null;
			render();
			return;
		}
	});
};
function uniqueName(base) {
	const names = new Set((state.configurations || []).map(o => o.name));
	if (!names.has(base)) return base;
	for (let i = 2; ; i++) if (!names.has(base + " " + i)) return base + " " + i;
}

// ---------- panel settings (the two panel-owned fields only) ----------
async function editSetting(name, value) {
	// The write is expected against the draft baseline, not a fresh re-read:
	// an intervening external edit must surface as a conflict instead of being
	// silently absorbed into the baseline.
	const expected = baselineRev || state.revisions;
	try {
		const r = await api.post("/api/settings", { name, value: String(value), expected });
		await loadState();
		baselineRev = r.revisions || state.revisions;
		render();
	} catch (e) { toast(e.message); render(); }
}
function renderSettingsRow() {
	const row = $("#setrow"); row.textContent = "";
	const v = (state.effective && state.effective.values) || {};
	const selLab = el("label"); const cb = document.createElement("input"); cb.type = "checkbox"; cb.checked = !!v.panelSelection;
	cb.onchange = () => editSetting("panelSelection", cb.checked ? "true" : "false");
	selLab.appendChild(cb); selLab.appendChild(document.createTextNode(" 受理时用面板选择（关闭则走对话）"));
	row.appendChild(selLab);
	const toLab = el("label"); toLab.appendChild(document.createTextNode("空闲超时（秒，0 不超时）"));
	const inp = document.createElement("input"); inp.type = "number"; inp.min = "0"; inp.max = "86400"; inp.value = v.panelIdleTimeout || 0;
	inp.onchange = () => { const n = Math.max(0, Math.min(86400, parseInt(inp.value) || 0)); inp.value = n; editSetting("panelIdleTimeout", n); };
	toLab.appendChild(inp);
	row.appendChild(toLab);
}

// ---------- save mode ----------
function renderSaveBar() {
	const bar = $("#saveBar"); bar.textContent = "";
	if (!saveMode || !$("#drawer").classList.contains("open")) return;
	const box = el("div", "savebar");
	const bound = boundRuns(editing);
	box.appendChild(el("div", "", "点列表中的配置保存到它；或存为新配置："));
	const row1 = el("div", "row");
	const nameInp = document.createElement("input"); nameInp.type = "text"; nameInp.value = uniqueName(editing + " 副本");
	const scopeSel = document.createElement("select"); scopeSel.style.width = "90px";
	scopeSel.appendChild(new Option("项目", "project")); scopeSel.appendChild(new Option("用户", "user"));
	const create = el("button", "sm", "存为新配置");
	create.onclick = () => { const nm = nameInp.value.trim(); if (nm) doSave(scopeSel.value, nm, false); };
	row1.appendChild(nameInp); row1.appendChild(scopeSel); row1.appendChild(create);
	box.appendChild(row1);
	const hint = el("div", "hint");
	const boundNote = bound.length <= 2 ? `（${bound.map(runRef).join("、")}）` : "";
	hint.textContent = bound.length ? `「${editing}」被 ${bound.length} 个未结束 run 绑定${boundNote}；保存它影响其后续派发。` : "保存到其他配置不影响任何已绑定 run。";
	hint.title = bound.join("、");
	box.appendChild(hint);
	const cancel = el("button", "sm", "取消");
	cancel.onclick = () => { saveMode = false; pendingDo = null; renderSaveBar(); };
	box.appendChild(cancel);
	bar.appendChild(box);
}
async function doSave(scope, name, overwrite) {
	try { await save(scope, name, overwrite); openDrawer(); } catch (e) { toast(e.message); }
}
function openSaveMode() { saveMode = true; openDrawer(); renderSaveBar(); renderDrawer(); }
if (DOM) $("#save").onclick = () => {
	if (!dirty && option(editing) && !isBuiltin(option(editing))) { toast("没有未保存的修改"); return; }
	openSaveMode();
};

// ---------- footer / select / window lifecycle ----------
function renderRunSelector() {
	const s = $("#runSel");
	const runs = (state && state.runs) || [];
	if (BOOT.mode === "intake" || !runs.length) { s.style.display = "none"; if (runID === null) runID = null; return; }
	s.style.display = "";
	const cur = s.value;
	s.textContent = "";
	s.appendChild(new Option("配置库（不选 run）", "", false, runID === ""));
	for (const r of runs) {
		const opt = new Option(runLabel(r), r.runId, false, r.runId === (runID || (selectedRun() || {}).runId));
		opt.title = r.runId;
		s.appendChild(opt);
	}
	const eff = s.value;
	if (runID === null) { runID = eff; }
	s.value = runID === null ? eff : runID;
	s.onchange = async () => {
		const v = s.value;
		await guardDirtyThen(async () => {
			runID = v;
			const run = selectedRun();
			const o = run && run.configuration && option(run.configuration);
			if (o) {
				editing = o.name;
				editingScope = o.scope || "project";
				work = structuredClone(o.configuration);
				dirty = false;
			}
			sel = null; selKey = null;
			render(); fitContent(0);
		});
		// Re-render the selector so a cancelled guard snaps the value back.
		renderRunSelector();
	};
}
function renderHeader() {
	$("#cfgTitle").textContent = editing || "—";
	const run = selectedRun();
	$("#curBadge").style.display = editing === currentName() ? "" : "none";
	$("#dirtyBadge").style.display = dirty ? "" : "none";
	const ri = $("#runinfo"); ri.textContent = "";
	if (run) {
		ri.appendChild(el("span", "dot"));
		const running = (run.nodes || []).filter(n => n.running).map(n => LABEL[n.node] || n.node).join("、");
		const info = el("span", "", `${runLabel(run)} · 绑定「${run.configuration}」` + (running ? " · 正在跑：" + running : ""));
		info.title = run.runId;
		ri.appendChild(info);
	} else if (selectedRunTerminal()) {
		ri.appendChild(el("span", "dot idle"));
		const info = el("span", "", runRef(runID));
		info.title = runID;
		ri.appendChild(info);
	} else {
		ri.appendChild(el("span", "dot idle"));
		ri.appendChild(el("span", "", BOOT.mode === "intake" ? "受理阶段/新建run" : "配置库管理"));
	}
}
function render() {
	if (!state) return;
	renderHeader(); renderRunSelector(); renderNodes(lastSigs); renderInspector(); renderDrawer(); renderFooter();
	lastSigs = snapshot();
}
let lastSigs = null;

// ---------- dialog ----------
function dlg({ title, msg, input, ok = "确定", cancel = "取消", extra, danger }) {
	return new Promise(res => {
		const ov = el("div", "dlgwrap");
		const box = el("div", "dlg");
		box.appendChild(el("div", "t", title));
		if (msg) box.appendChild(el("div", "m", msg));
		let inp = null;
		if (input !== undefined) { inp = document.createElement("input"); inp.type = "text"; inp.value = input; box.appendChild(inp); }
		const err = el("div", "e"); box.appendChild(err);
		const bs = el("div", "bs");
		const done = v => { ov.remove(); res(v); };
		const cancelB = el("button", "b", cancel); cancelB.onclick = () => done(extra ? false : undefined);
		bs.appendChild(cancelB);
		if (extra) { const eb = el("button", "b", extra); eb.onclick = () => done("extra"); bs.appendChild(eb); }
		const okB = el("button", "b " + (danger ? "dan" : "pri"), ok);
		okB.onclick = () => { const v = inp ? inp.value.trim() : true; if (inp && !v) { err.textContent = "名称不能为空"; return; } done(v); };
		bs.appendChild(okB); box.appendChild(bs); ov.appendChild(box); document.body.appendChild(ov);
		if (inp) setTimeout(() => { inp.focus(); inp.select(); });
		ov.addEventListener("keydown", e => { if (e.key === "Enter") okB.click(); if (e.key === "Escape") done(undefined); });
	});
}
function showClosed(text) {
	document.body.textContent = "";
	document.body.appendChild(el("div", "closed", text));
}

// ---------- polling / window lifecycle ----------
// A restarted service listens on a new address with a new token this window
// cannot know, so a poll-failure streak past POLL_DEAD_MS can never recover:
// the poll switches to the closed state instead of retrying forever.
const POLL_DEAD_MS = 30000;
async function pollWindow() {
	if (!BOOT.window) return;
	const w = await api.get("/api/window?window=" + BOOT.window);
	winInfo = w;
	if (!w.open) {
		gone = true;
		const r = w.result || {};
		if (r.status === "timeout") showClosed("空闲超时，窗口已关闭。" + (r.configuration ? `已按当前启用配置「${r.configuration}」作为本次选择。` : ""));
		else if (r.status === "selected") showClosed("已选择「" + (r.configuration || "") + "」。");
		else showClosed("窗口会话已结束。");
		return;
	}
	if (BOOT.mode === "intake" && w.mode === "library" && w.result && w.result.status === "superseded") {
		BOOT.mode = "library";
		banner("受理候选已失效：" + (w.result.reason || ""), null, false);
		render();
	}
}
async function poll() {
	if (gone) return;
	try {
		const prevRev = state && state.revisions;
		await loadState();
		downSince = 0;
		ensureDraft();
		lastLoadError = "";
		if (prevRev && JSON.stringify(prevRev) !== JSON.stringify(state.revisions)) {
			await onRevisionsChanged(layersChanged(state));
		}
		if (runID && selectedRunTerminal()) {
			const live = (state.runs || []).find(r => r.runId !== runID && r.status !== "COMPLETE" && r.status !== "ABORTED");
			if (live) runID = live.runId;
			else settleTerminal();
		}
		await pollWindow();
		if (!gone) render();
	} catch (e) {
		if (!downSince) downSince = Date.now();
		if (Date.now() - downSince >= POLL_DEAD_MS) {
			gone = true;
			showClosed("面板服务已停止（连接持续失败），此窗口可关闭。");
			return;
		}
		if (e.message !== lastLoadError) {
			lastLoadError = e.message;
			banner("状态读取失败（保留当前草稿，将持续重试）：" + e.message, null, false);
		}
	}
}
// The selected run ended with no other live run: keep the draft while the
// edited configuration still exists; once it is gone the displayed value is a
// fresh load of the active configuration, not a user draft.
function settleTerminal() {
	if (editing && option(editing)) return;
	editing = state.active;
	const o = option(editing);
	if (o) { work = structuredClone(o.configuration); dirty = false; editingScope = o.scope || "project"; }
}
async function heartbeat() {
	if (gone || !BOOT.window) return;
	try {
		await api.post("/api/window/heartbeat", { window: BOOT.window, activity });
		activity = false;
	} catch (e) {
		if (String(e.message).includes("404") || String(e.message).includes("unknown window")) {
			gone = true;
			showClosed("窗口会话已结束。");
		}
	}
}
if (DOM) window.addEventListener("beforeunload", e => {
	if (dirty && !closing) { e.preventDefault(); e.returnValue = ""; }
});

// ---------- footer actions ----------
function renderFooter() {
	const m = $("#msg"), a = $("#apply"), s = $("#save");
	m.textContent = "";
	const run = selectedRun();
	if (BOOT.mode === "intake") {
		a.textContent = "选好了";
		m.textContent = dirty ? "有未保存修改，选择前须先保存为具名配置" : `将以「${editing}」提交本次受理`;
		s.textContent = "保存";
		s.style.display = "";
		return;
	}
	if (run) {
		s.style.display = "none";
		a.textContent = dirty ? "保存并应用" : "应用";
		if (dirty) m.textContent = "有未保存的修改，保存后从下一节点边界生效";
		else if (editing === run.configuration) m.textContent = `本次 run 正在使用「${editing}」`;
		else m.textContent = `选中 run 使用的是「${run.configuration}」，编辑「${editing}」不影响它`;
		return;
	}
	s.style.display = "";
	s.textContent = "保存";
	a.textContent = dirty ? "保存并应用" : "应用";
	if (dirty) m.textContent = "有未保存的修改，保存后生效";
	else m.textContent = state.active === editing ? `「${editing}」是当前启用配置` : `当前启用的是「${state.active}」`;
}
async function doSelect() {
	try {
		const r = await api.post("/api/window/select", { window: BOOT.window, configuration: editing });
		const res = r.result || {};
		if (res.status === "superseded") {
			BOOT.mode = "library";
			banner("受理候选已失效：" + (res.reason || "未知原因"), null, false);
			render();
			return;
		}
		if (res.status && res.status !== "selected") { toast(res.reason || res.status); return; }
		closing = true;
		showClosed("已选择「" + editing + "」。请在对话中确认开始，配置选择本身不启动流程。");
	} catch (e) { toast(e.message); }
}
if (DOM) $("#apply").onclick = async () => {
	if (BOOT.mode === "intake") {
		if (dirty) {
			const r = await dlg({ title: "先保存", msg: "「选好了」只提交已保存的具名配置。", ok: "保存并继续", cancel: "取消" });
			if (r !== true) return;
			pendingDo = doSelect;
			openSaveMode();
			return;
		}
		await doSelect();
		return;
	}
	if (dirty) { openSaveMode(); return; }
	const run = selectedRun();
	if (run) {
		if (editing === run.configuration) toast(`「${editing}」已是该 run 的生效配置`);
		else toast(`该 run 绑定的是「${run.configuration}」，运行中不能换绑——请直接编辑那份配置。`);
		return;
	}
	if (editing === state.active) toast(`「${editing}」已是当前启用配置`);
	else toast(`当前启用的是「${state.active}」；在列表里用「设为默认」切换。`);
};
// Internal close posts the window close; it runs through the same dirty guard.
if (DOM) $("#winClose").onclick = async () => {
	await guardDirtyThen(async () => {
		closing = true;
		try { await api.post("/api/window/close", { window: BOOT.window }); } catch (e) { /* already gone */ }
		showClosed("已关闭。");
	});
};

// ensureDraft initializes the browser draft once real state arrives; it runs
// on every poll so a config repaired after a failed startup loads in place.
let drafted = false;
function ensureDraft() {
	if (drafted || !state) return;
	const run = selectedRun();
	editing = (run && run.configuration) || state.active || ((state.configurations || [])[0] || {}).name || BUILTIN;
	const o = option(editing);
	editingScope = (o && o.scope) || "project";
	work = structuredClone(o ? o.configuration : { qaModes: [] });
	drafted = true;
}

async function init() {
	if (!BOOT.window) { showClosed("窗口参数缺失或已过期。"); return; }
	try {
		const w = await api.get("/api/window?window=" + BOOT.window);
		winInfo = w;
		BOOT.mode = w.mode || BOOT.mode;
		if (w.result && w.result.status === "superseded") BOOT.mode = "library";
	} catch (e) { showClosed("窗口不存在或已关闭。"); return; }
	// Heartbeat and polling run even when the first state read fails, so a
	// broken configuration file shows its real error and recovers after repair.
	setInterval(poll, 2000);
	setInterval(heartbeat, 2000);
	try {
		await loadState();
	} catch (e) {
		lastLoadError = e.message;
		banner("状态读取失败（修复配置文件后自动恢复）：" + e.message, null, false);
		openDrawer();
		return;
	}
	ensureDraft();
	baselineRev = state.revisions;
	openDrawer();
	render();
	fitContent(0);
}
if (DOM) init();

if (typeof module !== "undefined") {
	module.exports = { RESP, dis, setSel, nodeEnabled, setNodeEnabled, roundKey, roundOf, setRound, execOf, setExec, gatesAll, isBuiltin, layersChanged, selectedRun, selectedRunTerminal, settleTerminal, buildGraph, scopeLabel, optionsFor, optCache, sigOptKey, effectiveChoice, fillEfforts, nodeStatusText, optionsErrorText, optionsErrorEl, shortRunId, runLabel, runRef, currentName, poll, POLL_DEAD_MS, NODE_W,
		env: {
			get state() { return state; }, set state(v) { state = v; },
			get winInfo() { return winInfo; }, set winInfo(v) { winInfo = v; },
			get runID() { return runID; }, set runID(v) { runID = v; },
			get editing() { return editing; }, set editing(v) { editing = v; },
			get sel() { return sel; }, set sel(v) { sel = v; },
			get editingScope() { return editingScope; }, set editingScope(v) { editingScope = v; },
			get work() { return work; }, set work(v) { work = v; },
			get dirty() { return dirty; }, set dirty(v) { dirty = v; },
			get baselineRev() { return baselineRev; }, set baselineRev(v) { baselineRev = v; },
			get gone() { return gone; }, set gone(v) { gone = v; },
			get downSince() { return downSince; }, set downSince(v) { downSince = v; },
		},
	};
}
