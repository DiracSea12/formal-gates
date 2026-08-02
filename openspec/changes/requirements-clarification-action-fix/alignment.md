# Requirements Alignment

Date: 2026-08-02
Status: confirmed

## RQ-P1 - 澄清问答在受理阶段进行，不得跳过

`prompts/actions/requirements-clarification.md` SHALL 明确：澄清问答在受理阶段进行（正
式流程启动之前），一次只问一个有实质影响的决策、基于仓库事实、不套固定问卷；除非确无
会实质改变公开行为、验收或架构的决策需要澄清（此时记录"无剩余缺口"），否则必须先与用
户完成澄清问答、取得用户最终确认，才能记录 PASS；不得跳过澄清问答。提示词全文不得把
澄清问答或写文件与任何后续流程步骤联系起来。

## RQ-P2 - 整合需求在受理阶段写入需求文件

整合后的完整需求与决策 SHALL 在受理阶段（正式流程启动之前）写入需求文件，作为本 run
的验收输入与事实来源。

## RQ-P3 - 确认粒度明确

提示词 SHALL 明确"确认"的含义：呈现完整整合需求与技术方案并取得用户最终确认；PASS 只
记录用户实际确认的内容。

## RQ-P4 - 精简克制

本阶段改动 SHALL 限定为 `prompts/actions/requirements-clarification.md`（catalog，经
B2 目录 delta 容忍）与 CHANGELOG.md（仓库惯例）；不新增文件、不改动其他文件，保持精
简。
