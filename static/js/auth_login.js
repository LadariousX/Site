// Login page — email + one-time code, two-step form. Server-templated
// values arrive via window.BOOT (set inline in login.html).

(function () {
  const emailForm = document.getElementById('email-form');
  const emailInput = document.getElementById('email-input');
  const emailError = document.getElementById('email-error');
  const sendCodeBtn = document.getElementById('send-code-btn');

  const codeForm = document.getElementById('code-form');
  const codeEmailDisplay = document.getElementById('code-email-display');
  const codeInput = document.getElementById('code-input');
  const codeError = document.getElementById('code-error');
  const verifyBtn = document.getElementById('verify-btn');
  const resendBtn = document.getElementById('resend-btn');

  const statusEl = document.getElementById('form-status');

  let widgetId = null;
  let pendingRequest = null; // { payload, resolve, reject }
  let currentEmail = '';

  function showStatus(msg, type) {
    statusEl.textContent = msg;
    statusEl.className = 'form-status ' + type;
    statusEl.hidden = false;
  }

  function hideStatus() {
    statusEl.hidden = true;
  }

  function setFieldError(el, msg) {
    el.textContent = msg;
    el.hidden = false;
  }

  function clearFieldErrors() {
    document.querySelectorAll('.field-error').forEach((el) => {
      el.hidden = true;
      el.textContent = '';
    });
  }

  function isValidEmail(email) {
    // Light client-side check only — the server is authoritative.
    return /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email);
  }

  // ---- turnstile-gated request to send the code ----

  function onloadTurnstile() {
    widgetId = turnstile.render('#turnstile-container', {
      sitekey: BOOT.turnstileSiteKey,
      size: 'invisible',
      appearance: 'interaction-only',
      callback: function (token) {
        if (pendingRequest) {
          const { payload, resolve, reject } = pendingRequest;
          pendingRequest = null;
          performRequestCode(payload, token)
            .then(resolve, reject)
            .finally(() => {
              if (widgetId !== null) turnstile.reset(widgetId);
            });
        }
      },
      'error-callback': function () {
        showStatus('Security check failed. Please refresh.', 'error');
        sendCodeBtn.disabled = false;
        if (pendingRequest) {
          pendingRequest.reject(new Error('Security check failed'));
          pendingRequest = null;
        }
      },
    });
  }
  window.onloadTurnstile = onloadTurnstile;

  function requestCode(email) {
    return new Promise((resolve, reject) => {
      pendingRequest = { payload: { email }, resolve, reject };
      turnstile.execute(widgetId);
    });
  }

  async function performRequestCode(payload, token) {
    const res = await fetch('/api/login/request-code', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...payload, turnstileToken: token }),
    });
    if (!res.ok) {
      const text = await res.text();
      throw new Error(text || res.statusText);
    }
  }

  async function sendCode(email) {
    sendCodeBtn.disabled = true;
    showStatus('Sending code...', 'pending');
    try {
      await requestCode(email);
      currentEmail = email;
      codeEmailDisplay.textContent = email;
      emailForm.hidden = true;
      codeForm.hidden = false;
      codeInput.value = '';
      codeInput.focus();
      showStatus('Code sent! Check your email.', 'success');
    } catch (err) {
      showStatus('Error: ' + err.message, 'error');
    } finally {
      sendCodeBtn.disabled = false;
    }
  }

  emailForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    clearFieldErrors();

    const email = emailInput.value.trim();
    if (!isValidEmail(email)) {
      setFieldError(emailError, 'Please enter a valid email address.');
      return;
    }

    await sendCode(email);
  });

  // ---- code verification (no turnstile — already gated at the request step) ----

  codeInput.addEventListener('input', () => {
    if (/^\d{6}$/.test(codeInput.value.trim())) {
      codeForm.requestSubmit();
    }
  });

  codeForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    clearFieldErrors();

    const code = codeInput.value.trim();
    if (!/^\d{6}$/.test(code)) {
      setFieldError(codeError, 'Enter the 6-digit code from your email.');
      return;
    }

    verifyBtn.disabled = true;
    showStatus('Verifying...', 'pending');
    try {
      const res = await fetch('/api/login/verify-code', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: currentEmail, code }),
      });
      if (!res.ok) {
        const text = await res.text();
        setFieldError(codeError, text || res.statusText);
        hideStatus();
        return;
      }
      showStatus('Signed in!', 'success');
      const redirect = new URLSearchParams(location.search).get('redirect') || '/account';
      location.href = redirect;
    } catch (err) {
      showStatus('Network error: ' + err.message, 'error');
    } finally {
      verifyBtn.disabled = false;
    }
  });

  resendBtn.addEventListener('click', async () => {
    if (!currentEmail) return;
    clearFieldErrors();
    await sendCode(currentEmail);
  });

  // if user is logged in, redirect to account page
  fetch('/api/me').then(function (r) { return r.ok ? r.json() : {}; }).then(function (data) {
    if (data && data.email) {
      window.location.replace("/account");
    }
  }).catch(function () {});
})();
