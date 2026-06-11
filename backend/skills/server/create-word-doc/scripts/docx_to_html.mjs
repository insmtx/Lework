#!/usr/bin/env node
/**
 * DOCX 转 HTML 脚本
 * 使用 mammoth 将 DOCX 转换为 HTML
 * 
 * 安装: npm install mammoth
 * 运行: node docx_to_html.mjs input.docx [output.html]
 */

import { convertToHtml } from 'mammoth';
import { readFile, writeFile } from 'fs/promises';
import { existsSync } from 'fs';

async function docxToHtml(inputPath, outputPath) {
    if (!existsSync(inputPath)) {
        console.error(`错误: 文件不存在: ${inputPath}`);
        process.exit(1);
    }

    console.log(`正在转换: ${inputPath}`);

    try {
        const result = await convertToHtml({ path: inputPath }, {
            styleMap: [
                "p[style-name='Heading 1'] => h1:fresh",
                "p[style-name='Heading 2'] => h2:fresh",
                "p[style-name='Heading 3'] => h3:fresh",
                "p[style-name='Quote'] => blockquote:fresh",
            ],
        });

        const html = `
<!DOCTYPE html>
<html lang="zh-CN">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>${inputPath.replace('.docx', '')}</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            max-width: 800px;
            margin: 0 auto;
            padding: 20px;
            line-height: 1.6;
        }
        h1 { border-bottom: 2px solid #333; padding-bottom: 10px; }
        h2 { border-bottom: 1px solid #ddd; padding-bottom: 5px; }
        blockquote {
            border-left: 4px solid #ddd;
            margin-left: 0;
            padding-left: 20px;
            color: #666;
        }
        table {
            border-collapse: collapse;
            width: 100%;
            margin: 20px 0;
        }
        th, td {
            border: 1px solid #ddd;
            padding: 8px 12px;
            text-align: left;
        }
        th { background-color: #f5f5f5; }
    </style>
</head>
<body>
${result.value}
</body>
</html>`;

        if (outputPath) {
            await writeFile(outputPath, html, 'utf-8');
            console.log(`已保存到: ${outputPath}`);
        } else {
            console.log(html);
        }

        if (result.messages && result.messages.length > 0) {
            console.log('\n警告:');
            result.messages.forEach(msg => console.log(`  - ${msg}`));
        }

    } catch (error) {
        console.error('转换失败:', error.message);
        process.exit(1);
    }
}

// 主程序
const args = process.argv.slice(2);
if (args.length === 0) {
    console.log('用法: node docx_to_html.mjs <input.docx> [output.html]');
    process.exit(1);
}

const inputPath = args[0];
const outputPath = args[1];
docxToHtml(inputPath, outputPath);
