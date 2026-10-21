"use strict";
// Node-side unit tests for the pure configuration semantics in app.js.
// Run: node app.test.js
const A = require("./app.js");

let failures = 0;
function check(name, cond) {
	if (cond) { console.log("PASS " + name); } else { failures++; console.log("FAIL " + name); }
}
function eq(name, got, want) { check(name, JSON.stringify(got) === JSON.stringify(want)); }

// --- QA normalization mirrors settings.SetQAMode/flow qa precedence ---
{
	const w = {};
	A.setNodeEnabled(w, "qa-blackbox", true);
	eq("enabling one QA mode clears the qa flow flag", [w.flowSelection.disabled, w.qaModes], [[], ["blackbox"]]);
	check("blackbox enabled, whitebox off", A.nodeEnabled(w, "qa-blackbox") && !A.nodeEnabled(w, "qa-whitebox"));
	A.setNodeEnabled(w, "qa-whitebox", true);
	A.setNodeEnabled(w, "qa-blackbox", false);
	eq("dropping one mode keeps the other", w.qaModes, ["whitebox"]);
	check("qa stays enabled while a mode remains", A.nodeEnabled(w, "qa-whitebox"));
	A.setNodeEnabled(w, "qa-whitebox", false);
	eq("last mode off disables the qa flow node", w.flowSelection.disabled, ["qa"]);
	check("qa node off with empty modes", !A.nodeEnabled(w, "qa-blackbox"));
	A.setNodeEnabled(w, "qa-blackbox", true);
	check("re-enable keeps qa enabled", A.nodeEnabled(w, "qa-blackbox"));
}
{
	const w = { flowSelection: { disabled: ["qa"] } };
	check("disabled qa beats listed modes", !A.nodeEnabled({ flowSelection: { disabled: ["qa"] }, qaModes: ["blackbox"] }, "qa-blackbox"));
	A.setNodeEnabled(w, "qa-blackbox", true);
	eq("enabling a mode removes the qa flag", w.flowSelection.disabled, []);
}

// --- start-readiness maps to the persisted tech-review round key ---
{
	check("start-readiness round key is tech-review", A.roundKey("start-readiness") === "tech-review");
	const w = {};
	A.setRound(w, "start-readiness", 7);
	eq("round written under tech-review", w.roundLimits, { "tech-review": 7 });
	check("roundOf reads tech-review", A.roundOf(w, "start-readiness") === 7);
	check("default round is 3", A.roundOf({}, "repair") === 3);
	eq("five responsibilities carry round limits",
		A.RESP.filter(n => n.round).map(n => n.round),
		["product-review", "tech-review", "qa-blackbox", "qa-whitebox", "repair"]);
}

// --- gates: null/omitted (all-inclusive) vs explicit list including empty ---
{
	check("omitted gates = all-inclusive", A.gatesAll({}));
	check("null gates = all-inclusive", A.gatesAll({ gates: null }));
	check("empty list is explicit, not all", !A.gatesAll({ gates: [] }));
	check("non-empty list is explicit", !A.gatesAll({ gates: ["merge"] }));
}

// --- external revision change detection per layer ---
{
	A.env.baselineRev = { user: "u1", project: "p1" };
	eq("no change", A.layersChanged({ revisions: { user: "u1", project: "p1" } }), []);
	eq("user layer changed", A.layersChanged({ revisions: { user: "u2", project: "p1" } }), ["user"]);
	eq("both layers changed", A.layersChanged({ revisions: { user: "u2", project: "p2" } }), ["user", "project"]);
	eq("missing layer reported", A.layersChanged({ revisions: {} }), ["user", "project"]);
}

// --- terminal run: draft preserved, editing survives while the config exists ---
{
	A.env.state = {
		active: "默认",
		configurations: [
			{ name: "默认", source: "builtin", configuration: { qaModes: null } },
			{ name: "工作", source: "project", configuration: { qaModes: ["whitebox"] } },
		],
		runs: [{ runId: "r1", status: "COMPLETE", configuration: "工作" }],
	};
	A.env.runID = "r1";
	A.env.editing = "工作";
	A.env.work = { qaModes: ["blackbox"], flowSelection: { disabled: ["gate"] } };
	const draft = A.env.work;
	A.env.dirty = true;
	A.settleTerminal();
	check("terminal run keeps dirty draft", A.env.dirty && A.env.work === draft);
	check("editing unchanged while config exists", A.env.editing === "工作");
}
{
	A.env.state = {
		active: "默认",
		configurations: [{ name: "默认", source: "builtin", configuration: { roundLimits: { repair: 9 } } }],
		runs: [],
	};
	A.env.runID = "r1";
	A.env.editing = "工作";
	A.env.work = { qaModes: ["blackbox"] };
	A.env.dirty = true;
	A.settleTerminal();
	eq("deleted config falls back to active value", A.env.work, { roundLimits: { repair: 9 } });
	check("dirty cleared when edited config is gone", !A.env.dirty);
	check("editing follows active", A.env.editing === "默认");
}
{
	A.env.state = {
		runs: [
			{ runId: "r1", status: "COMPLETE", configuration: "工作" },
			{ runId: "r2", status: "RUNNING", configuration: "别的" },
		],
	};
	A.env.runID = "r1";
	const run = A.selectedRun();
	check("selectedRun returns the picked run", run && run.runId === "r1");
	check("selected run is terminal", A.selectedRunTerminal());
	A.env.runID = null;
	const live = A.selectedRun();
	check("no selection falls back to first live run", live && live.runId === "r2");
}

// --- ordinary nodes toggle through flowSelection only ---
{
	const w = {};
	A.setNodeEnabled(w, "development", false);
	eq("disable appends id", w.flowSelection.disabled, ["development"]);
	check("gate untouched", A.gatesAll(w));
	A.setNodeEnabled(w, "development", true);
	eq("re-enable removes id", w.flowSelection.disabled, []);
}

// --- buildGraph: anchors, real batches, slice graphs, merge operation ---
{
	A.env.state = { runs: [] };
	A.env.runID = "";
	A.env.editing = "默认";
	A.env.work = { flowSelection: { disabled: ["qa"] }, qaModes: [] };
	const g = A.buildGraph();
	const resps = new Set(g.nodes.map(n => n.resp).filter(Boolean));
	check("all seven anchors visible with both QA off", A.RESP.every(r => resps.has(r.id)));
	check("merge QA absent when both QA modes off", !g.nodes.some(n => n.key === "m-qa"));
	check("verification->repair edge", g.edges.some(e => e.a === "v-gate" && e.b === "v-repair"));
	check("repair->verification loop edge", g.edges.some(e => e.a === "v-repair" && e.cls === "loop"));
	check("schematic merge is a non-configurable operation", g.nodes.some(n => n.key === "op-merge" && n.op === true && !n.resp));
}
{
	A.env.state = { runs: [{
		runId: "r1", status: "RUNNING", configuration: "工作",
		flow: { planConfirmed: true, developmentBatches: [
			{ id: "b1", required: true, status: "COMPLETE" },
			{ id: "b2", required: true, dependsOn: ["b1"], status: "RUNNING" },
			{ id: "sb1", owner: "core", required: false },
			{ id: "sb2", owner: "core", required: false, dependsOn: ["sb1"] },
		], enteredMainlineSlices: ["core"] },
		slices: [{ id: "core", runId: "c1", status: "RUNNING", enteredMainline: true,
			flow: { planConfirmed: true, developmentBatches: [
				{ id: "sb1", required: true, status: "COMPLETE" },
				{ id: "sb2", required: true, dependsOn: ["sb1"], status: "RUNNING" },
			] },
		}],
	}] };
	A.env.runID = "r1";
	A.env.work = { flowSelection: { disabled: [] }, qaModes: ["whitebox"] };
	const g = A.buildGraph();
	const keys = new Set(g.nodes.map(n => n.key));
	const edge = (a, b) => g.edges.some(e => e.a === a && e.b === b);
	check("rooted master batch hangs off readiness", edge("n-start-readiness", "n-dev-b1"));
	check("master batch dependency edge", edge("n-dev-b1", "n-dev-b2"));
	check("slice batches rendered from child flow", keys.has("sl-dev-core-sb1") && keys.has("sl-dev-core-sb2"));
	check("slice batch serial edge", edge("sl-dev-core-sb1", "sl-dev-core-sb2"));
	check("slice owns its gate", keys.has("sl-gate-core") && edge("sl-dev-core-sb2", "sl-gate-core"));
	check("slice batches carry no QA", !g.nodes.some(n => n.key.startsWith("sl-") && n.resp && n.resp.startsWith("qa")));
	const merge = g.nodes.find(n => n.key === "op-merge");
	check("merge is a non-configurable operation", !!merge && merge.op === true && !merge.resp);
	check("slice gate feeds merge", edge("sl-gate-core", "op-merge"));
	const mqa = g.nodes.find(n => n.key === "m-qa");
	check("whitebox-first merge QA", !!mqa && mqa.resp === "qa-whitebox");
	check("merge QA parallel to merge gate after merge", edge("op-merge", "m-qa") && edge("op-merge", "m-gate"));
	check("merge QA feeds its verification head", edge("m-qa", "v-qa-w") && edge("m-gate", "v-gate"));
	// Both QA off: merge QA disappears, QA anchors stay.
	A.env.work = { flowSelection: { disabled: ["qa"] }, qaModes: [] };
	const g2 = A.buildGraph();
	check("both-off removes merge QA", !g2.nodes.some(n => n.key === "m-qa"));
	check("both-off keeps qa anchors", g2.nodes.some(n => n.resp === "qa-blackbox") && g2.nodes.some(n => n.resp === "qa-whitebox"));
	A.env.work = { flowSelection: { disabled: [] }, qaModes: ["blackbox"] };
	const g3 = A.buildGraph();
	check("blackbox merge QA when only blackbox on", g3.nodes.some(n => n.key === "m-qa" && n.resp === "qa-blackbox"));
	A.env.runID = ""; A.env.state = { runs: [] };
}
// --- role-explicit labels, the terminal seal node and lane geometry ---
// Pins the canvas structure a user reads: every repeated development box names
// its role, the lane strip clears every node box, the seal is a display-only
// terminal operation right of repair, and each verification head gets exactly
// one return loop edge.
{
	A.env.state = { runs: [{
		runId: "r1", status: "RUNNING", configuration: "工作",
		flow: { planConfirmed: true, developmentBatches: [
			{ id: "b1", owner: "core", required: true, status: "TERMINAL" },
			{ id: "b2", required: true, dependsOn: ["b1"], status: "RUNNING" },
		] },
		slices: [{ id: "core", runId: "c1", status: "RUNNING",
			flow: { planConfirmed: true, developmentBatches: [{ id: "sb1", required: true, status: "RUNNING" }] } }],
	}] };
	A.env.runID = "r1";
	A.env.work = { flowSelection: { disabled: [] }, qaModes: ["blackbox"] };
	const g = A.buildGraph();
	const node = key => g.nodes.find(n => n.key === key) || {};
	eq("master batch label names its role", node("n-dev-b1").label, "开发批次 b1");
	eq("slice batch label names its role", node("sl-dev-core-sb1").label, "开发分片 core · 批次 sb1");
	eq("slice gate label names its role", node("sl-gate-core").label, "开发分片 core · 分片门");
	eq("slice lane header names its role", (g.groups[0] || {}).label, "开发分片 core");
	eq("owned batch subtitle keeps its parts", node("n-dev-b1").sub, "core · 已完成");
	eq("batch subtitle without an owner has no leading separator", node("n-dev-b2").sub, "运行中");
	check("lane strips clear every node box", g.groups.every(gr =>
		g.nodes.every(n => n.y >= gr.y + gr.h || n.x >= gr.x + gr.w || n.x + A.NODE_W <= gr.x)));
	check("lane strips are readable header bands", g.groups.every(gr => gr.h === 26 && gr.y >= 0));
	const seal = node("v-seal");
	check("seal node is the terminal operation", seal.op === true && seal.terminal === true && seal.resp === "" && seal.label === "封板 Seal" && seal.sub === "终态操作 · 不可配置");
	check("repair feeds the seal node", g.edges.some(e => e.a === "v-repair" && e.b === "v-seal"));
	check("seal sits right of the repair node", seal.x > node("v-repair").x);
	eq("every verification head gets one return loop edge", g.edges.filter(e => e.cls === "loop").map(e => e.b).sort(), ["v-gate", "v-qa-b"]);
	A.env.runID = ""; A.env.state = { runs: [] };
	const schematic = A.buildGraph();
	eq("schematic slice label names its role", (schematic.nodes.find(n => n.key === "s0-dev") || {}).label, "开发分片 A");
	eq("schematic slice gate label names its role", (schematic.nodes.find(n => n.key === "s0-gate") || {}).label, "开发分片 A · 分片门");
	check("schematic shows the terminal seal node", schematic.nodes.some(n => n.key === "v-seal" && n.op && n.terminal));
}
// Cross-slice dependency must resolve even when the depended-on slice sorts later.
{
	A.env.state = { runs: [{
		runId: "r1", status: "RUNNING", configuration: "工作",
		flow: { planConfirmed: true, developmentBatches: [
			{ id: "mb", owner: "master-merge", required: true },
			{ id: "za", owner: "z", required: false },
			{ id: "ab", owner: "a", required: false, dependsOn: ["za"] },
		] },
		slices: [
			{ id: "a", runId: "ca", status: "RUNNING", flow: { planConfirmed: true, developmentBatches: [{ id: "ab", required: true, dependsOn: ["za"], status: "QUEUED" }] } },
			{ id: "z", runId: "cz", status: "RUNNING", flow: { planConfirmed: true, developmentBatches: [{ id: "za", required: true, status: "TERMINAL" }] } },
		],
	}] };
	A.env.runID = "r1";
	A.env.work = { flowSelection: { disabled: [] }, qaModes: [] };
	const g = A.buildGraph();
	check("forward cross-slice edge resolves", g.edges.some(e => e.a === "sl-dev-z-za" && e.b === "sl-dev-a-ab"));
	A.env.runID = ""; A.env.state = { runs: [] };
}

// --- run labels: requirement name + time + status, full id only on tooltips ---
{
	eq("short id strips run- prefix and truncates", [A.shortRunId("run-20261019-abcdef123456"), A.shortRunId("panel-run-2"), A.shortRunId("")], ["20261019-abc", "panel-run-2", "?"]);
	const r = { runId: "run-20261019-abcdef123456", status: "RUNNING", startedAt: "2026-10-19T14:32:05Z", requirement: "requirements.md" };
	check("label leads with the requirement name and keeps status", A.runLabel(r).startsWith("requirements.md ·") && A.runLabel(r).endsWith("RUNNING"));
	check("opaque id fragment stays out of the label", !/abcdef/.test(A.runLabel(r)));
	check("label shows local time", /·\s*1?0-19\s+\d{2}:\d{2}\s*·/.test(A.runLabel(r)));
	check("terminal status reads as ended", A.runLabel({ runId: "x1", status: "COMPLETE", requirement: "requirements.md" }).endsWith("已结束"));
	check("run without a requirement falls back to short id", A.runLabel({ runId: "run-x1", status: "RUNNING" }) === "run x1 · RUNNING");
	check("missing start time just omits the when", A.runLabel({ runId: "x1", status: "RUNNING", requirement: "requirements.md" }) === "requirements.md · RUNNING");
	A.env.state = { runs: [{ runId: "run-abc123", status: "COMPLETE", requirement: "requirements.md" }] };
	check("runRef resolves listed run", A.runRef("run-abc123") === "requirements.md · 已结束");
	check("runRef falls back to short id", A.runRef("gone") === "run gone");
}
// --- （当前） tracks the run-bound config in run mode, the pointer otherwise ---
{
	A.env.state = { active: "默认", runs: [{ runId: "r1", status: "RUNNING", configuration: "工作" }] };
	A.env.runID = "r1";
	check("run mode: current is the bound config", A.currentName() === "工作");
	A.env.runID = "";
	check("library mode: current is the active pointer", A.currentName() === "默认");
	A.env.runID = null;
}

// --- backend option errors show a translated reason; raw text stays on the tooltip ---
{
	const raw = "RuntimeBlocked: UNSUPPORTED_NATIVE_HOST: devin; execution adapters: [dsh zcode]";
	eq("RuntimeBlocked wrapper strips, host code translates", A.optionsErrorText(raw), "该平台不支持原生执行通道");
	eq("UNREGISTERED_INSTALL translates", A.optionsErrorText("UNREGISTERED_INSTALL: run root does not match its admitted root"), "运行时未登记安装");
	eq("unavailable suffix translates", A.optionsErrorText("ZCODE_UNAVAILABLE: node or app-server script missing"), "平台可执行文件或脚本不可用");
	eq("capability suffix translates", A.optionsErrorText("RuntimeBlocked: DEVIN_CAPABILITY: ACP protocol version 1 required"), "平台能力不满足适配要求");
	eq("binding suffix translates", A.optionsErrorText("ZCODE_BINDING: missing or changed identity"), "会话绑定校验失败");
	eq("config suffix translates", A.optionsErrorText("DSH_CONFIG: model selection was not applied"), "平台配置未生效");
	eq("protocol suffix translates", A.optionsErrorText("CURSOR_PROTOCOL: handshake refused"), "平台协议不匹配");
	eq("unlisted code falls back", A.optionsErrorText("DSH_TRANSPORT: native ACP process closed its output"), "平台选项查询失败");
	eq("plain message falls back", A.optionsErrorText("dial tcp: connection refused"), "平台选项查询失败");
	global.document = { createElement: () => ({ children: [], appendChild(c) { this.children.push(c); } }) };
	const msg = A.optionsErrorEl(raw);
	eq("first line carries only the translated reason", msg.children[0].textContent, "暂取不到平台选项：该平台不支持原生执行通道");
	eq("manual-entry hint kept", msg.children[1].textContent, "可手工填写平台/模型/强度（原生值）。");
	check("raw error text stays on the tooltip", msg.title === raw);
	delete global.document;
}

// --- stale execution-option responses cannot poison another node's cache ---
(async () => {
	const calls = [];
	global.fetch = async path => {
		const u = new URLSearchParams(path.split("?")[1]);
		calls.push(u.get("node") + "|" + u.get("platform") + "|" + u.get("model"));
		// The development request resolves after the gate request.
		await new Promise(r => setTimeout(r, u.get("node") === "development" ? 40 : 0));
		return new Response(JSON.stringify({ effective: { provider: "p-" + u.get("node") } }), { status: 200 });
	};
	A.env.editing = "x";
	A.env.work = { execution: { gate: { provider: "gp", model: "gm" } } };
	A.env.sel = "development";
	const p1 = A.optionsFor();
	A.env.sel = "gate";
	const r2 = await A.optionsFor();
	const r1 = await p1;
	check("requests carry each node's own tuple", calls.some(c => c === "development||") && calls.some(c => c === "gate|gp|gm"));
	check("newer selection gets its own catalogue", r2 && r2.effective.provider === "p-gate");
	check("stale response caches under its own tuple", A.optCache["x|development||"] && A.optCache["x|development||"].effective.provider === "p-development");
	check("gate result cached under its tuple", A.optCache["x|gate|gp|gm"] && A.optCache["x|gate|gp|gm"].effective.provider === "p-gate");
	check("stale caller reads the current tuple's cache", !r1 || !r1.effective || r1.effective.provider !== "p-development");
	A.env.sel = null; A.env.work = {};
	for (const k of Object.keys(A.optCache)) delete A.optCache[k];

	// --- each (provider, model) tuple owns one cached catalogue answer ---
	const mcalls = [];
	global.fetch = async path => {
		const u = new URLSearchParams(path.split("?")[1]);
		const sent = u.get("model") || "";
		mcalls.push(sent || "(none)");
		// Native catalogues fill effort options only for the queried model.
		const filled = sent || "m0";
		return new Response(JSON.stringify({
			effective: { provider: "zp", model: "m0" },
			platforms: ["zp"], platform: "zp",
			catalogue: { models: [
				{ value: "m0", efforts: filled === "m0" ? ["e0"] : null },
				{ value: "m1", efforts: filled === "m1" ? ["e1a", "e1b"] : null, defaultEffort: filled === "m1" ? "e1a" : "" },
				{ value: "m2", efforts: filled === "m2" ? ["e2"] : null },
			], "default": { model: "m0" } },
		}), { status: 200 });
	};
	A.env.editing = "y";
	A.env.sel = "development";
	A.env.work = { execution: { development: { provider: "zp", model: "m1" } } };
	await A.optionsFor();
	eq("first visit queries the current model", mcalls, ["m1"]);
	A.setExec(A.env.work, "development", { model: "m0" });
	await A.optionsFor();
	A.setExec(A.env.work, "development", { model: "m2" });
	await A.optionsFor();
	eq("each model tuple fires its own query", mcalls, ["m1", "m0", "m2"]);
	const effortsOf = (entry, v) => ((((entry || {}).catalogue || {}).models || []).find(m => m.value === v) || {}).efforts;
	const e0 = A.optCache["y|development|zp|m0"], e1 = A.optCache["y|development|zp|m1"], e2 = A.optCache["y|development|zp|m2"];
	check("each tuple caches its own answer", !!e0 && !!e1 && !!e2 && e0 !== e1 && e1 !== e2);
	eq("a tuple's efforts stay inside its own answer", [effortsOf(e0, "m0"), effortsOf(e0, "m1"), effortsOf(e0, "m2")], [["e0"], null, null]);
	eq("another tuple carries its own model's efforts", [effortsOf(e1, "m1"), effortsOf(e2, "m2")], [["e1a", "e1b"], ["e2"]]);
	A.setExec(A.env.work, "development", { model: "m1" });
	const back = await A.optionsFor();
	check("switching back reuses the tuple's cached answer", back === e1);
	eq("no refetch for a cached tuple", mcalls, ["m1", "m0", "m2"]);

	// A repeated call while the tuple's fetch is in flight shares it.
	A.setExec(A.env.work, "development", { model: "m9" });
	const pa = A.optionsFor(), pb = A.optionsFor();
	const [ra, rb] = await Promise.all([pa, pb]);
	check("in-flight tuple shares one fetch", mcalls.filter(c => c === "m9").length === 1 && ra === rb);

	// A failed tuple caches its own error without touching sibling answers.
	A.setExec(A.env.work, "development", { model: "bad" });
	global.fetch = async () => new Response("boom", { status: 500 });
	const failed = await A.optionsFor();
	check("failed tuple resolves to its own error entry", !!failed && !!failed.error && A.optCache["y|development|zp|bad"] === failed);
	A.setExec(A.env.work, "development", { model: "m1" });
	const after = await A.optionsFor();
	check("a sibling tuple's cached answer survives the failure", after === e1);
	A.env.sel = null; A.env.work = {};
	for (const k of Object.keys(A.optCache)) delete A.optCache[k];
	delete global.fetch;

	// --- a dead backend ends polling in the closed state instead of retrying forever ---
	const bannerEl = { className: "", textContent: "", appended: [], appendChild(c) { this.appended.push(c); }, classList: { remove() {} } };
	let closedText = null;
	global.document = {
		createElement: () => ({}),
		querySelector: () => bannerEl,
		body: { textContent: "", appendChild(c) { closedText = c.textContent; } },
	};
	{
		A.env.state = null; A.env.runID = null; A.env.gone = false; A.env.downSince = 0;
		let fetches = 0;
		global.fetch = async () => { fetches++; throw new Error("connection refused"); };
		await A.poll();
		check("first failure keeps retrying under the deadline", !A.env.gone && closedText === null);
		check("failure shows the retry banner", bannerEl.appended.length === 1);
		A.env.downSince = Date.now() - A.POLL_DEAD_MS - 1;
		await A.poll();
		check("failure streak past the deadline closes the window", A.env.gone === true);
		eq("closed text explains the service stopped", closedText, "面板服务已停止（连接持续失败），此窗口可关闭。");
		await A.poll();
		check("closed window stops sending requests", fetches === 2);
	}
	{
		// A null state payload keeps the successful poll out of the DOM paths;
		// a set baselineRev keeps loadState from reading state.revisions.
		A.env.state = null; A.env.runID = null; A.env.gone = false;
		A.env.baselineRev = { user: "u", project: "p" };
		A.env.downSince = Date.now() - A.POLL_DEAD_MS - 1;
		global.fetch = async () => new Response("null", { status: 200 });
		await A.poll();
		check("a successful poll clears the failure streak", A.env.downSince === 0 && !A.env.gone);
		global.fetch = async () => { throw new Error("still refusing"); };
		bannerEl.appended.length = 0;
		await A.poll();
		check("a failure after recovery starts a fresh streak", !A.env.gone && bannerEl.appended.length === 1);
	}
	delete global.fetch; delete global.document;

	console.log(failures === 0 ? "ALL OK" : failures + " FAILURES");
	process.exitCode = failures === 0 ? 0 : 1;
})().catch(e => { console.log("FAIL async driver: " + (e && e.message)); process.exitCode = 1; });
