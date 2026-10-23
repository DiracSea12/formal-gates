// DOM-driving script appended to /app.js by the ui_dom_test middleware only.
// It exercises real user event flows and reports PASS/FAIL/SKIP lines into a
// <pre id="domresult"> node that --dump-dom serializes. A line is a result
// only when its predicate was actually evaluated.
(async () => {
const R = [];
let step="init";
const fin0 = () => { let p = q("#domresult"); if (!p) { p = document.createElement("pre"); p.id = "domresult"; document.body.appendChild(p); } p.textContent = "RESULT\n" + R.join("\n"); };
const ok = (n, c) => { R.push((c ? "PASS" : "FAIL") + " " + n); fin0(); };
const skip = n => { R.push("SKIP " + n); fin0(); };
const sleep = ms => new Promise(r => setTimeout(r, ms));
const pace = () => fetch("/api/state").catch(() => {});
const until = async (fn, n = 120) => { for (let i = 0; i < n; i++) { const v = await fn(); if (v) return v; await pace(); await sleep(50); } return null; };
const untilReal = async (fn, n) => { for (let i = 0; i < n; i++) { const v = await fn(); if (v) return v; await pace(); await pace(); await sleep(50); } return null; };
const must = async (fn, name, n = 120) => { const v = await until(fn, n); ok(name, !!v); return v; };
const q = s => document.querySelector(s);
const row = name => [...document.querySelectorAll(".cfg")].find(r => { const n = r.querySelector(".n span"); return n && n.textContent === name; });
const click = e => e && e.dispatchEvent(new MouseEvent("click", { bubbles: true }));
const fire = (e, t) => e.dispatchEvent(new Event(t, { bubbles: true }));
const setVal = (e, v) => { const p = Object.getPrototypeOf(e); const d = Object.getOwnPropertyDescriptor(p, "value"); d.set.call(e, v); fire(e, "input"); };
const btn = (root, text) => root && [...root.querySelectorAll("button")].find(b => b.textContent.trim().startsWith(text));
const esc = () => document.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true }));
const fin = () => { R.push("END"); fin0(); };
const state = async () => (await fetch("/api/state")).json();
const ready = () => fetch("/__ready", { method: "POST" }).catch(() => {});
const nodeByResp = resp => document.querySelector('.node[data-resp="' + resp + '"]');
const pickShown = () => { const a = q("#apply"); return a && a.style.display !== "none" && a.textContent === "选好了"; };
const dirtyShown = () => { const d = q("#dirtyBadge"); return d && d.style.display !== "none"; };
const insideViewport = (elm, name) => {
	if (!elm) { ok(name + " present", false); return; }
	const r = elm.getBoundingClientRect();
	ok(name + " fully inside viewport (L" + Math.round(r.left) + " T" + Math.round(r.top) + " R" + Math.round(r.right) + " B" + Math.round(r.bottom) + ")", r.left >= -1 && r.top >= -1 && r.right <= window.innerWidth + 1 && r.bottom <= window.innerHeight + 1);
};

const params = new URLSearchParams(location.search);

if (params.get("probe") === "bounds") {
	// Settle until the element's rect stops changing, so slide/fade transitions
	// cannot produce mid-animation bounds.
	const settle = async (elm, tries) => {
		let prev = null;
		for (let i = 0; i < (tries || 60); i++) {
			const r = elm.getBoundingClientRect();
			const s = [r.left, r.top, r.right, r.bottom].join(",");
			if (s === prev) return r;
			prev = s; await sleep(120);
		}
		return elm.getBoundingClientRect();
	};
	const inside = async (elm, name) => {
		if (!elm) { ok(name + " present", false); return; }
		const r = await settle(elm);
		ok(name + " fully inside viewport (L" + Math.round(r.left) + " T" + Math.round(r.top) + " R" + Math.round(r.right) + " B" + Math.round(r.bottom) + ")",
			r.left >= -1 && r.top >= -1 && r.right <= window.innerWidth + 1 && r.bottom <= window.innerHeight + 1);
	};
	await until(() => document.querySelectorAll(".node").length >= 1);
	const wantW = parseInt(params.get("wantW") || "390"), wantH = parseInt(params.get("wantH") || "844");
	ok("viewport is exactly " + wantW + "x" + wantH + " css px (got " + window.innerWidth + "x" + window.innerHeight + ")", window.innerWidth === wantW && window.innerHeight === wantH);
	ok("no horizontal overflow (scrollWidth=" + document.documentElement.scrollWidth + ")", document.documentElement.scrollWidth <= window.innerWidth + 1);
	const boxes = [...document.querySelectorAll(".node")].map(n => ({ x: parseFloat(n.style.left), y: parseFloat(n.style.top), w: n.offsetWidth, h: n.offsetHeight }));
	ok("graph cards do not overlap at narrow viewport", boxes.every((a, i) => boxes.slice(i + 1).every(b => a.x + a.w <= b.x || b.x + b.w <= a.x || a.y + a.h <= b.y || b.y + b.h <= a.y)));
	const footerButtons = [...document.querySelectorAll(".footer button")].filter(b => b.getClientRects().length > 0);
	ok("primary footer action is visible", footerButtons.length > 0 && footerButtons.includes(q("#apply")));
	if (BOOT.mode !== "intake" && selectedRun()) ok("run footer hides standalone save and keeps one apply action", q("#save").getClientRects().length === 0 && footerButtons.length === 1);
	else ok("library and intake expose save and apply", footerButtons.includes(q("#save")) && footerButtons.length === 2);
	for (const b of footerButtons) await inside(b, "footer button " + b.textContent.trim().slice(0, 8));
	await inside(q(".win"), ".win itself");
	await inside(q(".footer"), "footer");
	await inside(q("#winClose"), "window close button");
	await inside(q("#runSel"), "run selector");
	const dr = q(".drawer");
	ok("drawer open at measure time", !!(dr && dr.classList.contains("open")));
	await inside(dr, "drawer " + (dr && dr.className));
	// Close the drawer: its scrim legitimately owns clicks while open.
	click(q("#scrim"));
	await until(() => !q(".drawer.open"), 60);
	// In-viewport is not enough: buttons must be clickable through their
	// clipping ancestors too.
	for (const b of footerButtons) {
		await inside(b, "footer button after drawer closes " + b.textContent.trim().slice(0, 8));
		const r = b.getBoundingClientRect();
		const tag = b.textContent.trim().slice(0, 8);
		// Edge midpoints avoid border-radius corners while still covering
		// every side of the painted area.
		const pts = [["center", r.left + r.width / 2, r.top + r.height / 2],
			["top", r.left + r.width / 2, r.top + 1], ["bottom", r.left + r.width / 2, r.bottom - 1],
			["left", r.left + 1, r.top + r.height / 2], ["right", r.right - 1, r.top + r.height / 2]];
		for (const [where, x, y] of pts) {
			const hit = document.elementFromPoint(x, y);
			const label = hit ? (hit.id || hit.className || hit.tagName) : "null";
			ok("footer button " + tag + " hit@" + where + " (" + String(label).slice(0, 30) + ", rect " + [r.left, r.top, r.right, r.bottom].map(Math.round).join(",") + ")",
				hit === b || (hit && b.contains(hit)) || (hit && hit.closest && hit.closest("button") === b));
		}
	}
	click(nodeByResp("gate"));
	await until(() => q(".inspector input"), 60);
	await inside(q(".inspector"), "inspector open " + (q(".inspector") && q(".inspector").className));
	fin();
	return;
}

if (params.get("probe") === "badjson") {
	step = "badjson error banner";
	const b = await until(() => q(".banner.show"), 300);
	ok("invalid config shows real error", !!(b && /读取失败|json|JSON|invalid|格式/i.test(b.textContent)));
	await ready(); // the test restores valid bytes now
	step = "in-place recovery";
	const t = await untilReal(() => { const e = q("#cfgTitle"); return e && e.textContent === "修复后" ? e : null; }, 900);
	ok("repaired config loads in place", !!t);
	ok("graph nodes render after recovery", document.querySelectorAll(".node").length >= 1);
	fin();
	return;
}

if (params.get("probe") === "revisions") {
	await until(() => document.querySelectorAll(".node").length >= 1);
	step = "dirty draft before external edit";
	click(nodeByResp("repair"));
	click(await until(() => q(".inspector .tg")));
	ok("draft dirty before external edit", (await until(() => dirtyShown())) !== null);
	await ready(); // the test performs a legitimate external settings edit now
	step = "conflict banner";
	const conflict = await untilReal(() => { const b = q(".banner.show"); return b && /别处被修改/.test(b.textContent) ? b : null; }, 900);
	ok("external edit shows conflict banner naming a layer", !!conflict && /用户层|项目层/.test(conflict.textContent));
	const sExt = await state();
	ok("external value visible in state", !!(sExt.effective && sExt.effective.values && sExt.effective.values.panelIdleTimeout === 77));
	step = "stale flag edit rejected";
	const errText = () => ((q(".banner.show") || {}).textContent || "") + "|" + ((q("#msg") || {}).textContent || "");
	click(q("#setrow input[type=checkbox]"));
	const staleToast = await until(() => /changed since|已更改|修订|别处被修改/i.test(errText()) ? errText() : null, 120);
	ok("stale flag edit surfaces conflict", !!staleToast);
	const sSel = await state();
	ok("rejected flag edit left server value", !!(sSel.effective && sSel.effective.values && sSel.effective.values.panelSelection === true));
	await until(() => q("#setrow input[type=checkbox]") && q("#setrow input[type=checkbox]").checked === true, 60);
	ok("rejected checkbox reverted by re-render", !!(q("#setrow input[type=checkbox]") && q("#setrow input[type=checkbox]").checked === true));
	step = "stale config save rejected";
	click(q("#save"));
	await until(() => !!q("#saveBar .savebar"), 60);
	const targetName = q("#cfgTitle").textContent;
	click(row(targetName));
	const saveErr = await until(() => /changed since|已更改|修订|别处被修改/i.test(errText()) ? errText() : null, 180);
	ok("stale config save rejected", !!saveErr);
	ok("draft kept after rejected save", dirtyShown() && q("#cfgTitle").textContent === targetName);
	const sKeep = await state();
	const keepCfg = (sKeep.configurations || []).find(c => c.name === targetName);
	ok("stored bytes kept external edit", !!(keepCfg && !(keepCfg.configuration.flowSelection && (keepCfg.configuration.flowSelection.disabled || []).includes("repair"))));
	step = "explicit overwrite mine";
	click(btn(q(".banner.show"), "用我的覆盖"));
	ok("overwrite mine clears dirty", (await until(() => !dirtyShown(), 180)) !== null);
	const sDone = await state();
	const doneCfg = (sDone.configurations || []).find(c => c.name === targetName);
	ok("stored draft applied", !!(doneCfg && doneCfg.configuration.flowSelection && (doneCfg.configuration.flowSelection.disabled || []).includes("repair")));
	ok("external edit still present", !!(sDone.effective && sDone.effective.values && sDone.effective.values.panelIdleTimeout === 77));
	fin();
	return;
}

if (params.get("probe") === "runs") {
	await until(() => document.querySelectorAll(".node").length >= 1);
	const s0 = await state();
	const runs = (s0.runs || []).filter(r => r.status !== "COMPLETE" && r.status !== "ABORTED");
	ok("two live runs listed", runs.length === 2);
	if (runs.length !== 2) { fin(); return; }
	const sel0 = q("#runSel");
	ok("run selector lists both runs", !!sel0 && sel0.options.length === 3);
	ok("run selector labels each run by its requirement", !!sel0 && [...sel0.options].every(o => o.value === "" || (/requirements\.md ·/.test(o.textContent) && o.title === o.value)));
	const shown = q("#cfgTitle").textContent;
	const bound = runs.find(r => r.configuration === shown);
	ok("initial view shows a bound config", !!bound);
	const other = runs.find(r => r !== bound);
	step = "run switch loads bound config";
	sel0.value = other.runId; fire(sel0, "change");
	ok("run switch loads bound config", (await until(() => q("#cfgTitle").textContent === other.configuration)) !== null);
	ok("run badge shows the requirement label, not the raw id", (await until(() => { const ri = q("#runinfo"); return ri && /requirements\.md · \d{1,2}-\d{1,2} \d{2}:\d{2} · /.test(ri.textContent) && !ri.textContent.includes(other.runId) ? ri : null; })) !== null);
	ok("full run id kept in tooltip", !!(q("#runinfo [title]") && q("#runinfo [title]").title === other.runId));
	ok("selector option carries the requirement label", !!sel0 && [...sel0.options].some(o => o.value === other.runId && /requirements\.md ·/.test(o.textContent)));
	ok("（当前） badge marks the bound config in run mode", !!(row(other.configuration) && row(other.configuration).querySelector(".curmark")));
	ok("（当前） badge not left on the pointer target", !(row(bound.configuration) && row(bound.configuration).querySelector(".curmark")));
	ok("header badge follows the current config", q("#curBadge") && q("#curBadge").style.display !== "none" && q("#curBadge").textContent === "（当前）");
	step = "locked node read-only on bound config";
	const lockedNode = (other.nodes || []).find(n => !n.editable);
	if (lockedNode) {
		click(nodeByResp(lockedNode.node));
		const tg = await until(() => q(".inspector .tg"), 60);
		ok("locked node control disabled", !!tg && tg.disabled === true);
		esc(); // close inspector
	} else {
		skip("locked node read-only on bound config");
	}
	step = "disabled node keeps a usable enable switch";
	click(nodeByResp("qa-blackbox"));
	const tgOff = await until(() => q(".inspector .tg"), 60);
	ok("disabled node keeps a usable enable switch", !!tgOff && tgOff.disabled === false);
	ok("disabled node not reported as ended", !/已经结束/.test((q("#insp") || {}).textContent || ""));
	esc();
	await until(() => !q(".inspector.open"), 60);
	step = "dirty run switch guarded";
	if (await until(() => q(".inspector .tg"), 30) === null) {
		click(nodeByResp("qa-blackbox"));
		await until(() => q(".inspector .tg"), 60);
	}
	click(q(".inspector .tg"));
	ok("edit marks dirty", (await until(() => dirtyShown())) !== null);
	const sel1 = q("#runSel");
	sel1.value = bound.runId; fire(sel1, "change");
	const dlg = await until(() => q(".dlg"), 60);
	ok("dirty run switch guarded", !!(dlg && btn(dlg, "取消") && btn(dlg, "放弃修改")));
	click(btn(dlg, "取消"));
	await until(() => !q(".dlg"), 60);
	ok("cancel keeps draft and run", dirtyShown() && q("#cfgTitle").textContent === other.configuration && q("#runSel").value === other.runId);
	step = "discard switches bound config";
	const sel2 = q("#runSel");
	sel2.value = bound.runId; fire(sel2, "change");
	const dlg2 = await until(() => q(".dlg"), 60);
	click(btn(dlg2, "放弃修改"));
	ok("discard switches to other bound config", (await until(() => q("#cfgTitle").textContent === bound.configuration && !dirtyShown())) !== null);
	fin();
	return;
}

if (params.get("probe") === "savecancel") {
	await until(() => document.querySelectorAll(".node").length >= 1);
	for (const variant of ["esc", "scrim"]) {
		step = variant + " cancels pending continuation";
		if (!dirtyShown()) { click(nodeByResp("repair")); click(await until(() => q(".inspector .tg"))); await until(() => dirtyShown()); }
		const home = q("#cfgTitle").textContent;
		const away = home === "默认" ? "工作" : "默认";
		if (!q(".drawer.open")) click(q("#libBtn"));
		click(row(away));
		const dlg = await until(() => q(".dlg"), 60);
		click(btn(dlg, "保存"));
		await until(() => !!q("#saveBar .savebar"), 90);
		if (variant === "esc") esc(); else click(q("#scrim"));
		await until(() => !q(".drawer.open"), 60);
		// An unrelated save of the same draft must not run the cancelled
		// continuation (switching to the other configuration).
		click(q("#save"));
		await until(() => !!q("#saveBar .savebar"), 60);
		click(row(home));
		await until(() => !dirtyShown(), 180);
		ok(variant + ": cancelled continuation did not fire", q("#cfgTitle").textContent === home);
		if (variant === "esc") { // dirty again for the scrim pass
			click(nodeByResp("repair")); click(await until(() => q(".inspector .tg"))); await until(() => dirtyShown());
		}
	}
	fin();
	return;
}

if (params.get("probe") === "gates") {
	await until(() => nodeByResp("gate"));
	click(nodeByResp("gate"));
	await until(() => q('.inspector input[data-f="gates:all"]'), 120);
	const all = () => q('.inspector input[data-f="gates:all"]');
	const items = () => [...document.querySelectorAll('.inspector input[data-f^="gate:"]')];
	const gates = ["complexity-gate", "implementation-quality-gate"];
	const before = JSON.stringify(work);
	ok("all gates label contains no future wording", all().parentElement.textContent.trim() === "全部门" && !q("#insp").textContent.includes("将来新增"));
	if (params.get("directory") !== "available") {
		ok("unavailable or empty directory cannot claim all selected", all().disabled && !all().checked && !all().indeterminate && items().length === 0);
		ok("directory failure preserves saved selections", JSON.stringify(work) === before && work.gates[0] === "unreported-gate");
		fin(); return;
	}
	ok("implicit all shows every ordinary gate checked", items().length === gates.length && all().checked && items().every(c => c.checked));
	if (items().length !== gates.length) { fin(); return; }
	const summary = q(".gate-summary"), options = q(".gate-options"), descriptions = [...document.querySelectorAll(".gate-description")];
	const firstEntry = items()[0].closest(".gate-option"), nextEntry = items()[1].closest(".gate-option");
	ok("all gates is a separate summary above indented entries", !!summary && !!options && summary.parentElement === options.parentElement && !summary.closest(".gate-options") && options.getBoundingClientRect().left > summary.getBoundingClientRect().left);
	ok("summary has a distinct card surface", getComputedStyle(summary).backgroundColor !== getComputedStyle(firstEntry).backgroundColor && getComputedStyle(summary).borderTopStyle === "solid");
	const description = firstEntry.querySelector(".gate-description");
	const expectedDescription = "检查职责与复杂度。\nCheck ownership and boundaries.\n" + "UnbrokenGateDescription".repeat(12) + "\n<em>保持文本</em>";
	ok("gate description preserves exact multiline metadata as plain text", descriptions.length === 1 && description.textContent === expectedDescription && !description.querySelector("em"));
	ok("gate without a description has no placeholder", !nextEntry.querySelector(".gate-description") && nextEntry.textContent === gates[1]);
	const descRect = description.getBoundingClientRect(), nextRect = nextEntry.getBoundingClientRect(), copyRect = firstEntry.querySelector(".gate-copy").getBoundingClientRect();
	ok("long multiline description wraps within its own gate entry", getComputedStyle(description).whiteSpace === "pre-wrap" && getComputedStyle(description).overflowWrap === "anywhere" && description.scrollWidth <= description.clientWidth + 1 && descRect.right <= copyRect.right + 1 && descRect.bottom <= nextRect.top + 1);
	if (params.get("layout") === "1") {
		closeDrawer(); q(".gate-picker").scrollIntoView({ block: "start" });
		fin(); return;
	}
	step = "shared quality gate options";
	const gateKeys = [...document.querySelectorAll('.node[data-resp="gate"]')].map(n => n.dataset.key);
	const initialExecution = structuredClone(execOf(work, "gate"));
	const gateProvider = q('.inspector select[data-f="provider"]');
	gateProvider.value = "zcode"; fire(gateProvider, "change");
	const gateModel = await must(() => { const s = q('.inspector select[data-f="model"]'); return s && [...s.options].some(o => o.value === "model-B2") ? s : null; }, "shared gate platform models loaded", 240);
	gateModel.value = "model-B2"; fire(gateModel, "change");
	const gateEffort = await must(() => q('.inspector .efforts button[data-e="b2-high"]'), "shared gate effort loaded", 240);
	click(gateEffort); click(items()[0]);
	for (const key of gateKeys) {
		click(q('.node[data-key="' + key + '"]'));
		await must(() => q('.inspector select[data-f="model"]'), "gate inspector remains available at " + key);
		ok("gate choices are shared at " + key, !items()[0].checked && items()[1].checked && all().indeterminate);
		ok("execution choices are shared at " + key, q('.inspector select[data-f="provider"]').value === "zcode" && q('.inspector select[data-f="model"]').value === "model-B2" && q('.inspector .efforts button[data-e="b2-high"]').classList.contains("on"));
		ok("gate cards share execution chips at " + key, [...document.querySelectorAll('.node[data-resp="gate"]')].every(n => /zcode/.test(n.textContent) && /model-B2/.test(n.textContent) && /b2-high/.test(n.textContent)));
	}
	click(q('.inspector button[data-f="enabled"]'));
	ok("every quality gate closes through the same responsibility", [...document.querySelectorAll('.node[data-resp="gate"]')].every(n => n.classList.contains("disabled")));
	click(q('.node[data-key="' + gateKeys[0] + '"]'));
	click(await must(() => q('.inspector button[data-f="enabled"]'), "shared gate toggle restored"));
	await must(() => all() && items().length === gates.length, "shared gate choices restored");
	ok("every quality gate reopens through the same responsibility", [...document.querySelectorAll('.node[data-resp="gate"]')].every(n => !n.classList.contains("disabled")));
	click(items()[0]);
	const previousExecution = snapshot(); setExec(work, "gate", initialExecution); change(previousExecution); await renderInspector(true);
	await must(() => all() && items().length === gates.length, "original gate execution restored");
	const originalAll = all(), originalItems = items(), provider = q('.inspector select[data-f="provider"]');
	const queries = path => performance.getEntriesByType("resource").filter(r => new URL(r.name).pathname === path).length;
	const gateQueries = queries("/api/gates"), optionQueries = queries("/api/execution-options");
	click(items()[0]);
	ok("deselecting implicit all preserves remaining gates", JSON.stringify(work.gates) === JSON.stringify([gates[1]]) && !all().checked && all().indeterminate && !items()[0].checked && items()[1].checked);
	click(items()[0]);
	ok("selecting each gate makes all checked", all().checked && !all().indeterminate && work.gates.length === 2);
	click(all());
	ok("all can clear every gate", Array.isArray(work.gates) && work.gates.length === 0 && !all().checked && !all().indeterminate && items().every(c => !c.checked));
	click(all());
	ok("all can select every gate using omitted protocol value", gatesAll(work) && all().checked && items().every(c => c.checked));
	ok("gate toggles retain controls and make no metadata requests", all() === originalAll && items().every((c, i) => c === originalItems[i]) && q('.inspector select[data-f="provider"]') === provider && queries("/api/gates") === gateQueries && queries("/api/execution-options") === optionQueries);
	work.gates = [...gates, "unreported-gate"];
	await renderInspector(true);
	ok("explicit full list also checks all", all().checked && !all().indeterminate && items().every(c => c.checked));
	click(items()[0]);
	ok("individual edits preserve unreported saved gates", work.gates.includes("unreported-gate") && work.gates.includes(gates[1]) && !work.gates.includes(gates[0]) && all().indeterminate);
	const saveGates = async () => {
		click(q("#save")); await until(() => q("#saveBar .savebar")); click(row("门配置"));
		await must(() => !dirtyShown() && all() && !all().disabled && items().length === gates.length, "save restores gate selection controls", 180);
		return (await state()).configurations.find(c => c.name === "门配置").configuration;
	};
	let stored = await saveGates();
	ok("save persists the actual selected gate contents", JSON.stringify(stored.gates) === JSON.stringify([gates[1], "unreported-gate"]));
	click(row("默认")); await until(() => q("#cfgTitle").textContent === "默认");
	click(row("门配置")); await until(() => q("#cfgTitle").textContent === "门配置");
	click(nodeByResp("gate")); await until(() => items().length === 2);
	ok("reopening restores partial selection and unreported value", all().indeterminate && !items()[0].checked && items()[1].checked && work.gates.includes("unreported-gate"));
	click(all()); click(all());
	stored = await saveGates();
	ok("save persists explicit empty selection", Array.isArray(stored.gates) && stored.gates.length === 0);
	click(all()); stored = await saveGates();
	ok("save preserves omitted all selection semantics", stored.gates === undefined || stored.gates === null);
	click(nodeByResp("repair")); click(nodeByResp("gate")); await until(() => items().length === 2);
	ok("reopening implicit all still lists every gate", all().checked && items().every(c => c.checked));
	const projected = await loadState(), previousRuns = projected.runs, previousRunID = runID;
	projected.runs = [{ runId: "gate-lock", status: "RUNNING", configuration: editing, nodes: [{ node: "gate", enabled: true, editable: false }] }];
	runID = "gate-lock"; await renderInspector(true);
	ok("locked gate selection disables all and individual controls", all().disabled && items().every(c => c.disabled));
	projected.runs = previousRuns; runID = previousRunID; render();
	step = "complete route uses the same configuration library as other routes";
	const libraryBefore = await state();
	ok("full route is a builtin list entry with standard copy and activation actions", !!row("全量路线") && /内置/.test(row("全量路线").textContent) && !!btn(row("全量路线"), "复制") && !!btn(row("全量路线"), "设为默认") && !btn(row("全量路线"), "删除") && !btn(row("全量路线"), "重命名") && ![...document.querySelectorAll(".header button")].some(b => b.textContent === "全量路线"));
	click(row("全量路线")); await until(() => q("#cfgTitle").textContent === "全量路线");
	ok("selecting the builtin full route loads all nodes QA and ordinary gates cleanly", RESP.every(n => nodeEnabled(work, n.id)) && gatesAll(work) && !dirtyShown() && editing === "全量路线");
	const libraryAfter = await state();
	ok("selecting full route does not activate or start a run", libraryAfter.active === libraryBefore.active && JSON.stringify(libraryAfter.runs) === JSON.stringify(libraryBefore.runs));
	click(nodeByResp("qa-blackbox"));
	const warning = await until(() => q(".inspector .warning"));
	ok("blackbox cost warning is visible and shared with its node tooltip", !!warning && warning.textContent === "黑盒 QA 涉及实际操作与验证，工作量、耗时和 token 消耗可能较大。" && nodeByResp("qa-blackbox").title === warning.textContent);
	click(nodeByResp("qa-whitebox"));
	ok("whitebox does not inherit the blackbox warning", !q(".inspector .warning") && !nodeByResp("qa-whitebox").title);
	click(q("#save"));
	const fullName = await until(() => q("#saveBar input[type=text]"));
	setVal(fullName, "全量配置"); fire(fullName, "change"); click(btn(q("#saveBar"), "存为新配置"));
	await until(() => !dirtyShown() && q("#cfgTitle").textContent === "全量配置", 180);
	const savedRoute = (await state()).configurations.find(c => c.name === "全量配置").configuration;
	ok("save-as persists all nodes both QA modes and omitted gates", RESP.every(n => nodeEnabled(savedRoute, n.id)) && savedRoute.qaModes.includes("blackbox") && savedRoute.qaModes.includes("whitebox") && gatesAll(savedRoute));
	click(row("默认")); await until(() => q("#cfgTitle").textContent === "默认");
	click(row("全量配置")); await until(() => q("#cfgTitle").textContent === "全量配置");
	ok("reopening the saved complete route restores its selections", RESP.every(n => nodeEnabled(work, n.id)) && gatesAll(work));
	click(btn(row("全量路线"), "复制"));
	await until(() => q("#cfgTitle").textContent === "全量路线 副本" && !dirtyShown());
	const copiedRoute = (await state()).configurations.find(c => c.name === "全量路线 副本");
	ok("copy action preserves the actual builtin full selection", !!copiedRoute && RESP.every(n => nodeEnabled(copiedRoute.configuration, n.id)) && gatesAll(copiedRoute.configuration));
	click(btn(row("全量路线"), "设为默认"));
	await until(async () => (await state()).active === "全量路线");
	ok("builtin full route can be activated through the standard list action", (await state()).active === "全量路线");
	click(row("全量路线")); await until(() => q("#cfgTitle").textContent === "全量路线");
	click(q("#save")); await until(() => q("#saveBar .savebar"));
	ok("all builtin entries are excluded from overwrite targets", row("默认").classList.contains("skip") && row("全量路线").classList.contains("skip"));
	esc();
	fin(); return;
}

if (params.get("probe") === "qa") {
	await until(() => document.querySelectorAll(".node").length >= 1);
	step = "disabled qa anchors rendered";
	const bb = nodeByResp("qa-blackbox"), wb = nodeByResp("qa-whitebox");
	ok("disabled qa anchors still rendered", !!bb && !!wb);
	ok("disabled qa anchors carry disabled style", !!bb && bb.classList.contains("disabled") && !!wb && wb.classList.contains("disabled"));
	step = "enable blackbox from defaults";
	click(bb);
	const tg0 = await until(() => q(".inspector .tg"), 60);
	ok("blackbox inspector opens", !!tg0);
	click(tg0);
	ok("enabling blackbox marks dirty", (await until(() => dirtyShown())) !== null);
	step = "save as new keeps mapping";
	click(q("#save"));
	const nameInput = await until(() => q("#saveBar input[type=text]"), 60);
	setVal(nameInput, "qa测试"); fire(nameInput, "change");
	click(btn(q("#saveBar"), "存为新配置"));
	await until(() => !dirtyShown(), 180);
	const s1 = await state();
	const c1 = (s1.configurations || []).find(c => c.name === "qa测试");
	ok("stored qaModes has blackbox only", !!(c1 && (c1.configuration.qaModes || []).length === 1 && c1.configuration.qaModes[0] === "blackbox"));
	step = "enable whitebox";
	click(nodeByResp("qa-whitebox"));
	click(await until(() => q(".inspector .tg"), 60));
	click(q("#save"));
	await until(() => !!q("#saveBar .savebar"), 60);
	click(row("qa测试"));
	await until(() => !dirtyShown(), 180);
	const s2 = await state();
	const c2 = (s2.configurations || []).find(c => c.name === "qa测试");
	ok("stored qaModes has both", !!(c2 && (c2.configuration.qaModes || []).includes("blackbox") && (c2.configuration.qaModes || []).includes("whitebox")));
	step = "disable both";
	click(nodeByResp("qa-blackbox")); click(await until(() => q(".inspector .tg"), 60));
	click(nodeByResp("qa-whitebox")); click(await until(() => q(".inspector .tg"), 60));
	click(q("#save"));
	await until(() => !!q("#saveBar .savebar"), 60);
	click(row("qa测试"));
	await until(() => !dirtyShown(), 180);
	const s3 = await state();
	const c3 = (s3.configurations || []).find(c => c.name === "qa测试");
	ok("stored qaModes empty, qa disabled", !!(c3 && !(c3.configuration.qaModes || []).length && c3.configuration.flowSelection && (c3.configuration.flowSelection.disabled || []).includes("qa")));
	fin();
	return;
}

if (params.get("probe") === "execsel") {
	await until(() => document.querySelectorAll(".node").length >= 1);
	step = "duplicate new name rejected";
	click(q("#newCfg"));
	let d = await until(() => q(".dlg"), 60);
	ok("new dialog opens", !!d);
	const inp = d && d.querySelector("input");
	setVal(inp, "工作"); fire(inp, "change");
	click(btn(d, "创建"));
	const again = await until(() => q(".dlg"), 60);
	ok("duplicate name re-prompts instead of switching", !!again && again !== d);
	click(btn(again, "取消"));
	await until(() => !q(".dlg"), 60);
	ok("editing target unchanged after rejected name", q("#cfgTitle").textContent === "工作");
	ok("library has exactly one 工作 row", [...document.querySelectorAll(".cfg")].filter(r => { const n = r.querySelector(".n span"); return n && n.textContent === "工作"; }).length === 1);
	ok("no draft left by rejected name", !dirtyShown());
	step = "unsaved new config still gets dynamic selects";
	click(q("#newCfg"));
	d = await until(() => q(".dlg"), 60);
	const inp2 = d && d.querySelector("input");
	setVal(inp2, "未存配置"); fire(inp2, "change");
	click(btn(d, "创建"));
	ok("new draft becomes editing target", (await until(() => q("#cfgTitle").textContent === "未存配置" && dirtyShown())) !== null);
	click(nodeByResp("repair"));
	ok("options fetch shows a loading placeholder", !!q(".inspector .lockmsg.load"));
	const selP0 = await until(() => q('.inspector select[data-f="provider"]'), 240);
	ok("unsaved draft renders dynamic provider select", !!selP0);
	ok("loading placeholder cleared once options land", !q(".inspector .lockmsg.load"));
	ok("provider select lists fixture platforms", !!selP0 && [...selP0.options].some(o => o.value === "zcode"));
	ok("uninstalled platform marked and disabled", !!selP0 && [...selP0.options].some(o => o.value === "devin" && o.disabled && /未安装/.test(o.textContent) && o.title === "可执行文件不在 PATH（未安装或未加环境变量）"));
	if (!selP0) {
		R.push("FAIL diag selP0-absent " + JSON.stringify({editing, sel, selKey, inspGen, inspSig, active: document.activeElement ? document.activeElement.outerHTML.slice(0, 500) : null, inspector: q("#insp") ? q("#insp").outerHTML.slice(0, 3000) : null, cacheKeys: Object.keys(optCache)}));
		fin0(); fin(); return;
	}
	step = "shared default catalogue and selection tags";
	ok("enabled stage shows selected tag", nodeByResp("repair").querySelector(".st").textContent === "已选择");
	ok("disabled stage shows unselected tag", nodeByResp("qa-blackbox").querySelector(".st").textContent === "未选择");
	const selectedCard = nodeByResp("development"), unselectedCard = nodeByResp("qa-blackbox");
	const selectedStyle = getComputedStyle(selectedCard), unselectedStyle = getComputedStyle(unselectedCard);
	ok("selected cards have a bright solid border and glow", selectedStyle.borderTopStyle === "solid" && selectedStyle.boxShadow !== "none");
	ok("unselected cards use a separate dashed surface", unselectedStyle.borderTopStyle === "dashed" && unselectedStyle.backgroundColor !== selectedStyle.backgroundColor && unselectedStyle.boxShadow === "none");
	const selectedTag = getComputedStyle(selectedCard.querySelector(".st")), unselectedTag = getComputedStyle(unselectedCard.querySelector(".st"));
	ok("selection tags have distinct text and background colours", selectedTag.color !== unselectedTag.color && selectedTag.backgroundColor !== unselectedTag.backgroundColor);
	ok("legend distinguishes selection from current editing", q(".legend").textContent.includes("已选择") && q(".legend").textContent.includes("未选择") && q(".legend").textContent.includes("当前编辑"));
	const cacheCount = Object.keys(optCache).length;
	for (const resp of RESP) {
		click(nodeByResp(resp.id));
		await until(() => q('.inspector select[data-f="provider"]'), 60);
		ok("default catalogue renders immediately for " + resp.id, !q(".inspector .lockmsg.load"));
	}
	ok("all nodes share one default catalogue entry", Object.keys(optCache).length === cacheCount);
	click(nodeByResp("repair"));
	await until(() => q('.inspector select[data-f="provider"]'), 60);
	const defaultEffort = q('.inspector .efforts button[data-e=""]');
	ok("omitted effort selects follow default", !!defaultEffort && defaultEffort.classList.contains("on") && !execOf(work, "repair").reasoningEffort);
	ok("follow default shows configured effort ahead of the model recommendation", defaultEffort.textContent === "跟随默认（a-high）");
	ok("default model names the reported platform model", q('.inspector select[data-f="model"]').options[0].textContent === "跟随默认（model-A）");
	ok("current editing keeps a blue border distinct from enabled nodes", getComputedStyle(nodeByResp("repair")).borderTopColor !== getComputedStyle(nodeByResp("development")).borderTopColor);
	step = "committed provider change refreshes model list in place";
	const sharedProvider = q('.inspector select[data-f="provider"]');
	sharedProvider.focus();
	const roundBeforeProvider = q('.inspector input[data-f="round"]'), toggleBeforeProvider = q('.inspector button[data-f="enabled"]');
	const platformOptionsBefore = [...sharedProvider.options];
	sharedProvider.value = "zcode"; fire(sharedProvider, "change");
	ok("provider remains the same object while dependencies load", q('.inspector select[data-f="provider"]') === sharedProvider && [...sharedProvider.options].every((o, i) => o === platformOptionsBefore[i]));
	ok("provider commit preserves round and toggle objects", q('.inspector input[data-f="round"]') === roundBeforeProvider && q('.inspector button[data-f="enabled"]') === toggleBeforeProvider);
	const selM = await until(() => { const m = q('.inspector select[data-f="model"]'); return m && [...m.options].some(o => o.value === "model-B") ? m : null; }, 240);
	ok("model select refreshed to the committed provider", !!selM);
	ok("old provider model absent after commit", !!selM && ![...selM.options].some(o => o.value === "model-A"));
	ok("platform switch follows that platform's configured effort", q('.inspector .efforts button[data-e=""]').textContent === "跟随默认（b-high）" && !execOf(work, "repair").reasoningEffort);
	ok("platform switch recomputes the default model name", q('.inspector select[data-f="model"]').options[0].textContent === "跟随默认（model-B）");
	ok("provider focus survives dependent updates", document.activeElement === q('.inspector select[data-f="provider"]'));
	step = "committed model change fills only the effort row";
	selM.focus();
	const modelOptionsBefore = [...selM.options];
	selM.value = "model-B2"; fire(selM, "change");
	ok("model commit preserves provider and model objects while efforts load", q('.inspector select[data-f="provider"]') === sharedProvider && q('.inspector select[data-f="model"]') === selM && [...selM.options].every((o, i) => o === modelOptionsBefore[i]));
	ok("model commit keeps the catalogue mounted", !q(".inspector .lockmsg.load") && !!q('.inspector select[data-f="model"]'));
	ok("model select holds committed value", q('.inspector select[data-f="model"]') && q('.inspector select[data-f="model"]').value === "model-B2");
	ok("only the effort row waits for the tuple's own answer", !!q(".inspector .efforts .hint") && /正在获取强度档/.test(q(".inspector .efforts .hint").textContent));
	ok("model-specific efforts rendered", (await until(() => [...document.querySelectorAll(".inspector .efforts button")].some(b => b.textContent === "b2-high"), 240)) !== null);
	ok("big placeholder never appeared during the fill", !q(".inspector .lockmsg.load"));
	ok("model focus survives effort updates", document.activeElement === q('.inspector select[data-f="model"]'));
	ok("model change keeps effort following defaults", q('.inspector .efforts button[data-e=""]').classList.contains("on") && !execOf(work, "repair").reasoningEffort);
	ok("model switch keeps the platform's configured effort ahead of recommendations", q('.inspector .efforts button[data-e=""]').textContent === "跟随默认（b-high）");
	ok("explicit model does not replace the follow-default name", q('.inspector select[data-f="model"]').options[0].textContent === "跟随默认（model-B）");
	const effortButton = q('.inspector .efforts button[data-e="b2-high"]');
	click(effortButton);
	ok("effort choice preserves upstream controls and its own button", q('.inspector select[data-f="provider"]') === sharedProvider && q('.inspector select[data-f="model"]') === selM && q('.inspector input[data-f="round"]') === roundBeforeProvider && q('.inspector button[data-f="enabled"]') === toggleBeforeProvider && q('.inspector .efforts button[data-e="b2-high"]') === effortButton);
	ok("explicit effort is preserved in the draft", execOf(work, "repair").reasoningEffort === "b2-high" && q('.inspector .efforts button[data-e="b2-high"]').classList.contains("on"));
	click(q('.inspector .efforts button[data-e=""]'));
	ok("follow default clears the explicit effort", execOf(work, "repair").reasoningEffort === "" && q('.inspector .efforts button[data-e=""]').classList.contains("on"));
	ok("restoring follow default preserves the native configured value as display only", q('.inspector .efforts button[data-e=""]').textContent === "跟随默认（b-high）" && execOf(work, "repair").reasoningEffort === "");
	selM.blur(); await poll();
	ok("polling preserves locally updated control objects", q('.inspector select[data-f="provider"]') === sharedProvider && q('.inspector select[data-f="model"]') === selM && q('.inspector input[data-f="round"]') === roundBeforeProvider && q('.inspector button[data-f="enabled"]') === toggleBeforeProvider);

	step = "a new model tuple fills its own effort row once";
	const selM2 = q('.inspector select[data-f="model"]');
	selM2.value = "model-B"; fire(selM2, "change");
	ok("new model tuple keeps the catalogue mounted", !q(".inspector .lockmsg.load") && !!q('.inspector select[data-f="model"]'));
	ok("new model tuple waits only in the effort row", !!q(".inspector .efforts .hint") && /正在获取强度档/.test(q(".inspector .efforts .hint").textContent));
	ok("new model tuple efforts rendered", (await until(() => [...document.querySelectorAll(".inspector .efforts button")].some(b => b.textContent === "b-high"), 240)) !== null);
	step = "revisiting a cached model tuple never refetches";
	const selM3 = q('.inspector select[data-f="model"]');
	selM3.value = "model-B2"; fire(selM3, "change");
	ok("cached model tuple renders efforts instantly", (await until(() => [...document.querySelectorAll(".inspector .efforts button")].some(b => b.textContent === "b2-high"), 30)) !== null);
	ok("cached model tuple keeps the catalogue mounted", !q(".inspector .lockmsg.load"));
	step = "reversed same-node responses keep only latest form";
	// The provider stays mounted while its slower response is in flight;
	// reversing that same control must give ownership to the latest choice.
	const selP1 = q('.inspector select[data-f="provider"]');
	selP1.value = "codex"; fire(selP1, "change");
	selP1.value = "zcode"; fire(selP1, "change");
	// The codex response arrives late server-side; it must not repopulate the
	// newer zcode form nor duplicate the round field.
	const settled = await until(() => { const m = q('.inspector select[data-f="model"]'); return m && [...m.options].some(o => o.value === "model-B") && ![...m.options].some(o => o.value === "model-A") ? m : null; }, 400);
	ok("stale slower provider response did not overwrite", !!settled);
	ok("provider select holds latest committed value", settled && q('.inspector select[data-f="provider"]').value === "zcode");
	ok("round field rendered exactly once", document.querySelectorAll('.inspector input[data-f="round"]').length === 1);
	// The late codex reply still lands in the cache — under its own tuple,
	// where it can never reach the rendered zcode form.
	const staleEntry = await until(() => Object.values(optCache).find(o => o.platform === "codex" && o.catalogue) || null, 240);
	ok("stale provider response isolated in its own tuple", !!staleEntry && !!staleEntry.catalogue && staleEntry.catalogue.models.some(m => m.value === "model-A"));
	step = "saved uninstalled provider stays visible marked";
	click(row("devin配置"));
	const discard = await until(() => q(".dlg"), 60);
	if (discard) { click(btn(discard, "放弃修改")); await until(() => !q(".dlg"), 60); }
	await until(() => q("#cfgTitle").textContent === "devin配置", 60);
	click(nodeByResp("repair"));
	const pSel2 = await until(() => q('.inspector select[data-f="provider"]'), 240);
	const devOpt = pSel2 && [...pSel2.options].find(o => o.value === "devin");
	ok("saved uninstalled provider marked and disabled", !!devOpt && devOpt.disabled && /未安装/.test(devOpt.textContent));
	ok("uninstalled platform names the PATH check", !!devOpt && devOpt.title === "可执行文件不在 PATH（未安装或未加环境变量）");
	ok("saved uninstalled provider stays selected", !!devOpt && devOpt.selected && pSel2.value === "devin");
	ok("follow-default provider label keeps the run platform", !!pSel2 && /codex/.test(pSel2.options[0].textContent) && !/未安装/.test(pSel2.options[0].textContent));
	ok("empty catalogue keeps only the default model entry", !!q('.inspector select[data-f="model"]') && q('.inspector select[data-f="model"]').options.length === 1);
	ok("unreported effort list noted", (await until(() => { const h = q(".inspector .efforts .hint"); return h && /该平台未报告强度档/.test(h.textContent) ? h : null; }, 60)) !== null);
	if (pSel2) {
		pSel2.value = "codex"; fire(pSel2, "change");
		const fixed = await until(() => { const m = q('.inspector select[data-f="model"]'); return m && [...m.options].some(o => o.value === "model-A") ? m : null; }, 240);
		ok("switching to an adaptered platform restores the form", !!fixed);
	}
	step = "saved host provider renders 宿主（主代理）";
	click(row("宿主配置"));
	const discard2 = await until(() => q(".dlg"), 60);
	if (discard2) { click(btn(discard2, "放弃修改")); await until(() => !q(".dlg"), 60); }
	await until(() => q("#cfgTitle").textContent === "宿主配置", 60);
	click(nodeByResp("repair"));
	const bad = await until(() => { const lm = q(".inspector .lockmsg:not(.load)"); return lm && /宿主（主代理）/.test(lm.textContent) ? lm : null; }, 240);
	ok("host provider renders 宿主（主代理）", !!bad);
	ok("host label keeps the provider name", !!bad && /default/.test(bad.textContent));
	ok("no raw adapter error dumped", !(q("#insp") && /has no execution selection adapter/.test(q("#insp").textContent)));
	const pSel = await until(() => q('.inspector select[data-f="provider"]'), 60);
	ok("host never listed as a selectable platform", !!pSel && ![...pSel.options].some(o => /default|宿主/.test(o.textContent)));
	ok("unset entry shown while value is a host", !!pSel && pSel.selectedIndex >= 0 && /未选定执行平台/.test(pSel.options[pSel.selectedIndex].textContent) && pSel.value === "");
	if (pSel) {
		pSel.value = "codex"; fire(pSel, "change");
		const fixed2 = await until(() => { const m = q('.inspector select[data-f="model"]'); return m && [...m.options].some(o => o.value === "model-A") ? m : null; }, 240);
		ok("host path also recovers on an adaptered platform", !!fixed2);
		ok("host recovery keeps the platform control with an accurate run-default label", q('.inspector select[data-f="provider"]') === pSel && /跟随 run 默认/.test(pSel.options[0].textContent) && /codex/.test(pSel.options[0].textContent));
	}
	step = "manual directory refresh preserves draft and retries errors";
	const explicitModel = q('.inspector select[data-f="model"]');
	explicitModel.value = "model-A"; fire(explicitModel, "change");
	await until(() => !q('.inspector button[data-f="refresh-options"]').disabled, 240);
	const beforeRefresh = JSON.stringify(work), beforeDirty = dirtyShown();
	const modelFocus = q('.inspector select[data-f="model"]'); modelFocus.focus();
	const refresh = q('.inspector button[data-f="refresh-options"]');
	click(refresh); click(refresh);
	ok("manual refresh disables repeated requests while loading", refresh.disabled && /正在获取/.test(refresh.textContent));
	ok("manual refresh leaves current selections mounted", q('.inspector select[data-f="model"]').value === "model-A");
	ok("refresh failure keeps a retry button", (await until(() => q('.inspector input[data-f="exec:model"]') && !q('.inspector button[data-f="refresh-options"]').disabled, 240)) !== null);
	ok("failure preserves the explicit model", q('.inspector input[data-f="exec:model"]').value === "model-A" && JSON.stringify(work) === beforeRefresh);
	const retryFocus = q('.inspector input[data-f="exec:model"]'); retryFocus.focus();
	click(q('.inspector button[data-f="refresh-options"]'));
	const refreshedModel = await until(() => { const m = q('.inspector select[data-f="model"]'); return m && m.options[0].textContent === "跟随默认（快速）" ? m : null; }, 240);
	ok("retry obtains refreshed native names and maps the default label", !!refreshedModel);
	ok("refreshed catalogue preserves explicit model and draft", !!refreshedModel && refreshedModel.value === "model-A" && JSON.stringify(work) === beforeRefresh && dirtyShown() === beforeDirty);
	ok("refreshed effort still follows the configured native value", q('.inspector .efforts button[data-e=""]').textContent === "跟随默认（a-high）");
	refreshedModel.focus();
	click(q('.inspector button[data-f="refresh-options"]'));
	await until(() => !q('.inspector button[data-f="refresh-options"]').disabled, 240);
	ok("successful refresh preserves the focused model control", document.activeElement === q('.inspector select[data-f="model"]'));
	ok("successful refresh preserves the dirty draft and selections", JSON.stringify(work) === beforeRefresh && q('.inspector select[data-f="model"]').value === "model-A");
	setExec(work, "development", { provider: "codex", model: "model-A" });
	click(nodeByResp("development"));
	ok("another node shares the refreshed catalogue without loading", !q('.inspector .lockmsg.load') && q('.inspector select[data-f="model"]').options[0].textContent === "跟随默认（快速）");

	step = "late option replies respect current node locks";
	click(nodeByResp("repair"));
	const projected = await loadState(), previousRuns = projected.runs, previousRunID = runID;
	const lockState = { node: "repair", enabled: true, editable: true };
	projected.runs = [{ runId: "options-lock", status: "RUNNING", configuration: editing, nodes: [lockState] }];
	runID = "options-lock"; render();
	const lockingProvider = q('.inspector select[data-f="provider"]');
	lockingProvider.value = "zcode"; fire(lockingProvider, "change");
	lockState.editable = false;
	await until(() => q('.inspector select[data-f="model"]') && !q('.inspector button[data-f="refresh-options"]').disabled, 240);
	ok("late response keeps retained provider and new model fields locked", q('.inspector select[data-f="provider"]') === lockingProvider && lockingProvider.disabled && q('.inspector select[data-f="model"]').disabled && [...document.querySelectorAll('.inspector .efforts button')].every(b => b.disabled));
	ok("read-only option refresh remains available for a locked node", !q('.inspector button[data-f="refresh-options"]').disabled);
	projected.runs = previousRuns; runID = previousRunID; render();

	fin();
	return;
}

if (params.get("probe") === "zoomfit") {
	// CSS transitions do not advance under virtual time; geometry assertions
	// read the final transform, so kill transitions for this probe only.
	const noAnim = document.createElement("style");
	noAnim.textContent = "*{transition:none!important;animation:none!important}";
	document.head.appendChild(noAnim);
	await until(() => document.querySelectorAll(".node").length >= 4);
	const scale = () => { const m = /scale\(([-0-9.]+)\)/.exec(q("#world").style.transform || ""); return m ? parseFloat(m[1]) : null; };
	await until(() => scale() !== null, 120);
	ok("fit zoom stays above the readability floor", scale() >= 0.8);
	step = "overflowed fit anchors the first column";
	// The graph is wider than the canvas at the fit floor; with no clicked
	// node the fit pins the content's left edge to the margin. The drawer is
	// an overlay that covers the canvas's left edge while open, so the
	// leftmost column must land right of the drawer's measured width.
	const leftCol = q('.node[data-key="n-product-review"]');
	const leftRect = leftCol && leftCol.getBoundingClientRect();
	const dr = q("#drawer"), edge = dr && dr.classList.contains("open") ? dr.offsetWidth : 0;
	ok("first column stays clear of the drawer (L" + (leftRect ? Math.round(leftRect.left) : "absent") + ")", !!leftRect && leftRect.left >= edge + 29);
	step = "clicked repeated node stays visible at minimum fit";
	const far = q('.node[data-key="sl-dev-z-za"]');
	ok("repeated slice node instance present", !!far);
	if (far) {
		click(far);
		const opened = await until(() => q(".inspector.open"), 240);
		ok("inspector opened on clicked node", !!opened);
		// Each render rebuilds node elements; re-query the same instance key
		// once the fit animation class has cleared so the transform is final.
		const again = await until(() => {
			const n = q('.node[data-key="sl-dev-z-za"]'); const r = n && n.getBoundingClientRect();
			return r && r.width > 0 && getComputedStyle(q("#world")).transform !== "none" ? n : null;
		}, 240);
		ok("clicked instance re-rendered", !!again);
		ok("fit stays at or above the readability floor after picking (z=" + scale() + ")", scale() >= 0.8);
		const r = again.getBoundingClientRect(), c = q("#canvas").getBoundingClientRect();
		ok("clicked slice node's center stays inside the canvas (node " + Math.round(r.left) + ".." + Math.round(r.right) + "x" + Math.round(r.top) + ".." + Math.round(r.bottom) + " canvas " + Math.round(c.right) + "x" + Math.round(c.bottom) + ")",
			r.left + r.width / 2 >= c.left && r.left + r.width / 2 <= c.right &&
			r.top + r.height / 2 >= c.top && r.top + r.height / 2 <= c.bottom);
	}
	fin();
	return;
}

if (params.get("probe") === "intakectx") {
	await until(() => document.querySelectorAll(".node").length >= 1);
	ok("intake opens the active configuration", q("#cfgTitle").textContent === "新受理");
	ok("intake footer shows the select action", pickShown());
	ok("run selector hidden during intake", !q("#runSel") || q("#runSel").style.display === "none");
	ok("intake header announces a new run", !!(q("#runinfo") && q("#runinfo").textContent.includes("受理阶段/新建run")));
	ok("intake graph marked schematic", [...document.querySelectorAll(".group")].some(g => g.textContent.includes("示意图")));
	ok("schematic shows an ordinary gate for each development batch", !!q('.node[data-key="n-gate-0"]') && !!q('.node[data-key="n-gate-1"]'));
	const schematicNodes = [...document.querySelectorAll(".node")].map(n => ({ x: parseFloat(n.style.left), y: parseFloat(n.style.top), w: n.offsetWidth, h: n.offsetHeight }));
	ok("schematic node boxes do not overlap", schematicNodes.every((a, i) => schematicNodes.slice(i + 1).every(b => a.x + a.w <= b.x || b.x + b.w <= a.x || a.y + a.h <= b.y || b.y + b.h <= a.y)));
	for (const id of ["product-review", "start-readiness", "development", "qa-blackbox", "qa-whitebox", "gate", "repair"]) {
		click(nodeByResp(id));
		const tg = await until(() => q(".inspector .tg"), 60);
		ok("intake keeps " + id + " editable", !!tg && tg.disabled === false);
		esc();
		await until(() => !q(".inspector.open"), 60);
	}
	// The configuration bound to the still-running run is editable in intake.
	click(row("工作"));
	ok("bound configuration selectable in intake", (await until(() => q("#cfgTitle").textContent === "工作" && !dirtyShown(), 90)) !== null);
	click(nodeByResp("qa-blackbox"));
	click(await until(() => q(".inspector .tg"), 60));
	ok("edit marks dirty", (await until(() => dirtyShown(), 60)) !== null);
	click(q("#save"));
	await until(() => !!q("#saveBar .savebar"), 90);
	click(row("工作"));
	ok("saved over the bound configuration", (await until(() => !dirtyShown(), 180)) !== null);
	// Leave the draft dirty, then let the test supersede the candidate.
	click(nodeByResp("repair"));
	click(await until(() => q(".inspector .tg"), 60));
	await until(() => dirtyShown(), 60);
	await ready();
	const gone = await untilReal(() => !pickShown() && q(".banner.show"), 1200);
	ok("superseded intake drops the select action", !!gone);
	ok("superseded restores the run selector", !!(gone && q("#runSel") && q("#runSel").style.display !== "none"));
	ok("superseded restores the live run context", !!(gone && q("#runinfo") && /requirements\.md ·/.test(q("#runinfo").textContent) && q("#runinfo [title]") && q("#runinfo [title]").title === "panel-run"));
	ok("superseded keeps the dirty draft", !!(gone && dirtyShown() && q("#cfgTitle").textContent === "工作"));
	fin();
	return;
}
if (params.get("probe") === "boundruns") {
	await until(() => document.querySelectorAll(".cfg").length >= 1);
	const card = row("别的");
	// The drawer slide-in is a compositor transform transition that never
	// produces frames under --dump-dom; pin it to its open state so geometry
	// asserts measure the resting layout instead of a frozen animation frame.
	const dr = q("#drawer"); dr.style.transition = "none"; void dr.offsetWidth;
	const badge = card && card.querySelector(".boundruns summary.badge");
	ok("crowded binding collapses to a count", !!badge && /绑定 3 个 run/.test(badge.textContent));
	ok("collapsed badge does not spill run labels", !!badge && !/run ghost/.test(badge.textContent));
	const title0 = q("#cfgTitle").textContent;
	click(badge);
	const cardNow = () => row("别的");
	const list = await until(() => { const c = cardNow(); const d = c && c.querySelector(".boundruns[open]"); return d && d.querySelectorAll("div").length === 3 ? d : null; }, 60);
	ok("badge expands the full run list", !!list);
	ok("expanded list carries every run label", !!list && [...list.querySelectorAll("div")].every(d => /run ghost-\d/.test(d.textContent)));
	ok("badge click did not pick the configuration", q("#cfgTitle").textContent === title0);
	if (list) {
		const c = cardNow();
		c.scrollIntoView({ block: "nearest" });
		const d = c.querySelector(".boundruns[open]");
		const lr = d.getBoundingClientRect(), cr = c.getBoundingClientRect();
		ok("expanded list stays inside the card (list L" + Math.round(lr.left) + " R" + Math.round(lr.right) + " card L" + Math.round(cr.left) + " R" + Math.round(cr.right) + ")",
			lr.left >= cr.left - 1 && lr.right <= cr.right + 1);
	}
	ok("no horizontal overflow after expansion", document.documentElement.scrollWidth <= window.innerWidth + 1);
	ok("drawer open for card geometry", !!q(".drawer.open"));
	insideViewport(cardNow(), "expanded config card");
	// A state poll rebuilds the list; re-query rather than hold a stale node.
	const open2 = await until(() => { const c = row("别的"); return c && c.querySelector(".boundruns[open]") ? c : null; }, 50);
	ok("expansion survives a state poll", !!open2);
	click(open2 && open2.querySelector("summary.badge"));
	ok("badge collapses the list again", (await until(() => { const c = row("别的"); return c && !c.querySelector(".boundruns[open]"); }, 40)) !== null);
	fin();
	return;
}

if (window.__PANEL__ && window.__PANEL__.mode === "intake") {
	await until(() => pickShown() || q(".banner.show"), 300);
	ok("intake footer shows select", pickShown());
	await ready();
	const gone = await untilReal(() => !pickShown() && q(".banner.show"), 1200);
	ok("superseded removes select and shows banner", !!gone);
	ok("banner names expiry", !!(gone && /失效|已过期/.test(gone.textContent)));
	const r = await fetch("/api/window/select", { method: "POST", headers: { "Content-Type": "application/json", "X-Formal-Gates-Token": window.__PANEL__.token }, body: JSON.stringify({ window: window.__PANEL__.window, configuration: "默认" }) }).then(x => x.json());
	ok("stale select reports superseded", !!(r.ok && r.result && r.result.status === "superseded"));
	fin();
	return;
}

// library window flows
await until(() => document.querySelectorAll(".node").length >= 1);
step="drawer opens by default";ok("drawer opens by default", !!q(".drawer.open"));
step="settings row rendered";ok("settings row rendered", !!q("#setrow input[type=number]"));

// panel settings write through the real API
const idle = q("#setrow input[type=number]");
setVal(idle, "42"); fire(idle, "change");
const st1 = await until(async () => { const s = await state(); return s.effective && s.effective.values && s.effective.values.panelIdleTimeout === 42 ? s : null; });
step="panelIdleTimeout persisted via /api/settings";ok("panelIdleTimeout persisted via /api/settings", !!st1);
await must(() => !idle.isConnected && q("#setrow input[type=number]")?.value === "42", "panelIdleTimeout refresh completes");
const sel = q("#setrow input[type=checkbox]");
click(sel);
const st2 = await until(async () => { const s = await state(); return s.effective && s.effective.values && s.effective.values.panelSelection === false ? s : null; });
step="panelSelection persisted via /api/settings";ok("panelSelection persisted via /api/settings", !!st2);
await must(() => !sel.isConnected && q("#setrow input[type=checkbox]")?.checked === false, "panelSelection refresh completes");

// builtin copy creates an editable copy with a real scope
click(btn(row("默认"), "复制"));
const copy = await until(() => row("默认 副本"));
step="builtin copy creates 默认 副本";ok("builtin copy creates 默认 副本", !!copy);
step="copy carries scope badge";ok("copy carries scope badge", !!(copy && copy.querySelector(".badge.end") && /用户|项目/.test(copy.querySelector(".badge.end").textContent)));

// select copy; toggle the gate node; dirty guard on switch
click(row("默认 副本"));
await until(() => q("#cfgTitle").textContent === "默认 副本");
click(nodeByResp("gate"));
const enabled = await until(() => q('.inspector button.tg[data-f="enabled"]'));
click(enabled);
step="edit marks draft dirty";ok("edit marks draft dirty", await until(() => q("#dirtyBadge") && q("#dirtyBadge").style.display !== "none", 20) !== null);

click(row("默认"));
const dlg1 = await until(() => q(".dlg"));
step="dirty switch opens Save/Discard/Cancel dialog";ok("dirty switch opens Save/Discard/Cancel dialog", !!(dlg1 && btn(dlg1, "取消") && btn(dlg1, "放弃修改") && btn(dlg1, "保存")));
click(btn(dlg1, "取消"));
await until(() => !q(".dlg"));
step="cancel keeps editing target and draft";ok("cancel keeps editing target and draft", q("#cfgTitle").textContent === "默认 副本" && q("#dirtyBadge").style.display !== "none");

click(row("默认"));
const dlg2 = await until(() => q(".dlg"));
click(btn(dlg2, "放弃修改"));
await until(() => q("#cfgTitle").textContent === "默认" && !q(".dlg"));
step="discard switches cleanly";ok("discard switches cleanly", q("#dirtyBadge").style.display === "none");

// internal close on a dirty draft prompts and cancel preserves
click(row("默认 副本"));
await until(() => q("#cfgTitle").textContent === "默认 副本");
click(nodeByResp("gate"));
const enabled2 = await until(() => q('.inspector button.tg[data-f="enabled"]'));
click(enabled2);
click(q("#winClose"));
const dlg3 = await until(() => q(".dlg"));
step="internal close guards dirty draft";ok("internal close guards dirty draft", !!(dlg3 && btn(dlg3, "取消")));
click(btn(dlg3, "取消"));
step="close cancel keeps draft";ok("close cancel keeps draft", q("#dirtyBadge").style.display !== "none");

// save drawer: current named entry writes in one click
click(q("#save"));
await until(() => !!q("#saveBar .savebar"), 30);
click(row("默认 副本"));
step="save-mode current target writes without extra confirm";ok("save-mode current target writes without extra confirm", (await until(() => q("#dirtyBadge").style.display === "none")) !== null && !q(".dlg"));

// dirty again; save-mode other target asks once then overwrites
click(nodeByResp("gate"));
const enabled3 = await until(() => q('.inspector button.tg[data-f="enabled"]'));
click(enabled3);
click(q("#save"));
await until(() => !!q("#saveBar .savebar"), 30);
step="builtin not a save target";ok("builtin not a save target", row("默认") && row("默认").classList.contains("skip"));
click(row("工作"));
const dlg4 = await until(() => q(".dlg"));
step="other target requires overwrite confirm";ok("other target requires overwrite confirm", !!(dlg4 && btn(dlg4, "覆盖")));
click(btn(dlg4, "覆盖"));
step="overwrite switched editing to target";ok("overwrite switched editing to target", (await until(() => q("#cfgTitle").textContent === "工作" && q("#dirtyBadge").style.display === "none")) !== null);

// save-as-new through the drawer name field
click(nodeByResp("gate"));
const enabled4 = await until(() => q('.inspector button.tg[data-f="enabled"]'));
click(enabled4);
click(q("#save"));
const nameInput = await until(() => q("#saveBar input[type=text]"));
setVal(nameInput, "新配置"); fire(nameInput, "change");
click(btn(q("#saveBar"), "存为新配置"));
step="new configuration persisted";ok("new configuration persisted", (await until(() => q("#cfgTitle").textContent === "新配置" && q("#dirtyBadge").style.display === "none")) !== null && (await state()).configurations.some(c => c.name === "新配置"));

// typing survives refresh polls
click(nodeByResp("start-readiness"));
const limit = await until(() => q(".inspector input[type=number]"));
limit.focus();
setVal(limit, "9");
await sleep(4600);
const cur = q(".inspector input[type=number]");
step="focused input keeps value across polls";ok("focused input keeps value across polls", !!cur && cur.value === "9" && document.activeElement === cur);

step = "chrome text not selectable";
ok("node text is not user-selectable", !!q(".node") && getComputedStyle(q(".node")).userSelect === "none");
ok("input text stays selectable", !!cur && getComputedStyle(cur).userSelect !== "none");

// run selector: only a real two-run fixture may claim a switch
const runSel = q("#runSel");
if (runSel && runSel.style.display !== "none" && runSel.options.length > 1) {
	const first = q("#runinfo").textContent;
	runSel.value = runSel.options[1].value;
	fire(runSel, "change");
	ok("run selector rebinds view", (await until(() => q("#runinfo") && q("#runinfo").textContent !== first)) !== null);
} else {
	skip("run selector rebinds view (single-run fixture)");
}

await ready();
R.push("END");
fin0();
})().catch(e => {
	const p = document.createElement("pre");
	p.id = "domresult";
	p.textContent = "RESULT\nFAIL driver-crashed step=" + step + " err=" + (e && e.message) + " @" + (e && e.stack && e.stack.split("\n")[1]);
	document.body.appendChild(p);
});
