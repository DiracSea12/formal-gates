# B5 本机新增来源索引

这是本机安装包只读发现，不是模型调用结果，也不是验收 PASS。此前仅有 CLI help/README；本次找到随安装包发布的接口文档及实现。

## WorkBuddy

文档目录：/Applications/WorkBuddy.app/Contents/Resources/app.asar.unpacked/cli/dist/web-ui/docs/cn/cli

配置位置、SDK 结果/用量类型见 settings.md、sdk-typescript.md；ACP 协议见 acp.md。无需读取用户凭证文件。

## ZCode

安装包源码：/Applications/ZCode.app/Contents/Resources/glm/zcode.cjs

字符位置按当前 Python 文本索引；方法映射 session/create、session/resume、session/send、session/stop 在 461900 左右。请求/响应 schema 在前方；可按方法映射变量追溯，不能把这些定位片段当完整合同。

```js

nId:pe}).strict(),yEt=f.object({deleted:f.boolean()}).strict(),rr={computerUseOperationEvent:"computer-use/operation-event",sessionCreate:"session/create",sessionResume:"session/resume",sessionList:"session/list",sessionSubagents:"session/subagents",sessionRequestRuntimePreferences:"session/requestRuntimePreferences",sessionRead:"session/read",sessionMessages:"session/messages",sessionEvents:"session/events",sessionSubscribe:"session/subscribe",sessionSend:"session/send",sessionStop:"session/stop",sessionCancelBackgroundTask:"session/cancelBackgroundTask",sessionFork:"session/fork",sessionCompact:"session/compact",sessionGoal:"session/goal",sessionClose:"session/close",sessionSetModel:"session/setModel",sessionSetThoughtLevel:"session/setThoughtLevel",sessionUpdateRuntimeModelConfig:"session/updateRuntimeModelConfig",sessionSetMode:"session/setMode",workspaceReadState:"workspace/readState",workspaceHookTrustGrant:"workspace/hooks/trustGrant",workspaceUpdateProviderRegistry:"workspace/updateProviderRegistry",workspaceUpdateInteractionPreferences:"workspace/updateInteractionPreferences",workspaceUpdateModelIoPreferences:"workspace/updateModelIoPreferences",workspaceUpsertModelProvider:"workspace/upsertModelProvider",workspaceRemoveModelProvider:"workspace/removeModelProvider",workspaceSetDefaultModel:"workspace/setDefaultModel",workspaceSetDefaultThoughtLevel:"workspace/setDefaultThoughtLevel",workspaceSetDefaultMode:"workspace/setDefaultMode",workspaceGenerateText:"workspace/generateText",workspaceCancelGenerateText:"workspac

```

## DSH

ACP 实现：/Users/suxinyang/.local/share/deepseek-harness/node_modules/.pnpm/@deepseek-ai+dsh-acp@0.1.5-rc.1_c8d942ef55fce9edae43460782a3f5c2/node_modules/@deepseek-ai/dsh-acp/lib/index.js

同目录 lib/types 下有类型；实现含方法注册、更新及停止处理，可只读定位。

```js

// session/update

otify = async (notification) => {
		try {
			await conn.notify(methods.client.session.update, notification);
		} catch (error) {
			logger.warn(`acp: session/update failed: ${String(error)}`);
		}
		/* v8 ignore stop */
	};
	ctx.on("session/event", (session, event) => {
		const record = sessions.get(session.header.id);
		if (record?.ownsSession(session) === true) record.onSessionEvent(session, event);
	});
	ctx.on("agent/inbox/claimed", ({ agent, message, turn }) => {
		ownedRecord(agent)?.onInboxClaimed(message, turn);
	});
	ctx.on("agent/error", ({ agent, turn, error }) => {
		ownedRecord(agent)?.onAgentError(turn, error);
	});
	ctx.on("llm

// stopReason

 error;
			} finally {
				inflight.finishAdmission();
			}
			if (inflight.cancelRequested) {
				this.settleAfterQuiescence(inflight);
				return { stopReason: await completion.promise };
			}
			if (admissionFailure !== void 0) {
				this.inflight = void 0;
				if (admissionFailure instanceof AcpContentError) throw admissionFailure.kind === "invalid" ? invalidParams$1(admissionFailure.message) : internalError$1(admissionFailure.message);
				if (admissionFailure instanceof RequestError) throw admissionFailure;
				throw internalError$1(`prompt was not queued: ${admissionFailure.message}`);
			}
			this.settleAfterQuiescence(inflight);
			r

// initialize

ncelled") return "cancelled";
			return outcome.optionId === "allow-once" ? "allowed-once" : "rejected";
		});
	});
	const implementation = {
		async initialize(_params) {
			imagePromptEnabled = await supportsAcpImagePrompts(ctx, config.provider, config.model);
			return {
				protocolVersion: PROTOCOL_VERSION,
				agentInfo: {
					name: "deepseek-harness-acp",
					version: "0.0.1"
				},
				agentCapabilities: {
					mcpCapabilities: { http: true },
					promptCapabilities: {
						image: imagePromptEnabled,
						audio: false,
						embeddedContext: false
					},
					sessionCapabilities: {
						close: {},
						list: {},
						resume: 

```
