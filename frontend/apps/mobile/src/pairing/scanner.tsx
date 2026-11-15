import { CameraView, useCameraPermissions } from "expo-camera";
import { useEffect, useRef, useState } from "react";
import { Linking, Modal, Pressable, StyleSheet, Text, View } from "react-native";

import { type PairPayload, parsePairLink } from "./link";

export interface PairScannerProps {
	visible: boolean;
	onClose: () => void;
	onScanned: (payload: PairPayload) => void;
}

/**
 * PairScanner 用相机扫控制面打印的配对二维码。
 *
 * 只在识别出合法链接时回调一次：相机每帧都会回调，必须用 ref 去重，
 * 否则一次扫描会连续触发多次连接。识别失败不解码、不关闭，让用户重扫。
 */
/**
 * explainScanFailure 把扫描失败翻译成用户能照做的提示。
 *
 * 最常见的一种错是扫了 Metro 的二维码：`expo start` 也在终端打印二维码，
 * 内容是不带查询串的 exp:// 地址，和配对码长得很像。
 */
function explainScanFailure(data: string, scanError: unknown): string {
	const message = scanError instanceof Error ? scanError.message : "二维码无法识别";
	if (/^exps?:\/\//i.test(data.trim()) && !data.includes("?")) {
		return "这是 Expo 开发服务器的二维码；请扫控制面启动时打印的那一个（二维码下方带 token 的链接）";
	}
	return message;
}

export function PairScanner({ visible, onClose, onScanned }: PairScannerProps) {
	const [permission, requestPermission] = useCameraPermissions();
	const [error, setError] = useState<string | null>(null);
	const [raw, setRaw] = useState<string | null>(null);
	const handled = useRef(false);

	useEffect(() => {
		if (!visible) {
			handled.current = false;
			setError(null);
			setRaw(null);
			return;
		}
		if (permission && !permission.granted && permission.canAskAgain) {
			void requestPermission();
		}
	}, [permission, requestPermission, visible]);

	const handleBarcode = (data: string) => {
		if (handled.current) return;
		try {
			const payload = parsePairLink(data);
			handled.current = true;
			onScanned(payload);
		} catch (scanError) {
			// 把识别到的原文留在屏幕上：扫错码时（例如顺手扫了 Metro 的二维码）
			// 用户能直接看出扫到了什么，不用再去猜。
			setRaw(data);
			setError(explainScanFailure(data, scanError));
		}
	};

	const askForPermission = () => {
		if (permission?.canAskAgain === false) {
			void Linking.openSettings();
			return;
		}
		void requestPermission();
	};

	return (
		<Modal visible={visible} animationType="slide" onRequestClose={onClose}>
			<View style={styles.screen}>
				{permission?.granted ? (
					<CameraView
						style={StyleSheet.absoluteFill}
						facing="back"
						barcodeScannerSettings={{ barcodeTypes: ["qr"] }}
						onBarcodeScanned={({ data }) => handleBarcode(data)}
					/>
				) : (
					<View style={styles.permission}>
						<Text style={styles.permissionText}>
							{permission?.canAskAgain === false
								? "相机权限已被拒绝，请到系统设置里开启。"
								: "需要相机权限才能扫描配对二维码。"}
						</Text>
						<Pressable style={styles.button} onPress={askForPermission}>
							<Text style={styles.buttonText}>
								{permission?.canAskAgain === false ? "打开系统设置" : "允许使用相机"}
							</Text>
						</Pressable>
					</View>
				)}

				<View style={styles.hintWrap} pointerEvents="none">
					<Text style={styles.hint}>对准控制面启动时打印的二维码</Text>
					{error ? <Text style={styles.error}>{error}</Text> : null}
					{raw ? (
						<Text style={styles.raw} selectable numberOfLines={4}>
							识别到的内容：{raw}
						</Text>
					) : null}
				</View>

				<Pressable style={styles.close} onPress={onClose} accessibilityRole="button">
					<Text style={styles.closeText}>关闭</Text>
				</Pressable>
			</View>
		</Modal>
	);
}

const styles = StyleSheet.create({
	screen: { flex: 1, backgroundColor: "#000" },
	permission: {
		flex: 1,
		alignItems: "center",
		justifyContent: "center",
		gap: 12,
		paddingHorizontal: 24,
	},
	permissionText: { color: "#e2e8f0", fontSize: 14, textAlign: "center" },
	button: {
		paddingHorizontal: 18,
		paddingVertical: 10,
		borderRadius: 10,
		backgroundColor: "#f8fafc",
	},
	buttonText: { color: "#0f172a", fontSize: 15, fontWeight: "600" },
	hintWrap: { position: "absolute", left: 0, right: 0, bottom: 96, alignItems: "center", gap: 6 },
	hint: { color: "#e2e8f0", fontSize: 14 },
	error: { color: "#fca5a5", fontSize: 13, paddingHorizontal: 24, textAlign: "center" },
	raw: {
		color: "#cbd5e1",
		fontSize: 11,
		paddingHorizontal: 24,
		textAlign: "center",
	},
	close: {
		position: "absolute",
		bottom: 32,
		alignSelf: "center",
		paddingHorizontal: 24,
		paddingVertical: 10,
		borderRadius: 10,
		backgroundColor: "rgba(248,250,252,0.16)",
	},
	closeText: { color: "#f8fafc", fontSize: 15, fontWeight: "600" },
});
