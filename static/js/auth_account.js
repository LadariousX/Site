(function () {
  const statusEl = document.getElementById('form-status');

  function showStatus(msg, type) {
    statusEl.textContent = msg;
    statusEl.className = 'form-status ' + type;
    statusEl.hidden = false;
  }

  document.querySelectorAll('.account-delete-btn').forEach((btn) => {
    btn.addEventListener('click', async () => {
      const id = btn.dataset.id;
      const alias = btn.dataset.alias;
      if (!confirm('Delete ' + alias + '? This cannot be undone.')) return;

      btn.disabled = true;
      try {
        const res = await fetch('/api/account/links/' + id, { method: 'DELETE' });
        if (!res.ok) {
          const text = await res.text();
          showStatus('Failed to delete: ' + (text || res.statusText), 'error');
          btn.disabled = false;
          return;
        }
        const row = btn.closest('tr');
        if (row) row.remove();
      } catch (err) {
        showStatus('Network error: ' + err.message, 'error');
        btn.disabled = false;
      }
    });
  });

  const deleteAccountBtn = document.getElementById('delete-account-btn');
  if (deleteAccountBtn) {
    deleteAccountBtn.addEventListener('click', async () => {
      if (!confirm('Delete your account? This permanently removes your account, all your shortened links, and all your uploaded files. This cannot be undone.')) return;

      deleteAccountBtn.disabled = true;
      try {
        const res = await fetch('/api/account', { method: 'DELETE' });
        if (!res.ok) {
          const text = await res.text();
          showStatus('Failed to delete account: ' + (text || res.statusText), 'error');
          deleteAccountBtn.disabled = false;
          return;
        }
        window.location.href = '/';
      } catch (err) {
        showStatus('Network error: ' + err.message, 'error');
        deleteAccountBtn.disabled = false;
      }
    });
  }
})();
