"use client";

import { BRANDING_CHANGED_EVENT, readBrandLogo, readBrandName } from "@leros/store";
import { useEffect, useState } from "react";

/** 订阅本地品牌配置变更，供侧边栏等展示位实时回读。 */
export function useBrandIdentity() {
	const [logo, setLogo] = useState<string | null>(() => readBrandLogo());
	const [name, setName] = useState(() => readBrandName());

	useEffect(() => {
		const sync = () => {
			setLogo(readBrandLogo());
			setName(readBrandName());
		};
		window.addEventListener(BRANDING_CHANGED_EVENT, sync);
		window.addEventListener("storage", sync);
		return () => {
			window.removeEventListener(BRANDING_CHANGED_EVENT, sync);
			window.removeEventListener("storage", sync);
		};
	}, []);

	useEffect(() => {
		document.title = name;
	}, [name]);

	return { logo, name };
}
