const path = require("node:path");
const { getDefaultConfig } = require("expo/metro-config");

const projectRoot = __dirname;
const workspaceRoot = path.resolve(projectRoot, "../..");

const config = getDefaultConfig(projectRoot);

// @lework/api 是 workspace 源码包，必须让 Metro 监视仓库根，才能解析并转译它。
config.watchFolders = [workspaceRoot];

module.exports = config;
