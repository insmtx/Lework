import type { PluginRevisionFile } from "@leros/store";

export interface SkillFileTreeNode {
	name: string;
	path: string;
	type: "directory" | "file";
	file?: PluginRevisionFile;
	children: SkillFileTreeNode[];
}

export function buildSkillFileTree(files: PluginRevisionFile[]): SkillFileTreeNode[] {
	const roots: SkillFileTreeNode[] = [];
	const directories = new Map<string, SkillFileTreeNode>();

	for (const file of [...files].sort((left, right) => left.path.localeCompare(right.path))) {
		const parts = file.path.split("/").filter(Boolean);
		if (parts.length === 0) continue;

		let parentPath = "";
		let siblings = roots;
		for (let index = 0; index < parts.length - 1; index += 1) {
			const name = parts[index];
			if (name === undefined) break;
			const directoryPath = parentPath ? `${parentPath}/${name}` : name;
			let directory = directories.get(directoryPath);
			if (!directory) {
				directory = {
					name,
					path: directoryPath,
					type: "directory",
					children: [],
				};
				directories.set(directoryPath, directory);
				siblings.push(directory);
			}
			parentPath = directoryPath;
			siblings = directory.children;
		}

		const fileName = parts.at(-1);
		if (fileName === undefined) continue;
		siblings.push({
			name: fileName,
			path: file.path,
			type: "file",
			file,
			children: [],
		});
	}

	sortSkillFileTree(roots);
	return roots;
}

function sortSkillFileTree(nodes: SkillFileTreeNode[]) {
	nodes.sort((left, right) => {
		if (left.type !== right.type) return left.type === "directory" ? -1 : 1;
		return left.name.localeCompare(right.name);
	});
	for (const node of nodes) {
		sortSkillFileTree(node.children);
	}
}
