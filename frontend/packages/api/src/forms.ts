// 表单解析与校验是 Web 与移动端共用的纯逻辑：两端都不应该各自实现一遍
// `when`、hidden 与 external 规则，否则同一张表单在两个界面上会得到不同结论。
import type { ControlEvent, FormAnswerValue, FormField, PendingForm } from "./types";

export class FormValidationError extends Error {
	constructor(
		readonly fieldKey: string,
		message: string,
	) {
		super(message);
		this.name = "FormValidationError";
	}
}

/** parseForm 从 form.created 的 data.form 解析一张表单；结构不完整时返回 null。 */
export function parseForm(event: ControlEvent): PendingForm | null {
	if (event.type !== "form.created" || !isRecord(event.data)) return null;
	const form = event.data.form;
	if (!isRecord(form)) return null;
	if (typeof form.id !== "string" || form.id === "") return null;
	if (!Array.isArray(form.fields)) return null;

	const sessionID =
		typeof form.sessionID === "string" && form.sessionID !== ""
			? form.sessionID
			: (event.session_id ?? "global");

	return {
		id: form.id,
		session_id: sessionID,
		title: typeof form.title === "string" ? form.title : "",
		metadata: isRecord(form.metadata) ? (form.metadata as PendingForm["metadata"]) : undefined,
		fields: form.fields as FormField[],
	};
}

/** mergeForm 把表单事件折进列表；未知事件原样返回，避免多余渲染。 */
export function mergeForm(list: PendingForm[], event: ControlEvent): PendingForm[] {
	switch (event.type) {
		case "form.created": {
			const form = parseForm(event);
			if (!form || list.some((item) => item.id === form.id)) return list;
			return [...list, form];
		}
		case "form.replied":
		case "form.cancelled": {
			if (!isRecord(event.data) || typeof event.data.id !== "string") return list;
			const formID = event.data.id;
			return list.filter((form) => form.id !== formID);
		}
		default:
			return list;
	}
}

/** initialAnswers 给出字段默认值；boolean 缺省 false，其余缺省不填。 */
export function initialAnswers(form: PendingForm): Record<string, FormAnswerValue> {
	const answers: Record<string, FormAnswerValue> = {};
	for (const field of form.fields) {
		if (field.default !== undefined) {
			answers[field.key] = Array.isArray(field.default) ? [...field.default] : field.default;
		} else if (field.type === "boolean") {
			answers[field.key] = false;
		}
	}
	return answers;
}

/** isFieldActive 按 when 判断字段是否生效；依赖未回答时一律不生效。 */
export function isFieldActive(
	field: FormField,
	answers: Record<string, FormAnswerValue>,
): boolean {
	if (!field.when || field.when.length === 0) return true;

	return field.when.every((condition) => {
		const current = answers[condition.key];
		if (current === undefined) return false;
		const equal = Array.isArray(current)
			? current.includes(condition.value as string)
			: current === condition.value;
		return condition.op === "eq" ? equal : !equal;
	});
}

/** visibleFields 返回界面上要渲染的字段：hidden 与非 active 都不渲染。 */
export function visibleFields(
	form: PendingForm,
	answers: Record<string, FormAnswerValue>,
): FormField[] {
	return form.fields.filter((field) => !field.hidden && isFieldActive(field, answers));
}

/** validateForm 返回全部字段错误，供 UI 按字段就地展示。 */
export function validateForm(
	form: PendingForm,
	answers: Record<string, FormAnswerValue>,
): FormValidationError[] {
	const errors: FormValidationError[] = [];

	for (const field of form.fields) {
		if (field.hidden && field.required && field.default === undefined) {
			errors.push(
				new FormValidationError(
					field.key,
					"表单包含隐藏必填字段，当前客户端不兼容，无法提交",
				),
			);
			continue;
		}

		if (field.hidden) continue;
		if (!isFieldActive(field, answers)) continue;

		const value = answers[field.key];

		if (field.type === "external") {
			if (value !== true) {
				errors.push(new FormValidationError(field.key, "请先打开链接并确认"));
			}
			continue;
		}

		if (field.required && isMissing(value)) {
			errors.push(new FormValidationError(field.key, "此项为必填"));
			continue;
		}
		if (isMissing(value)) continue;

		switch (field.type) {
			case "number":
			case "integer": {
				if (typeof value !== "number" || Number.isNaN(value)) {
					errors.push(new FormValidationError(field.key, "请输入有效数字"));
					break;
				}
				if (field.type === "integer" && !Number.isInteger(value)) {
					errors.push(new FormValidationError(field.key, "请输入整数"));
					break;
				}
				if (field.minimum !== undefined && value < field.minimum) {
					errors.push(new FormValidationError(field.key, `不得小于 ${field.minimum}`));
				}
				if (field.maximum !== undefined && value > field.maximum) {
					errors.push(new FormValidationError(field.key, `不得大于 ${field.maximum}`));
				}
				break;
			}
			case "multiselect": {
				if (!Array.isArray(value)) {
					errors.push(new FormValidationError(field.key, "请选择选项"));
					break;
				}
				if (field.minItems !== undefined && value.length < field.minItems) {
					errors.push(new FormValidationError(field.key, `至少选择 ${field.minItems} 项`));
				}
				if (field.maxItems !== undefined && value.length > field.maxItems) {
					errors.push(new FormValidationError(field.key, `最多选择 ${field.maxItems} 项`));
				}
				break;
			}
			case "string": {
				if (typeof value !== "string") {
					errors.push(new FormValidationError(field.key, "请输入文本"));
					break;
				}
				if (field.minLength !== undefined && value.length < field.minLength) {
					errors.push(new FormValidationError(field.key, `至少 ${field.minLength} 个字符`));
				}
				if (field.maxLength !== undefined && value.length > field.maxLength) {
					errors.push(new FormValidationError(field.key, `最多 ${field.maxLength} 个字符`));
				}
				break;
			}
		}
	}

	return errors;
}

/** buildAnswer 生成提交给 OpenCode 的答案；有错误时抛第一条。 */
export function buildAnswer(
	form: PendingForm,
	answers: Record<string, FormAnswerValue>,
): Record<string, FormAnswerValue> {
	const errors = validateForm(form, answers);
	if (errors.length > 0) throw errors[0];

	const answer: Record<string, FormAnswerValue> = {};
	for (const field of form.fields) {
		// external 一律提交 true：未渲染时用户看不到链接，也不能让服务端
		// 因为缺字段而继续等待。
		if (field.type === "external") {
			answer[field.key] = true;
			continue;
		}
		if (!isFieldActive(field, answers)) continue;

		if (field.hidden) {
			if (field.default !== undefined) answer[field.key] = field.default;
			continue;
		}

		const value = answers[field.key];
		if (value === undefined || isMissing(value)) continue;
		answer[field.key] = value;
	}
	return answer;
}

/** applyCustom 把自定义输入并入选项答案；空文本不改变原值。 */
export function applyCustom(
	field: FormField,
	value: FormAnswerValue | undefined,
	customText: string,
): FormAnswerValue | undefined {
	if (customText.trim() === "") return value;
	if (field.type === "multiselect") {
		return Array.isArray(value) ? [...value, customText] : [customText];
	}
	return customText;
}

function isMissing(value: FormAnswerValue | undefined | null): boolean {
	return (
		value === undefined ||
		value === null ||
		value === "" ||
		(Array.isArray(value) && value.length === 0)
	);
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}
