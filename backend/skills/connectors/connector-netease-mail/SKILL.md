---
name: connector-netease-mail
description: 通过 IMAP/SMTP 收发、搜索和管理邮箱邮件及附件。用户提到邮件、邮箱、收件箱、发邮件、163、126、yeah.net、email、inbox 或 send mail 时使用。
---

# 邮箱连接器

使用已安装的命令行脚本访问用户连接的邮箱。脚本会根据邮箱域名识别常见服务商，支持网易邮箱以及其他标准 IMAP/SMTP 邮箱。

## 运行规则

- 发信和测试 SMTP 使用 `node "$LEROS_RUN_SKILLS_DIR/connector-netease-mail/scripts/smtp.bundle.js"`。
- 收信、搜索、附件和邮件状态管理使用 `node "$LEROS_RUN_SKILLS_DIR/connector-netease-mail/scripts/imap.bundle.js"`。
- 凭证由连接器注入 `NETEASE_EMAIL_USER` 和 `NETEASE_EMAIL_PASS`，不要向用户索取或输出授权码。
- 命令输出 JSON。必须检查 `success`；失败时根据 `message` 向用户说明原因。
- 发信前复述收件人、主题和附件并获得用户明确确认。

## 前置条件

用户需要在“邮箱”连接器中填写完整邮箱地址和 IMAP/SMTP 客户端授权码。授权码不是网页登录密码。若缺少凭证或认证失败，提醒用户在邮箱网页端开启 IMAP/SMTP 服务、重新生成授权码并重连。

## 发信

```bash
node "$LEROS_RUN_SKILLS_DIR/connector-netease-mail/scripts/smtp.bundle.js" send \
  --to "recipient@example.com" \
  --subject "邮件主题" \
  --body "邮件正文"
```

可选参数：

- `--html`：将 `--body` 作为 HTML。
- `--body-file PATH`：从文件读取正文。
- `--cc ADDRESSES`、`--bcc ADDRESSES`：抄送和密送。
- `--attach PATHS`：附件路径，多个路径用逗号分隔。
- `--from ADDRESS`：覆盖默认发件人。

发送前可测试 SMTP：

```bash
node "$LEROS_RUN_SKILLS_DIR/connector-netease-mail/scripts/smtp.bundle.js" test
```

## 查信与搜索

查看最近邮件：

```bash
node "$LEROS_RUN_SKILLS_DIR/connector-netease-mail/scripts/imap.bundle.js" check --limit 10 --recent 2h
```

可添加 `--unseen` 仅看未读，或用 `--mailbox NAME` 指定文件夹。`--recent` 支持 `30m`、`2h`、`7d`。

搜索邮件：

```bash
node "$LEROS_RUN_SKILLS_DIR/connector-netease-mail/scripts/imap.bundle.js" search \
  --subject "发票" --recent 7d --limit 20
```

搜索还支持 `--from`、`--unseen` 和 `--seen`。

获取某封邮件全文：

```bash
node "$LEROS_RUN_SKILLS_DIR/connector-netease-mail/scripts/imap.bundle.js" fetch 12345
```

UID 来自 `check` 或 `search` 的结果。

## 附件与状态

下载附件：

```bash
node "$LEROS_RUN_SKILLS_DIR/connector-netease-mail/scripts/imap.bundle.js" download 12345 \
  --dir "$HOME/Downloads" --file "report.pdf"
```

不传 `--file` 会下载该邮件的全部附件。

标记已读或未读：

```bash
node "$LEROS_RUN_SKILLS_DIR/connector-netease-mail/scripts/imap.bundle.js" mark-read 12345
node "$LEROS_RUN_SKILLS_DIR/connector-netease-mail/scripts/imap.bundle.js" mark-unread 12345
```

列出邮箱文件夹：

```bash
node "$LEROS_RUN_SKILLS_DIR/connector-netease-mail/scripts/imap.bundle.js" list-mailboxes
```

## 错误处理

- 缺少凭证：提示用户配置邮箱连接器，不要让用户在对话中发送授权码。
- 认证失败：检查填写的是客户端授权码，并确认 IMAP/SMTP 已开启。
- 超时或限流：说明网络或服务端暂时不可用，建议稍后重试。
- 授权失效：让用户生成新授权码，断开旧连接后重新连接。
