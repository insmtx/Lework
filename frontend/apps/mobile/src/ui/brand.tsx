import { Image } from "react-native";

const BRAND_LOGO = require("../../assets/logo.png");

/**
 * BrandMark 是应用内使用的品牌标识。
 *
 * 统一走 PNG 资源而不是重新画 SVG，避免移动端为一个小图标引入 svg 运行时；
 * 图标、启动图与通知图标由 scripts/generate-assets.mjs 从 Web logo 生成。
 */
export function BrandMark({ size = 28 }: { size?: number }) {
	return (
		<Image
			source={BRAND_LOGO}
			style={{ width: size, height: size }}
			resizeMode="contain"
			accessibilityRole="image"
			accessibilityLabel="lework"
		/>
	);
}
