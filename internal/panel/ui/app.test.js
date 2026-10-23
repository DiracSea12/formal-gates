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

// --- builtin presentation follows the configuration library source ---
{
	check("default-source configurations are builtin regardless of name", A.isBuiltin({ name: "全量路线", source: "default" }));
	check("saved configurations are writable regardless of display name", !A.isBuiltin({ name: "默认", source: "/project/config.json", scope: "project" }));
	eq("builtin scope label comes from source", A.scopeLabel({ name: "全量路线", source: "default" }), "内置");
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
	check("schematic batch gates separate consecutive development", g.edges.some(e => e.a === "n-dev-0" && e.b === "n-gate-0") && g.edges.some(e => e.a === "n-gate-0" && e.b === "n-dev-1") && g.edges.some(e => e.a === "n-dev-1" && e.b === "n-gate-1"));
}
{
	A.env.state = { runs: [{
		runId: "r1", status: "RUNNING", configuration: "工作",
		flow: { planConfirmed: true, developmentBatches: [
			{ id: "b1", required: true, status: "COMPLETE", gate: { status: "PASS", selected: ["quality"] } },
			{ id: "b2", required: true, dependsOn: ["b1"], status: "RUNNING", gate: { status: "QUEUED", selected: ["quality"] } },
			{ id: "sb1", owner: "core", required: false },
			{ id: "sb2", owner: "core", required: false, dependsOn: ["sb1"] },
		], enteredMainlineSlices: ["core"] },
		slices: [{ id: "core", runId: "c1", status: "RUNNING", enteredMainline: true,
			flow: { planConfirmed: true, developmentBatches: [
				{ id: "sb1", required: true, status: "COMPLETE", gate: { status: "PASS", selected: ["quality"] } },
				{ id: "sb2", required: true, dependsOn: ["sb1"], status: "RUNNING", gate: { status: "RUNNING", selected: ["quality"] } },
			] },
		}],
	}] };
	A.env.runID = "r1";
	A.env.work = { flowSelection: { disabled: [] }, qaModes: ["whitebox"] };
	const g = A.buildGraph();
	const keys = new Set(g.nodes.map(n => n.key));
	const edge = (a, b) => g.edges.some(e => e.a === a && e.b === b);
	check("rooted master batch hangs off readiness", edge("n-start-readiness", "n-dev-b1"));
	check("master batch gate precedes dependent implementation", edge("n-dev-b1", "n-gate-b1") && edge("n-gate-b1", "n-dev-b2") && !edge("n-dev-b1", "n-dev-b2"));
	check("master batch gate uses its engine status", g.nodes.find(n => n.key === "n-gate-b1").gateStatus === "PASS" && g.nodes.find(n => n.key === "n-gate-b2").gateStatus === "QUEUED");
	check("slice batches rendered from child flow", keys.has("sl-dev-core-sb1") && keys.has("sl-dev-core-sb2"));
	check("slice batch gate precedes dependent implementation", edge("sl-dev-core-sb1", "sl-batch-gate-core-sb1") && edge("sl-batch-gate-core-sb1", "sl-dev-core-sb2") && !edge("sl-dev-core-sb1", "sl-dev-core-sb2"));
	check("slice batch gate uses its engine status", g.nodes.find(n => n.key === "sl-batch-gate-core-sb2").gateStatus === "RUNNING");
	check("slice owns its gate", keys.has("sl-gate-core") && edge("sl-batch-gate-core-sb2", "sl-gate-core"));
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
	check("forward cross-slice gate edge resolves", g.edges.some(e => e.a === "sl-batch-gate-z-za" && e.b === "sl-dev-a-ab"));
	A.env.runID = ""; A.env.state = { runs: [] };
}

// Independent batches have their own gates and retain parallel readiness.
{
	A.env.state = { runs: [{ runId: "parallel", configuration: "工作", flow: { planConfirmed: true, developmentBatches: [
		{ id: "left", required: true, gate: { status: "DISABLED", selected: [] } },
		{ id: "right", required: true, gate: { status: "NOT_APPLICABLE", selected: [] } },
	] } }] };
	A.env.runID = "parallel";
	A.env.work = { flowSelection: { disabled: ["gate"] }, qaModes: ["blackbox"] };
	const g = A.buildGraph();
	const edge = (a, b) => g.edges.some(e => e.a === a && e.b === b);
	check("independent batches both leave readiness", edge("n-start-readiness", "n-dev-left") && edge("n-start-readiness", "n-dev-right"));
	check("independent batch gates both feed final verification", edge("n-gate-left", "v-gate") && edge("n-gate-right", "v-gate"));
	check("independent batches are not serialized by the diagram", !edge("n-gate-left", "n-dev-right") && !edge("n-gate-right", "n-dev-left"));
	check("disabled and spike gate projection stays explicit", g.nodes.find(n => n.key === "n-gate-left").gateStatus === "DISABLED" && g.nodes.find(n => n.key === "n-gate-right").gateStatus === "NOT_APPLICABLE");
	for (const [status, label] of Object.entries({ DISABLED: "未启用", NOT_APPLICABLE: "不适用", QUEUED: "待验证", RUNNING: "运行中", PASS: "已通过", FAIL: "未通过" })) eq("batch gate status " + status, A.nodeStatusText("gate", status), label);
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

// --- effort labels prefer execution defaults and native configuration ---
{
	global.document = { createElement: () => ({ children: [], dataset: {}, appendChild(c) { this.children.push(c); } }) };
	const answer = {
		effective: { provider: "p", reasoningEffort: "formal-effort" },
		catalogue: { models: [{ value: "m", efforts: ["low", "high"], defaultEffort: "low" }, { value: "other", efforts: ["low", "high"], defaultEffort: "low" }], default: { model: "m", effort: "high" } },
	};
	const buttons = (options, ex) => {
		const row = document.createElement("div");
		A.fillEfforts(row, options, ex, false, false);
		return row.children;
	};
	eq("formal execution effort precedes native configuration", buttons(answer, {})[0].textContent, "跟随默认（formal-effort）");
	const native = { ...answer, effective: { provider: "p" } };
	const following = buttons(native, {});
	eq("native configured high precedes recommended low", following[0].textContent, "跟随默认（high）");
	check("following defaults highlights only the empty choice", following[0].className === "on" && !following[2].className);
	eq("changing model keeps native configured effort", buttons(native, { model: "other" })[0].textContent, "跟随默认（high）");
	const explicit = buttons(native, { reasoningEffort: "low" });
	check("explicit effort remains selected ahead of the follow choice", !explicit[0].className && explicit[1].className === "on");
	const otherPlatform = { ...answer, platform: "other-platform", catalogue: { ...answer.catalogue, default: { model: "m", effort: "other-high" } } };
	eq("another platform clears formal effort and uses its own native default", buttons(otherPlatform, { provider: "other-platform" })[0].textContent, "跟随默认（other-high）");
	const recommended = { ...native, catalogue: { ...native.catalogue, default: { model: "m" } } };
	eq("model recommendation fills an unreported native effort", buttons(recommended, {})[0].textContent, "跟随默认（low）");
	eq("unreported native facts keep the default unspecified", buttons({ effective: {}, catalogue: { models: [] } }, {})[0].textContent, "跟随默认");
	delete global.document;
}

// --- idle run projections leave selection labels to the current draft ---
{
	const node = { node: "repair", enabled: false, editable: true };
	A.env.state = { runs: [{ runId: "labels", configuration: "labels", status: "RUNNING", nodes: [node] }] };
	A.env.editing = "labels"; A.env.runID = "labels";
	A.env.work = {};
	eq("idle disabled projection supplies no runtime label", A.nodeStatusText("repair"), "");
	check("bound idle draft starts selected", A.nodeEnabled(A.env.work, "repair"));
	A.setNodeEnabled(A.env.work, "repair", false);
	check("bound idle draft can be unselected", !A.nodeEnabled(A.env.work, "repair") && A.nodeStatusText("repair") === "");
	A.setNodeEnabled(A.env.work, "repair", true);
	check("bound idle draft can be selected again", A.nodeEnabled(A.env.work, "repair") && A.nodeStatusText("repair") === "");
	for (const [flag, label] of [["running", "运行中"], ["closing", "关闭中"], ["completed", "已完成"], ["bypassed", "已越过"]]) {
		node[flag] = true;
		eq("runtime " + flag + " retains its label", A.nodeStatusText("repair"), label);
		delete node[flag];
	}
	A.env.runID = "";
}

// --- shared catalogues preserve node defaults and isolate native models ---
(async () => {
	const calls = [];
	global.fetch = async path => {
		const u = new URLSearchParams(path.split("?")[1]);
		calls.push([u.get("configuration"), u.get("node"), u.get("platform"), u.get("model")]);
		const sent = u.get("model") || "m0";
		await new Promise(r => setTimeout(r, sent === "slow" ? 40 : 0));
		if (sent === "bad") return new Response("boom", { status: 500 });
		return new Response(JSON.stringify({
			effective: { provider: "zp", model: "m0", reasoningEffort: "run-effort" },
			platforms: ["zp", "other"], platform: u.get("platform") || "zp",
			catalogue: { models: ["m0", "m1", "m2", "slow"].map(value => ({
				value, efforts: sent === value ? [value + "-low", value + "-high"] : null,
				defaultEffort: sent === value ? value + "-low" : "",
			})), "default": { model: "m0", effort: "m0-low" } },
		}), { status: 200 });
	};
	A.env.state = { revisions: { user: "u1", project: "p1" } };
	A.env.editing = "x";
	A.env.work = {};
	for (const node of A.RESP) { A.env.sel = node.id; await A.optionsFor(); }
	eq("all default nodes share one native request", calls, [["默认", "development", "", ""]]);
	A.env.editing = "another configuration";
	check("another configuration shares the default answer", (await A.optionsFor()).effective.model === "m0" && calls.length === 1);
	const defaults = await A.optionsFor();
	eq("shared answer retains run defaults", A.effectiveChoice(defaults, {}), { provider: "zp", model: "m0", reasoningEffort: "run-effort" });
	eq("node overrides keep inherited effort on the same platform", A.effectiveChoice(defaults, { provider: "zp", model: "m1", reasoningEffort: "" }), { provider: "zp", model: "m1", reasoningEffort: "run-effort" });
	eq("another platform clears inherited native model and effort", A.effectiveChoice(defaults, { provider: "other" }), { provider: "other" });
	eq("explicit node effort remains its own value", A.effectiveChoice(defaults, { reasoningEffort: "chosen" }).reasoningEffort, "chosen");

	A.env.sel = "development";
	A.env.work = { execution: { development: { provider: "zp", model: "m1" }, gate: { provider: "zp", model: "m1", reasoningEffort: "chosen" } } };
	const pa = A.optionsFor();
	A.env.sel = "gate";
	const pb = A.optionsFor();
	const [, e1] = await Promise.all([pa, pb]);
	eq("different nodes share an in-flight model query", calls.filter(c => c[3] === "m1").length, 1);
	check("cached model answer shared across node effort overrides", await A.optionsFor() === e1);
	const effortsOf = (entry, v) => entry.catalogue.models.find(m => m.value === v).efforts;
	eq("only the requested model carries efforts", [effortsOf(e1, "m0"), effortsOf(e1, "m1")], [null, ["m1-low", "m1-high"]]);
	A.setExec(A.env.work, "gate", { model: "m2" });
	const e2 = await A.optionsFor();
	eq("different model gets its own efforts", effortsOf(e2, "m2"), ["m2-low", "m2-high"]);
	A.setExec(A.env.work, "gate", { model: "m1" });
	check("revisiting a model reuses its answer", await A.optionsFor() === e1);

	A.setExec(A.env.work, "gate", { model: "slow" });
	const slowKey = A.sigOptKey(), late = A.optionsFor();
	A.setExec(A.env.work, "gate", { model: "m2" });
	await A.optionsFor();
	check("late model cannot replace current answer", await late === e2 && effortsOf(A.optCache[slowKey], "slow")[0] === "slow-low");
	A.setExec(A.env.work, "gate", { model: "bad" });
	const failed = await A.optionsFor();
	check("failed model caches its own error", !!failed.error && A.optCache[A.sigOptKey()] === failed);
	A.setExec(A.env.work, "gate", { model: "m1" });
	check("model error preserves sibling catalogue", await A.optionsFor() === e1);

	A.setExec(A.env.work, "gate", { provider: "other", model: "m1" });
	const other = await A.optionsFor();
	check("platforms have separate cache entries", other !== e1 && calls.filter(c => c[2] === "other" && c[3] === "m1").length === 1);
	A.setExec(A.env.work, "gate", { model: "slow" });
	const oldKey = A.sigOptKey(), oldRequest = A.optionsFor();
	A.env.state.revisions.project = "p2";
	const freshKey = A.sigOptKey(), freshRequest = A.optionsFor();
	await Promise.all([oldRequest, freshRequest]);
	check("revision change refetches without accepting an old cache entry", oldKey !== freshKey && A.optCache[oldKey] !== A.optCache[freshKey] && calls.filter(c => c[2] === "other" && c[3] === "slow").length === 2);
	// Manual refresh replaces the revision's directory answers and supersedes
	// older native replies without letting them remove the new pending owner.
	const refreshCalls = [];
	global.fetch = (path, init) => new Promise(resolve => refreshCalls.push({ path, init, resolve }));
	A.env.sel = "gate";
	A.env.work = { execution: { gate: { provider: "refresh", model: "model", reasoningEffort: "chosen" }, development: { provider: "refresh", model: "model" } } };
	const draftBefore = JSON.stringify(A.env.work);
	const old = A.optionsFor(), refreshKey = A.sigOptKey();
	const fresh = A.optionsFor(true);
	eq("manual refresh starts one replacement request", refreshCalls.length, 2);
	refreshCalls[0].resolve(new Response(JSON.stringify({ catalogue: { models: [] }, marker: "old" }), { status: 200 }));
	await old;
	check("superseded reply cannot refill the cache", A.optCache[refreshKey] === undefined);
	A.env.sel = "development";
	const sharedRefresh = A.optionsFor();
	eq("another node joins the refreshed in-flight request", refreshCalls.length, 2);
	refreshCalls[1].resolve(new Response(JSON.stringify({ catalogue: { models: [] }, marker: "fresh" }), { status: 200 }));
	await Promise.all([fresh, sharedRefresh]);
	eq("replacement remains the shared authoritative answer", A.optCache[refreshKey].marker, "fresh");
	eq("manual refresh preserves all draft values", JSON.stringify(A.env.work), draftBefore);
	const failRefresh = A.optionsFor(true);
	refreshCalls[2].resolve(new Response(JSON.stringify({ error: "native unavailable" }), { status: 500 }));
	check("failed refresh exposes its own error", !!(await failRefresh).error);
	const retryRefresh = A.optionsFor(true);
	refreshCalls[3].resolve(new Response(JSON.stringify({ catalogue: { models: [] }, marker: "recovered" }), { status: 200 }));
	eq("manual refresh can retry a failed directory query", (await retryRefresh).marker, "recovered");
	check("force refresh only sends catalogue reads", refreshCalls.length === 4 && refreshCalls.every(c => c.path.startsWith("/api/execution-options?") && (!c.init || !c.init.method || c.init.method === "GET")));
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
