(() => {
  'use strict';
  const view = document.getElementById('app-view');
  const alert = document.getElementById('app-alert');
  const nav = document.getElementById('app-nav');
  const escape = (v) => String(v ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  let token, user, manga = [], library = [], socket;
  const message = text => { alert.textContent = text; alert.hidden = !text; };
  async function api(path, method = 'GET', data) {
    const r = await fetch(path, {method, headers: {Authorization:`Bearer ${token}`, ...(data ? {'Content-Type':'application/json'} : {})}, ...(data ? {body:JSON.stringify(data)} : {})});
    const result = await r.json();
    if(r.status === 401) { sessionStorage.removeItem('cyberlab.session'); location.replace('/login'); }
    if(!r.ok) throw Error(result.error || 'Request failed');
    return result;
  }
  async function refresh() { const [c,l] = await Promise.all([api('/api/manga'),api('/api/library')]); manga=c.manga; library=l.library; }
  const heading = (title, subtitle) => `<div class="page-heading"><h1>${title}</h1><p class="lede">${subtitle}</p></div>`;
  const card = m => `<article class="web-card"><div class="card-tags"><span>${escape(m.status)}</span><span>${m.total_chapters} chapters</span></div><h3>${escape(m.title)}</h3><p>${escape(m.author)} · ${escape(m.genres.join(', '))}</p><p>${escape(m.description)}</p><a href="#manga/${encodeURIComponent(m.id)}">View manga →</a></article>`;
  async function route() {
    if(socket) { socket.close(); socket=null; }
    message('');
    const [page='overview', rawID] = location.hash.slice(1).split('/');
    nav.querySelectorAll('a').forEach(a => a.classList.toggle('active',a.dataset.page===page));
    await refresh();
    if(page==='manga' && rawID) {
      const id=decodeURIComponent(rawID),m=await api('/api/manga/'+encodeURIComponent(id));
      view.innerHTML=`<a href="#browser">← Catalog</a>${heading(escape(m.title),escape(m.author))}<div class="web-card"><p>${escape(m.genres.join(', '))} · ${escape(m.status)} · ${m.total_chapters} chapters</p><p>${escape(m.description)}</p><button class="primary-link" id="add-manga">Add to Library</button></div>`;
      document.getElementById('add-manga').onclick=()=>api('/api/library','POST',{manga_id:id}).then(()=>message('Added to your library.')).catch(e=>message(e.message));
    } else if(page==='browser') {
      view.innerHTML=`${heading('Manga Browser','Search manga metadata and add titles to your library.')}<div class="filters"><input id="manga-query" aria-label="Search title" placeholder="Search title"></div><p id="catalog-count"></p><div class="web-grid" id="manga-results"></div>`;
      const render=()=>{const q=document.getElementById('manga-query').value.toLowerCase();const items=manga.filter(m=>m.title.toLowerCase().includes(q));document.getElementById('manga-results').innerHTML=items.map(card).join('')||'<p>No manga found.</p>';document.getElementById('catalog-count').textContent=`${items.length} titles`;};document.getElementById('manga-query').oninput=render;render();
    } else if(page==='library') {
      view.innerHTML=heading('My Library','Update your reading status and current chapter.')+`<div class="web-grid">${library.map(e=>{const m=manga.find(m=>m.id===e.manga_id);return `<form class="web-card reading-form" data-id="${escape(e.manga_id)}"><h3>${escape(m?.title||e.manga_id)}</h3><p>${e.current_chapter} / ${m?.total_chapters??'?'} chapters · v${e.version}</p><label>Status <select name="status">${['plan-to-read','reading','completed'].map(s=>`<option ${s===e.status?'selected':''}>${s}</option>`).join('')}</select></label><label>Current chapter <input name="chapter" type="number" min="0" max="${m?.total_chapters??0}" value="${e.current_chapter}" required></label><button class="primary-link">Save progress</button></form>`;}).join('')||'<p>Your library is empty. Add a title from Manga Browser.</p>'}</div>`;
      view.querySelectorAll('.reading-form').forEach(f=>f.onsubmit=async event=>{event.preventDefault();try{await api('/api/library/'+encodeURIComponent(f.dataset.id)+'/progress','PUT',{status:f.elements.status.value,current_chapter:Number(f.elements.chapter.value)});await route();message('Reading progress saved.');}catch(e){message(e.message);}});
    } else if(page==='discussions') {
      view.innerHTML=`${heading('Discussions','Live room chat. Messages are not stored after disconnect.')}<form id="join-room" class="chat-form"><input id="room-name" value="general" aria-label="Room" pattern="[a-z0-9][a-z0-9-]{2,63}" required><button class="primary-link">Join room</button></form><p id="room-status">Choose a room</p><div id="chat-log" class="chat-messages"></div><form id="send-chat" class="chat-form"><input id="chat-body" aria-label="Message" placeholder="Write a message" maxlength="2048" required><button class="primary-link">Send</button></form>`;
      document.getElementById('join-room').onsubmit=event=>{event.preventDefault();if(socket)socket.close();const s=new WebSocket(`${location.protocol==='https:'?'wss':'ws'}://${location.host}/api/ws/rooms/${encodeURIComponent(document.getElementById('room-name').value)}`,['cyberlab',`token.${token}`]);socket=s;s.onopen=()=>{if(socket===s)document.getElementById('room-status').textContent='Connected';};s.onclose=()=>{if(socket===s)document.getElementById('room-status').textContent='Disconnected';};s.onmessage=event=>{if(socket!==s)return;const m=JSON.parse(event.data);if(m.type!=='message'){if(m.error)message(m.error);return;}const item=document.createElement('div');const header=document.createElement('div');header.className='chat-message-header';const who=document.createElement('strong');who.textContent=m.username;const time=document.createElement('time');time.dateTime=m.sent_at;time.textContent=new Date(m.sent_at).toLocaleString('vi-VN');header.append(who,time);const body=document.createElement('p');body.textContent=m.body;item.append(header,body);const log=document.getElementById('chat-log');log.append(item);log.scrollTop=log.scrollHeight;};};
      document.getElementById('send-chat').onsubmit=event=>{event.preventDefault();if(socket?.readyState!==WebSocket.OPEN){message('Join a connected room first.');return;}socket.send(JSON.stringify({body:document.getElementById('chat-body').value}));document.getElementById('chat-body').value='';};
    } else if(page==='notifications') {
      view.innerHTML=heading('Chapter Updates','Current chapter counts from the catalog.')+`<p>Use the required Go TUI to subscribe to live UDP chapter alerts. This supplementary web view refreshes over HTTP.</p><div class="web-grid">${manga.map(card).join('')}</div>`;
    } else if(page==='admin' && user.role==='admin') {
      view.innerHTML=`${heading('Release Chapter','Update a manga chapter count through the catalog service.')}<form id="release-chapter" class="web-card admin-form"><label>Manga<select name="manga">${manga.map(m=>`<option value="${escape(m.id)}">${escape(m.title)} (${m.total_chapters})</option>`).join('')}</select></label><label>New total chapters<input name="total" type="number" min="1" max="1000000" required></label><button class="primary-link">Release chapter</button></form>`;
      document.getElementById('release-chapter').onsubmit=async e=>{e.preventDefault();const f=e.target;try{await api('/api/admin/manga/'+encodeURIComponent(f.elements.manga.value)+'/release','POST',{total_chapters:Number(f.elements.total.value)});await route();message('Chapter released. Subscribed TUIs receive a best-effort UDP alert.');}catch(e){message(e.message);}};
    } else {
      view.innerHTML=`<div class="web-hero">${heading('MangaHub','Discover titles, keep a personal library, and track your reading.')}<a class="primary-link" href="#browser">Explore manga</a></div><div class="stats progress-stats"><article><small>Catalog</small><strong>${manga.length} manga</strong></article><article><small>My Library</small><strong>${library.length} titles</strong></article><article><small>Completed</small><strong>${library.filter(e=>e.status==='completed').length}</strong></article></div><h2>Featured manga</h2><div class="web-grid">${manga.slice(0,3).map(card).join('')}</div>`;
    }
  }
  document.getElementById('logout-button').onclick=()=>{sessionStorage.removeItem('cyberlab.session');location.replace('/login');};
  window.addEventListener('hashchange',()=>route().catch(e=>message(e.message)));
  (async()=>{try{const session=JSON.parse(sessionStorage.getItem('cyberlab.session'));if(!session?.token){location.replace('/login');return;}token=session.token;user=await api('/api/me');document.getElementById('sidebar-username').textContent=user.username;document.getElementById('sidebar-role').textContent=user.role==='admin'?'Administrator':'Reader';document.getElementById('user-avatar').textContent=user.username.slice(0,2).toUpperCase();document.getElementById('workspace-label').textContent='Reading workspace';document.getElementById('nav-label').textContent='MangaHub';document.getElementById('session-status').textContent='Backend connected';nav.innerHTML=[['overview','Overview'],['browser','Manga Browser'],['library','My Library'],['discussions','Discussions'],['notifications','Chapter Updates'],...(user.role==='admin'?[['admin','Release Chapter']]:[])].map(([p,t])=>`<a class="nav-item" href="#${p}" data-page="${p}">${t}</a>`).join('');await route();}catch(e){message(e.message);}})();
})();
