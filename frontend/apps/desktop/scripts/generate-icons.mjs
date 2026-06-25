import { mkdir, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import pngToIco from 'png-to-ico'
import sharp from 'sharp'

const currentDir = dirname(fileURLToPath(import.meta.url))
const appDir = resolve(currentDir, '..')
const sourceLogo = resolve(appDir, '../../packages/app-ui/assets/logo.svg')
const resourcesDir = join(appDir, 'resources')

const iconPngPath = join(resourcesDir, 'icon.png')
const iconIcoPath = join(resourcesDir, 'icon.ico')

await mkdir(resourcesDir, { recursive: true })
await sharp(await renderIcon(1024)).toFile(iconPngPath)

// 中文注释：Windows 安装包和快捷方式优先读取 ICO 资源，因此这里额外生成多尺寸桌面图标。
await generateWindowsIcon(iconIcoPath)

async function renderIcon(size) {
  // 中文注释：桌面图标直接使用透明底主体，并尽量放大到接近满幅但避免裁边。
  const logoScale = size <= 64 ? 1 : 0.98
  const logoSize = Math.round(size * logoScale)
  const logoOffset = Math.round((size - logoSize) / 2)

  const logo = await sharp(sourceLogo)
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

async function generateWindowsIcon(iconIcoPath) {
  const iconSizes = [256, 128, 64, 48, 32, 16]
  const iconPngBuffers = await Promise.all(iconSizes.map((size) => renderIcon(size)))
  const iconIcoBuffer = await pngToIco(iconPngBuffers)

  await writeFile(iconIcoPath, iconIcoBuffer)
}
