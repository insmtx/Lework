字体目录。

当前已放入 CTAN Fandol 字体：

- `FandolFang-Regular.otf`：正文仿宋近似字体
- `FandolSong-Regular.otf`：红头/宋体备用
- `FandolHei-Regular.otf`：标题/章节/表头黑体近似字体

如果需要进一步贴近特定范本，可将可合法使用的完整字体文件放入本目录。ReportLab 后备路径会按以下文件名尝试加载 TTF：

- 正文：`FangSong_GB2312.ttf`、`FangSong.ttf`、`仿宋_GB2312.ttf`
- 标题：`FZXBSJW.ttf`、`方正小标宋简体.ttf`、`SimHei.ttf`

不要放入从 PDF 中抽取的子集字体；子集字体通常缺字，可能导致新文档出现乱码或方框。
