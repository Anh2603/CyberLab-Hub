(() => {
  'use strict';
  const view = document.getElementById('app-view');
  const alert = document.getElementById('app-alert');
  const nav = document.getElementById('app-nav');
  const escape = (v) => String(v ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  let token, user, manga = [], library = [], socket, routeVersion = 0;
  const historicalTitles = new Map();
  const message = text => { alert.textContent = text; alert.hidden = !text; };
  const connection = text => { document.getElementById('session-status').textContent = text === 'Backend connected' ? `Backend reachable · checked at ${new Date().toLocaleTimeString()}` : text; };
  async function action(element, work) {
    if(element.dataset.busy) return;
    element.dataset.busy = 'true';
    const buttons = element.matches('button') ? [element] : [...element.querySelectorAll('button')];
    buttons.forEach(b => b.disabled = true);
    element.setAttribute('aria-busy', 'true');
    try { await work(); } catch(e) { message(e.message); }
    finally { delete element.dataset.busy; buttons.forEach(b => b.disabled = false); element.removeAttribute('aria-busy'); }
  }
  async function api(path, method = 'GET', data) {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 8000);
    try {
    const r = await fetch(path, {signal:controller.signal, method, headers: {Authorization:`Bearer ${token}`, ...(data ? {'Content-Type':'application/json'} : {})}, ...(data ? {body:JSON.stringify(data)} : {})});
    connection(r.status >= 500 ? 'Service unavailable — retry your action' : 'Backend connected');
    const result = await r.json().catch(() => ({}));
    if(r.status === 401) { sessionStorage.removeItem('mangahub.session'); location.replace('/login'); }
    if(!r.ok) throw Error(result.error || 'Request failed');
    return result;
    } catch(e) {
      if(e.name === 'AbortError' || e.name === 'TypeError') {
        connection('Backend unavailable — retry your action');
        throw Error('Connection failed or timed out. Your changes may not be saved.');
      }
      throw e;
    } finally { clearTimeout(timer); }
  }
  async function refresh() { const [c,l] = await Promise.all([api('/api/manga'),api('/api/library')]); manga=c.manga; library=l.library; }
  const heading = (title, subtitle) => `<div class="page-heading"><h1>${title}</h1><p class="lede">${subtitle}</p></div>`;
  const card = m => `<article class="web-card"><div class="card-tags"><span>${escape(m.status)}</span><span>${m.total_chapters} chapters</span></div><h3>${escape(m.title)}</h3><p>${escape(m.author)} · ${escape(m.genres.join(', '))}</p><p>${escape(m.description)}</p><a href="#manga/${encodeURIComponent(m.id)}">View manga →</a></article>`;
  async function route() {
    const version = ++routeVersion;
    if(socket) { socket.close(); socket=null; }
    message('');
    const [page='overview', rawID] = location.hash.slice(1).split('/');
    nav.querySelectorAll('a').forEach(a => a.classList.toggle('active',a.dataset.page===page));
    view.innerHTML = '<p role="status">Loading…</p>';
    if(page !== 'discussions') await refresh();
    if(version !== routeVersion) return;
    if(page==='manga' && rawID) {
      const id=decodeURIComponent(rawID),m=await api('/api/manga/'+encodeURIComponent(id));
      view.innerHTML=`<a href="#browser">← Catalog</a>${heading(escape(m.title),escape(m.author))}<div class="web-card"><p>${escape(m.genres.join(', '))} · ${escape(m.status)} · ${m.total_chapters} chapters</p><p>${escape(m.description)}</p><button class="primary-link" id="add-manga">Add to Library</button></div>`;
      document.getElementById('add-manga').onclick=event=>action(event.target,async()=>{await api('/api/library','POST',{manga_id:id});message('Added to your library.');});
    } else if(page==='browser') {
      view.innerHTML=`${heading('Manga Browser','Search manga metadata and add titles to your library.')}<div class="filters"><input id="manga-query" aria-label="Search title" placeholder="Search title"></div><p id="catalog-count"></p><div class="web-grid" id="manga-results"></div>`;
      const render=()=>{const q=document.getElementById('manga-query').value.toLowerCase();const items=manga.filter(m=>m.title.toLowerCase().includes(q));document.getElementById('manga-results').innerHTML=items.map(card).join('')||'<p>No manga found.</p>';document.getElementById('catalog-count').textContent=`${items.length} titles`;};document.getElementById('manga-query').oninput=render;render();
    } else if(page==='library') {
      await Promise.all(library.filter(e=>!manga.some(m=>m.id===e.manga_id)).map(async e=>{historicalTitles.set(e.manga_id,await api('/api/manga/'+encodeURIComponent(e.manga_id)));}));
      view.innerHTML=heading('My Library','Update your reading status and current chapter.')+`<div class="web-grid">${library.map(e=>{const m=manga.find(m=>m.id===e.manga_id)||historicalTitles.get(e.manga_id);return `<form class="web-card reading-form" data-id="${escape(e.manga_id)}"><h3>${escape(m?.title||e.manga_id)}</h3>${historicalTitles.has(e.manga_id)?'<small>Historical demo entry</small>':''}<p>${e.current_chapter} / ${m?.total_chapters??'?'} chapters · v${e.version}</p><label>Status <select name="status">${['plan-to-read','reading','completed'].map(s=>`<option ${s===e.status?'selected':''}>${s}</option>`).join('')}</select></label><label>Current chapter <input name="chapter" type="number" min="0" max="${m?.total_chapters??0}" value="${e.current_chapter}" required></label><button class="primary-link">Save progress</button></form>`;}).join('')||'<p>Your library is empty. Add a title from Manga Browser.</p>'}</div>`;
      view.querySelectorAll('.reading-form').forEach(f=>f.onsubmit=event=>{event.preventDefault();action(f,async()=>{await api('/api/library/'+encodeURIComponent(f.dataset.id)+'/progress','PUT',{status:f.elements.status.value,current_chapter:Number(f.elements.chapter.value)});if(version===routeVersion){await route();message('Reading progress saved.');}});});
    } else if(page==='discussions') {
      view.innerHTML=`${heading('Discussions','Live room chat. Messages are not stored after disconnect.')}<form id="join-room" class="chat-form"><input id="room-name" value="general" aria-label="Room" pattern="[a-z0-9][a-z0-9-]{2,63}" required><button class="primary-link">Join room</button></form><p id="room-status">Choose a room</p><div id="chat-log" class="chat-messages"></div><form id="send-chat" class="chat-form"><input id="chat-body" aria-label="Message" placeholder="Write a message" maxlength="2048" required><button class="primary-link">Send</button></form>`;
      const input = document.getElementById('chat-body');
      const sendButton = document.getElementById('send-chat').querySelector('button');
      let pending = null;
      const releasePending = () => { if(pending) clearTimeout(pending.timer); pending=null; sendButton.disabled=false; };
      document.getElementById('join-room').onsubmit=event=>{
        event.preventDefault();
        if(socket) socket.close();
        releasePending();
        const status = document.getElementById('room-status');
        status.textContent='Connecting...';
        document.getElementById('chat-log').replaceChildren();
        const s=new WebSocket(`${location.protocol==='https:'?'wss':'ws'}://${location.host}/api/ws/rooms/${encodeURIComponent(document.getElementById('room-name').value)}`,['mangahub',`token.${token}`]);
        socket=s;
        s.onopen=()=>{if(socket===s){status.textContent='Connected';connection('Backend connected');}};
        s.onclose=()=>{if(socket===s){status.textContent='Disconnected - join the room again';connection('Chat disconnected');releasePending();message('Chat disconnected. Unconfirmed draft kept; check for duplicates before resending.');}};
        s.onerror=()=>{if(socket===s)status.textContent='Chat connection failed';};
        s.onmessage=event=>{
          if(socket!==s) return;
          let m; try { m=JSON.parse(event.data); } catch { message('Invalid chat response.'); return; }
          if(m.type!=='message'){if(m.error){releasePending();message(m.error);}return;}
          if(pending && m.username===user.username && m.body===pending.body){
            if(input.value===pending.draft) input.value='';
            releasePending();
          }
          const item=document.createElement('div');
          const header=document.createElement('div');header.className='chat-message-header';
          const who=document.createElement('strong');who.textContent=m.username;
          const time=document.createElement('time');time.dateTime=m.sent_at;time.textContent=new Date(m.sent_at).toLocaleString('vi-VN');
          header.append(who,time);
          const body=document.createElement('p');body.textContent=m.body;item.append(header,body);
          const log=document.getElementById('chat-log');log.append(item);log.scrollTop=log.scrollHeight;
        };
      };
      document.getElementById('send-chat').onsubmit=event=>{
        event.preventDefault();
        if(pending) return;
        if(socket?.readyState!==WebSocket.OPEN){message('Join a connected room first.');return;}
        const draft=input.value, body=draft.trim();
        if(!body || new TextEncoder().encode(body).length>2048){message('Message must contain 1-2048 bytes.');return;}
        const currentSocket=socket;
        pending={body,draft,timer:setTimeout(()=>{
          if(socket!==currentSocket){releasePending();return;}
          releasePending();message('Send confirmation timed out. Draft kept; check for duplicates before resending.');
        },8000)};
        sendButton.disabled=true;
        try{socket.send(JSON.stringify({body}));}catch(e){releasePending();message('Send failed. Draft kept.');}
      };
    } else if(page==='notifications') {
      view.innerHTML=heading('Chapter Updates','Current chapter counts from the catalog.')+`<p>Use the required Go TUI to subscribe to live UDP chapter alerts. This supplementary web view refreshes over HTTP.</p><div class="web-grid">${manga.map(card).join('')}</div>`;
    } else if(page==='admin' && user.role==='admin') {
      view.innerHTML=`${heading('Release Chapter','Update a manga chapter count through the catalog service.')}<form id="release-chapter" class="web-card admin-form"><label>Manga<select name="manga">${manga.map(m=>`<option value="${escape(m.id)}">${escape(m.title)} (${m.total_chapters})</option>`).join('')}</select></label><label>New total chapters<input name="total" type="number" min="1" max="1000000" required></label><button class="primary-link">Release chapter</button></form>`;
      document.getElementById('release-chapter').onsubmit=e=>{e.preventDefault();const f=e.target;action(f,async()=>{await api('/api/admin/manga/'+encodeURIComponent(f.elements.manga.value)+'/release','POST',{total_chapters:Number(f.elements.total.value)});if(version===routeVersion){await route();message('Chapter released. Subscribed TUIs receive a best-effort UDP alert.');}});};
    } else {
      const featured=['one-piece','naruto','bleach'].map(id=>manga.find(m=>m.id===id)).filter(Boolean);
      view.innerHTML=`<div class="web-hero">${heading('MangaHub','Discover titles, keep a personal library, and track your reading.')}<a class="primary-link" href="#browser">Explore manga</a></div><div class="stats progress-stats"><article><small>Catalog</small><strong>${manga.length} manga</strong></article><article><small>My Library</small><strong>${library.length} titles</strong></article><article><small>Completed</small><strong>${library.filter(e=>e.status==='completed').length}</strong></article></div><h2>Featured manga</h2><div class="web-grid">${(featured.length?featured:manga.slice(0,3)).map(card).join('')}</div>`;
    }
  }
  document.getElementById('logout-button').onclick=()=>{sessionStorage.removeItem('mangahub.session');location.replace('/login');};
  window.addEventListener('hashchange',()=>route().catch(e=>message(e.message)));
  (async()=>{try{const session=JSON.parse(sessionStorage.getItem('mangahub.session'));if(!session?.token){location.replace('/login');return;}token=session.token;user=await api('/api/me');document.getElementById('sidebar-username').textContent=user.username;document.getElementById('sidebar-role').textContent=user.role==='admin'?'Administrator':'Reader';document.getElementById('user-avatar').textContent=user.username.slice(0,2).toUpperCase();document.getElementById('workspace-label').textContent='Reading workspace';document.getElementById('nav-label').textContent='MangaHub';connection('Backend connected');nav.innerHTML=[['overview','Overview'],['browser','Manga Browser'],['library','My Library'],['discussions','Discussions'],['notifications','Chapter Updates'],...(user.role==='admin'?[['admin','Release Chapter']]:[])].map(([p,t])=>`<a class="nav-item" href="#${p}" data-page="${p}">${t}</a>`).join('');await route();}catch(e){message(e.message);}})();
})();
