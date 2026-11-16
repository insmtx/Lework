// 版本比较工具：规则与桌面端 frontend/apps/desktop/src/main/linux-update.ts 的
// isVersionNewer 保持一致，供发布脚本判断 RELEASE.txt 是否应单调前进。
import process from "node:process";
import { pathToFileURL } from "node:url";

// 解析可选 v 前缀、点分数字主版本、-prerelease 与 +build 后缀；build 后缀不参与比较。
const VERSION_PATTERN = /^v?(\d+(?:\.\d+)*)(?:-([0-9A-Za-z.-]+))?(?:\+[0-9A-Za-z.-]+)?$/;

function parseVersion(value) {
  if (typeof value !== "string") {
    return null;
  }

  const match = value.trim().match(VERSION_PATTERN);
  if (!match) {
    return null;
  }

  return {
    main: match[1].split(".").map(Number),
    prerelease: match[2]?.split(".") ?? [],
  };
}

// 预发布比较：无预发布段视为稳定版、高于任何预发布；数字段按数值，非数字按字符串，
// 数字段低于非数字段；短的一方在共享前缀下更小。
function comparePrerelease(left, right) {
  if (left.length === 0 || right.length === 0) {
    if (left.length === right.length) return 0;
    return left.length === 0 ? 1 : -1;
  }

  const length = Math.max(left.length, right.length);
  for (let index = 0; index < length; index += 1) {
    const leftValue = left[index];
    const rightValue = right[index];
    if (leftValue === undefined || rightValue === undefined) {
      if (leftValue === rightValue) return 0;
      return leftValue === undefined ? -1 : 1;
    }
    if (leftValue === rightValue) continue;

    const leftNumber = /^\d+$/.test(leftValue) ? Number(leftValue) : null;
    const rightNumber = /^\d+$/.test(rightValue) ? Number(rightValue) : null;
    if (leftNumber !== null && rightNumber !== null) {
      return leftNumber > rightNumber ? 1 : -1;
    }
    if (leftNumber !== null || rightNumber !== null) {
      return leftNumber !== null ? -1 : 1;
    }
    return leftValue > rightValue ? 1 : -1;
  }

  return 0;
}

/** 比较两个版本号，返回 1 / 0 / -1；非法版本抛错。 */
export function compareVersions(left, right) {
  const leftVersion = parseVersion(left);
  const rightVersion = parseVersion(right);
  if (!leftVersion || !rightVersion) {
    throw new Error(`无法比较版本号：${left} / ${right}`);
  }

  const length = Math.max(leftVersion.main.length, rightVersion.main.length);
  for (let index = 0; index < length; index += 1) {
    const leftPart = leftVersion.main[index] ?? 0;
    const rightPart = rightVersion.main[index] ?? 0;
    if (leftPart !== rightPart) {
      return leftPart > rightPart ? 1 : -1;
    }
  }

  return comparePrerelease(leftVersion.prerelease, rightVersion.prerelease);
}

/** 仅纯数字点分版本（无 v 前缀、无预发布与 build 后缀）视为稳定版。 */
export function isStableVersion(value) {
  if (typeof value !== "string") {
    return false;
  }

  const raw = value.trim();
  if (raw.startsWith("v")) {
    return false;
  }

  const parsed = parseVersion(raw);
  if (!parsed) {
    return false;
  }

  return parsed.prerelease.length === 0 && !raw.includes("+");
}

// 仅在作为脚本直接执行时启用 CLI；被 import 时不产生副作用。
if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const [command, left, right] = process.argv.slice(2);
  try {
    if (command === "gt") {
      process.stdout.write(`${compareVersions(left, right) > 0}\n`);
    } else if (command === "is-stable") {
      process.stdout.write(`${isStableVersion(left)}\n`);
    } else {
      process.stderr.write("用法：version.mjs <gt <left> <right> | is-stable <version>>\n");
      process.exitCode = 1;
    }
  } catch (error) {
    process.stderr.write(`${error.message}\n`);
    process.exitCode = 1;
  }
}
