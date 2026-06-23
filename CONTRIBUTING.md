# 🤝 为 Lework 做贡献

欢迎来到 Lework！很高兴你愿意为我们的企业数字员工操作系统贡献力量。本指南将帮助你快速开始参与项目。

## 🔍 行为准则

参与本项目即表示你同意遵守行为准则。我们承诺共同营造开放、友好的协作环境。

## 🎯 贡献类型

我们欢迎多种形式的贡献：

### 💡 想法与反馈
- 报告缺陷
- 提出新功能建议
- 对现有功能提供反馈

### 🛠️ 开发贡献
- 修复缺陷
- 实现新功能
- 完善文档
- 编写测试

### 📚 文档贡献
- 更新 README 文件
- 编写指南和教程
- 改进 API 文档

## 🛠️ 开发贡献

### 1. Fork 仓库

访问 [Lework GitHub 仓库](https://github.com/insmtx/Lework) 并点击右上角的 "Fork" 按钮，创建你自己的派生仓库。

### 2. 克隆仓库

```bash
git clone https://github.com/insmtx/Lework.git
cd Lework
```

### 3. 创建功能分支

```bash
git checkout -b feat/your-feature-name
```

分支命名规范：
- `feat/` - 新功能
- `fix/` - 缺陷修复
- `docs/` - 文档更新
- `refactor/` - 代码重构
- `test/` - 测试相关

### 4. 完成代码修改

按照[代码规范](#-代码规范)进行开发。

### 5. 运行测试

```bash
# 运行全部测试
make test

# 运行带覆盖率的测试
make test-cover
```

### 6. 提交更改

```bash
git add .
git commit -m "feat(scope): your changes"
git push origin feat/your-feature-name
```

### 7. 提交 Pull Request

1. 访问 [Lework 仓库](https://github.com/insmtx/Lework)
2. 点击 "Pull requests" 标签
3. 点击 "New pull request"
4. 选择 "Compare across forks"
5. 选择你的派生仓库和功能分支
6. 填写 PR 标题和描述
7. 提交 PR

## 📋 提交 Issue

### 报告缺陷

访问 [Issues](https://github.com/insmtx/Lework/issues/new) 提交缺陷报告，请包含：

1. **详细描述问题现象**
2. **提供复现步骤**
3. **说明系统或环境信息**（Go 版本、操作系统等）
4. **附上相关日志或截图**

### 功能建议

访问 [Issues](https://github.com/insmtx/Lework/issues/new) 提出新功能建议，请说明：

1. **功能描述** - 你想要实现什么
2. **使用场景** - 为什么需要这个功能
3. **预期行为** - 功能应该如何工作
4. **替代方案** - 是否考虑过其他解决方案

## 📝 代码规范

### Go 代码
- 遵循 Go 语言最佳实践
- 使用清晰且具描述性的变量名和函数名
- 保持统一格式（使用 `gofmt`）
- 为导出函数添加注释
- 为新增功能编写单元测试

### Commit 规范
- 使用约定式提交格式：`<type>(<scope>): <subject>`
- Type 类型包括但不限于：
  - `feat`: 新功能(feature)
  - `fix`: 修正缺陷(fix)
  - `docs`: 文档(documentation)
  - `style`: 代码格式调整
  - `refactor`: 重写(refactor)
  - `test`: 测试相关
  - `chore`: 构建过程或辅助工具变动
- 适当情况下，在主体部分详细描述变更内容，包含技术实现和业务说明

### 文档规范
- 文档保持清晰、简洁
- 术语使用保持一致
- 在合适处补充示例

## 🐛 缺陷报告

提交缺陷报告时，请访问 [Issues](https://github.com/insmtx/Lework/issues) 并尽量包含：

1. **详细描述问题现象**
2. **提供复现步骤**
3. **说明系统或环境信息**
4. **附上相关日志或截图**

## 🔧 Pull Request 流程

1. **在构建场景下，确保安装或构建依赖不会残留在最终镜像层中**
2. **若接口有变更，请同步更新 README.md**
3. **如果适用，请在示例文件和 README.md 中更新版本号**
4. **获得至少一位开发者审批后再合并 Pull Request**

## 📋 贡献检查清单

- [ ] 我的代码遵循项目代码规范
- [ ] 我已对代码进行自检
- [ ] 我已为难以理解的代码添加必要注释
- [ ] 我已同步更新相关文档
- [ ] 我的改动未引入新的警告
- [ ] 我已添加测试来验证修复或新功能有效

## 🌟 许可协议

通过提交贡献，你同意你的代码将按 Apache License 2.0 进行许可。

## 📬 联系方式

- GitHub Issues: [https://github.com/insmtx/Lework/issues](https://github.com/insmtx/Lework/issues)
- GitHub Discussions: [https://github.com/insmtx/Lework/discussions](https://github.com/insmtx/Lework/discussions)

感谢你为 Lework 做出贡献！ 🐶