import { describe, expect, it } from "vitest";
import type { ControlEvent, FormField, PendingForm } from "./types";
import {
	applyCustom,
	buildAnswer,
	FormValidationError,
	initialAnswers,
	isFieldActive,
	mergeForm,
	parseForm,
	validateForm,
	visibleFields,
} from "./forms";

function event(type: string, data: unknown): ControlEvent {
	return { node_id: "node_1", seq: 1, type, data, time: "2026-11-24T00:00:00Z" };
}

function form(fields: FormField[], extra: Partial<PendingForm> = {}): PendingForm {
	return { id: "frm_1", session_id: "ses_1", title: "Questions", fields, ...extra };
}

describe("parseForm", () => {
	it("解析 data.form", () => {
		const parsed = parseForm(
			event("form.created", {
				form: {
					id: "frm_1",
					sessionID: "ses_1",
					title: "Questions",
					metadata: { kind: "question" },
					fields: [{ key: "q0", type: "string" }],
				},
			}),
		);
		expect(parsed?.id).toBe("frm_1");
		expect(parsed?.session_id).toBe("ses_1");
		expect(parsed?.metadata).toEqual({ kind: "question" });
		expect(parsed?.fields).toHaveLength(1);
	});

	it("保留 global 会话", () => {
		const parsed = parseForm(
			event("form.created", { form: { id: "frm_2", sessionID: "global", fields: [] } }),
		);
		expect(parsed?.session_id).toBe("global");
	});

	it("缺 id 或 fields 返回 null", () => {
		expect(parseForm(event("form.created", { form: { fields: [] } }))).toBeNull();
		expect(parseForm(event("form.created", { form: { id: "frm_1" } }))).toBeNull();
		expect(parseForm(event("form.created", null))).toBeNull();
		expect(parseForm(event("form.replied", { form: { id: "x", fields: [] } }))).toBeNull();
	});
});

describe("mergeForm", () => {
	const created = event("form.created", {
		form: { id: "frm_1", sessionID: "ses_1", title: "Q", fields: [] },
	});

	it("created 按 id 去重", () => {
		const once = mergeForm([], created);
		const twice = mergeForm(once, created);
		expect(twice).toHaveLength(1);
	});

	it("replied / cancelled 用 data.id 删除", () => {
		const list = mergeForm([], created);
		expect(mergeForm(list, event("form.replied", { id: "frm_1" }))).toHaveLength(0);
		expect(mergeForm(list, event("form.cancelled", { id: "frm_1" }))).toHaveLength(0);
	});

	it("其他事件原样返回", () => {
		const list = mergeForm([], created);
		expect(mergeForm(list, event("permission.asked", { id: "frm_1" }))).toBe(list);
	});
});

describe("initialAnswers", () => {
	it("default 生效、boolean 缺省 false、其他不填", () => {
		const answers = initialAnswers(
			form([
				{ key: "s", type: "string", default: "hi" },
				{ key: "b", type: "boolean" },
				{ key: "n", type: "number" },
				{ key: "m", type: "multiselect", default: ["a"] },
			]),
		);
		expect(answers).toEqual({ s: "hi", b: false, m: ["a"] });
	});
});

describe("isFieldActive", () => {
	it("无 when 为 true", () => {
		expect(isFieldActive({ key: "q", type: "string" }, {})).toBe(true);
	});

	it("依赖未回答时 eq 与 neq 都不满足", () => {
		const field: FormField = { key: "q", type: "string", when: [{ key: "a", op: "eq", value: "x" }] };
		expect(isFieldActive(field, {})).toBe(false);
		const neq: FormField = { key: "q", type: "string", when: [{ key: "a", op: "neq", value: "x" }] };
		expect(isFieldActive(neq, {})).toBe(false);
	});

	it("eq 严格相等、neq 取反", () => {
		const eq: FormField = { key: "q", type: "string", when: [{ key: "a", op: "eq", value: "x" }] };
		expect(isFieldActive(eq, { a: "x" })).toBe(true);
		expect(isFieldActive(eq, { a: "y" })).toBe(false);
		const num: FormField = { key: "q", type: "string", when: [{ key: "a", op: "eq", value: 2 }] };
		expect(isFieldActive(num, { a: 2 })).toBe(true);
		expect(isFieldActive(num, { a: "2" })).toBe(false);
		expect(isFieldActive(eq, { a: "y" })).toBe(false);
		const neq: FormField = { key: "q", type: "string", when: [{ key: "a", op: "neq", value: "x" }] };
		expect(isFieldActive(neq, { a: "y" })).toBe(true);
		expect(isFieldActive(neq, { a: "x" })).toBe(false);
	});

	it("multiselect 用包含语义", () => {
		const eq: FormField = { key: "q", type: "string", when: [{ key: "a", op: "eq", value: "x" }] };
		expect(isFieldActive(eq, { a: ["x", "y"] })).toBe(true);
		expect(isFieldActive(eq, { a: ["y"] })).toBe(false);
		const neq: FormField = { key: "q", type: "string", when: [{ key: "a", op: "neq", value: "x" }] };
		expect(isFieldActive(neq, { a: ["x"] })).toBe(false);
		expect(isFieldActive(neq, { a: ["y"] })).toBe(true);
	});

	it("多条 when 全部满足才 active", () => {
		const field: FormField = {
			key: "q",
			type: "string",
			when: [
				{ key: "a", op: "eq", value: "x" },
				{ key: "b", op: "eq", value: true },
			],
		};
		expect(isFieldActive(field, { a: "x", b: true })).toBe(true);
		expect(isFieldActive(field, { a: "x", b: false })).toBe(false);
	});
});

describe("visibleFields", () => {
	it("过滤 hidden 与非 active", () => {
		const f = form([
			{ key: "a", type: "string" },
			{ key: "b", type: "string", hidden: true },
			{ key: "c", type: "string", when: [{ key: "a", op: "eq", value: "show" }] },
		]);
		expect(visibleFields(f, { a: "x" }).map((v) => v.key)).toEqual(["a"]);
		expect(visibleFields(f, { a: "show" }).map((v) => v.key)).toEqual(["a", "c"]);
	});
});

describe("validateForm", () => {
	it("required 空值报错", () => {
		const errors = validateForm(form([{ key: "q0", type: "string", required: true }]), { q0: "" });
		expect(errors).toHaveLength(1);
		expect(errors[0]?.fieldKey).toBe("q0");
		expect(errors[0]?.message).toContain("必填");
	});

	it("number / integer 校验", () => {
		const f = form([{ key: "n", type: "number" }, { key: "i", type: "integer" }]);
		expect(validateForm(f, { n: "abc" })[0]?.fieldKey).toBe("n");
		expect(validateForm(f, { i: 1.5 })[0]?.fieldKey).toBe("i");
		expect(validateForm(f, { n: 5, i: 2 })).toHaveLength(0);
	});

	it("min / max 校验", () => {
		const f = form([{ key: "n", type: "number", minimum: 1, maximum: 10 }]);
		expect(validateForm(f, { n: 0 })).toHaveLength(1);
		expect(validateForm(f, { n: 11 })).toHaveLength(1);
		expect(validateForm(f, { n: 5 })).toHaveLength(0);
	});

	it("multiselect minItems / maxItems", () => {
		const f = form([{ key: "m", type: "multiselect", minItems: 2, maxItems: 3 }]);
		expect(validateForm(f, { m: ["a"] })).toHaveLength(1);
		expect(validateForm(f, { m: ["a", "b", "c", "d"] })).toHaveLength(1);
		expect(validateForm(f, { m: ["a", "b"] })).toHaveLength(0);
	});

	it("string minLength / maxLength", () => {
		const f = form([{ key: "s", type: "string", minLength: 2, maxLength: 4 }]);
		expect(validateForm(f, { s: "a" })).toHaveLength(1);
		expect(validateForm(f, { s: "abcde" })).toHaveLength(1);
		expect(validateForm(f, { s: "abc" })).toHaveLength(0);
	});

	it("external active 未确认报错、hidden external 不报错", () => {
		const active = form([{ key: "e", type: "external", required: true, url: "https://x" }]);
		expect(validateForm(active, {})).toHaveLength(1);
		expect(validateForm(active, { e: true })).toHaveLength(0);
		const hidden = form([{ key: "e", type: "external", hidden: true }]);
		expect(validateForm(hidden, {})).toHaveLength(0);
	});

	it("hidden + required + 无 default 报不兼容", () => {
		const f = form([{ key: "h", type: "string", hidden: true, required: true }]);
		const errors = validateForm(f, {});
		expect(errors).toHaveLength(1);
		expect(errors[0]?.message).toContain("不兼容");
	});

	it("返回多条错误", () => {
		const f = form([
			{ key: "a", type: "string", required: true },
			{ key: "b", type: "string", required: true },
		]);
		expect(validateForm(f, {})).toHaveLength(2);
	});
});

describe("buildAnswer", () => {
	it("只提交 active 非 external 字段，external 恒为 true", () => {
		const f = form([
			{ key: "a", type: "string" },
			{ key: "hidden_external", type: "external", hidden: true },
			{ key: "e", type: "external", url: "https://x" },
			{ key: "off", type: "string", when: [{ key: "a", op: "eq", value: "show" }] },
		]);
		const answer = buildAnswer(f, { a: "x", hidden_external: false, e: true });
		expect(answer).toEqual({ a: "x", hidden_external: true, e: true });
	});

	it("hidden + default 在 active 时提交 default", () => {
		const f = form([{ key: "h", type: "string", hidden: true, default: "auto" }]);
		expect(buildAnswer(f, {})).toEqual({ h: "auto" });
	});

	it("校验失败抛 FormValidationError", () => {
		const f = form([{ key: "q0", type: "string", required: true }]);
		try {
			buildAnswer(f, {});
			throw new Error("expected throw");
		} catch (error) {
			expect(error).toBeInstanceOf(FormValidationError);
			expect((error as FormValidationError).fieldKey).toBe("q0");
		}
	});
});

describe("applyCustom", () => {
	it("string 覆盖、multiselect 追加、空串不改", () => {
		expect(applyCustom({ key: "s", type: "string" }, "a", "b")).toBe("b");
		expect(applyCustom({ key: "m", type: "multiselect" }, ["a"], "b")).toEqual(["a", "b"]);
		expect(applyCustom({ key: "s", type: "string" }, "a", "  ")).toBe("a");
	});
});
