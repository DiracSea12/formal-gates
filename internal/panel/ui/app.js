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
	{ id: "qa-blackbox", t: "黑盒 QA", qa: "blackbox", round: "qa-blackbox", rh: "设计↔审查返工连续 FAIL 的上限" },
	{ id: "qa-whitebox", t: "白盒 QA", qa: "whitebox", round: "qa-whitebox", rh: "设计↔审查返工连续 FAIL 的上限" },
	{ id: "gate", t: "质量门", d: "已选普通门 + 合并门" },
	{ id: "repair", t: "业务返修", round: "repair", rh: "验证轮与返修轮合计达到上限后需授权", d: "已成立问题的返修" },
];
const LABEL = Object.fromEntries(RESP.map(n => [n.id, n.t]));
const BUILTIN = "默认";

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
let pendingDo = null;      // continuation to run after a successful dirty-guard save
let inspSig = "";          // inspector content signature; rebuilds only on change
let inspGen = 0;           // bumped on each inspector rebuild so stale async work bails

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
// Gates: undefined/null = all-inclusive (含将来新增); array = explicit list.
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
// buildGraph draws the real structure per §3.1.1: reviews → serial
// development batches → dependency-parallel slices (each own dev + slice
// gates, no QA inside) → mainline merge → merge QA + merge gate →
// verification QA and ordinary gates in parallel → repair → verification.
// Before a plan is confirmed the canvas is an explicitly marked schematic.
function buildGraph() {
	const run = selectedRun();
	const nodes = [], edges = [], groups = [];
	const X = 200, Y = 118;
	const put = (key, resp, label, sub, col, row, op) => {
		nodes.push({ key, resp, label, sub, op: !!op, x: 40 + col * X, y: 50 + row * Y });
	};
	const link = (a, b, cls) => edges.push({ a, b, cls });
	const qaMergeResp = () => nodeEnabled(work, "qa-whitebox") ? "qa-whitebox" : nodeEnabled(work, "qa-blackbox") ? "qa-blackbox" : null;
	const verifyTail = col => {
		let col2 = col;
		const qaOn = nodeEnabled(work, "qa-blackbox") || nodeEnabled(work, "qa-whitebox");
		put("v-qa-b", "qa-blackbox", "验证·黑盒 QA", "执行验收", col2, 0.4);
		put("v-qa-w", "qa-whitebox", "验证·白盒 QA", "执行验收", col2, 1.4);
		put("v-gate", "gate", "验证·质量门", "已选普通门", col2, 2.4);
		put("v-repair", "repair", "业务返修", "", col2 + 1, 1.4);
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
	// slice-owned batches are rendered inside their slice.
	const batches = ((flow && flow.developmentBatches) || []).filter(b => b.required !== false);
	const slices = (run && run.slices) || [];
	const split = slices.length > 0;
	const planReady = !!(flow && flow.planConfirmed);

	if (!run || !planReady) {
		// Schematic: intake or a run whose plan is not yet confirmed.
		put("n-product-review", "product-review", "产品审查", "", 0, 1);
		put("n-start-readiness", "start-readiness", "开发前技术审查", "", 1, 1);
		put("n-dev-0", "development", "开发批次 1", "", 2, 1);
		put("n-dev-1", "development", "开发批次 2", "批次串行", 3, 1);
		put("n-qa-blackbox", "qa-blackbox", "黑盒 QA 设计/审查", "与开发并行", 2.6, 0);
		put("n-qa-whitebox", "qa-whitebox", "白盒 QA 设计/审查", "与开发并行", 2.6, 2.1);
		link("n-product-review", "n-start-readiness"); link("n-start-readiness", "n-dev-0"); link("n-dev-0", "n-dev-1");
		link("n-start-readiness", "n-qa-blackbox");
		link("n-start-readiness", "n-qa-whitebox");
		put("s0-dev", "development", "分片 A · 开发", "", 4, 0.3);
		put("s0-gate", "gate", "分片 A · 分片门", "", 4.8, 0.3);
		put("s1-dev", "development", "分片 B · 开发", "", 4, 1.7);
		put("s1-gate", "gate", "分片 B · 分片门", "", 4.8, 1.7);
		link("n-dev-1", "s0-dev"); link("n-dev-1", "s1-dev"); link("s0-dev", "s0-gate"); link("s1-dev", "s1-gate");
		mergeTail(["s0-gate", "s1-gate"], 5.6);
		groups.push({ x: 16, y: 8, w: 900, h: 22, label: "示意图 · 受理阶段的批次与分片数量以拓扑确认为准；分片内无 QA" });
		return { nodes, edges, groups, schematic: true };
	}

	// Actual run: confirmed plan batches serial by DependsOn.
	put("n-product-review", "product-review", "产品审查", "", 0, 1);
	put("n-start-readiness", "start-readiness", "开发前技术审查", "", 1, 1);
	link("n-product-review", "n-start-readiness");
	const batchKey = {};
	batches.forEach((b, i) => {
		const key = "n-dev-" + b.id;
		batchKey[b.id] = key;
		const statusText = { QUEUED: "排队", ISSUED: "已签发", RUNNING: "运行中", VALIDATING: "校验中", TERMINAL: "已完成" }[b.status] || b.status;
		put(key, "development", "批次 " + b.id, (b.owner || "") + (statusText ? " · " + statusText : ""), 2 + i, split ? 0.15 : 0.7);
	});
	for (const b of batches) {
		const deps = (b.dependsOn || []).filter(d => batchKey[d]);
		for (const d of deps) link(batchKey[d], batchKey[b.id]);
		if (!deps.length) link("n-start-readiness", batchKey[b.id]);
	}
	if (!batches.length) {
		put("n-dev", "development", "初始开发实现", "", 2, split ? 0.6 : 1);
		link("n-start-readiness", "n-dev");
	}
	const devHeads = batches.length ? batches.map(b => batchKey[b.id]) : ["n-dev"];
	const lastDev = batches.length ? batchKey[batches[batches.length - 1].id] : "n-dev";
	put("n-qa-blackbox", "qa-blackbox", "黑盒 QA 设计/审查", "与开发并行", 2, 1.6); link("n-start-readiness", "n-qa-blackbox");
	put("n-qa-whitebox", "qa-whitebox", "白盒 QA 设计/审查", "与开发并行", 2, 2.4); link("n-start-readiness", "n-qa-whitebox");

	if (split) {
		const sliceCol = 2 + Math.max(batches.length, 1) + 0.6;
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
			const entered = s.enteredMainline;
			const childBatches = (s.flow && s.flow.developmentBatches) || [];
			if (childBatches.length > widest) widest = childBatches.length;
			const localKey = {};
			childBatches.forEach((b, j) => {
				const key = "sl-dev-" + s.id + "-" + b.id;
				localKey[b.id] = key;
				const st = { QUEUED: "待执行", ISSUED: "已派发", RUNNING: "执行中", VALIDATING: "验证中", TERMINAL: "已结束" }[b.status] || "";
				put(key, "development", "分片 " + s.id + " · 批次 " + b.id,
					(entered ? "已进主线，随主线配置" : st || (s.status || "")), sliceCol + j * 0.72, row);
			});
			const gt = "sl-gate-" + s.id;
			put(gt, "gate", "分片 " + s.id + " · 分片门", entered ? "已进主线" : "",
				sliceCol + Math.max(childBatches.length * 0.72, 0.9), row);
			if (!childBatches.length) {
				const dv = "sl-dev-" + s.id;
				put(dv, "development", "分片 " + s.id + " · 开发", (entered ? "已进主线，随主线配置" : "运行中 " + (s.status || "")), sliceCol, row);
				localKey[""] = dv;
			}
			sliceGates.push(gt);
			groups.push({ x: 20 + sliceCol * X, y: 38 + row * Y, w: X * Math.max(1.7, childBatches.length * 0.72 + 0.9), h: 18, label: "分片 " + s.id + (entered ? " · 已进主线，随主线配置" : "") });
			return { s, localKey, childBatches, gt };
		});
		// Pass 2: dependency edges can now resolve cross-slice sources.
		for (const info of sliceInfo) {
			if (!info.childBatches.length) { link(lastDev, info.localKey[""]); link(info.localKey[""], info.gt); continue; }
			for (const b of info.childBatches) {
				let incoming = false;
				for (const d of b.dependsOn || []) {
					if (info.localKey[d]) { link(info.localKey[d], info.localKey[b.id]); incoming = true; continue; }
					const src = ownerOf[d] && ownerOf[d] !== info.s.id ? "sl-dev-" + ownerOf[d] + "-" + d : null;
					if (src && nodes.some(n => n.key === src)) { link(src, info.localKey[b.id]); incoming = true; }
				}
				if (!incoming) link(lastDev, info.localKey[b.id]);
			}
			link(info.localKey[info.childBatches[info.childBatches.length - 1].id], info.gt);
		}
		mergeTail(sliceGates, sliceCol + widest * 0.72 + 1.0);
	} else {
		verifyTail(2 + Math.max(batches.length, 1) + 0.8);
		for (const d of devHeads) link(d, "v-gate");
	}
	// QA design/review feeds its verification-execution counterpart.
	if (nodeEnabled(work, "qa-blackbox") && nodes.some(n => n.key === "v-qa-b")) link("n-qa-blackbox", "v-qa-b");
	if (nodeEnabled(work, "qa-whitebox") && nodes.some(n => n.key === "v-qa-w")) link("n-qa-whitebox", "v-qa-w");
	return { nodes, edges, groups, schematic: false };
}

// ---------- view / pan / zoom ----------
const view = { x: 0, y: 0, z: 1 };
let world = null, canvas = null;
const NODE_H = {};
function inspectorInset() { return DOM && window.innerWidth > 760 && $("#insp").classList.contains("open") ? 320 : 0; }
function applyView() {
	const W = canvas.clientWidth - inspectorInset(), H = canvas.clientHeight;
	const g = graph; if (!g.nodes.length) return;
	const cx0 = Math.min(...g.nodes.map(n => n.x)) - 40, cx1 = Math.max(...g.nodes.map(n => n.x + 176)) + 40;
	const cy0 = Math.min(...g.nodes.map(n => n.y)) - 40, cy1 = Math.max(...g.nodes.map(n => n.y + (NODE_H[n.key] || 96))) + 40;
	const z = view.z, pad = 70;
	const lx = [W - pad - cx1 * z, pad - cx0 * z], ly = [H - pad - cy1 * z, pad - cy0 * z];
	view.x = lx[0] > lx[1] ? (W - (cx0 + cx1) * z) / 2 : Math.min(lx[1], Math.max(lx[0], view.x));
	view.y = ly[0] > ly[1] ? (H - (cy0 + cy1) * z) / 2 : Math.min(ly[1], Math.max(ly[0], view.y));
	world.style.transform = `translate(${view.x}px,${view.y}px) scale(${z})`;
}
function fitContent(inset) {
	const W = canvas.clientWidth - inset, H = canvas.clientHeight, m = 30, g = graph;
	if (!g.nodes.length) return;
	const cx0 = Math.min(...g.nodes.map(n => n.x)) - 40, cx1 = Math.max(...g.nodes.map(n => n.x + 176)) + 40;
	const cy0 = Math.min(...g.nodes.map(n => n.y)) - 40, cy1 = Math.max(...g.nodes.map(n => n.y + (NODE_H[n.key] || 96))) + 40;
	// Fit never zooms in past 1 nor drops below the wheel minimum; when the
	// whole graph still overflows at that floor the clicked node stays in view.
	const nz = Math.max(.5, Math.min(1, (W - 2 * m) / (cx1 - cx0), (H - 2 * m) / (cy1 - cy0)));
	const sx0 = view.x + cx0 * view.z, sx1 = view.x + cx1 * view.z, sy0 = view.y + cy0 * view.z, sy1 = view.y + cy1 * view.z;
	if (sx0 >= m && sx1 <= W - m && sy0 >= m && sy1 <= H - m && Math.abs(nz - view.z) < 0.01) return;
	view.z = nz;
	const clicked = selKey && g.nodes.find(n => n.key === selKey);
	if (clicked && ((cx1 - cx0) * nz > W - 2 * m || (cy1 - cy0) * nz > H - 2 * m)) {
		view.x = W / 2 - (clicked.x + 88) * nz;
		view.y = H / 2 - (clicked.y + (NODE_H[clicked.key] || 96) / 2) * nz;
	} else {
		view.x = (W - (cx0 + cx1) * view.z) / 2; view.y = (H - (cy0 + cy1) * view.z) / 2;
	}
	world.classList.add("anim"); applyView(); setTimeout(() => world.classList.remove("anim"), 300);
}
let panning = false, px = 0, py = 0, moved = 0;
if (DOM) {
world = $("#world"); canvas = $("#canvas");
canvas.addEventListener("mousedown", e => { if (e.target.closest(".node") || e.target.closest(".legend")) return; panning = true; moved = 0; px = e.clientX; py = e.clientY; canvas.classList.add("panning"); });
window.addEventListener("mousemove", e => { if (!panning) return; view.x += e.clientX - px; view.y += e.clientY - py; moved += Math.abs(e.clientX - px) + Math.abs(e.clientY - py); px = e.clientX; py = e.clientY; applyView(); });
window.addEventListener("mouseup", () => { panning = false; canvas.classList.remove("panning"); });
canvas.addEventListener("touchstart", e => { if (e.target.closest(".node") || e.target.closest(".legend")) return; const t = e.touches[0]; panning = true; moved = 0; px = t.clientX; py = t.clientY; }, { passive: true });
canvas.addEventListener("touchmove", e => { if (!panning) return; const t = e.touches[0]; view.x += t.clientX - px; view.y += t.clientY - py; moved += Math.abs(t.clientX - px) + Math.abs(t.clientY - py); px = t.clientX; py = t.clientY; applyView(); }, { passive: true });
canvas.addEventListener("touchend", () => { panning = false; });
canvas.addEventListener("click", e => { if (moved > 4) return; if (e.target.closest(".node") || e.target.closest(".legend")) return; if (!sel) return; sel = null; selKey = null; render(); fitContent(0); });
canvas.addEventListener("dblclick", e => { if (e.target.closest(".node") || e.target.closest(".legend")) return; view.x = view.y = 0; view.z = 1; world.classList.add("anim"); applyView(); setTimeout(() => world.classList.remove("anim"), 300); });
canvas.addEventListener("wheel", e => { e.preventDefault(); const r = canvas.getBoundingClientRect(), mx = e.clientX - r.left, my = e.clientY - r.top; const nz = Math.min(1.8, Math.max(.5, view.z * (e.deltaY < 0 ? 1.12 : 1 / 1.12))); view.x = mx - (mx - view.x) * nz / view.z; view.y = my - (my - view.y) * nz / view.z; view.z = nz; applyView(); }, { passive: false });
document.addEventListener("keydown", e => {
	if (e.key === "Escape") {
		if ($("#drawer").classList.contains("open")) { closeDrawer(); return; }
		if ($("#banner").classList.contains("show")) { $("#banner").classList.remove("show"); return; }
		if (sel) { sel = null; selKey = null; render(); fitContent(0); } return;
	}
	if (e.target.closest("input,select,textarea")) return;
	const k = { ArrowLeft: [40, 0], ArrowRight: [-40, 0], ArrowUp: [0, 40], ArrowDown: [0, -40] }[e.key];
	if (!k) return; e.preventDefault(); view.x += k[0]; view.y += k[1]; applyView();
});
}

// ---------- node rendering ----------
let graph = { nodes: [], edges: [], groups: [] };
function nodeStatusClass(resp) {
	const st = nodeState(resp);
	if (!st) return "";
	if (st.running) return " running";
	if (st.completed || st.bypassed) return " done";
	return "";
}
function nodeStatusText(resp) {
	const st = nodeState(resp);
	if (!st) return null;
	if (st.running) return "运行中";
	if (st.closing) return "关闭中";
	if (st.completed) return "已完成";
	if (st.bypassed) return "已越过";
	if (!st.enabled) return "已关闭";
	return "";
}
function renderNodes(prev) {
	graph = buildGraph();
	world.querySelectorAll(".node,.group").forEach(n => n.remove());
	for (const grp of graph.groups || []) {
		const g = el("div", "group", grp.label); Object.assign(g.style, { left: grp.x + "px", top: grp.y + "px", width: grp.w + "px", height: grp.h + "px" });
		world.appendChild(g);
	}
	for (const n of graph.nodes) {
		const e = el("div", "node" + (n.op ? " op" : "") + nodeStatusClass(n.resp) + (n.resp && nodeEnabled(work, n.resp) ? "" : " disabled") + (sel === n.resp && RESP.find(r => r.id === n.resp) ? " sel" : ""));
		e.style.left = n.x + "px"; e.style.top = n.y + "px"; e.dataset.key = n.key; if (n.resp) e.dataset.resp = n.resp; e.tabIndex = n.op ? -1 : 0;
		const st = nodeStatusText(n.resp);
		const ex = n.resp ? execOf(work, n.resp) : {};
		const stEl = el("div", "st", st || (n.op ? "" : nodeEnabled(work, n.resp) ? "" : "已关闭"));
		const t = el("div", "t", n.label);
		e.appendChild(stEl); e.appendChild(t);
		if (n.sub) e.appendChild(el("div", "d", n.sub));
		if (n.key.startsWith("sl-dev-") || n.key.startsWith("sl-gate-") || n.key.startsWith("n-dev-") && graph.nodes.filter(x => x.resp === "development").length > 1) {
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
function anchor(key, side) {
	const n = graph.nodes.find(n => n.key === key);
	const h = NODE_H[key] || 90;
	return side === "r" ? [n.x + 176, n.y + h / 2] : side === "l" ? [n.x, n.y + h / 2] : side === "b" ? [n.x + 88, n.y + h] : [n.x + 88, n.y];
}
function drawEdges() {
	const svg = $("#edges"); svg.querySelectorAll("path").forEach(p => p.remove());
	const add = (d, cls) => {
		const p = document.createElementNS("http://www.w3.org/2000/svg", "path");
		p.setAttribute("d", d); p.setAttribute("class", "edge " + cls);
		p.setAttribute("marker-end", cls.includes("bypass") ? "url(#arrB)" : "url(#arr)");
		svg.appendChild(p);
	};
	for (const e of graph.edges) {
		const na = graph.nodes.find(n => n.key === e.a), nb = graph.nodes.find(n => n.key === e.b);
		if (!na || !nb) continue;
		const [x1, y1] = anchor(e.a, "r"), [x2, y2] = anchor(e.b, "l");
		const mx = (x1 + x2) / 2;
		const off = (na.resp && !nodeEnabled(work, na.resp)) || (nb.resp && !nodeEnabled(work, nb.resp));
		add(`M${x1},${y1} C${mx},${y1} ${mx},${y2} ${x2},${y2}`, e.cls === "loop" ? "loop" : off ? "off" : "flow");
	}
	for (const n of graph.nodes) {
		if (!n.resp || nodeEnabled(work, n.resp)) continue;
		const ins = graph.edges.filter(e => e.b === n.key).map(e => e.a);
		const outs = graph.edges.filter(e => e.a === n.key).map(e => e.b);
		for (const a of ins) for (const b of outs) {
			const [x1, y1] = anchor(a, "r"), [x2, y2] = anchor(b, "l");
			add(`M${x1},${y1} C${x1 + 70},${n.y - 70} ${x2 - 70},${n.y - 70} ${x2},${y2}`, "bypass");
		}
	}
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
function sigOptKey() { return editing + "|" + sel + "|" + (execOf(work, sel).provider || "") + "|" + (execOf(work, sel).model || ""); }
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
		i.querySelectorAll("input,select,textarea,button").forEach(c => { if (!c.classList.contains("iconbtn")) c.disabled = flocked; });
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
	x.onclick = () => { sel = null; selKey = null; render(); fitContent(0); };
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

// Options are keyed to the exact (configuration, node, provider, model) tuple
// in the draft; a fetch that resolves after the user moved on is discarded.
const optCache = {};
async function fetchOptions(editingName, node, provider, model) {
	// The catalogue query needs a stored configuration as its base; an unsaved
	// draft borrows the built-in entry without changing editing identity.
	const base = option(editingName) ? editingName : BUILTIN;
	const q = new URLSearchParams({ configuration: base, node, platform: provider || "", model: model || "", effort: "" });
	return api.get("/api/execution-options?" + q);
}
async function optionsFor() {
	const key = sigOptKey();
	if (optCache[key] !== undefined) return optCache[key];
	const seq = ++optSeq;
	const editingName = editing, node = sel;
	const provider = execOf(work, node).provider || "", model = execOf(work, node).model || "";
	let result;
	try { result = await fetchOptions(editingName, node, provider, model); }
	catch (e) { result = { error: e.message }; }
	if (seq !== optSeq || sel !== node || editing !== editingName) return optCache[sigOptKey()];
	optCache[key] = result;
	return result;
}
async function renderExecFields(i, meta, locked, editingName, gen) {
	const wrap = el("div"); i.appendChild(wrap);
	const options = await optionsFor();
	if (gen !== inspGen || sel !== meta.id || editing !== editingName) return; // a newer rebuild owns the form now
	if (options && options.error) {
		const lm = el("div", "lockmsg");
		lm.appendChild(el("b", "", "暂取不到平台选项：" + options.error));
		lm.appendChild(el("div", "", "可手工填写平台/模型/强度（原生值）。"));
		wrap.appendChild(lm);
		const ex = execOf(work, sel);
		for (const [key, label] of [["provider", "平台"], ["model", "模型"], ["reasoningEffort", "推理强度"]]) {
			const f = el("div", "field"); f.appendChild(el("label", "", label));
			const inp = document.createElement("input"); inp.type = "text"; inp.value = ex[key] || ""; inp.disabled = locked; inp.dataset.f = "exec:" + key;
			inp.onchange = () => { const prev = snapshot(); setExec(work, sel, { [key]: inp.value }); change(prev); renderInspector(true); };
			f.appendChild(inp); wrap.appendChild(f);
		}
		return;
	}
	if (!options) return;
	const ex = execOf(work, sel);
	const eff = options.effective || {};
	const pf = el("div", "field"); pf.appendChild(el("label", "", "平台"));
	const selP = document.createElement("select"); selP.disabled = locked; selP.dataset.f = "provider";
	selP.appendChild(new Option("跟随 run 默认（" + (eff.provider || options.platform || "?") + "）", "", false, !ex.provider));
	for (const p of options.platforms || []) selP.appendChild(new Option(p, p, false, p === ex.provider));
	pf.appendChild(selP); wrap.appendChild(pf);
	const cat = options.catalogue || { models: [] };
	const mf = el("div", "field"); mf.appendChild(el("label", "", "模型（按平台实时获取）"));
	const selM = document.createElement("select"); selM.disabled = locked; selM.dataset.f = "model";
	const effModel = ex.model || eff.model || (cat["default"] && cat["default"].model) || "";
	selM.appendChild(new Option("跟随默认（" + (eff.model || "目录默认") + "）", "", false, !ex.model));
	for (const m of cat.models || []) selM.appendChild(new Option(m.label || m.value, m.value, false, m.value === ex.model));
	mf.appendChild(selM); wrap.appendChild(mf);
	const modelEntry = (cat.models || []).find(m => m.value === effModel) || (cat.models || [])[0];
	const effList = (modelEntry && modelEntry.efforts) || [];
	const effDefault = ex.reasoningEffort || eff.reasoningEffort || (modelEntry && modelEntry.defaultEffort) || (cat["default"] && cat["default"].effort) || "";
	const ef = el("div", "field"); ef.appendChild(el("label", "", "推理强度"));
	const efforts = el("div", "efforts");
	for (const e of effList) {
		const b = el("button", e === effDefault ? "on" : "", e);
		b.dataset.e = e; b.dataset.f = "effort"; b.type = "button"; b.disabled = locked;
		b.onclick = () => { const prev = snapshot(); setExec(work, sel, { reasoningEffort: e }); change(prev); renderInspector(true); };
		efforts.appendChild(b);
	}
	if (!effList.length) efforts.appendChild(el("span", "hint", "该平台未报告强度档"));
	ef.appendChild(efforts); wrap.appendChild(ef);
	selP.onchange = () => {
		const prev = snapshot(); setExec(work, sel, { provider: selP.value, model: "", reasoningEffort: "" }); change(prev); renderInspector(true);
	};
	selM.onchange = () => {
		const prev = snapshot(); setExec(work, sel, { model: selM.value, reasoningEffort: "" }); change(prev); renderInspector(true);
	};
}
async function renderGateField(i, locked, editingName, gen) {
	const f = el("div", "field"); f.appendChild(el("label", "", "普通门（合并门、分片门随质量门节点开关，不并入普通列表）"));
	const list = el("div", "checks");
	const all = el("label"); const allCb = document.createElement("input"); allCb.type = "checkbox"; allCb.checked = gatesAll(work); allCb.disabled = locked; allCb.dataset.f = "gates:all";
	allCb.onchange = () => { const prev = snapshot(); work.gates = allCb.checked ? undefined : []; change(prev); renderInspector(true); };
	all.appendChild(allCb); all.appendChild(document.createTextNode(" 全部门（含将来新增）"));
	list.appendChild(all);
	let available = [];
	try { available = (await api.get("/api/gates")).gates || []; } catch (e) { available = []; }
	if (gen !== inspGen || sel !== "gate" || editing !== editingName) return; // a newer rebuild owns the form now
	if (gatesAll(work)) {
		list.appendChild(el("div", "hint", "当前包含全部已发现普通门及将来新增门。"));
	} else {
		for (const g of available) {
			const lab = el("label"); const cb = document.createElement("input"); cb.type = "checkbox"; cb.checked = work.gates.includes(g); cb.disabled = locked; cb.dataset.f = "gate:" + g;
			cb.onchange = () => { const prev = snapshot(); work.gates = setSel(work.gates, g, cb.checked); change(prev); renderInspector(true); };
			lab.appendChild(cb); lab.appendChild(document.createTextNode(" " + g)); list.appendChild(lab);
		}
		if (!available.length) list.appendChild(el("div", "hint", "未能获取门列表；显式列表保留原样保存。"));
	}
	if (!nodeEnabled(work, "gate")) list.appendChild(el("div", "hint", "质量门节点已关闭：普通门、分片门、合并门均不派发。"));
	f.appendChild(list); i.appendChild(f);
}
function snapshot() { const o = {}; for (const n of RESP) o[n.id] = nodeSig(n.id); return o; }
function change() { dirty = true; render(); }

// ---------- drawer ----------
function openDrawer() { $("#drawer").classList.add("open"); $("#scrim").classList.add("show"); }
function closeDrawer() { $("#drawer").classList.remove("open"); $("#scrim").classList.remove("show"); saveMode = false; pendingDo = null; renderSaveBar(); }
if (DOM) {
$("#scrim").onclick = () => closeDrawer();
$("#libBtn").onclick = () => { $("#drawer").classList.contains("open") ? closeDrawer() : openDrawer(); };
}

function scopeLabel(o) { return o.name === BUILTIN ? "内置" : o.scope === "user" ? "用户层" : o.scope === "project" ? "项目层" : "未知层"; }

// In save mode a list click is a save target per §3.4.2; otherwise it picks
// the configuration to edit (through the dirty guard).
async function listClick(o) {
	if (saveMode) { saveTarget(o); return; }
	await guardDirtyThen(() => pickConfiguration(o.name));
}
async function saveTarget(o) {
	if (o.name === BUILTIN) { toast("内建「默认」不能作为覆盖目标"); return; }
	if (o.name === editing && option(editing)) { await doSave(o.scope || "project", o.name, true); return; }
	const ok = await dlg({ title: "覆盖确认", msg: `用当前草稿覆盖「${o.name}」（${scopeLabel(o)}）？`, ok: "覆盖", danger: true });
	if (ok) await doSave(o.scope || "project", o.name, true);
}
function renderDrawer() {
	const list = $("#cfgList"); list.textContent = "";
	for (const o of state.configurations || []) {
		const item = el("div", "cfg" + (o.name === editing ? " on" : "") + (saveMode && o.name === BUILTIN ? " skip" : ""));
		const n = el("div", "n");
		n.appendChild(el("span", "", o.name));
		if (o.active) n.appendChild(el("span", "badge cur", "当前"));
		if (o.name === BUILTIN) n.appendChild(el("span", "badge end", "内置"));
		else n.appendChild(el("span", "badge end", o.scope === "user" ? "用户" : "项目"));
		const bound = boundRuns(o.name);
		if (bound.length) n.appendChild(el("span", "badge run", `绑定 ${bound.join("、")}`));
		if ((o.shadowed || []).length) n.appendChild(el("span", "badge shadow", "有同名遮蔽项被忽略"));
		item.appendChild(n);
		const m = el("div", "m");
		const qs = (o.configuration.qaModes || []).join("+") || "无 QA";
		m.textContent = `${scopeLabel(o)} · ${qs}`;
		item.appendChild(m);
		const acts = el("div", "acts");
		const isBuiltin = o.name === BUILTIN;
		const canRD = !isBuiltin && !bound.length;
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
			const use = el("button", "", "设为当前");
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
			if (option(name) || name === BUILTIN) { toast(`「${name}」已存在，请换一个名字`); continue; }
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
	hint.textContent = bound.length ? `「${editing}」被 ${bound.length} 个未结束 run 绑定（${bound.join("、")}）；保存它影响其后续派发。` : "保存到其他配置不影响任何已绑定 run。";
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
	if (!dirty && option(editing) && editing !== BUILTIN) { toast("没有未保存的修改"); return; }
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
	for (const r of runs) s.appendChild(new Option(`${r.runId} · ${r.status === "COMPLETE" || r.status === "ABORTED" ? "已结束" : r.status}`, r.runId, false, r.runId === (runID || (selectedRun() || {}).runId)));
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
	$("#curBadge").style.display = state.active === editing ? "" : "none";
	$("#dirtyBadge").style.display = dirty ? "" : "none";
	const ri = $("#runinfo"); ri.textContent = "";
	if (run) {
		ri.appendChild(el("span", "dot"));
		const running = (run.nodes || []).filter(n => n.running).map(n => LABEL[n.node] || n.node).join("、");
		ri.appendChild(el("span", "", `run ${run.runId} · ${run.status} · 绑定「${run.configuration}」` + (running ? " · 正在跑：" + running : "")));
	} else if (selectedRunTerminal()) {
		ri.appendChild(el("span", "dot idle"));
		ri.appendChild(el("span", "", `run ${runID} · 已结束`));
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
		showClosed("已选择「" + editing + "」。这不是启动批准——宿主还会弹出原生确认。");
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
	else toast(`当前启用的是「${state.active}」；在列表里用「设为当前」切换。`);
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
	module.exports = { RESP, dis, setSel, nodeEnabled, setNodeEnabled, roundKey, roundOf, setRound, execOf, setExec, gatesAll, layersChanged, selectedRun, selectedRunTerminal, settleTerminal, buildGraph, scopeLabel, optionsFor, optCache,
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
		},
	};
}
