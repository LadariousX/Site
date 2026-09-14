// Link Manager — create/edit page logic. Server-templated values arrive via
// window.BOOT (set inline in link-manager.html before this file loads).

(function () {
  const PASSWORD_PLACEHOLDER = '••••••••';
  const EXP_UNITS = ['days', 'hr', 'min'];
  // The site owner's account isn't held to the normal 90-day expiration cap.
  const MAX_EXP = BOOT.isOwner ? 36500 : 90;

  const modeSwitch = document.getElementById('mode-switch');
  const modeButtons = document.querySelectorAll('.mode-btn');
  const linkForm = document.getElementById('link-form');
  const signinPrompt = document.getElementById('signin-prompt');
  const urlField = document.getElementById('url-field');
  const fileField = document.getElementById('file-field');
  let currentMode = BOOT.editMode ? (BOOT.type === 'file' ? 'file' : 'link') : 'link';

  const urlInput = document.getElementById('url-input');
  const urlError = document.getElementById('url-error');
  const aliasInput = document.getElementById('alias-input');
  const aliasError = document.getElementById('alias-error');
  const dropZone = document.getElementById('drop-zone');
  const browseBtn = document.getElementById('browse-btn');
  const fileInput = document.getElementById('file-input');
  const filesError = document.getElementById('files-error');
  const stagedTable = document.getElementById('staged-table');
  const stagedFilesBody = document.getElementById('staged-files');
  const resetExpRow = document.getElementById('reset-exp-row');
  const resetExpCheckbox = document.getElementById('reset-exp-checkbox');
  const expValue = document.getElementById('exp-value');
  const expUnitToggle = document.getElementById('exp-unit-toggle');
  const unitLabel = expUnitToggle.querySelector('.unit-label');
  const unitDots = expUnitToggle.querySelectorAll('.unit-dot');
  const expirationError = document.getElementById('expiration-error');
  const retentionLink = document.getElementById('retention-link');
  const retentionPolicyText = document.getElementById('retention-policy-text');
  const passwordInput = document.getElementById('password-input');
  const passwordError = document.getElementById('password-error');
  const createBtn = document.getElementById('create-btn');
  const statusEl = document.getElementById('form-status');

  let stagedFiles = [];
  let widgetId = null;

  // Uploads are queued rather than all fired at once — dragging in a big
  // batch used to launch one concurrent request per file, piling them up
  // against the server (and the per-account storage-limit check's row lock)
  // until enough of them missed Cloudflare's edge timeout and came back as
  // 524s.
  const MAX_CONCURRENT_UPLOADS = 3;
  let activeUploads = 0;
  const uploadQueue = [];

  function processUploadQueue() {
    while (activeUploads < MAX_CONCURRENT_UPLOADS && uploadQueue.length > 0) {
      const entry = uploadQueue.shift();
      if (!stagedFiles.includes(entry)) continue; // removed before its turn came up
      activeUploads++;
      uploadEntry(entry).finally(() => {
        activeUploads--;
        processUploadQueue();
      });
    }
  }
  let pendingRequest = null; // { url, payload, resolve, reject }
  let draftId = BOOT.editMode ? BOOT.linkId : null;
  let draftPromise = null;

  // ---- mode toggle: click anywhere in the switch flips URL/File ----

  function applyModeUI() {
    urlField.hidden = currentMode !== 'link';
    fileField.hidden = currentMode !== 'file';
    if (signinPrompt) signinPrompt.hidden = BOOT.signedIn || currentMode !== 'link';
    modeButtons.forEach((b) => b.classList.toggle('active', b.dataset.mode === currentMode));
    createBtn.textContent = BOOT.editMode ? 'Update' : (currentMode === 'file' ? 'Create Folder' : 'Create Short Link');
    updateSubmitState();
  }

  if (modeSwitch) {
    if (BOOT.editMode) {
      modeSwitch.hidden = true;
    } else {
      modeSwitch.addEventListener('click', () => {
        const next = currentMode === 'link' ? 'file' : 'link';
        if (next === 'file' && !BOOT.signedIn) {
          location.href = '/login?redirect=/link-manager';
          return;
        }
        currentMode = next;
        applyModeUI();
      });
    }
  }
  applyModeUI();

  function updateSubmitState() {
    createBtn.disabled = !BOOT.editMode && currentMode === 'file' && stagedFiles.length === 0;
  }

  // ---- missing-link redirect message ----

  const params = new URLSearchParams(location.search);
  if (params.get('error') === 'not_found') {
    showStatus('That link does not exist or has expired.', 'error');
    history.replaceState(null, '', location.pathname);
  }

  // ---- password show/hide toggles ----

  document.querySelectorAll('.pw-toggle').forEach((btn) => {
    btn.addEventListener('click', () => {
      const target = document.getElementById(btn.dataset.target);
      const showing = target.type === 'text';
      target.type = showing ? 'password' : 'text';
      btn.textContent = showing ? 'Show' : 'Hide';
    });
  });

  // ---- expiration control ----

  function setExpirationLocked(locked) {
    expValue.disabled = locked;
    document.querySelectorAll('.exp-step').forEach((b) => { b.disabled = locked; });
    expUnitToggle.disabled = locked;
  }

  function setUnit(unit) {
    expUnitToggle.dataset.unit = unit;
    unitLabel.textContent = unit;
    unitDots.forEach((d) => d.classList.toggle('active', d.dataset.unit === unit));
  }
  setUnit(expUnitToggle.dataset.unit);

  expValue.addEventListener('input', () => {
    const v = parseInt(expValue.value, 10);
    if (!isNaN(v)) expValue.value = Math.min(MAX_EXP, Math.max(1, v));
  });
  document.querySelectorAll('.exp-step').forEach((btn) => {
    btn.addEventListener('click', () => {
      const v = (parseInt(expValue.value, 10) || 1) + (btn.dataset.dir === 'up' ? 1 : -1);
      expValue.value = Math.min(MAX_EXP, Math.max(1, v));
    });
  });
  expUnitToggle.addEventListener('click', () => {
    setUnit(EXP_UNITS[(EXP_UNITS.indexOf(expUnitToggle.dataset.unit) + 1) % EXP_UNITS.length]);
  });
  retentionLink.addEventListener('click', () => {
    retentionPolicyText.hidden = !retentionPolicyText.hidden;
  });

  function roundToLargestUnit(seconds) {
    if (seconds >= 86400) return { value: Math.max(1, Math.round(seconds / 86400)), unit: 'days' };
    if (seconds >= 3600) return { value: Math.max(1, Math.round(seconds / 3600)), unit: 'hr' };
    return { value: Math.max(1, Math.round(seconds / 60)), unit: 'min' };
  }

  function readExpiration() {
    const value = parseInt(expValue.value, 10);
    if (!value || value < 1 || value > MAX_EXP) {
      setFieldError(expirationError, 'Expiration must be between 1 and ' + MAX_EXP + '.');
      return null;
    }
    return { expirationValue: value, expirationUnit: expUnitToggle.dataset.unit };
  }

  // ---- edit-mode bootstrap ----

  if (BOOT.editMode) {
    if (BOOT.type === 'url') urlInput.value = BOOT.destination;
    aliasInput.value = BOOT.alias;

    resetExpRow.hidden = false;

    const remainingSeconds = Math.max(BOOT.expiresAtUnix - Math.floor(Date.now() / 1000), 60);
    const rounded = roundToLargestUnit(remainingSeconds);
    expValue.value = rounded.value;
    setUnit(rounded.unit);
    setExpirationLocked(true);

    if (BOOT.hasPassword) passwordInput.value = PASSWORD_PLACEHOLDER;

    if (Array.isArray(BOOT.files) && BOOT.files.length > 0) {
      stagedFiles = BOOT.files.map((f) => ({
        file: { name: f.fileName, size: f.sizeBytes },
        uploaded: true,
        serverFileName: f.fileName,
      }));
      renderStagedFiles();
    }
  }

  resetExpCheckbox.addEventListener('change', () => {
    setExpirationLocked(!resetExpCheckbox.checked);
  });

  // ---- staged files table ----

  function humanSize(bytes) {
    if (bytes < 1024) return bytes + ' B';
    const units = ['KB', 'MB', 'GB'];
    let val = bytes;
    let i = -1;
    do {
      val /= 1024;
      i++;
    } while (val >= 1024 && i < units.length - 1);
    return val.toFixed(1) + ' ' + units[i];
  }

  function renderStagedFiles() {
    stagedFilesBody.innerHTML = '';
    stagedTable.hidden = stagedFiles.length === 0;
    const downloadAlias = BOOT.editMode ? BOOT.alias : null;

    stagedFiles.forEach((entry) => {
      const row = document.createElement('tr');
      const nameCell = document.createElement('td');
      nameCell.textContent = entry.file.name;
      const sizeCell = document.createElement('td');
      sizeCell.textContent = humanSize(entry.file.size);
      const progressCell = document.createElement('td');
      const progress = document.createElement('progress');
      progress.max = 100;
      progress.value = entry.uploaded ? 100 : 0;
      progressCell.appendChild(progress);
      entry.progressEl = progress;

      const downloadCell = document.createElement('td');
      const downloadLink = document.createElement('a');
      downloadLink.className = 'file-download-btn';
      downloadLink.setAttribute('aria-label', 'Download ' + entry.file.name);
      downloadLink.innerHTML = '<svg viewBox="0 0 12 12" width="12" height="12"><path d="M6 1v7M3 5l3 3 3-3M2 10h8" fill="none" stroke="currentColor" stroke-width="1.3"/></svg>';
      if (entry.uploaded && downloadAlias) {
        const nameOnServer = entry.serverFileName || entry.file.name;
        downloadLink.setAttribute('download', '');
        downloadLink.href = '/l/' + encodeURIComponent(downloadAlias) + '/' + encodeURIComponent(nameOnServer);
      } else {
        // No download before the link is finalized with an alias — a
        // pre-finalize file has no password yet and its numeric draft ID is
        // guessable, so there's no safe route to serve it from here. You
        // just dragged it in from your own disk anyway.
        downloadLink.classList.add('disabled');
        downloadLink.setAttribute('aria-disabled', 'true');
      }
      downloadCell.appendChild(downloadLink);

      const removeCell = document.createElement('td');
      const removeBtn = document.createElement('button');
      removeBtn.type = 'button';
      removeBtn.className = 'file-remove-btn';
      removeBtn.textContent = '×';
      removeBtn.setAttribute('aria-label', 'Remove ' + entry.file.name);
      removeBtn.addEventListener('click', () => removeEntry(entry));
      removeCell.appendChild(removeBtn);

      row.append(nameCell, sizeCell, progressCell, downloadCell, removeCell);
      stagedFilesBody.appendChild(row);

      const msgRow = document.createElement('tr');
      msgRow.className = 'file-row-msg';
      msgRow.hidden = true;
      const msgCell = document.createElement('td');
      msgCell.colSpan = 5;
      const msgSpan = document.createElement('span');
      msgSpan.className = 'row-error';
      msgCell.appendChild(msgSpan);
      msgRow.appendChild(msgCell);
      stagedFilesBody.appendChild(msgRow);
      entry.msgRow = msgRow;
    });
    updateSubmitState();
  }

  function setRowError(entry, msg) {
    if (entry.msgRow) {
      entry.msgRow.hidden = false;
      entry.msgRow.querySelector('.row-error').textContent = msg;
    }
  }

  function addFiles(fileList) {
    const newEntries = Array.from(fileList).map((file) => ({ file, progressEl: null }));
    stagedFiles.push(...newEntries);
    renderStagedFiles();
    uploadQueue.push(...newEntries);
    processUploadQueue();
  }

  function removeEntry(entry) {
    if (entry.xhr && !entry.uploaded) entry.xhr.abort();
    stagedFiles = stagedFiles.filter((e) => e !== entry);
    renderStagedFiles();

    const id = BOOT.editMode ? BOOT.linkId : draftId;
    if (id && entry.uploaded) {
      const nameOnServer = entry.serverFileName || entry.file.name;
      fetch('/api/link-manager/links/' + id + '/files/' + encodeURIComponent(nameOnServer), { method: 'DELETE' }).catch(() => {});
    }
  }

  browseBtn.addEventListener('click', () => fileInput.click());
  fileInput.addEventListener('change', () => {
    addFiles(fileInput.files);
    fileInput.value = '';
  });

  // ---- drag & drop: the whole page accepts a drop, not just the drop zone ----

  document.addEventListener('dragover', (e) => {
    e.preventDefault();
  });
  document.addEventListener('dragenter', (e) => {
    if (dropZone.contains(e.target)) dropZone.classList.add('dragover');
  });
  document.addEventListener('dragleave', (e) => {
    if (dropZone.contains(e.target) && !dropZone.contains(e.relatedTarget)) dropZone.classList.remove('dragover');
  });
  document.addEventListener('drop', (e) => {
    e.preventDefault();
    dropZone.classList.remove('dragover');
    if (!e.dataTransfer || !e.dataTransfer.files || e.dataTransfer.files.length === 0) return;
    if (BOOT.editMode && BOOT.type !== 'file') return;
    if (!BOOT.editMode && currentMode !== 'file') {
      if (!BOOT.signedIn) {
        location.href = '/login?redirect=/link-manager';
        return;
      }
      currentMode = 'file';
      applyModeUI();
    }
    addFiles(e.dataTransfer.files);
  });

  // ---- status / field-error helpers ----

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

  function setAliasTakenMessage(alias) {
    aliasError.textContent = 'This alias is already taken.';
    aliasError.hidden = false;
  }

  function isValidAccessPassword(pw) {
    return pw.length >= 6;
  }

  async function checkAlias(alias, excludeId) {
    try {
      const res = await fetch('/api/link-manager/check-alias', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ alias, excludeId: excludeId || 0 }),
      });
      const data = await res.json();
      return { available: data.available !== false };
    } catch (err) {
      // If the check itself fails, assume available — create/update enforce
      // uniqueness authoritatively server-side regardless.
      return { available: true };
    }
  }

  async function aliasAvailableOrShowError(alias, excludeId) {
    if (!alias) return true;
    const check = await checkAlias(alias, excludeId);
    if (!check.available) {
      setAliasTakenMessage(alias);
      return false;
    }
    return true;
  }

  aliasInput.addEventListener('blur', async () => {
    const alias = aliasInput.value.trim();
    aliasError.hidden = true;
    if (!alias || (BOOT.editMode && alias === BOOT.alias)) return;
    await aliasAvailableOrShowError(alias, BOOT.editMode ? BOOT.linkId : 0);
  });

  // ---- turnstile-gated requests (draft + finalize create) ----

  function onloadTurnstile() {
    widgetId = turnstile.render('#turnstile-container', {
      sitekey: BOOT.turnstileSiteKey,
      size: 'invisible',
      appearance: 'interaction-only',
      callback: function (token) {
        if (pendingRequest) {
          const { url, payload, resolve, reject } = pendingRequest;
          pendingRequest = null;
          performRequest(url, payload, token)
            .then(resolve, reject)
            .finally(() => {
              if (widgetId !== null) turnstile.reset(widgetId);
            });
        }
      },
      'error-callback': function () {
        showStatus('Security check failed. Please refresh.', 'error');
        createBtn.disabled = false;
        if (pendingRequest) {
          pendingRequest.reject(new Error('Security check failed'));
          pendingRequest = null;
        }
      },
    });
  }
  window.onloadTurnstile = onloadTurnstile;

  function requestWithTurnstile(url, payload) {
    return new Promise((resolve, reject) => {
      pendingRequest = { url, payload, resolve, reject };
      turnstile.execute(widgetId);
    });
  }

  async function performRequest(url, payload, token) {
    const res = await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ...payload, turnstileToken: token }),
    });
    if (!res.ok) {
      if (res.status === 409) {
        const err = new Error('That alias is already taken.');
        err.aliasTaken = true;
        throw err;
      }
      const text = await res.text();
      throw new Error(text || res.statusText);
    }
    return res.json();
  }

  function requestDraft(payload) {
    return requestWithTurnstile('/api/link-manager/links/draft', payload);
  }

  function ensureDraft() {
    if (draftId) return Promise.resolve(draftId);
    if (draftPromise) return draftPromise;

    showStatus('Preparing upload...', 'pending');
    draftPromise = requestDraft({ type: 'file' })
      .then((data) => {
        draftId = data.id;
        hideStatus();
        return draftId;
      })
      .catch((err) => {
        draftPromise = null;
        showStatus('Failed to prepare upload: ' + err.message, 'error');
        throw err;
      });
    return draftPromise;
  }

  async function uploadEntry(entry) {
    try {
      const id = BOOT.editMode ? BOOT.linkId : await ensureDraft();
      await uploadOne(id, entry);
      if (entry.failed) setRowError(entry, entry.failureMessage || ('Failed to upload ' + entry.file.name + '.'));
    } catch (err) {
      // ensureDraft already surfaced the error via showStatus.
    }
  }

  function uploadOne(id, entry) {
    return new Promise((resolve) => {
      const xhr = new XMLHttpRequest();
      entry.xhr = xhr;
      xhr.open('POST', '/api/link-manager/links/' + id + '/files');
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable && entry.progressEl) entry.progressEl.value = (e.loaded / e.total) * 100;
      };
      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          try {
            entry.serverFileName = JSON.parse(xhr.responseText).fileName;
          } catch (err) {
            // falls back to the original filename
          }
          entry.uploaded = true;
          if (entry.progressEl) entry.progressEl.value = 100;
          renderStagedFiles();
        } else {
          entry.failed = true;
          // A plain-text body is our own server's error message; anything
          // that looks like a full HTML page is an intermediary (Cloudflare,
          // nginx) error page, not something to show verbatim in the row.
          const text = xhr.responseText && xhr.responseText.trim();
          entry.failureMessage = text && !/^<(!doctype|html)/i.test(text) ? text : '';
        }
        resolve();
      };
      xhr.onerror = () => {
        entry.failed = true;
        resolve();
      };
      xhr.onabort = () => resolve();
      const formData = new FormData();
      formData.append('file', entry.file);
      xhr.send(formData);
    });
  }

  // ---- submit ----

  linkForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    clearFieldErrors();

    if (BOOT.editMode) {
      await handleUpdateSubmit();
    } else if (currentMode === 'file') {
      await handleFileCreateSubmit();
    } else {
      await handleLinkCreateSubmit();
    }
  });

  // Shared expiration + password validation for both create flows (link and
  // file). Returns null (after showing the relevant field error) if
  // anything's invalid.
  function readCommonCreateFields() {
    const exp = readExpiration();
    if (!exp) return null;

    const password = passwordInput.value;
    if (password && !isValidAccessPassword(password)) {
      setFieldError(passwordError, 'Password must be at least 6 characters.');
      return null;
    }

    return { expirationValue: exp.expirationValue, expirationUnit: exp.expirationUnit, password };
  }

  function goToSummary(alias) {
    location.href = '/link-manager/summary?alias=' + encodeURIComponent(alias);
  }

  async function submitCreate(payload) {
    createBtn.disabled = true;
    showStatus('Creating...', 'pending');
    try {
      const data = await requestCreate(payload);
      goToSummary(data.alias);
    } catch (err) {
      if (err.aliasTaken) {
        setAliasTakenMessage(payload.alias);
        hideStatus();
      } else {
        showStatus('Error: ' + err.message, 'error');
      }
      createBtn.disabled = false;
    }
  }

  function requestCreate(payload) {
    return requestWithTurnstile('/api/link-manager/links', payload);
  }

  async function handleLinkCreateSubmit() {
    const destination = urlInput.value.trim();
    if (!destination) {
      setFieldError(urlError, 'Please enter a URL.');
      return;
    }

    const common = readCommonCreateFields();
    if (!common) return;

    const alias = aliasInput.value.trim();
    if (!(await aliasAvailableOrShowError(alias, 0))) return;

    await submitCreate({ alias, type: 'url', destination, ...common });
  }

  async function handleFileCreateSubmit() {
    if (stagedFiles.length === 0) {
      setFieldError(filesError, 'Please add at least one file.');
      return;
    }
    if (!draftId) {
      setFieldError(filesError, 'Please wait for your files to finish preparing.');
      return;
    }

    const common = readCommonCreateFields();
    if (!common) return;

    const alias = aliasInput.value.trim();
    if (!(await aliasAvailableOrShowError(alias, 0))) return;

    await submitCreate({ id: draftId, alias, type: 'file', ...common });
  }

  async function handleUpdateSubmit() {
    const alias = aliasInput.value.trim();
    if (!alias) {
      setFieldError(aliasError, 'Alias is required.');
      return;
    }

    let destination = '';
    if (BOOT.type === 'url') {
      destination = urlInput.value.trim();
      if (!destination) {
        setFieldError(urlError, 'Please enter a URL.');
        return;
      }
    }

    const resetExpiration = resetExpCheckbox.checked;
    let expirationValue = 0;
    let expirationUnit = 'days';
    if (resetExpiration) {
      const exp = readExpiration();
      if (!exp) return;
      expirationValue = exp.expirationValue;
      expirationUnit = exp.expirationUnit;
    }

    // Clearing the password field removes it; leaving the placeholder dots
    // untouched keeps whatever's already set; anything else is a new password.
    let removePassword = false;
    let password = '';
    const pwValue = passwordInput.value;
    if (BOOT.hasPassword && pwValue === PASSWORD_PLACEHOLDER) {
      // unchanged
    } else if (pwValue === '') {
      removePassword = true;
    } else {
      password = pwValue;
      if (!isValidAccessPassword(password)) {
        setFieldError(passwordError, 'Password must be at least 6 characters.');
        return;
      }
    }

    if (alias !== BOOT.alias && !(await aliasAvailableOrShowError(alias, BOOT.linkId))) return;

    createBtn.disabled = true;
    showStatus('Updating...', 'pending');
    try {
      const res = await fetch('/api/link-manager/links/' + BOOT.linkId, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ alias, destination, resetExpiration, expirationValue, expirationUnit, password, removePassword }),
      });
      if (!res.ok) {
        if (res.status === 409) {
          setAliasTakenMessage(alias);
          hideStatus();
        } else if (res.status === 401) {
          showStatus('Please sign in to edit this link. Redirecting...', 'error');
          setTimeout(() => { location.href = '/login?redirect=' + encodeURIComponent('/l/' + BOOT.alias + '/edit'); }, 1200);
        } else {
          const text = await res.text();
          showStatus('Error: ' + (text || res.statusText), 'error');
        }
        createBtn.disabled = false;
        return;
      }
      const data = await res.json();
      goToSummary(data.alias);
    } catch (err) {
      showStatus('Network error: ' + err.message, 'error');
      createBtn.disabled = false;
    }
  }
})();
