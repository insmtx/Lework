import { clipText, type DiffLine, type ToolBlock } from "@lework/tool-view";
import { Text, View } from "react-native";

import { useToolStyles } from "./font-scale";

/** DIFF_LINE_LIMIT 防止一次编辑把整条消息撑成几屏，超出的部分明确标注截断。 */
const DIFF_LINE_LIMIT = 200;

// 块和行都没有自带 id；key 里带上 kind/内容和位置，既避免直接使用数组下标，
// 又能在同一次渲染里保持稳定。
function blockKey(block: ToolBlock, position: number): string {
	const label = "label" in block ? (block.label ?? "") : "";
	return `${block.kind}:${label}:${position}`;
}

function diffLineKey(line: DiffLine, position: number): string {
	return `${line.kind}:${position}`;
}

function itemKey(item: string, position: number): string {
	return `${item}:${position}`;
}

function BlockLabel({ label }: { label?: string }) {
	const styles = useToolStyles();
	return label ? <Text style={styles.label}>{label}</Text> : null;
}

function TerminalBlock({ block }: { block: Extract<ToolBlock, { kind: "terminal" }> }) {
	const styles = useToolStyles();
	const meta = [block.exit !== undefined ? `exit ${block.exit}` : "", block.duration ?? ""]
		.filter((part) => part !== "")
		.join(" · ");
	const failed = block.exit !== undefined && block.exit !== 0;

	return (
		<View style={styles.block}>
			<BlockLabel label={block.label} />
			<View style={styles.terminal}>
				<Text style={styles.command} selectable>
					$ {block.command}
				</Text>
				{block.workdir ? (
					<Text style={styles.workdir} selectable>
						{block.workdir}
					</Text>
				) : null}
				{block.output !== "" ? (
					<Text style={styles.terminalOutput} selectable>
						{clipText(block.output)}
					</Text>
				) : null}
				{meta !== "" ? (
					<Text style={[styles.terminalMeta, failed && styles.errorText]}>{meta}</Text>
				) : null}
			</View>
		</View>
	);
}

function CodeBlock({ block }: { block: Extract<ToolBlock, { kind: "code" }> }) {
	const styles = useToolStyles();
	return (
		<View style={styles.block}>
			<BlockLabel label={block.label} />
			<Text style={styles.code} selectable>
				{clipText(block.text)}
			</Text>
		</View>
	);
}

function DiffBlock({ block }: { block: Extract<ToolBlock, { kind: "diff" }> }) {
	const styles = useToolStyles();
	const lines = block.lines.slice(0, DIFF_LINE_LIMIT);
	const truncated = block.lines.length > DIFF_LINE_LIMIT;

	return (
		<View style={styles.block}>
			<BlockLabel label={block.label} />
			<View style={styles.diff}>
				{lines.map((line, index) => (
					<Text
						key={diffLineKey(line, index)}
						selectable
						style={[
							styles.diffLine,
							line.kind === "add" && styles.diffAdd,
							line.kind === "del" && styles.diffDel,
							line.kind === "meta" && styles.diffMeta,
						]}
					>
						{diffPrefix(line.kind)}
						{line.text === "" ? " " : line.text}
					</Text>
				))}
				{truncated ? <Text style={styles.muted}>… 已截断</Text> : null}
			</View>
		</View>
	);
}

function diffPrefix(kind: "add" | "del" | "context" | "meta"): string {
	if (kind === "add") return "+";
	if (kind === "del") return "-";
	if (kind === "context") return " ";
	return "";
}

function ListBlock({ block }: { block: Extract<ToolBlock, { kind: "list" }> }) {
	const styles = useToolStyles();
	return (
		<View style={styles.block}>
			<BlockLabel label={block.label} />
			{block.items.length === 0 ? (
				<Text style={styles.empty}>无匹配</Text>
			) : (
				block.items.map((item, index) => (
					<Text key={itemKey(item, index)} style={styles.listItem} numberOfLines={2}>
						• {item}
					</Text>
				))
			)}
		</View>
	);
}

function ChecklistBlock({ block }: { block: Extract<ToolBlock, { kind: "checklist" }> }) {
	const styles = useToolStyles();
	return (
		<View style={styles.block}>
			<BlockLabel label={block.label} />
			{block.items.map((item, index) => (
				<Text
					key={itemKey(item.text, index)}
					style={[styles.checkItem, item.done && styles.checkDone]}
				>
					{item.done ? "☑" : "☐"} {item.text}
				</Text>
			))}
		</View>
	);
}

function FieldsBlock({ block }: { block: Extract<ToolBlock, { kind: "fields" }> }) {
	const styles = useToolStyles();
	return (
		<View style={styles.block}>
			<BlockLabel label={block.label} />
			<View style={styles.fields}>
				{block.fields.map((field) => (
					<View key={field.label} style={styles.fieldRow}>
						<Text style={styles.fieldLabel}>{field.label}</Text>
						<Text style={styles.fieldValue} selectable>
							{clipText(field.value, 1200)}
						</Text>
					</View>
				))}
			</View>
		</View>
	);
}

function TextBlock({ block }: { block: Extract<ToolBlock, { kind: "text" }> }) {
	const styles = useToolStyles();
	return (
		<View style={styles.block}>
			<BlockLabel label={block.label} />
			<Text
				style={[
					styles.text,
					block.tone === "muted" && styles.textMuted,
					block.tone === "error" && styles.textError,
				]}
				selectable
			>
				{clipText(block.text)}
			</Text>
		</View>
	);
}

/** ToolBlocks 把展示模型的块列表渲染成手机端排版，不关心具体工具名。 */
export function ToolBlocks({ blocks }: { blocks: ToolBlock[] }) {
	const styles = useToolStyles();

	return (
		<View style={styles.blocks}>
			{blocks.map((block, index) => {
				switch (block.kind) {
					case "terminal":
						return <TerminalBlock key={blockKey(block, index)} block={block} />;
					case "code":
						return <CodeBlock key={blockKey(block, index)} block={block} />;
					case "diff":
						return <DiffBlock key={blockKey(block, index)} block={block} />;
					case "list":
						return <ListBlock key={blockKey(block, index)} block={block} />;
					case "checklist":
						return <ChecklistBlock key={blockKey(block, index)} block={block} />;
					case "fields":
						return <FieldsBlock key={blockKey(block, index)} block={block} />;
					default:
						return <TextBlock key={blockKey(block, index)} block={block} />;
				}
			})}
		</View>
	);
}
