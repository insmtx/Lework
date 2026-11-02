import type { BackendProjectFileVersionList } from "@lework/store";
import { describe, expect, it, vi } from "vitest";
import {
	buildProjectFileVersionEntries,
	getCurrentProjectFileVersionEntry,
	getLatestProjectFileVersion,
	waitForProjectFileVersionChange,
} from "./project-file-version-sync";

function versionList(currentId: string, versionNo: number): BackendProjectFileVersionList {
	return {
		initial_file_public_id: "file-v1",
		current_file_public_id: currentId,
		items: [
			{
				public_id: currentId,
				initial_file_public_id: "file-v1",
				relative_path: "artifacts/report.docx",
				name: "report.docx",
				version_no: versionNo,
				version_label: `第 ${versionNo} 版`,
			},
		],
	};
}

describe("project file version sync", () => {
	it("gives repeated file ids stable row identities and selects only the latest occurrence", () => {
		const repeated = versionList("file-v2", 2);
		const baseVersion = repeated.items[0];
		if (!baseVersion) throw new Error("base version is missing");
		repeated.items.push(
			{
				...baseVersion,
				public_id: "file-v1",
				version_no: 1,
				created_at: 20,
			},
			{
				...baseVersion,
				public_id: "file-v1",
				version_no: 1,
				created_at: 20,
			},
		);
		const entries = buildProjectFileVersionEntries(repeated.items);

		expect(entries).toHaveLength(3);
		expect(new Set(entries.map((entry) => entry.key))).toHaveProperty("size", 3);
		expect(getCurrentProjectFileVersionEntry(entries, "file-v2")?.key).toBe(entries[0]?.key);
	});

	it("uses current_file_public_id as the latest version", () => {
		const versions = versionList("file-v3", 3);
		expect(getLatestProjectFileVersion(versions)?.public_id).toBe("file-v3");
	});

	it("waits until a newer immutable version is visible", async () => {
		const loadVersions = vi
			.fn<() => Promise<BackendProjectFileVersionList | undefined>>()
			.mockResolvedValueOnce(versionList("file-v2", 2))
			.mockResolvedValueOnce(versionList("file-v3", 3));

		const result = await waitForProjectFileVersionChange({
			loadVersions,
			baselinePublicId: "file-v2",
			baselineVersionNo: 2,
			delays: [0, 0],
		});

		expect(loadVersions).toHaveBeenCalledTimes(2);
		expect(result?.latest.public_id).toBe("file-v3");
		expect(result?.latest.version_no).toBe(3);
	});
});
