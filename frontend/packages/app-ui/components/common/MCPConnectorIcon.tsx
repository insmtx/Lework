import { cn } from "@leros/ui/lib/utils";
import { Server } from "lucide-react";

const BAIDU_NETDISK_ICON_SRC = new URL("../../assets/icons/baidu-netdisk.svg", import.meta.url)
	.href;
const COREKG_ICON_SRC = new URL("../../assets/icons/corekg.svg", import.meta.url).href;
const NETEASE_MAIL_ICON_SRC = new URL("../../assets/icons/netease-mail.svg", import.meta.url).href;

function isBaiduNetdiskConnector(code: string) {
	const normalizedCode = code.trim().toLocaleLowerCase();
	return normalizedCode === "baidu-netdisk" || normalizedCode.startsWith("baidu-netdisk-");
}

function isCoreKGConnector(code: string) {
	const normalizedCode = code.trim().toLocaleLowerCase();
	return normalizedCode === "corekg" || normalizedCode.startsWith("corekg-");
}

function isNeteaseMailConnector(code: string) {
	const normalizedCode = code.trim().toLocaleLowerCase();
	return normalizedCode === "netease-mail" || normalizedCode.startsWith("netease-mail-");
}

export function MCPConnectorIcon({
	code,
	name,
	className,
}: {
	code: string;
	name?: string;
	className?: string;
}) {
	if (isBaiduNetdiskConnector(code)) {
		return (
			<img
				src={BAIDU_NETDISK_ICON_SRC}
				alt={`${name || "百度网盘"} Logo`}
				className={cn("size-7 shrink-0 rounded-lg", className)}
			/>
		);
	}

	if (isCoreKGConnector(code)) {
		return (
			<img
				src={COREKG_ICON_SRC}
				alt={`${name || "知识库"} Logo`}
				className={cn("size-7 shrink-0 rounded-lg", className)}
			/>
		);
	}

	if (isNeteaseMailConnector(code)) {
		return (
			<img
				src={NETEASE_MAIL_ICON_SRC}
				alt={`${name || "网易邮箱"} Logo`}
				className={cn("size-7 shrink-0 rounded-lg", className)}
			/>
		);
	}

	return (
		<div
			className={cn(
				"flex size-7 shrink-0 items-center justify-center rounded-lg bg-blue-50 text-blue-600",
				className,
			)}
		>
			<Server className="size-3.5" />
		</div>
	);
}
