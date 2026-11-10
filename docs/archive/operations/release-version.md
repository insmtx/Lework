# 发版版本更新（改哪些文件）

发版只需改下面两个文件。其它 frontend package 的 `0.0.0` / `0.1.0` **不要动**。

## 版本怎么定

1. 当前版本看 `frontend/apps/desktop/package.json` 的 `version`，并与已有发版记录对齐。
2. 默认小版本 +1（patch：`0.X.Y` → `0.X.(Y+1)`）。
3. 升「大版本」指 `0.X.Y` → `0.(X+1).0`；也可直接指定目标号（如 `0.3.12`）。

## 1. `frontend/apps/desktop/package.json`

只改根级 `"version"` 为 `X.Y.Z`（**不要**带 `v` 前缀）：

```json
{
  "name": "@lework/desktop",
  "version": "0.3.12"
}
```

## 2. 根目录 `CHANGELOG.md`

在 `# Changelog` 之后、上一版之前插入新节。写法直接对照文件里已有条目即可（版本号、日期、主题、总述、`-` 列表）。