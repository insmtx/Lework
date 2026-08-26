import { mkdir, writeFile } from "node:fs/promises"
import { dirname, extname, join, resolve } from "node:path"
import process from "node:process"
import { fileURLToPath } from "node:url"

const currentDir = dirname(fileURLToPath(import.meta.url))
const appDir = resolve(currentDir, "..")
const rendererPublicDir = join(appDir, "src/renderer/public")

const LOGO_CANDIDATES = ["logo.svg", "logo.png"]
const MODE_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/
const LOGO_OBJECT_PREFIX = "fronted"

export function sanitizeDeployMode (value) {
	const mode = typeof value === "string" ? value.trim() : ""
	if (!mode) return ""
	if (mode.includes("..") || mode.includes("/") || mode.includes("\\") || !MODE_PATTERN.test(mode)) {
		throw new Error(`Invalid LEROS_DEPLOY_MODE: ${value}`)
	}
	return mode
}

/** 规范化可选 S3 域名；只接受 http(s)，去掉末尾斜杠。 */
export function sanitizeS3Domain (value) {
	const raw = typeof value === "string" ? value.trim() : ""
	if (!raw) return ""
	let parsed
	try {
		parsed = new URL(raw)
	} catch {
		throw new Error(`Invalid LEROS_DEPLOY_S3_DOMAIN: ${value}`)
	}
	if ((parsed.protocol !== "https:" && parsed.protocol !== "http:") || parsed.username || parsed.password) {
		throw new Error(`Invalid LEROS_DEPLOY_S3_DOMAIN: ${value}`)
	}
	const path = parsed.pathname.replace(/\/+$/, "")
	return path && path !== "/" ? `${parsed.origin}${path}` : parsed.origin
}

export function renderDeployConfigScript (config) {
	return `window.__DEPLOYCONFIG = ${JSON.stringify(config, null, 2)};\n`
}

async function downloadLogoFromS3 (s3Domain, mode, brandDir, fetchImpl) {
	const prefix = `${s3Domain}/${LOGO_OBJECT_PREFIX}/${mode}`
	for (const fileName of LOGO_CANDIDATES) {
		const url = `${prefix}/${fileName}`
		let response
		try {
			response = await fetchImpl(url)
		} catch (error) {
			const message = error instanceof Error ? error.message : String(error)
			throw new Error(`Failed to download logo ${url}: ${message}`)
		}
		if (response.status === 404) continue
		if (!response.ok) {
			throw new Error(`Failed to download logo ${url}: HTTP ${response.status}`)
		}
		const bytes = Buffer.from(await response.arrayBuffer())
		if (bytes.length === 0) continue
		const outputName = `logo${extname(fileName).toLowerCase()}`
		await writeFile(join(brandDir, outputName), bytes)
		console.log(`[inject-deploy-config] using private logo ${url}`)
		return `./brand/${outputName}`
	}
	return ""
}

export async function injectDeployConfig ({
	mode = process.env.LEROS_DEPLOY_MODE,
	appName = process.env.LEROS_DEPLOY_APP_NAME,
	s3Domain = process.env.LEROS_DEPLOY_S3_DOMAIN,
	deploymentMode = process.env.VITE_LEROS_DEPLOYMENT_MODE,
	rendererPublicDir: rendererPublicDirArg = rendererPublicDir,
	fetchImpl = globalThis.fetch,
} = {}) {
	const version = deploymentMode === "private" ? "private" : "public"
	const sanitizedMode = version === "private" ? sanitizeDeployMode(mode) : ""
	const trimmedAppName = version === "private" && typeof appName === "string" ? appName.trim() : ""
	const sanitizedS3Domain = version === "private" ? sanitizeS3Domain(s3Domain) : ""
	const resolvedPublicDir = rendererPublicDirArg
	const resolvedBrandDir = join(resolvedPublicDir, "brand")
	const resolvedConfigPath = join(resolvedPublicDir, "config.js")

	await mkdir(resolvedBrandDir, { recursive: true })

	let logoPublicPath = ""
	if (sanitizedS3Domain && !sanitizedMode) {
		throw new Error("LEROS_DEPLOY_S3_DOMAIN requires LEROS_DEPLOY_MODE")
	}
	if (sanitizedMode && sanitizedS3Domain) {
		logoPublicPath = await downloadLogoFromS3(sanitizedS3Domain, sanitizedMode, resolvedBrandDir, fetchImpl)
		if (!logoPublicPath) {
			const prefix = `${sanitizedS3Domain}/${LOGO_OBJECT_PREFIX}/${sanitizedMode}`
			throw new Error(
				`未找到定制 Logo：${prefix}/logo.svg 与 ${prefix}/logo.png 均不存在或为空，打包已中止`,
			)
		}
	} else if (sanitizedMode) {
		console.log(
			`[inject-deploy-config] LEROS_DEPLOY_MODE=${sanitizedMode} but LEROS_DEPLOY_S3_DOMAIN is empty; keep default Lework logo`,
		)
	}

	const config = {
		version,
		mode: sanitizedMode,
		appName: trimmedAppName || "Lework",
		logo: logoPublicPath,
	}

	await writeFile(resolvedConfigPath, renderDeployConfigScript(config), "utf8")
	console.log(`[inject-deploy-config] wrote ${resolvedConfigPath}`)
	return config
}

const isDirectRun = process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)
if (isDirectRun) {
	await injectDeployConfig()
}
