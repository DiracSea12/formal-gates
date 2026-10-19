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
	check("stale response never enters the cache", A.optCache["x|development||"] === undefined);
	check("gate result cached under its tuple", A.optCache["x|gate|gp|gm"] && A.optCache["x|gate|gp|gm"].effective.provider === "p-gate");
	check("stale caller reads the current tuple's cache", !r1 || !r1.effective || r1.effective.provider !== "p-development");
	A.env.sel = null; A.env.work = {};
	delete global.fetch;

	console.log(failures === 0 ? "ALL OK" : failures + " FAILURES");
	process.exitCode = failures === 0 ? 0 : 1;
})().catch(e => { console.log("FAIL async driver: " + (e && e.message)); process.exitCode = 1; });
