import { mkdir, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import pngToIco from 'png-to-ico'
import sharp from 'sharp'

const currentDir = dirname(fileURLToPath(import.meta.url))
const appDir = resolve(currentDir, '..')
const sourceLogo = resolve(appDir, '../../packages/app-ui/assets/logo.svg')
const sourceTrayLogo = resolve(appDir, '../../packages/app-ui/assets/logo-white.svg')
const resourcesDir = join(appDir, 'resources')

const iconPngPath = join(resourcesDir, 'icon.png')
const iconMacPngPath = join(resourcesDir, 'icon-mac.png')
const iconIcoPath = join(resourcesDir, 'icon.ico')
const trayIconPngPath = join(resourcesDir, 'tray-icon.png')

// 中文注释：与前端品牌色保持一致，供 macOS 应用图标底板使用。
const macIconBackgroundTop = '#6366f1'
const macIconBackgroundBottom = '#4338ca'

await mkdir(resourcesDir, { recursive: true })
await sharp(await renderIcon(1024)).toFile(iconPngPath)
await sharp(await renderMacIcon(1024)).toFile(iconMacPngPath)
await sharp(await renderIcon(128, { source: sourceTrayLogo, logoScale: 0.9 })).toFile(trayIconPngPath)

// 中文注释：Windows 安装包和快捷方式优先读取 ICO 资源，因此这里额外生成多尺寸桌面图标。
await generateWindowsIcon(iconIcoPath)

async function renderIcon(size, options = {}) {
  // 中文注释：Windows/Linux 使用透明底主体，并尽量放大到接近满幅但避免裁边。
  const source = options.source ?? sourceLogo
  const logoScale = options.logoScale ?? (size <= 64 ? 1 : 0.98)
  const logoSize = Math.round(size * logoScale)
  const logoOffset = Math.round((size - logoSize) / 2)

  const logo = await sharp(source)
    .resize(logoSize, logoSize, { fit: 'contain' })
    .png()
    .toBuffer()

  return sharp({
    create: {
      width: size,
      height: size,
      channels: 4,
      background: { r: 0, g: 0, b: 0, alpha: 0 },
    },
  })
    .composite([{ input: logo, left: logoOffset, top: logoOffset }])
    .png()
    .toBuffer()
}

async function renderMacIcon(size, options = {}) {
  // 中文注释：macOS Dock 会自动套圆角底板，图标主体需留足边距并铺满方形背景，避免显得过大。
  const source = options.source ?? sourceLogo
  const logoScale = options.logoScale ?? 0.62
  const logoSize = Math.round(size * logoScale)
  const logoOffset = Math.round((size - logoSize) / 2)

  const background = Buffer.from(
    `<svg width="${size}" height="${size}" xmlns="http://www.w3.org/2000/svg">
      <defs>
        <linearGradient id="bg" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stop-color="${macIconBackgroundTop}" />
          <stop offset="100%" stop-color="${macIconBackgroundBottom}" />
        </linearGradient>
      </defs>
      <rect width="100%" height="100%" fill="url(#bg)" />
    </svg>`,
  )

  const logo = await sharp(source)
    .resize(logoSize, logoSize, { fit: 'contain' })
    .png()
    .toBuffer()

  return sharp(background)
    .composite([{ input: logo, left: logoOffset, top: logoOffset }])
    .flatten({ background: macIconBackgroundBottom })
    .removeAlpha()
    .png()
    .toBuffer()
}

async function generateWindowsIcon(iconIcoPath) {
  const iconSizes = [256, 128, 64, 48, 32, 16]
  const iconPngBuffers = await Promise.all(iconSizes.map((size) => renderIcon(size)))
  const iconIcoBuffer = await pngToIco(iconPngBuffers)

  await writeFile(iconIcoPath, iconIcoBuffer)
}

export { renderIcon, renderMacIcon }
