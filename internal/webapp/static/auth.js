(() => {
  'use strict';

  const sessionKey = 'mangahub.session';
  const form = document.getElementById('auth-form');
  if (!form) return;

  const alertBox = document.getElementById('form-alert');
  const submitButton = document.getElementById('submit-button');
  const mode = form.dataset.mode;

  document.querySelectorAll('[data-reveal]').forEach((button) => {
    button.addEventListener('click', () => {
      const input = document.getElementById(button.dataset.reveal);
      if (!input) return;
      const reveal = input.type === 'password';
      input.type = reveal ? 'text' : 'password';
      button.textContent = reveal ? 'Hide' : 'Show';
      button.setAttribute('aria-label', `${reveal ? 'Hide' : 'Show'} password`);
    });
  });

  function showError(message) {
    alertBox.textContent = message;
    alertBox.hidden = false;
    alertBox.focus();
  }

  function setBusy(busy) {
    submitButton.disabled = busy;
    submitButton.classList.toggle('loading', busy);
    form.setAttribute('aria-busy', String(busy));
  }

  form.addEventListener('submit', async (event) => {
    event.preventDefault();
    alertBox.hidden = true;
    if (!form.reportValidity()) return;

    const username = form.elements.username.value.trim();
    const password = form.elements.password.value;
    if (mode === 'signup' && password !== form.elements.confirmPassword.value) {
      showError('Passwords do not match.');
      form.elements.confirmPassword.focus();
      return;
    }

    setBusy(true);
    try {
      const endpoint = mode === 'signup' ? '/api/auth/register' : '/api/auth/login';
      const response = await fetch(endpoint, {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({username, password}),
      });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(payload.error || 'Authentication failed. Please try again.');
      if (!payload.token || !payload.user) throw new Error('The server returned an invalid session.');

      sessionStorage.setItem(sessionKey, JSON.stringify({token: payload.token, user: payload.user}));
      window.location.assign('/app');
    } catch (error) {
      showError(error instanceof Error ? error.message : 'Authentication failed. Please try again.');
    } finally {
      setBusy(false);
    }
  });
})();
