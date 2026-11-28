// Run the shipped browser script with controlled DOM/network boundaries.
const {test} = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const path = require('node:path');
const source = fs.readFileSync(path.join(__dirname, '../internal/webapp/static/manga.js'), 'utf8');
const flush = () => new Promise(resolve => setImmediate(resolve));

async function workspace(fetchImpl) {
  const nodes = new Map();
  class Element {
    constructor() { this.dataset={}; this.value=''; this.children=[]; this.classList={toggle(){}}; }
    querySelectorAll() { return []; }
    querySelector() { return this.button ||= new Element(); }
    matches(selector) { return selector === 'button' && this.isButton; }
    setAttribute() {}
    removeAttribute() {}
    append(...items) { this.children.push(...items); }
    replaceChildren(...items) { this.children=items; }
  }
  const element = id => { if(!nodes.has(id)) nodes.set(id,new Element()); return nodes.get(id); };
  const sockets = [];
  class Socket {
    static OPEN = 1;
    constructor() { this.readyState=1; this.sent=[]; sockets.push(this); }
    send(value) { this.sent.push(JSON.parse(value)); }
    close() { this.readyState=3; this.onclose?.(); }
  }
  const calls=[];
  const context = vm.createContext({
    document:{getElementById:element, createElement:()=>new Element()},
    location:{hash:'#discussions',protocol:'http:',host:'localhost',replace(){}},
    window:{addEventListener(){}},
    sessionStorage:{getItem:()=>JSON.stringify({token:'test'}),removeItem(){}},
    fetch:async(url,options)=>{calls.push(url); return fetchImpl ? fetchImpl(url,options) : {ok:true,status:200,json:async()=>({username:'alice',role:'reader'})};},
    WebSocket:Socket, AbortController, TextEncoder, setTimeout, clearTimeout, console,
  });
  vm.runInContext(source.replace(/\}\)\(\);\s*$/, 'globalThis.hooks={api,action,route};})();'), context);
  await flush(); await flush();
  return {element,sockets,calls,context};
}

test('Discussions opens without requesting unavailable catalog or library', async()=>{
  const w=await workspace(async url=>{
    assert.equal(url,'/api/me');
    return {ok:true,status:200,json:async()=>({username:'alice',role:'reader'})};
  });
  assert.match(w.element('app-view').innerHTML,/Join room/);
  assert.deepEqual(w.calls,['/api/me']);
});

test('chat retains pending/failed draft and prevents duplicate sends until server echo',async()=>{
  const w=await workspace();
  w.element('room-name').value='general';
  const event={preventDefault(){}};
  w.element('join-room').onsubmit(event);
  const socket=w.sockets[0]; socket.onopen();
  w.element('chat-body').value='hello';
  w.element('send-chat').onsubmit(event);
  w.element('send-chat').onsubmit(event);
  assert.equal(socket.sent.length,1);
  assert.equal(w.element('chat-body').value,'hello');
  socket.onmessage({data:JSON.stringify({type:'message',username:'alice',body:'hello',sent_at:new Date().toISOString()})});
  assert.equal(w.element('chat-body').value,'');
  w.element('chat-body').value='keep me';
  w.element('send-chat').onsubmit(event);
  socket.close();
  assert.equal(w.element('chat-body').value,'keep me');
  assert.match(w.element('room-status').textContent,/Disconnected/);
  assert.equal(w.element('send-chat').querySelector('button').disabled,false);
});

test('mutation guard sends once, then unlocks after a failed request',async()=>{
  const w=await workspace();
  const button=w.element('save'); button.isButton=true;
  let reject, calls=0;
  const work=()=>{calls++;return new Promise((_,r)=>{reject=r;});};
  const pending=w.context.hooks.action(button,work);
  await w.context.hooks.action(button,work);
  assert.equal(calls,1); assert.equal(button.disabled,true);
  reject(new Error('failed save')); await pending;
  assert.equal(button.disabled,false); assert.equal(button.dataset.busy,undefined);
  assert.match(w.element('app-alert').textContent,/failed save/);
});

test('backend transport failure updates connection status without clearing session',async()=>{
  const w=await workspace(async url=>{
    if(url==='/api/me')return {ok:true,status:200,json:async()=>({username:'alice',role:'reader'})};
    throw new TypeError('offline');
  });
  await assert.rejects(w.context.hooks.api('/api/library'),/Connection failed/);
  assert.match(w.element('session-status').textContent,/Backend unavailable/);
});
