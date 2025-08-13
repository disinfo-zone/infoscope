/**
 * Admin Settings Page Module
 * - Handles settings save, image uploads, password change, backup import/export
 * - Wires UI actions without inline handlers (CSP-friendly)
 */

import { csrf } from './csrf.js';
import { showNotification } from './ux-enhancements.js';

function getEl(id) { return document.getElementById(id); }

function bindSettingsForm() {
  const form = getEl('settingsForm');
  if (!form) return;

  form.addEventListener('submit', async (e) => {
    e.preventDefault();

    try {
      const token = csrf.getToken();

      // Handle optional image uploads
      const imageInput = getEl('footerImage');
      const faviconInput = getEl('favicon');
      const metaImageInput = getEl('metaImage');

      let imageFilename = form.querySelector('[name="footerImageURLPreview"]')?.value || '';
      let faviconFilename = form.querySelector('[name="faviconURLPreview"]')?.value || '';
      let metaImageFilename = form.querySelector('[name="metaImageURLPreview"]')?.value || '';

      // Footer image
      if (imageInput && imageInput.files && imageInput.files[0]) {
        const fd = new FormData();
        fd.append('image', imageInput.files[0]);
        const res = await fetch('/admin/upload-image', {
          method: 'POST',
          headers: token ? { 'X-CSRF-Token': token } : undefined,
          body: fd,
          credentials: 'same-origin'
        });
        if (!res.ok) throw new Error(`Image upload failed: ${await res.text()}`);
        const data = await res.json();
        imageFilename = data.filename || '';
      }

      // Favicon
      if (faviconInput && faviconInput.files && faviconInput.files[0]) {
        const fd = new FormData();
        fd.append('favicon', faviconInput.files[0]);
        const res = await fetch('/admin/upload-favicon', {
          method: 'POST',
          headers: token ? { 'X-CSRF-Token': token } : undefined,
          body: fd,
          credentials: 'same-origin'
        });
        if (!res.ok) throw new Error(`Favicon upload failed: ${await res.text()}`);
        const data = await res.json();
        faviconFilename = data.filename || '';
      }

      // Meta image
      if (metaImageInput && metaImageInput.files && metaImageInput.files[0]) {
        const fd = new FormData();
        fd.append('image', metaImageInput.files[0]);
        const res = await fetch('/admin/upload-meta-image', {
          method: 'POST',
          headers: token ? { 'X-CSRF-Token': token } : undefined,
          body: fd,
          credentials: 'same-origin'
        });
        if (!res.ok) throw new Error(`Meta image upload failed: ${await res.text()}`);
        const data = await res.json();
        metaImageFilename = data.filename || '';
      }

      // Build settings payload
      const formData = new FormData(form);
      const settings = {};
      const fieldMapping = {
        'show_blog_name': 'showBlogName',
        'show_body_text': 'showBodyText',
        'body_text_length': 'bodyTextLength'
      };

      for (const [key, value] of formData.entries()) {
        if (key === 'image' || key === 'favicon' || key === 'footerImage' || key === 'metaImage') continue;
        const jsonKey = fieldMapping[key] || key;
        if (key === 'show_blog_name' || key === 'show_body_text') {
          settings[jsonKey] = true;
        } else if (key === 'maxPosts' || key === 'updateInterval' || key === 'body_text_length') {
          settings[jsonKey] = parseInt(value, 10) || 0;
        } else {
          settings[jsonKey] = value;
        }
      }

      if (!formData.has('show_blog_name')) settings['showBlogName'] = false;
      if (!formData.has('show_body_text')) settings['showBodyText'] = false;

      // Uploaded file names override related fields
      if (imageFilename) settings.footerImageURL = imageFilename;
      if (faviconFilename) settings.faviconURL = faviconFilename;
      if (metaImageFilename) settings.metaImageURL = metaImageFilename;

      // Save settings
      const saveRes = await csrf.fetch('/admin/settings', {
        method: 'POST',
        body: JSON.stringify(settings)
      });
      if (!saveRes.ok) throw new Error(await saveRes.text());
      showNotification('Settings saved successfully!', 'success', 8000);
    } catch (err) {
      showNotification(`Error: ${err.message}`, 'error', 10000);
    }
  });
}

function bindPasswordForm() {
  const form = getEl('passwordChangeForm');
  if (!form) return;
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const currentPassword = getEl('currentPassword')?.value || '';
    const newPassword = getEl('newPassword')?.value || '';
    const confirmNewPassword = getEl('confirmNewPassword')?.value || '';
    if (newPassword !== confirmNewPassword) {
      showNotification('New passwords do not match.', 'error');
      return;
    }
    if (newPassword.length < 8) {
      showNotification('New password must be at least 8 characters long.', 'error');
      return;
    }
    try {
      const res = await csrf.fetch('/admin/change-password', {
        method: 'POST',
        body: JSON.stringify({ currentPassword, newPassword })
      });
      if (!res.ok) throw new Error(await res.text());
      showNotification('Password changed successfully!', 'success');
      form.reset();
    } catch (err) {
      showNotification(err.message, 'error');
    }
  });
}

function bindImageInputs() {
  const ids = ['footerImage', 'favicon', 'metaImage'];
  ids.forEach((inputId) => {
    const input = getEl(inputId);
    if (!input) return;
    input.addEventListener('change', (e) => {
      const file = e.target.files[0];
      const preview = getEl(inputId + 'Preview');
      const status = getEl(inputId + 'Status');
      if (!file) {
        if (preview) preview.style.display = 'none';
        if (status) { status.className = 'image-upload-status'; status.textContent = ''; }
        return;
      }
      if (file.size > 1024 * 1024) {
        if (status) { status.className = 'image-upload-status error'; status.textContent = 'File size must be less than 1MB'; }
        input.value = '';
        return;
      }
      const validTypes = inputId === 'favicon'
        ? ['image/x-icon', 'image/vnd.microsoft.icon', 'image/png']
        : ['image/jpeg', 'image/jpg', 'image/png', 'image/gif', 'image/webp'];
      if (!validTypes.includes(file.type)) {
        if (status) {
          status.className = 'image-upload-status error';
          status.textContent = inputId === 'favicon' ? 'Please select an ICO or PNG file' : 'Please select a valid image file';
        }
        input.value = '';
        return;
      }
      if (status) { status.className = 'image-upload-status success'; status.textContent = `Selected: ${file.name}`; }
      // Store preview name so we can keep it if user doesn't change the file before saving
      const hiddenNameId = inputId + 'URLPreview';
      let hidden = document.querySelector(`[name="${hiddenNameId}"]`);
      if (!hidden) {
        hidden = document.createElement('input');
        hidden.type = 'hidden';
        hidden.name = hiddenNameId;
        input.closest('form')?.appendChild(hidden);
      }
      hidden.value = file.name;
      if (preview) {
        preview.style.display = 'block';
        const img = preview.querySelector('img');
        if (img) {
          const reader = new FileReader();
          reader.onload = (ev) => { img.src = ev.target.result; };
          reader.readAsDataURL(file);
        }
      }
    });
  });
}

function bindBackup() {
  document.addEventListener('click', async (e) => {
    const btn = e.target.closest('[data-action]');
    if (!btn) return;
    const action = btn.getAttribute('data-action');
    if (action === 'export-backup') {
      e.preventDefault();
      const res = await fetch('/admin/backup/export', { credentials: 'same-origin' });
      if (!res.ok) { showNotification(`Export failed: ${await res.text()}`, 'error'); return; }
      const blob = await res.blob();
      const url = window.URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `infoscope-backup-${new Date().toISOString().split('T')[0]}.json`;
      document.body.appendChild(a); a.click(); document.body.removeChild(a); window.URL.revokeObjectURL(url);
      showNotification('Backup exported successfully', 'success');
    } else if (action === 'trigger-import') {
      e.preventDefault();
      getEl('importFile')?.click();
    }
  });

  const importInput = getEl('importFile');
  if (importInput) {
    importInput.addEventListener('change', async () => {
      const file = importInput.files[0];
      if (!file) return;
      if (!confirm('Importing will replace current data. Continue?')) { importInput.value = ''; return; }
      try {
        const fd = new FormData();
        fd.append('backup', file);
        const token = csrf.getToken();
      const res = await fetch('/admin/backup/import', {
          method: 'POST',
          headers: token ? { 'X-CSRF-Token': token } : undefined,
          body: fd,
          credentials: 'same-origin'
        });
        if (!res.ok) throw new Error(await res.text());
        showNotification('Backup imported successfully.', 'success', 6000);
        // Replace to avoid double navigations and preserve toast reading time
        location.assign(window.location.pathname + window.location.search);
      } catch (err) {
        showNotification(`Import error: ${err.message}`, 'error');
      } finally {
        importInput.value = '';
      }
    });
  }
}

document.addEventListener('DOMContentLoaded', () => {
  bindSettingsForm();
  bindPasswordForm();
  bindImageInputs();
  bindBackup();

  // Delegated actions to replace inline handlers
  document.addEventListener('click', async (e) => {
    const target = e.target.closest('[data-action]');
    if (!target) return;
    const action = target.getAttribute('data-action');

    if (action === 'open-create-group-modal') {
      e.preventDefault();
      document.getElementById('createGroupModal')?.classList.add('show');
    } else if (action === 'open-create-filter-modal') {
      e.preventDefault();
      document.getElementById('createFilterModal')?.classList.add('show');
    } else if (action === 'close-modal') {
      e.preventDefault();
      const id = target.getAttribute('data-modal-id');
      document.getElementById(id)?.classList.remove('show');
    } else if (action === 'toggle-group') {
      const groupId = target.getAttribute('data-group-id');
      const isActive = target.getAttribute('data-is-active') === 'true';
      await toggleGroup(groupId, isActive);
    } else if (action === 'edit-group') {
      const groupId = target.getAttribute('data-group-id');
      await editGroup(groupId);
    } else if (action === 'delete-group') {
      const groupId = target.getAttribute('data-group-id');
      await deleteGroup(groupId);
    } else if (action === 'add-filters-to-group') {
      const groupId = target.getAttribute('data-group-id');
      await addFiltersToGroup(groupId);
    } else if (action === 'edit-filter') {
      const filterId = target.getAttribute('data-filter-id');
      await editFilter(filterId);
    } else if (action === 'delete-filter') {
      const filterId = target.getAttribute('data-filter-id');
      await deleteFilter(filterId);
    } else if (action === 'save-group') {
      await saveGroup();
    } else if (action === 'save-filter') {
      await saveFilter();
    } else if (action === 'update-group') {
      await updateGroup();
    } else if (action === 'update-filter') {
      await updateFilter();
    } else if (action === 'confirm-delete-filter') {
      await confirmDeleteFilter();
    } else if (action === 'confirm-delete-group') {
      await confirmDeleteGroup();
    }
  });

  // Close notifications
  document.addEventListener('click', (e) => {
    const btn = e.target.closest('.notification-close');
    if (btn && btn.parentElement) btn.parentElement.remove();
  });
});


