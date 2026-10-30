// 最小原型只识别约定的服务记录，不把广播内容当作登录凭据。
export function normalizeAddress(value) {
  const url = new URL(value);
  if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) {
    throw new Error('请输入不含账号、查询参数的 http:// 或 https:// 地址');
  }
  const path = url.pathname.replace(/\/+$/, '');
  url.pathname = path.endsWith('/v1') ? path : `${path}/v1`;
  return url.toString().replace(/\/+$/, '');
}

export function parseService(service) {
  const txt = service.txt ?? {};
  const host = String(service.host ?? '').replace(/\.$/, '');
  if (String(txt.v) !== '1' || !/^[a-zA-Z0-9._-]{1,128}$/.test(String(txt.id ?? '')) ||
      !/^[a-zA-Z0-9.-]+\.local$/i.test(host) || !['http', 'https'].includes(txt.scheme) ||
      txt.path !== '/v1' || !Number.isInteger(service.port) || service.port < 1 || service.port > 65535) return null;
  return {
    id: txt.id, name: String(service.name ?? host).slice(0, 160),
    url: normalizeAddress(`${txt.scheme}://${host}:${service.port}${txt.path}`),
    addresses: (service.addresses ?? []).filter(value => typeof value === 'string'),
    // fullname 用于撤销记录，id 用于展示部署身份。
    key: String(service.fqdn ?? `${host}:${service.port}`),
  };
}

export async function testConnection(address) {
  const url = normalizeAddress(address);
  const response = await fetch(`${url}/GlobalConfig`, {
    headers: { Accept: 'application/json' }, credentials: 'omit',
    redirect: 'error', signal: AbortSignal.timeout(8000),
  });
  if (!response.ok) throw new Error(`服务返回 HTTP ${response.status}`);
  const payload = await response.json();
  if (!['oss', 'enterprise'].includes(payload?.data?.edition)) throw new Error('该地址没有返回 Lework 配置');
  return { url, edition: payload.data.edition };
}
