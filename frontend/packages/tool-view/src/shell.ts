import type { ContentPart } from "@lework/api";

import { baseView, errorBlock } from "./base";
import { toolDuration, toolOutputText, toolState } from "./state";
import type { ToolView } from "./types";
import { asNumber, asRecord, asString, firstLine, stripAnsi } from "./values";

/** shellView 把 shell 调用渲染成终端：命令一行，输出一段，末尾是退出码和耗时。 */
export function shellView(part: ContentPart): ToolView {
	const state = toolState(part);
	const view = baseView(part);
	const input = asRecord(state.input) ?? {};
	const command = asString(input.command) ?? asString(input.cmd) ?? "";
	const workdir = asString(input.workdir);
	const output = stripAnsi(toolOutputText(state)).replace(/\s+$/, "");
	const metadata = asRecord(state.metadata) ?? {};
	const exit = asNumber(metadata.exit) ?? asNumber(metadata.exitCode) ?? asNumber(metadata.code);
	const duration = toolDuration(state);

	view.summary = command !== "" ? command : firstLine(output);
	if (command !== "") {
		view.blocks.push({
			kind: "terminal",
			command,
			...(workdir ? { workdir } : {}),
			output,
			...(exit !== undefined ? { exit } : {}),
			...(duration ? { duration } : {}),
		});
	} else if (output !== "") {
		view.blocks.push({ kind: "code", text: output });
	}
	if (exit !== undefined && exit !== 0) view.tone = "error";
	view.blocks.push(...errorBlock(state));
	return view;
}
