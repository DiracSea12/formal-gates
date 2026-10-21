// Test-only CDP driver for ui_dom_test: launches the installed Chromium
// headless with real device metrics (Emulation.setDeviceMetricsOverride),
// navigates to the panel window, and prints the driver's RESULT block once
// the in-page script finishes. No production code knows about this file.
// Usage: node cdp_probe.js <browserPath> <url> <width> <height> [shotPath]
"use strict";
const { spawn } = require("child_process");
const fs = require("fs"), path = require("path"), os = require("os");

const [bin, url, w = "390", h = "844", shotPath = ""] = process.argv.slice(2);
const width = parseInt(w), height = parseInt(h);
const profile = fs.mkdtempSync(path.join(os.tmpdir(), "cdp-"));
// CI Linux runners restrict unprivileged user namespaces, so the headless
// browser runs without the OS sandbox; it only loads the local test page.
const proc = spawn(bin, [
	"--headless=new", "--no-sandbox", "--disable-gpu", "--no-first-run", "--disable-extensions",
	"--window-size=" + width + "," + height,
	"--remote-debugging-port=0", "--user-data-dir=" + profile, "about:blank",
], { stdio: ["ignore", "ignore", "pipe"] });
const sleep = ms => new Promise(r => setTimeout(r, ms));
// A browser that never starts is diagnosed from its own exit status and
// stderr tail; a loaded runner can take far longer than a few seconds to
// publish the debugging port.
let childStatus = "", stderrTail = "";
proc.on("error", e => { childStatus = "spawn error: " + (e && e.message); });
proc.on("exit", (code, signal) => { childStatus = "browser exit=" + code + (signal ? " signal=" + signal : ""); });
proc.stderr.on("data", d => { stderrTail = (stderrTail + d).slice(-2000); });
const bail = msg => {
	const detail = [childStatus, stderrTail.replace(/\s+/g, " ").trim().slice(-600)].filter(Boolean);
	console.log("RESULT\nFAIL cdp-probe " + msg + (detail.length ? " (" + detail.join("; ") + ")" : ""));
	try { proc.kill(); } catch {}
	process.exit(0);
};

async function devtoolsPort() {
	const f = path.join(profile, "DevToolsActivePort");
	for (let i = 0; i < 600; i++) {
		try { const t = fs.readFileSync(f, "utf8").split("\n"); if (t[0]) return parseInt(t[0]); } catch {}
		if (childStatus) break;
		await sleep(100);
	}
	throw new Error("no DevToolsActivePort file" + (childStatus ? "" : " (browser still running after 60s)"));
}

(async () => {
	const port = await devtoolsPort();
	let page;
	for (let i = 0; i < 50 && !page; i++) {
		const list = await (await fetch(`http://127.0.0.1:${port}/json/list`)).json().catch(() => []);
		page = list.find(t => t.type === "page");
		if (!page) await sleep(150);
	}
	if (!page) throw new Error("no page target");
	const ws = new WebSocket(page.webSocketDebuggerUrl);
	let seq = 0;
	const pending = new Map();
	ws.onmessage = ev => { const m = JSON.parse(ev.data); if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id); } };
	const send = (method, params) => new Promise((res, rej) => {
		const id = ++seq;
		pending.set(id, m => m.error ? rej(new Error(JSON.stringify(m.error))) : res(m.result));
		ws.send(JSON.stringify({ id, method, params: params || {} }));
	});
	await new Promise((r, j) => { ws.onopen = r; ws.onerror = j; });
	await send("Emulation.setDeviceMetricsOverride", { width, height, deviceScaleFactor: 1, mobile: true });
	await send("Page.enable");
	await send("Page.navigate", { url });
	// Screenshot-only runs have no driver block; give them a short settle.
	const deadline = Date.now() + (shotPath ? 20000 : 110000);
	for (;;) {
		await sleep(500);
		const r = await send("Runtime.evaluate", { expression: "(document.querySelector('#domresult')||{}).textContent || ''", returnByValue: true });
		const text = (r && r.result && r.result.value) || "";
		if (/END|driver-crashed/.test(text)) { console.log(text); break; }
		if (shotPath) {
			const nodes = await send("Runtime.evaluate", { expression: "document.querySelectorAll('.node').length", returnByValue: true });
			if ((nodes && nodes.result && nodes.result.value) > 0) break;
		}
		if (Date.now() > deadline) { console.log("RESULT\nFAIL cdp-probe-timeout\n" + text); break; }
	}
	if (shotPath) await sleep(1500);
	if (shotPath) {
		const shot = await send("Page.captureScreenshot", { format: "png" });
		fs.writeFileSync(shotPath, Buffer.from(shot.data, "base64"));
	}
	try { ws.close(); } catch {}
	proc.kill();
	process.exit(0);
})().catch(e => bail(e && e.message));
