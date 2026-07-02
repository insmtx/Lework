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

// 中文注释：macOS 图标底板采用白色（与 VS Code、企业微信等应用一致），让彩色 logo 主体突出。
const macIconBackgroundTop = '#ffffff'
const macIconBackgroundBottom = '#ffffff'

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
  // 中文注释：按照 Apple 图标规范绘制——1024 画布中图标本体是 824 的圆角方块（约 80.5%），
  // 四周保留透明边距，圆角半径约为本体的 22.5%，这样 Dock 中的大小和圆角才能与系统应用一致。
  const source = options.source ?? sourceLogo
  const plateScale = 824 / 1024
  const plateSize = Math.round(size * plateScale)
  const plateOffset = Math.round((size - plateSize) / 2)
  const cornerRadius = Math.round(plateSize * 0.225)

  // 中文注释：logo 相对底板缩放，保证章鱼在圆角底板内留出呼吸空间。
  const logoScale = options.logoScale ?? 0.72
  const logoSize = Math.round(plateSize * logoScale)
  const logoOffset = Math.round((size - logoSize) / 2)

  const background = Buffer.from(
    `<svg width="${size}" height="${size}" xmlns="http://www.w3.org/2000/svg">
      <defs>
        <linearGradient id="bg" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stop-color="${macIconBackgroundTop}" />
          <stop offset="100%" stop-color="${macIconBackgroundBottom}" />
        </linearGradient>
      </defs>
      <rect x="${plateOffset}" y="${plateOffset}" width="${plateSize}" height="${plateSize}"
        rx="${cornerRadius}" ry="${cornerRadius}" fill="url(#bg)" />
    </svg>`,
  )

  const logo = await sharp(source)
    .resize(logoSize, logoSize, { fit: 'contain' })
    .png()
    .toBuffer()

  return sharp(background)
    .composite([{ input: logo, left: logoOffset, top: logoOffset }])
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
