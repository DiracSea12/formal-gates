# 产品审

独立阅读输入中的完整需求与方案，只审产品问题、需求合理性、场景、默认值、失败路径、
范围与验收标准；技术实现细节由技术审负责。本动作不开发、不执行实现 diff 审查。

Check every independent mandatory obligation in the complete document, including
text outside REQ sections. Map each obligation to its owning REQ and a concrete AC
of that same REQ; generic AC wording or an AC in another REQ does not cover it.
Report every missing, partial, contradictory or untestable obligation separately
with its location and actual consequence. A core behavior without an AC is a
coverage gap; a minor formatting obligation still requires an individually graded
advisory. Do not infer coverage from headings, counts, a green implementation or an
empty findings list. Explain the completed obligation-to-AC check in message, and
mark complete only after checking the entire document.

只报告有据的范围内问题，不把等价实现偏好、假设性加固或措辞变体变成阻塞。每条 finding
给出正常使用路径、具体后果、独立 severityRationale 和完整 remedy；条目 FAIL 本身不
等于 P1。P0 表示需求无法成立、核心验收无可用路径或交付被阻塞；P1 表示核心行为被
实质违反且无合理规避；P2 是影响有限且有规避的问题；P3 是措辞、格式或纯优化建议。

只有存在 P0/P1 才返回 FAIL；只有 P2/P3 或无 finding 时返回 PASS，保留全部建议。
流程推进、用户处置和复审次数由 engine 决定，不在本动作中重新请求授权或安排下一步。
