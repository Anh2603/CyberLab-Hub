(() => {
  'use strict';

  const sessionKey = 'cyberlab.session';
  const alertBox = document.getElementById('app-alert');

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
    document.getElementById('workspace-title').textContent = `Welcome, ${identity.username}`;
    document.getElementById('workspace-message').textContent = 'Your web session is connected to the existing CyberLab Hub authentication service.';
    document.getElementById('auth-status').textContent = 'Authenticated';
    document.getElementById('role-status').textContent = roleName;
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
      showSession(await response.json());
    } catch {
      alertBox.textContent = 'Could not reach the CyberLab Hub backend. Check that the service is running, then reload this page.';
      alertBox.hidden = false;
      document.getElementById('session-status').textContent = 'Backend unavailable';
    }
  }

  document.getElementById('logout-button').addEventListener('click', () => {
    clearSession();
    window.location.assign('/login');
  });

  verify();
})();
