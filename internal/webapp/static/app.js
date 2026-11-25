(() => {
  'use strict';

  const sessionKey = 'cyberlab.session';
  const alertBox = document.getElementById('app-alert');
  const view = document.getElementById('app-view');
  const nav = document.getElementById('app-nav');
  let token = '';
  let user = null;
  let challenges = [];
  let progress = [];
  let roomSocket = null;
  let currentRoom = 'general';
  const knownKey = 'cyberlab.known-challenges';
  const escapeHTML = (value) => String(value ?? '').replace(/[&<>"']/g, (c) => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
  const label = (value) => ({'log-analysis':'Network Security','web-security':'Web Security','forensics':'File Analysis'}[value] || String(value || '').replace(/-/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase()));
  const solved = (id) => progress.some((item) => item.challenge_id === id);
  const dateOf = (id) => progress.find((item) => item.challenge_id === id)?.solved_at;
  const publishedProgress = () => progress.filter((item) => challenges.some((c) => c.id === item.challenge_id));
  const completedAt = (value) => {
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? 'Completed' : date.toLocaleString();
  };
  const categoryProgress = () => [...new Set(challenges.map((c) => c.category))]
    .sort((a, b) => label(a).localeCompare(label(b)))
    .map((category) => {
      const items = challenges.filter((c) => c.category === category);
      return {category, total: items.length, completed: items.filter((c) => solved(c.id)).length};
    });

  function notify(message) { alertBox.textContent = message; alertBox.hidden = !message; }
  async function api(path, options = {}) {
    const response = await fetch(path, {...options, headers: {Authorization: `Bearer ${token}`, ...(options.body ? {'Content-Type':'application/json'} : {})}});
    const data = await response.json().catch(() => ({}));
    if (response.status === 401) { clearSession(); location.replace('/login'); throw new Error('Session expired'); }
    if (!response.ok) throw new Error(data.error || `Request failed (${response.status})`);
    return data;
  }
  async function refresh() {
    const [catalog, account] = await Promise.all([api('/api/challenges'), api('/api/progress')]);
    challenges = catalog.challenges || [];
    progress = account.progress || [];
  }
  const card = (challenge) => {
    const sections = challenge.description.split(/\n\s*\n/);
    const summary = (sections.length > 1 ? sections.slice(1).join(' ').trim() : '') || sections[0].split('\n')[0];
    const evidence = sections[0].split('\n').slice(1, 3).join('\n');
    return `<article class="web-card"><div class="card-tags"><span>${escapeHTML(label(challenge.category))}</span><span>${escapeHTML(label(challenge.difficulty))}</span></div><h3>${escapeHTML(challenge.title)}</h3><p class="card-summary">${escapeHTML(summary)}</p>${evidence ? `<pre class="card-preview">${escapeHTML(evidence)}</pre>` : ''}<div class="card-foot"><span class="${solved(challenge.id) ? 'solved' : 'muted'}">${solved(challenge.id) ? 'Solved' : 'Not solved'}</span><a class="secondary-button" href="#challenge/${encodeURIComponent(challenge.id)}">View challenge</a></div></article>`;
  };
  const heading = (tag, title, description) => `<p class="eyebrow">${tag}</p><h1>${title}</h1><p class="lede">${description}</p>`;

  function overview() {
    const completions = publishedProgress().sort((a,b) => new Date(b.solved_at) - new Date(a.solved_at));
    const count = completions.length;
    const recent = completions[0];
    const recentChallenge = challenges.find((c) => c.id === recent?.challenge_id);
    const activity = completions.slice(0, 5).map((item) => {
      const challenge = challenges.find((c) => c.id === item.challenge_id);
      return `<li><span class="activity-dot" aria-hidden="true"></span><div><strong>${escapeHTML(challenge.title)}</strong><small>Solved · ${escapeHTML(completedAt(item.solved_at))}</small></div><a href="#challenge/${encodeURIComponent(challenge.id)}">View challenge</a></li>`;
    }).join('');
    view.innerHTML = `<div class="web-hero">${heading('Learner workspace','CyberLab Hub Learning Workspace','Inspect synthetic evidence, solve cybersecurity challenges, and track your learning progress.')}<a class="primary-link" href="#challenges">Explore challenges</a></div><div class="stats"><article><small>Challenges solved</small><strong>${count} of ${challenges.length}</strong><span>${challenges.length ? Math.round(count / challenges.length * 100) : 0}% complete</span></article><article><small>Available challenges</small><strong>${challenges.length}</strong><span>Published catalog</span></article><article><small>Categories</small><strong>${new Set(challenges.map((c) => c.category)).size}</strong><span>Challenge topics</span></article><article><small>Recent completion</small><strong>${escapeHTML(recentChallenge?.title || 'None yet')}</strong><span>${escapeHTML(recentChallenge ? label(recentChallenge.category) : 'Explore challenges')}</span></article></div><div class="section-title"><h2>Featured challenges</h2><a href="#challenges">View all →</a></div><div class="web-grid">${challenges.slice(0,3).map(card).join('') || '<p>No published challenges yet.</p>'}</div><div class="overview-lower"><section class="web-card"><div class="section-title"><h2>Recent activity</h2><a href="#progress">My Progress →</a></div><p class="muted">Completed challenges in the current catalog</p>${activity ? `<ul class="activity-list">${activity}</ul>` : '<p>No completed challenges yet. Explore a challenge to get started.</p>'}</section><section class="web-card"><h2>Discussion rooms</h2><p>Ask questions and discuss challenge concepts with other learners.</p><div class="room-links"><a href="#discussions/general">General</a><a href="#discussions/network-security">Network Security</a><a href="#discussions/web-security">Web Security</a><a href="#discussions/file-analysis">File Analysis</a></div></section></div>`;
  }
  function browser() {
    view.innerHTML = `${heading('Challenge catalog','Challenges','Browse published challenges and inspect synthetic evidence.')}<div class="filters"><input id="search" type="search" placeholder="Search challenges" aria-label="Search challenges"><select id="category" aria-label="Category"><option value="">All categories</option>${[...new Set(challenges.map((c)=>c.category))].map((c)=>`<option value="${escapeHTML(c)}">${escapeHTML(label(c))}</option>`).join('')}</select><select id="difficulty" aria-label="Difficulty"><option value="">All difficulties</option><option value="easy">Easy</option><option value="medium">Medium</option><option value="hard">Hard</option></select><select id="status" aria-label="Solved status"><option value="">All statuses</option><option value="solved">Solved</option><option value="unsolved">Not solved</option></select></div><p id="result-count" class="muted"></p><div id="results" class="web-grid"></div>`;
    const update = () => {
      const query = document.getElementById('search').value.toLowerCase();
      const category = document.getElementById('category').value;
      const difficulty = document.getElementById('difficulty').value;
      const status = document.getElementById('status').value;
      const matches = challenges.filter((c) => (!query || `${c.title} ${c.description}`.toLowerCase().includes(query)) && (!category || c.category === category) && (!difficulty || c.difficulty === difficulty) && (!status || (solved(c.id) ? 'solved' : 'unsolved') === status));
      document.getElementById('result-count').textContent = `${matches.length} challenge${matches.length === 1 ? '' : 's'}`;
      document.getElementById('results').innerHTML = matches.map(card).join('') || '<div class="empty-state">No matching challenges.</div>';
    };
    view.querySelectorAll('.filters input, .filters select').forEach((el) => el.addEventListener('input', update));
    update();
  }
  async function challengePage(id) {
    view.innerHTML = '<div class="loading-state">Loading challenge…</div>';
    const c = await api(`/api/challenges/${encodeURIComponent(id)}`);
    const parts = c.description.split(/\n\s*\n/);
    const evidence = parts[0] || c.description;
    const instruction = parts.slice(1).join('\n\n') || 'Inspect the evidence and submit your answer.';
    view.innerHTML = `<a href="#challenges">← Back to challenges</a><div class="section-title"><div><p class="eyebrow">${escapeHTML(label(c.category))} · ${escapeHTML(label(c.difficulty))}</p><h1>${escapeHTML(c.title)}</h1></div><span id="detail-status" class="${solved(id)?'solved':'muted'}">${solved(id)?'Solved':'Not solved'}</span></div><div class="detail-grid"><div><article class="web-card"><h2>Objective</h2><p class="preserve">${escapeHTML(instruction)}</p></article><article class="web-card evidence"><div class="section-title"><h2>Synthetic evidence</h2><button class="secondary-button" id="copy-evidence">Copy</button></div><pre id="evidence-text">${escapeHTML(evidence)}</pre></article></div><article class="web-card answer-card"><h2>Submit answer</h2><p class="muted">Your answer is checked by the challenge catalog.</p><form id="answer-form"><label for="answer">Answer</label><input id="answer" name="answer" required autocomplete="off" placeholder="flag{answer}"><button class="primary-button" id="submit-answer">Submit answer</button></form><div id="feedback" role="status" aria-live="polite"></div></article></div>`;
    document.getElementById('copy-evidence').addEventListener('click', async () => { try { await navigator.clipboard.writeText(evidence); notify('Evidence copied.'); } catch { notify('Copy was unavailable.'); } });
    document.getElementById('answer-form').addEventListener('submit', async (event) => {
      event.preventDefault();
      const button = document.getElementById('submit-answer');
      const feedback = document.getElementById('feedback');
      button.disabled = true; button.textContent = 'Submitting…'; feedback.textContent = '';
      try {
        const result = await api(`/api/challenges/${encodeURIComponent(id)}/submissions`, {method:'POST',body:JSON.stringify({answer:document.getElementById('answer').value})});
        feedback.className = result.correct ? 'success-message' : 'error-message';
        feedback.textContent = result.correct ? 'Correct answer. Challenge solved.' : 'Incorrect answer. Try again.';
        if (result.correct) { await refresh(); document.getElementById('detail-status').textContent = 'Solved'; document.getElementById('detail-status').className = 'solved'; }
      } catch (error) { feedback.className = 'error-message'; feedback.textContent = error.message; }
      finally { button.disabled = false; button.textContent = 'Submit answer'; }
    });
  }
  function myProgress() {
    const count = publishedProgress().length;
    const categories = categoryProgress().map(({category, total, completed}) => `<article class="web-card category-card"><div class="category-heading"><h3>${escapeHTML(label(category))}</h3><strong>${completed} / ${total}</strong></div><progress value="${completed}" max="${total}" aria-label="${escapeHTML(label(category))} completed"></progress><small>${Math.round(completed / total * 100)}% complete</small></article>`).join('');
    view.innerHTML = `${heading('Learner workspace','My Progress','Review your solved challenges and current learning progress.')}<div class="stats progress-stats"><article><small>Challenges solved</small><strong>${count} of ${challenges.length}</strong><span>${challenges.length ? Math.round(count / challenges.length * 100) : 0}% complete</span></article><article><small>Available challenges</small><strong>${challenges.length}</strong><span>Published catalog</span></article><article><small>Categories</small><strong>${new Set(challenges.map((c)=>c.category)).size}</strong><span>Challenge topics</span></article></div><h2>Progress by category</h2><div class="category-grid">${categories || '<p>No published challenges yet.</p>'}</div><div class="section-title"><h2>Challenge progress</h2><span class="muted">${challenges.length} published challenge${challenges.length === 1 ? '' : 's'}</span></div><div class="progress-list">${challenges.map((c)=>`<div><span>${escapeHTML(c.title)}</span><span>${escapeHTML(label(c.category))}</span><span class="${solved(c.id)?'solved':'muted'}">${solved(c.id)?`Solved · ${escapeHTML(completedAt(dateOf(c.id)))}`:'Not solved'}</span><a href="#challenge/${encodeURIComponent(c.id)}">View challenge</a></div>`).join('') || '<p>No published challenges.</p>'}</div>`;
  }
  function discussions(selectedRoom) {
    const rooms = ['general','network-security','web-security','file-analysis'];
    if (rooms.includes(selectedRoom)) currentRoom = selectedRoom;
    view.innerHTML = `${heading('Learner workspace','Discussions','Discuss challenge concepts with other learners.')}<div class="discussion-layout"><div class="web-card room-list"><h2>Discussion rooms</h2>${rooms.map((room)=>`<button class="room-button secondary-button" data-room="${room}">${escapeHTML(label(room))}</button>`).join('')}</div><div class="web-card chat-panel"><div class="section-title"><h2 id="room-title">${escapeHTML(label(currentRoom))}</h2><span id="chat-status" class="muted">Connecting…</span></div><div id="chat-messages" class="chat-messages" aria-live="polite"><p>Messages appear while you are connected to this room.</p></div><form id="chat-form" class="chat-form"><input id="chat-input" required maxlength="2048" autocomplete="off" placeholder="Write a message" aria-label="Message"><button class="primary-link">Send</button></form></div></div>`;
    const connect = (room) => {
      if (roomSocket) roomSocket.close();
      currentRoom = room;
      document.getElementById('room-title').textContent = label(room);
      const status = document.getElementById('chat-status');
      const messages = document.getElementById('chat-messages');
      status.textContent = 'Connecting…'; messages.innerHTML = '<p>Messages appear while you are connected to this room.</p>';
      const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:';
      const socket = new WebSocket(`${scheme}//${location.host}/api/ws/rooms/${room}`, ['cyberlab', `token.${token}`]);
      roomSocket = socket;
      socket.onopen = () => { if(roomSocket === socket) status.textContent = 'Connected'; };
      socket.onmessage = (event) => { if(roomSocket !== socket) return; try { const data=JSON.parse(event.data); if(data.type !== 'message') return; const entry=document.createElement('div'); const who=document.createElement('strong'); who.textContent=data.username; const body=document.createElement('p'); body.textContent=data.body; entry.append(who,body); messages.append(entry); messages.scrollTop=messages.scrollHeight; } catch {} };
      socket.onclose = () => { if(roomSocket === socket) status.textContent = 'Disconnected'; };
      socket.onerror = () => { if(roomSocket === socket) status.textContent = 'Connection error'; };
    };
    view.querySelectorAll('.room-button').forEach((button)=>button.onclick=()=>connect(button.dataset.room));
    document.getElementById('chat-form').onsubmit=(event)=>{event.preventDefault();const input=document.getElementById('chat-input');if(roomSocket?.readyState===WebSocket.OPEN){roomSocket.send(JSON.stringify({body:input.value}));input.value='';}else notify('Discussion room is disconnected. Choose a room to reconnect.');};
    connect(currentRoom);
  }
  async function notifications() {
    await refresh();
    let known = [];
    try { known = JSON.parse(sessionStorage.getItem(knownKey) || '[]'); } catch {}
    const previous = new Set(Array.isArray(known) ? known : []);
    const newItems = challenges.filter((c)=>!previous.has(c.id));
    sessionStorage.setItem(knownKey,JSON.stringify(challenges.map((c)=>c.id)));
    view.innerHTML = `${heading('Learner workspace','Notifications','New challenges found in the published catalog during this browser session.')}<div class="web-card"><h2>Catalog updates</h2>${newItems.length ? newItems.map((c)=>`<div class="notification-item"><div><strong>${escapeHTML(c.title)}</strong><p>${escapeHTML(label(c.category))} · ${escapeHTML(label(c.difficulty))}</p></div><a href="#challenge/${encodeURIComponent(c.id)}">View challenge →</a></div>`).join('') : '<p>No new challenge publications in this browser session.</p>'}<button id="refresh-notifications" class="secondary-button">Check for new challenges</button></div>`;
    document.getElementById('refresh-notifications').onclick=()=>notifications().catch((error)=>notify(error.message));
  }
  async function adminPage() {
    view.innerHTML = `${heading('Admin workspace','Challenge Management','Publish and archive cybersecurity challenges for learners.')}<button id="publish-toggle" class="primary-link">Publish challenge</button><div class="stats"><article><small>Published</small><strong>${challenges.length}</strong></article><article><small>Categories</small><strong>${new Set(challenges.map((c)=>c.category)).size}</strong></article></div><form id="publish-form" class="web-card admin-form" hidden><h2>Publish challenge</h2><label>ID<input name="id" required pattern="[a-z0-9-]+" placeholder="new-challenge"></label><label>Title<input name="title" required></label><label>Category<input name="category" required></label><label>Difficulty<select name="difficulty"><option>easy</option><option>medium</option><option>hard</option></select></label><label>Description and synthetic evidence<textarea name="description" rows="8" required></textarea></label><label>Expected answer<input name="answer" type="password" required></label><div class="form-actions"><button type="button" id="cancel-publish" class="secondary-button">Cancel</button><button class="primary-button">Publish challenge</button></div></form><h2>Published challenges</h2><div class="progress-list" id="admin-list">${challenges.map((c)=>`<div><span>${escapeHTML(c.title)}</span><span>${escapeHTML(label(c.category))}</span><span>${escapeHTML(label(c.difficulty))}</span><button class="archive-button secondary-button" data-id="${escapeHTML(c.id)}">Archive</button></div>`).join('') || '<p>No published challenges.</p>'}</div>`;
    const form = document.getElementById('publish-form');
    document.getElementById('publish-toggle').onclick = () => { form.hidden = false; form.querySelector('input').focus(); };
    document.getElementById('cancel-publish').onclick = () => { form.hidden = true; form.reset(); };
    form.onsubmit = async (event) => { event.preventDefault(); const data = Object.fromEntries(new FormData(form)); try { await api('/api/admin/challenges',{method:'POST',body:JSON.stringify(data)}); await refresh(); notify('Challenge published.'); adminPage(); } catch(error) { notify(error.message); } };
    view.querySelectorAll('.archive-button').forEach((button) => button.onclick = async () => { const c=challenges.find((item)=>item.id===button.dataset.id); if (!confirm(`Archive ${c.title}? It will disappear from learner searches.`)) return; try { await api(`/api/admin/challenges/${encodeURIComponent(c.id)}`,{method:'DELETE'}); await refresh(); notify('Challenge archived.'); adminPage(); } catch(error) { notify(error.message); } });
  }
  async function route() {
    notify('');
    if (roomSocket) { roomSocket.close(); roomSocket = null; }
    const [name,id] = decodeURIComponent(location.hash.slice(1) || (user?.role === 'admin' ? 'admin' : 'overview')).split('/');
    if (user?.role === 'admin') { if (name !== 'admin') location.replace('#admin'); else await adminPage(); }
    else if (name === 'challenge' && id) await challengePage(id);
    else if (name === 'challenges') browser();
    else if (name === 'progress') myProgress();
    else if (name === 'discussions') discussions(id);
    else if (name === 'notifications') await notifications();
    else overview();
    nav.querySelectorAll('a').forEach((a) => { const active = a.dataset.route === (name === 'challenge' ? 'challenges' : name); a.classList.toggle('active',active); if(active)a.setAttribute('aria-current','page');else a.removeAttribute('aria-current'); });
  }

  function loadSession() {
    try {
      const value = JSON.parse(sessionStorage.getItem(sessionKey));
      return value && typeof value.token === 'string' ? value : null;
    } catch {
      return null;
    }
  }

  function clearSession() {
    sessionStorage.removeItem(sessionKey);
  }

  function showSession(identity) {
    const roleName = identity.role === 'admin' ? 'Administrator' : 'Learner';
    const initials = identity.username.slice(0, 2).toUpperCase();
    document.getElementById('user-avatar').textContent = initials;
    document.getElementById('sidebar-username').textContent = identity.username;
    document.getElementById('sidebar-role').textContent = roleName;
    document.getElementById('workspace-label').textContent = `${roleName} workspace`;
    document.getElementById('session-status').textContent = 'Backend connected';
    document.getElementById('nav-label').textContent = `${roleName} workspace`;
    nav.innerHTML = identity.role === 'admin' ? '<a class="nav-item" data-route="admin" href="#admin">Challenge Management</a>' : '<a class="nav-item" data-route="overview" href="#overview">Overview</a><a class="nav-item" data-route="challenges" href="#challenges">Challenges</a><a class="nav-item" data-route="progress" href="#progress">My Progress</a><a class="nav-item" data-route="discussions" href="#discussions">Discussions</a><a class="nav-item" data-route="notifications" href="#notifications">Notifications</a>';
  }

  async function verify() {
    const session = loadSession();
    if (!session) {
      window.location.replace('/login');
      return;
    }

    try {
      const response = await fetch('/api/me', {headers: {Authorization: `Bearer ${session.token}`}});
      if (!response.ok) {
        clearSession();
        window.location.replace('/login');
        return;
      }
      user = await response.json(); token = session.token;
      showSession(user);
      await refresh();
      if (!sessionStorage.getItem(knownKey)) sessionStorage.setItem(knownKey, JSON.stringify(challenges.map((c)=>c.id)));
      await route();
    } catch (error) {
      alertBox.textContent = error instanceof Error ? error.message : 'Could not load the workspace. Reload this page.';
      alertBox.hidden = false;
      document.getElementById('session-status').textContent = 'Backend unavailable';
    }
  }

  document.getElementById('logout-button').addEventListener('click', () => {
    clearSession();
    window.location.assign('/login');
  });

  window.addEventListener('hashchange', () => { if (user) route().catch((error) => notify(error.message)); });

  verify();
})();
