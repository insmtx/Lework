import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createServer } from 'node:http';
import { normalizeAddress, parseService, testConnection } from './protocol.mjs';

test('手填与广播地址规范化；拒绝携带凭据或非 HTTP 地址', () => {
  assert.equal(normalizeAddress('http://172.16.0.64:38081'), 'http://172.16.0.64:38081/v1');
  assert.throws(() => normalizeAddress('file:///etc/passwd'));
  assert.throws(() => normalizeAddress('http://user:password@spark.local'));
});
test('解析真实服务形状；不接受广播中的任意 URL 或非法端口', () => {
  const service={name:'Spark',host:'spark.local.',port:38081,txt:{v:'1',id:'test-device',scheme:'http',path:'/v1'}};
  assert.equal(parseService(service).url,'http://spark.local:38081/v1');
  assert.equal(parseService({...service,port:0}),null);
  assert.equal(parseService({...service,host:'evil.example/path'}),null);
});
test('测试请求不携带认证，拒绝重定向和非 Lework 服务', async () => {
  let redirect=false;let valid=true;
  const server=createServer((req,res)=>{
    assert.equal(req.headers.authorization,undefined);
    assert.equal(req.headers.cookie,undefined);
    if(redirect){res.writeHead(302,{Location:'/other'});return res.end();}
    res.setHeader('Content-Type','application/json');res.end(JSON.stringify(valid?{data:{edition:'enterprise'}}:{data:{}}));
  });
  await new Promise(resolve=>server.listen(0,'127.0.0.1',resolve));
  const address=`http://127.0.0.1:${server.address().port}`;
  try{
    assert.equal((await testConnection(address)).edition,'enterprise');
    valid=false;await assert.rejects(testConnection(address),/Lework/);
    redirect=true;await assert.rejects(testConnection(address));
  }finally{await new Promise(resolve=>server.close(resolve));}
});
