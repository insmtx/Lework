"use client";

import { Button } from "@lework/ui/components/ui/button";
import { Eye, FileSpreadsheet, FolderOpen, Upload, X } from "lucide-react";
import { useRef } from "react";
import {
	filesForRole,
	PAYROLL_SOURCE_SLOTS,
	type PayrollSelectedFile,
	type PayrollSourceKind,
} from "./payroll-accounting-mock";

export function PayrollFileSlots({
	files,
	roles,
	onAddFiles,
	onRemove,
	onPreview,
	onPickProject,
}: {
	files: PayrollSelectedFile[];
	roles?: PayrollSourceKind[];
	onAddFiles: (selected: FileList | null, role: PayrollSourceKind) => void;
	onRemove: (id: string) => void;
	onPreview: (file: PayrollSelectedFile) => void;
	onPickProject: (role: PayrollSourceKind) => void;
}) {
	const rosterInputRef = useRef<HTMLInputElement>(null);
	const historicalInputRef = useRef<HTMLInputElement>(null);
	const attendanceInputRef = useRef<HTMLInputElement>(null);
	const inputRef = {
		roster: rosterInputRef,
		historical_payroll: historicalInputRef,
		attendance: attendanceInputRef,
	} as const;
	const slots = PAYROLL_SOURCE_SLOTS.filter((slot) => !roles || roles.includes(slot.role));

	const canPreview = (file: PayrollSelectedFile) =>
		Boolean(file.previewUrl || file.publicId || file.storageUri);

	return (
		<>
			{slots.map((slot) => {
				const roleFiles = filesForRole(files, slot.role);
				return (
					<section key={slot.role}>
						<div className="mb-2 flex items-end justify-between gap-3">
							<div>
								<div className="text-sm font-semibold text-slate-800">
									{slot.title}
									<span className="text-red-500"> *</span>
								</div>
								<p className="mt-0.5 text-xs text-slate-400">{slot.hint}</p>
							</div>
							<span className="shrink-0 text-xs text-slate-400">{roleFiles.length} 个</span>
						</div>
						<div className="rounded-xl border border-dashed border-slate-200 bg-slate-50/60 p-3">
							{roleFiles.length ? (
								<div className="space-y-2">
									{roleFiles.map((selected) => (
										<div
											key={selected.id}
											className="flex min-h-10 items-center gap-2 rounded-lg bg-white px-3 py-2 text-sm shadow-sm"
										>
											<FileSpreadsheet className="size-4 shrink-0 text-emerald-600" />
											<span className="min-w-0 flex-1 truncate text-slate-700">
												{selected.name}
											</span>
											{canPreview(selected) ? (
												<button
													type="button"
													onClick={() => onPreview(selected)}
													className="rounded-md p-1 text-slate-400 hover:bg-[var(--lework-primary-softer)] hover:text-[var(--lework-primary)]"
													title="预览文件"
												>
													<Eye className="size-4" />
												</button>
											) : null}
											<button
												type="button"
												onClick={() => onRemove(selected.id)}
												className="rounded-md p-1 text-slate-400 hover:bg-slate-50 hover:text-slate-700"
												aria-label={`移除 ${selected.name}`}
											>
												<X className="size-4" />
											</button>
										</div>
									))}
								</div>
							) : (
								<p className="py-3 text-center text-xs text-slate-400">
									可上传多份，或从项目文件中选择
								</p>
							)}
							<div className="mt-3 flex flex-wrap gap-2">
								<Button
									type="button"
									size="sm"
									variant="outline"
									onClick={() => inputRef[slot.role].current?.click()}
								>
									<Upload className="size-3.5" />
									上传文件
								</Button>
								<Button
									type="button"
									size="sm"
									variant="outline"
									onClick={() => onPickProject(slot.role)}
								>
									<FolderOpen className="size-3.5" />
									项目文件
								</Button>
							</div>
						</div>
					</section>
				);
			})}
			{slots.some((slot) => slot.role === "roster") ? (
				<input
					ref={rosterInputRef}
					aria-label="上传人员底表文件"
					type="file"
					className="hidden"
					multiple
					accept=".xlsx"
					onChange={(event) => {
						onAddFiles(event.target.files, "roster");
						event.target.value = "";
					}}
				/>
			) : null}
			{slots.some((slot) => slot.role === "historical_payroll") ? (
				<input
					ref={historicalInputRef}
					aria-label="上传历史工资文件"
					type="file"
					className="hidden"
					multiple
					accept=".xlsx"
					onChange={(event) => {
						onAddFiles(event.target.files, "historical_payroll");
						event.target.value = "";
					}}
				/>
			) : null}
			{slots.some((slot) => slot.role === "attendance") ? (
				<input
					ref={attendanceInputRef}
					aria-label="上传当月考勤文件"
					type="file"
					className="hidden"
					multiple
					accept=".xlsx,.pdf"
					onChange={(event) => {
						onAddFiles(event.target.files, "attendance");
						event.target.value = "";
					}}
				/>
			) : null}
		</>
	);
}
