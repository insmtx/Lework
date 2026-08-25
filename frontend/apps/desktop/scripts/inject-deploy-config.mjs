import { copyFile, mkdir, readdir, writeFile } from "node:fs/promises"
import { existsSync } from "node:fs"
import { dirname, extname, join, resolve } from "node:path"
import process from "node:process"
import { fileURLToPath } from "node:url"

const currentDir = dirname(fileURLToPath(import.meta.url))
const appDir = resolve(currentDir, "..")
const frontendRoot = resolve(appDir, "../..")
const rendererPublicDir = join(appDir, "src/renderer/public")

const LOGO_CANDIDATES = ["logo.svg", "logo.png"]
const MODE_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$/

export function sanitizeDeployMode (value) {
	const mode = typeof value === "string" ? value.trim() : ""
	if (!mode) return ""
	if (mode.includes("..") || mode.includes("/") || mode.includes("\\") || !MODE_PATTERN.test(mode)) {
		throw new Error(`Invalid LEROS_DEPLOY_MODE: ${value}`)
	}
	return mode
}

export function renderDeployConfigScript (config) {
	return `window.__DEPLOYCONFIG = ${JSON.stringify(config, null, 2)};\n`
}

function pickLogoFile (modeDir) {
	for (const fileName of LOGO_CANDIDATES) {
		const filePath = join(modeDir, fileName)
		if (existsSync(filePath)) return filePath
	}
	return null
}

export async function injectDeployConfig ({
	mode = process.env.LEROS_DEPLOY_MODE,
	appName = process.env.LEROS_DEPLOY_APP_NAME,
	deploymentMode = process.env.VITE_LEROS_DEPLOYMENT_MODE,
	frontendRoot: frontendRootArg = frontendRoot,
	rendererPublicDir: rendererPublicDirArg = rendererPublicDir,
} = {}) {
	const sanitizedMode = sanitizeDeployMode(mode)
	const trimmedAppName = typeof appName === "string" ? appName.trim() : ""
	const version = deploymentMode === "private" ? "private" : "public"
	const resolvedPublicDir = rendererPublicDirArg
	const resolvedBrandDir = join(resolvedPublicDir, "brand")
	const resolvedConfigPath = join(resolvedPublicDir, "config.js")
	const resolvedLogoRoot = join(frontendRootArg, "private/logo")

	await mkdir(resolvedBrandDir, { recursive: true })

	let logoPublicPath = ""
	if (sanitizedMode) {
		const modeDir = join(resolvedLogoRoot, sanitizedMode)
		if (existsSync(modeDir)) {
			const logoFile = pickLogoFile(modeDir)
			if (!logoFile) {
				const entries = await readdir(modeDir)
				throw new Error(
					`frontend/private/logo/${sanitizedMode}/ exists but has no logo.svg or logo.png (${entries.join(", ") || "empty"})`,
				)
			}
			const outputName = `logo${extname(logoFile).toLowerCase()}`
			await copyFile(logoFile, join(resolvedBrandDir, outputName))
			logoPublicPath = `./brand/${outputName}`
			console.log(`[inject-deploy-config] using private logo ${logoFile}`)
		} else {
			console.log(
				`[inject-deploy-config] LEROS_DEPLOY_MODE=${sanitizedMode} but logo directory is missing; keep default Lework logo`,
			)
		}
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
