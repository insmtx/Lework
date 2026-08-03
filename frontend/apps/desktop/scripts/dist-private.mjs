import process from "node:process";

process.env.VITE_LEROS_DEPLOYMENT_MODE = "private";

await import("./dist-local.mjs");
