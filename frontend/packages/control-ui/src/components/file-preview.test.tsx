// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { FilePreview } from "./file-preview";

afterEach(cleanup);

describe("FilePreview", () => {
	it("renders utf-8 text", () => {
		render(
			<FilePreview
				file={{
					path: "a.ts",
					encoding: "utf-8",
					content: "export const a = 1;",
					size: 19,
					truncated: false,
				}}
			/>,
		);
		expect(screen.getByText("export const a = 1;")).toBeTruthy();
	});

	it("offers a download for complete binary content", () => {
		render(
			<FilePreview
				file={{ path: "a.bin", encoding: "base64", content: "AAEC", size: 3, truncated: false }}
			/>,
		);
		expect(screen.getByText("Binary file (3 B)")).toBeTruthy();
		expect(screen.getByText("Download")).toBeTruthy();
	});

	it("warns instead of downloading truncated content", () => {
		render(
			<FilePreview
				file={{
					path: "big.bin",
					encoding: "base64",
					content: "AAEC",
					size: 900000,
					truncated: true,
				}}
			/>,
		);
		expect(screen.queryByText("Download")).toBeNull();
		expect(screen.getByText("Showing the first 512 KiB of this file.")).toBeTruthy();
	});
});
