import process from "node:process";

process.env.VITE_LEWORK_DEPLOYMENT_MODE = "private";

await import("./dist-local.mjs");
