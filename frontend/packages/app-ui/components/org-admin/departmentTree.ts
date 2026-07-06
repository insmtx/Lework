import type { Department } from "@leros/store";

export type DepartmentTreeNode = Department & {
	children: DepartmentTreeNode[];
};

export function buildDepartmentTree(departments: Department[]): DepartmentTreeNode[] {
	const sorted = [...departments].sort((a, b) => a.sort - b.sort || a.id - b.id);
	const nodes = new Map<number, DepartmentTreeNode>();

	for (const department of sorted) {
		nodes.set(department.id, { ...department, children: [] });
	}

	const roots: DepartmentTreeNode[] = [];
	for (const node of nodes.values()) {
		if (node.parent_id === 0) {
			roots.push(node);
			continue;
		}
		const parent = nodes.get(node.parent_id);
		if (parent) {
			parent.children.push(node);
		} else {
			roots.push(node);
		}
	}

	return roots;
}

export function filterDepartmentTree(
	nodes: DepartmentTreeNode[],
	query: string,
): DepartmentTreeNode[] {
	const normalized = query.trim().toLowerCase();
	if (!normalized) return nodes;

	const filterNode = (node: DepartmentTreeNode): DepartmentTreeNode | null => {
		const filteredChildren = node.children
			.map(filterNode)
			.filter((item): item is DepartmentTreeNode => item !== null);
		const selfMatch = node.name.toLowerCase().includes(normalized);
		if (!selfMatch && filteredChildren.length === 0) {
			return null;
		}
		return { ...node, children: filteredChildren };
	};

	return nodes.map(filterNode).filter((item): item is DepartmentTreeNode => item !== null);
}

export function countDepartments(nodes: DepartmentTreeNode[]): number {
	return nodes.reduce((total, node) => total + 1 + countDepartments(node.children), 0);
}
