/**
 * 生成推送提示音。
 *
 * 两个提示音必须同名出现在 `app.json` 的 expo-notifications `sounds` 数组里，
 * 否则 Android 渠道找不到资源会回落到系统默认音、iOS 推送也会退回默认音。
 * 参数改完重新生成：
 *   pnpm --filter @lework/mobile run sounds
 *
 * 输出是 44.1kHz / 16-bit / 单声道 WAV：时长都在半秒内，控制安装包体积。
 */
import { mkdir, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";

const SAMPLE_RATE = 44_100;
const AMPLITUDE = 0.32;
const SOUNDS_DIR = new URL("../assets/sounds/", import.meta.url);

/** note 生成一段带起音与指数衰减的正弦音，避免方波和硬切造成爆音。 */
function note(freq, durationMs, { attackMs = 12, decay = 3.4 } = {}) {
	const length = Math.round((SAMPLE_RATE * durationMs) / 1000);
	const attack = Math.round((SAMPLE_RATE * attackMs) / 1000);
	const samples = new Float64Array(length);
	for (let i = 0; i < length; i++) {
		const envelope =
			i < attack ? i / attack : Math.exp((-decay * (i - attack)) / Math.max(1, length - attack));
		samples[i] = Math.sin((2 * Math.PI * freq * i) / SAMPLE_RATE) * AMPLITUDE * envelope;
	}
	return samples;
}

function silence(durationMs) {
	return new Float64Array(Math.round((SAMPLE_RATE * durationMs) / 1000));
}

function concat(parts) {
	const total = parts.reduce((sum, part) => sum + part.length, 0);
	const samples = new Float64Array(total);
	let offset = 0;
	for (const part of parts) {
		samples.set(part, offset);
		offset += part.length;
	}
	return samples;
}

/** toWav 写标准 44 字节头加 16-bit PCM 数据。 */
function toWav(samples) {
	const dataBytes = samples.length * 2;
	const buffer = Buffer.alloc(44 + dataBytes);
	buffer.write("RIFF", 0, "ascii");
	buffer.writeUInt32LE(36 + dataBytes, 4);
	buffer.write("WAVE", 8, "ascii");
	buffer.write("fmt ", 12, "ascii");
	buffer.writeUInt32LE(16, 16); // fmt 块长度
	buffer.writeUInt16LE(1, 20); // PCM
	buffer.writeUInt16LE(1, 22); // 单声道
	buffer.writeUInt32LE(SAMPLE_RATE, 24);
	buffer.writeUInt32LE(SAMPLE_RATE * 2, 28); // 字节率
	buffer.writeUInt16LE(2, 32); // 块对齐
	buffer.writeUInt16LE(16, 34); // 位深
	buffer.write("data", 36, "ascii");
	buffer.writeUInt32LE(dataBytes, 40);
	for (let i = 0; i < samples.length; i++) {
		const clamped = Math.max(-1, Math.min(1, samples[i]));
		buffer.writeInt16LE(Math.round(clamped * 32_767), 44 + i * 2);
	}
	return buffer;
}

// 审批：上行两声短促提示，用来打断用户。
const approval = concat([note(988, 110), silence(55), note(1319, 150)]);
// 完成：下行两声柔和收敛，表示事情结束。
const done = concat([note(784, 130), silence(45), note(523.25, 260)]);

await mkdir(SOUNDS_DIR, { recursive: true });
await writeFile(new URL("approval.wav", SOUNDS_DIR), toWav(approval));
await writeFile(new URL("done.wav", SOUNDS_DIR), toWav(done));

console.log(
	`wrote approval.wav (${(approval.length / SAMPLE_RATE).toFixed(2)}s) and done.wav (${(
		done.length / SAMPLE_RATE
	).toFixed(2)}s) to ${fileURLToPath(SOUNDS_DIR)}`,
);
