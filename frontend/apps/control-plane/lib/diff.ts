export type DiffRowKind = "context" | "add" | "delete" | "hunk";

export interface DiffRow {
	oldNo: number | null;
	newNo: number | null;
	kind: DiffRowKind;
	text: string;
}

/**
 * Parses the unified patches returned by OpenCode's session diff. It is a
 * display parser, not a patch applier; malformed lines are kept as context so
 * the user still sees what the model produced.
 */
export function parsePatch(patch: string): DiffRow[] {
	const rows: DiffRow[] = [];
	let oldNo = 0;
	let newNo = 0;

	for (const line of patch.split("\n")) {
		if (line.startsWith("@@")) {
			const match = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/.exec(line);
			if (match) {
				oldNo = Number(match[1]) - 1;
				newNo = Number(match[2]) - 1;
			}
			rows.push({ oldNo: null, newNo: null, kind: "hunk", text: line });
			continue;
		}
		if (line.startsWith("---") || line.startsWith("+++") || line.startsWith("\\")) {
			continue;
		}
		if (line.startsWith("+")) {
			newNo += 1;
			rows.push({ oldNo: null, newNo, kind: "add", text: line.slice(1) });
			continue;
		}
		if (line.startsWith("-")) {
			oldNo += 1;
			rows.push({ oldNo, newNo: null, kind: "delete", text: line.slice(1) });
			continue;
		}
		if (line.startsWith(" ")) {
			oldNo += 1;
			newNo += 1;
			rows.push({ oldNo, newNo, kind: "context", text: line.slice(1) });
			continue;
		}
		if (line.length > 0) {
			rows.push({ oldNo: null, newNo: null, kind: "context", text: line });
		}
	}

	return rows;
}
