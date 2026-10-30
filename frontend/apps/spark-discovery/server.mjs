import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { Bonjour } from 'bonjour-service';
import { parseService, testConnection } from './protocol.mjs';

const port = Number(process.env.SPARK_DISCOVERY_PORT || 8787);
const origin = `http://127.0.0.1:${port}`;
const services = new Map();
let discoveryError = '';
const bonjour = new Bonjour({}, error => { discoveryError = error.message; });
const browser = bonjour.find({ type: 'lework', protocol: 'tcp' });
browser.on('up', service => {
  const parsed = parseService(service);
  if (!parsed || services.size >= 32) return;
  services.set(parsed.key, parsed);
  discoveryError = '';
  console.log(`发现 ${parsed.name}: ${parsed.url}`);
});
browser.on('down', service => {
  const parsed = parseService(service);
  if (parsed) services.delete(parsed.key);
});

function json(res, status, value) {
  res.writeHead(status, { 'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store' });
  res.end(JSON.stringify(value));
}

let testing = 0;
const server = createServer(async (req, res) => {
  // 原型仅接受本机页面；不向局域网开放一个任意地址测试代理。
  if (req.headers.host !== `127.0.0.1:${port}` || (req.headers.origin && req.headers.origin !== origin)) {
    return json(res, 403, { error: '只允许本机原型页面访问' });
  }
  try {
    if (req.method === 'GET' && req.url === '/') {
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' });
      return res.end(await readFile(new URL('./index.html', import.meta.url)));
    }
    if (req.method === 'GET' && req.url === '/api/services') {
      return json(res, 200, { services: [...services.values()], error: discoveryError });
    }
    if (req.method === 'POST' && req.url === '/api/rescan') {
      // 不重建 socket，避免每次刷新累积监听器。
      browser.update();
      return json(res, 200, { ok: true });
    }
    if (req.method === 'POST' && req.url === '/api/test') {
      if (req.headers['content-type'] !== 'application/json') return json(res, 415, { error: '需要 JSON' });
      if (testing >= 3) return json(res, 429, { error: '正在测试，请稍后重试' });
      let body = '';
      for await (const chunk of req) {
        body += chunk;
        if (body.length > 2048) return json(res, 413, { error: '地址过长' });
      }
      const { address } = JSON.parse(body);
      if (typeof address !== 'string') return json(res, 400, { error: '请输入服务地址' });
      testing++;
      try { return json(res, 200, await testConnection(address)); }
      finally { testing--; }
    }
    return json(res, 404, { error: '不存在的接口' });
  } catch (error) {
    return json(res, 400, { error: error.name === 'TimeoutError' ? '连接超时' : error.message });
  }
});

server.listen(port, '127.0.0.1', () => console.log(`Spark 发现原型：${origin}`));
function stop() {
  browser.stop();
  bonjour.destroy();
  server.close();
}
process.on('SIGINT', stop);
process.on('SIGTERM', stop);
server.on('error', error => { console.error(error.message); stop(); process.exitCode = 1; });
