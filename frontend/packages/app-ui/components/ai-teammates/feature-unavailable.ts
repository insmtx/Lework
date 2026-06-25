import { toast } from "sonner";

/** 中文注释：暂未上线功能的统一提示文案，供 AI 队友等静态页复用。 */
export const FEATURE_UNAVAILABLE_MESSAGE = "此功能暂未上线，敬请期待";

export function notifyFeatureUnavailable() {
	toast.message(FEATURE_UNAVAILABLE_MESSAGE);
}
