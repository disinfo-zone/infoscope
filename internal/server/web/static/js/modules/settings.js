/**
 * Admin Settings Page Module
 * - Handles settings save, image uploads, password change, backup import/export
 * - Wires UI actions without inline handlers (CSP-friendly)
 */

import { csrf } from './csrf.js';
import { showNotification } from './ux-enhancements.js';

function getEl(id) { return document.getElementById(id); }

function getCurrentImageFilenameFor(inputId) {
  const input = getEl(inputId);
  if (!input) return '';
  const group = input.closest('.setting-group') || document;
  const img = group.querySelector('.current-image img');
  if (!img) return '';
  const src = img.getAttribute('src') || '';
  const marker = '/static/images/';
  const idx = src.indexOf(marker);
  if (idx === -1) return '';
  return src.slice(idx + marker.length);
}

// Filters UI removed from Settings page

// Theme detection to allow page reload when theme changes
let initialThemeName = '';
function getCurrentThemeFromDOM() {
  const links = Array.from(document.querySelectorAll('link[rel="stylesheet"]'));
  for (const link of links) {
    const href = link.getAttribute('href') || '';
    const idx = href.indexOf('/static/css/themes/');
    if (idx !== -1) {
      const after = href.slice(idx + '/static/css/themes/'.length);
      const parts = after.split('/');
      if (parts.length > 1) return parts[0];
    }
  }
  return '';
}

async function editGroup(groupId) {
  currentEditingGroupId = groupId;
  try {
    const res = await csrf.fetch(`/admin/filter-groups/${groupId}`, { headers: { 'Accept': 'application/json' } });
    if (!res.ok) throw new Error(await res.text());
    const result = await res.json();
    const group = result.data || result;

    getEl('editGroupName').value = group.name || '';
    getEl('editGroupAction').value = group.action || 'keep';
    getEl('editGroupPriority').value = group.priority ?? 0;
    getEl('editGroupCategory').value = group.apply_to_category || '';
    getEl('editGroupActive').checked = !!group.is_active;
    getEl('editGroupModalTitle').textContent = `Edit Filter Group: ${group.name}`;

    await loadFiltersForAssignment(groupId);
    getEl('editGroupModal')?.classList.add('show');
  } catch (err) {
    showNotification(`Error loading group: ${err.message}`, 'error');
  }
}

async function addFiltersToGroup(groupId) {
  currentEditingGroupId = groupId;
  try {
    const res = await csrf.fetch(`/admin/filter-groups/${groupId}`, { headers: { 'Accept': 'application/json' } });
    if (!res.ok) throw new Error(await res.text());
    const result = await res.json();
    const group = result.data || result;
    getEl('editGroupModalTitle').textContent = `Add Filters to Group: ${group.name}`;
    getEl('editGroupName').value = group.name || '';
    getEl('editGroupAction').value = group.action || 'keep';
    getEl('editGroupPriority').value = group.priority ?? 0;
    getEl('editGroupActive').checked = !!group.is_active;
    await loadFiltersForAssignment(groupId);
    getEl('editGroupModal')?.classList.add('show');
  } catch (err) {
    showNotification(`Error loading group: ${err.message}`, 'error');
  }
}

async function saveGroup() {
  const name = getEl('modalGroupName')?.value.trim();
  const action = getEl('modalGroupAction')?.value;
  const priority = parseInt(getEl('modalGroupPriority')?.value || '0', 10) || 0;
  const category = getEl('modalGroupCategory')?.value.trim() || '';
  const isActive = !!getEl('modalGroupActive')?.checked;
  if (!name) { showNotification('Please enter a group name', 'error'); return; }
  try {
    const res = await csrf.fetch('/admin/filter-groups', {
      method: 'POST',
      body: JSON.stringify({ name, action, priority, apply_to_category: category, is_active: isActive })
    });
    if (!res.ok) throw new Error(await res.text());
    showNotification('Filter group created successfully', 'success');
    getEl('createGroupModal')?.classList.remove('show');
    location.assign(location.pathname);
  } catch (err) {
    showNotification(`Error creating group: ${err.message}`, 'error');
  }
}

async function updateGroup() {
  if (!currentEditingGroupId) return;
  const name = getEl('editGroupName')?.value.trim();
  const action = getEl('editGroupAction')?.value;
  const priority = parseInt(getEl('editGroupPriority')?.value || '0', 10) || 0;
  const category = getEl('editGroupCategory')?.value.trim() || '';
  const isActive = !!getEl('editGroupActive')?.checked;
  if (!name) { showNotification('Please enter a group name', 'error'); return; }

  // Build rules from assigned list
  const assigned = getEl('assignedFilters');
  const items = assigned ? Array.from(assigned.querySelectorAll('.filter-assignment-item')) : [];
  const rules = items.map((item, index) => ({
    filter_id: parseInt(item.dataset.filterId, 10),
    operator: index === 0 ? 'AND' : (item.querySelector('.assignment-operator')?.value || 'AND'),
    position: index
  }));

  try {
    const res = await csrf.fetch(`/admin/filter-groups/${currentEditingGroupId}`, {
      method: 'PUT',
      body: JSON.stringify({ name, action, priority, apply_to_category: category, is_active: isActive })
    });
    if (!res.ok) throw new Error(await res.text());

    const resRules = await csrf.fetch(`/admin/filter-groups/${currentEditingGroupId}/rules`, {
      method: 'PUT',
      body: JSON.stringify({ rules })
    });
    if (!resRules.ok) throw new Error(await resRules.text());

    showNotification('Filter group updated successfully', 'success');
    getEl('editGroupModal')?.classList.remove('show');
    currentEditingGroupId = null;
    location.assign(location.pathname);
  } catch (err) {
    showNotification(`Error updating group: ${err.message}`, 'error');
  }
}

async function deleteGroup(groupId) {
  try {
    const res = await csrf.fetch(`/admin/filter-groups/${groupId}`, { headers: { 'Accept': 'application/json' } });
    if (!res.ok) throw new Error(await res.text());
    const result = await res.json();
    const group = result.data || result;
    const info = getEl('deleteGroupInfo');
    if (info) {
      info.innerHTML = `<h5>${group.name}</h5><div class="description">Action: <span class="action-badge ${group.action}">${group.action.toUpperCase()}</span> | Priority: <span class="priority-badge">P${group.priority}</span></div>`;
    }
    currentDeletingGroupId = groupId;
    getEl('deleteGroupModal')?.classList.add('show');
  } catch (err) {
    showNotification(`Error loading group: ${err.message}`, 'error');
  }
}

async function confirmDeleteGroup() {
  if (!currentDeletingGroupId) { showNotification('No group selected for deletion', 'error'); return; }
  try {
    const res = await csrf.fetch(`/admin/filter-groups/${currentDeletingGroupId}`, { method: 'DELETE' });
    if (!res.ok) throw new Error(await res.text());
    showNotification('Filter group deleted successfully', 'success');
    getEl('deleteGroupModal')?.classList.remove('show');
    currentDeletingGroupId = null;
    location.assign(location.pathname);
  } catch (err) {
    showNotification(`Error deleting group: ${err.message}`, 'error');
  }
}

async function loadFiltersForAssignment(groupId) {
  try {
    const [filtersRes, rulesRes] = await Promise.all([
      csrf.fetch('/admin/filters', { headers: { 'Accept': 'application/json' } }),
      csrf.fetch(`/admin/filter-groups/${groupId}/rules`, { headers: { 'Accept': 'application/json' } })
    ]);
    const filtersData = await filtersRes.json();
    const rulesData = await rulesRes.json();
    availableFilters = filtersData.data || filtersData || [];
    const groupRules = rulesData.data || rulesData || [];

    const assignedContainer = getEl('assignedFilters');
    const availableContainer = getEl('availableFilters');
    if (!assignedContainer || !availableContainer) return;
    assignedContainer.innerHTML = '';
    availableContainer.innerHTML = '';

    const assignedIds = groupRules.map(r => r.filter_id || (r.filter && r.filter.id));

    // Render assigned rules
    groupRules.forEach((rule, index) => {
      const filter = availableFilters.find(f => f.id === (rule.filter_id || (rule.filter && rule.filter.id)));
      if (filter) assignedContainer.appendChild(createAssignmentItem(filter, true, rule.operator || (index === 0 ? 'AND' : 'AND'), index));
    });

    // Render available filters
    availableFilters.forEach(filter => {
      if (!assignedIds.includes(filter.id)) availableContainer.appendChild(createAssignmentItem(filter, false));
    });

    if (assignedContainer.children.length === 0) assignedContainer.innerHTML = '<div class="empty-state small">No filters assigned</div>';
    if (availableContainer.children.length === 0) availableContainer.innerHTML = '<div class="empty-state small">No available filters</div>';
  } catch (err) {
    showNotification(`Error loading filters: ${err.message}`, 'error');
  }
}

function createAssignmentItem(filter, isAssigned, operator = 'AND', position = 0) {
  const item = document.createElement('div');
  item.className = 'filter-assignment-item';
  item.dataset.filterId = filter.id;
  const operatorHTML = isAssigned && position > 0 ? `
    <div class="logic-operator-section">
      <select class="assignment-operator" data-filter-id="${filter.id}">
        <option value="AND" ${operator === 'AND' ? 'selected' : ''}>AND</option>
        <option value="OR" ${operator === 'OR' ? 'selected' : ''}>OR</option>
      </select>
    </div>
  ` : '';
  const actionsHTML = isAssigned
    ? `<button class="assignment-btn" data-action="remove-filter-from-group" data-filter-id="${filter.id}">Remove</button>`
    : `<button class="assignment-btn primary" data-action="add-filter-to-group" data-filter-id="${filter.id}" data-group-id="${currentEditingGroupId}">Add</button>`;
  item.innerHTML = `
    ${operatorHTML}
    <div class="assignment-info">
      <div class="assignment-name">${filter.name}</div>
      <div class="assignment-pattern">${filter.pattern}</div>
    </div>
    <div class="assignment-actions">${actionsHTML}</div>
  `;
  return item;
}

async function addFilterToGroup(filterId, groupId) {
  try {
    const rulesRes = await csrf.fetch(`/admin/filter-groups/${groupId}/rules`, { headers: { 'Accept': 'application/json' } });
    const rulesData = await rulesRes.json();
    const rulesArray = Array.isArray(rulesData.data) ? rulesData.data : (Array.isArray(rulesData) ? rulesData : []);
    if (rulesArray.some(rule => (rule.filter_id || (rule.filter && rule.filter.id)) === parseInt(filterId, 10))) {
      showNotification('Filter already assigned to this group', 'error');
      return;
    }
    const newRules = [
      ...rulesArray.map((rule, index) => ({
        filter_id: rule.filter_id || (rule.filter && rule.filter.id),
        operator: rule.operator || (index === 0 ? 'AND' : 'AND'),
        position: index
      })),
      { filter_id: parseInt(filterId, 10), operator: rulesArray.length === 0 ? 'AND' : 'OR', position: rulesArray.length }
    ];
    const put = await csrf.fetch(`/admin/filter-groups/${groupId}/rules`, { method: 'PUT', body: JSON.stringify({ rules: newRules }) });
    if (!put.ok) throw new Error(await put.text());
    showNotification('Filter added to group', 'success');
    if (currentEditingGroupId) await loadFiltersForAssignment(currentEditingGroupId);
  } catch (err) {
    showNotification(`Error adding filter: ${err.message}`, 'error');
  }
}

async function removeFilterFromGroup(filterId) {
  if (!currentEditingGroupId) return;
  try {
    const rulesRes = await csrf.fetch(`/admin/filter-groups/${currentEditingGroupId}/rules`, { headers: { 'Accept': 'application/json' } });
    const rulesData = await rulesRes.json();
    const rulesArray = Array.isArray(rulesData.data) ? rulesData.data : (Array.isArray(rulesData) ? rulesData : []);
    const newRules = rulesArray
      .filter(rule => (rule.filter_id || (rule.filter && rule.filter.id)) !== parseInt(filterId, 10))
      .map((rule, index) => ({ filter_id: rule.filter_id || (rule.filter && rule.filter.id), operator: rule.operator || (index === 0 ? 'AND' : 'AND'), position: index }));
    const put = await csrf.fetch(`/admin/filter-groups/${currentEditingGroupId}/rules`, { method: 'PUT', body: JSON.stringify({ rules: newRules }) });
    if (!put.ok) throw new Error(await put.text());
    showNotification('Filter removed from group', 'success');
    await loadFiltersForAssignment(currentEditingGroupId);
  } catch (err) {
    showNotification(`Error removing filter: ${err.message}`, 'error');
  }
}

async function updateFilterOperator(filterId, newOperator) {
  if (!currentEditingGroupId) return;
  if (newOperator !== 'AND' && newOperator !== 'OR') { showNotification('Invalid operator', 'error'); return; }
  try {
    const rulesRes = await csrf.fetch(`/admin/filter-groups/${currentEditingGroupId}/rules`, { headers: { 'Accept': 'application/json' } });
    const rulesData = await rulesRes.json();
    const rulesArray = Array.isArray(rulesData.data) ? rulesData.data : (Array.isArray(rulesData) ? rulesData : []);
    const updated = rulesArray.map((rule, index) => ({
      filter_id: rule.filter_id || (rule.filter && rule.filter.id),
      operator: (rule.filter_id || (rule.filter && rule.filter.id)) === parseInt(filterId, 10) ? newOperator : (rule.operator || (index === 0 ? 'AND' : 'AND')),
      position: index
    }));
    const put = await csrf.fetch(`/admin/filter-groups/${currentEditingGroupId}/rules`, { method: 'PUT', body: JSON.stringify({ rules: updated }) });
    if (!put.ok) throw new Error(await put.text());
    showNotification(`Operator updated to ${newOperator}`, 'success');
  } catch (err) {
    showNotification(`Error updating operator: ${err.message}`, 'error');
  }
}

// ------- Filters: CRUD -------
async function saveFilter() {
  const name = getEl('modalFilterName')?.value.trim();
  const pattern = getEl('modalFilterPattern')?.value.trim();
  const pattern_type = getEl('modalFilterType')?.value || 'keyword';
  const target_type = getEl('modalFilterTargetType')?.value || 'title';
  const case_sensitive = !!getEl('modalFilterCaseSensitive')?.checked;
  if (!name || !pattern) { showNotification('Please enter both name and pattern', 'error'); return; }
  try {
    const res = await csrf.fetch('/admin/filters', { method: 'POST', body: JSON.stringify({ name, pattern, pattern_type, target_type, case_sensitive }) });
    if (!res.ok) throw new Error(await res.text());
    showNotification('Filter created successfully', 'success');
    getEl('createFilterModal')?.classList.remove('show');
    location.assign(location.pathname);
  } catch (err) {
    showNotification(`Error creating filter: ${err.message}`, 'error');
  }
}

async function editFilter(filterId) {
  currentEditingFilterId = parseInt(filterId, 10);
  try {
    const res = await csrf.fetch(`/admin/filters/${filterId}`, { headers: { 'Accept': 'application/json' } });
    if (!res.ok) throw new Error(await res.text());
    const result = await res.json();
    const filter = result.data || result;
    getEl('editFilterName').value = filter.name || '';
    getEl('editFilterPattern').value = filter.pattern || '';
    getEl('editFilterType').value = filter.pattern_type || 'keyword';
    getEl('editFilterTargetType').value = filter.target_type || 'title';
    getEl('editFilterCaseSensitive').checked = !!filter.case_sensitive;
    getEl('editFilterModal')?.classList.add('show');
  } catch (err) {
    showNotification(`Error loading filter: ${err.message}`, 'error');
  }
}

async function updateFilter() {
  if (!currentEditingFilterId) { showNotification('No filter selected for editing', 'error'); return; }
  const name = getEl('editFilterName')?.value.trim();
  const pattern = getEl('editFilterPattern')?.value.trim();
  const pattern_type = getEl('editFilterType')?.value || 'keyword';
  const target_type = getEl('editFilterTargetType')?.value || 'title';
  const case_sensitive = !!getEl('editFilterCaseSensitive')?.checked;
  if (!name || !pattern) { showNotification('Please enter both name and pattern', 'error'); return; }
  try {
    const res = await csrf.fetch(`/admin/filters/${currentEditingFilterId}`, { method: 'PUT', body: JSON.stringify({ name, pattern, pattern_type, target_type, case_sensitive }) });
    if (!res.ok) throw new Error(await res.text());
    showNotification('Filter updated successfully', 'success');
    getEl('editFilterModal')?.classList.remove('show');
    currentEditingFilterId = null;
    location.assign(location.pathname);
  } catch (err) {
    showNotification(`Error updating filter: ${err.message}`, 'error');
  }
}

async function deleteFilter(filterId) {
  try {
    const res = await csrf.fetch(`/admin/filters/${filterId}`, { headers: { 'Accept': 'application/json' } });
    if (!res.ok) throw new Error(await res.text());
    const result = await res.json();
    const filter = result.data || result;
    const info = getEl('deleteFilterInfo');
    if (info) {
      info.innerHTML = `
        <h5>${filter.name}</h5>
        <div class="pattern">${filter.pattern}</div>
        <div class="filter-meta">
          <span class="type-badge ${filter.pattern_type}">${(filter.pattern_type || '').toUpperCase()}</span>
          ${filter.case_sensitive ? '<span class="feature-badge">Case Sensitive</span>' : ''}
        </div>`;
    }
    currentDeletingFilterId = parseInt(filterId, 10);
    getEl('deleteFilterModal')?.classList.add('show');
  } catch (err) {
    showNotification(`Error loading filter: ${err.message}`, 'error');
  }
}

async function confirmDeleteFilter() {
  if (!currentDeletingFilterId) { showNotification('No filter selected for deletion', 'error'); return; }
  try {
    const res = await csrf.fetch(`/admin/filters/${currentDeletingFilterId}`, { method: 'DELETE' });
    if (!res.ok) throw new Error(await res.text());
    showNotification('Filter deleted successfully', 'success');
    getEl('deleteFilterModal')?.classList.remove('show');
    currentDeletingFilterId = null;
    location.assign(location.pathname);
  } catch (err) {
    showNotification(`Error deleting filter: ${err.message}`, 'error');
  }
}

function bindSettingsForm() {
  const form = getEl('settingsForm');
  if (!form) return;

  form.addEventListener('submit', async (e) => {
    e.preventDefault();

    try {
      const token = csrf.getToken();
      const currentTheme = initialThemeName || getCurrentThemeFromDOM();

      // Handle optional image uploads
      const imageInput = getEl('footerImage');
      const faviconInput = getEl('favicon');
      const metaImageInput = getEl('metaImage');

      let imageFilename = form.querySelector('[name="footerImageURLPreview"]')?.value || '';
      let faviconFilename = form.querySelector('[name="faviconURLPreview"]')?.value || '';
      let metaImageFilename = form.querySelector('[name="metaImageURLPreview"]')?.value || '';

      // Fallback to current values shown on the page when no new files are selected
      if (!imageFilename) imageFilename = getCurrentImageFilenameFor('footerImage');
      if (!faviconFilename) faviconFilename = getCurrentImageFilenameFor('favicon');
      if (!metaImageFilename) metaImageFilename = getCurrentImageFilenameFor('metaImage');

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
        } else if (key === 'maxPosts' || key === 'updateInterval' || key === 'body_text_length' || key === 'backupIntervalHours' || key === 'backupRetentionDays') {
          settings[jsonKey] = parseInt(value, 10) || 0;
        } else {
          settings[jsonKey] = value;
        }
      }

      if (!formData.has('show_blog_name')) settings['showBlogName'] = false;
      if (!formData.has('show_body_text')) settings['showBodyText'] = false;
      // Auto backup checkbox
      settings['backupEnabled'] = !!formData.get('backupEnabled');

      // Uploaded file names override related fields
      if (imageFilename) settings.footerImageURL = imageFilename;
      if (faviconFilename) settings.faviconURL = faviconFilename;
      if (metaImageFilename) settings.metaImageURL = metaImageFilename;

      // Determine if theme changed
      const selectedPublicTheme = (formData.get('publicTheme') || '').toString().trim().toLowerCase();
      const selectedAdminTheme = (formData.get('adminTheme') || '').toString().trim().toLowerCase();

      // Save settings
      const saveRes = await csrf.fetch('/admin/settings', {
        method: 'POST',
        body: JSON.stringify(settings)
      });
      if (!saveRes.ok) throw new Error(await saveRes.text());
      // If admin theme changed, reload to apply new admin stylesheets
      if (selectedAdminTheme && currentTheme && selectedAdminTheme !== currentTheme) {
        // Reload to apply new theme stylesheets
        location.assign(window.location.pathname + window.location.search);
        return;
      }
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
        if (preview) {
          preview.classList.add('hidden');
          preview.style.removeProperty('display');
        }
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
        preview.classList.remove('hidden');
        preview.style.removeProperty('display');
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

  // Server-side backup management
  const listContainer = getEl('backupList');
  async function refreshBackupList() {
    if (!listContainer) return;
    try {
      const res = await fetch('/admin/backup/files', { credentials: 'same-origin' });
      if (!res.ok) return;
      const data = await res.json();
      const files = Array.isArray(data.files) ? data.files : [];
      if (files.length === 0) { listContainer.innerHTML = '<div class="empty-state small">No backups found</div>'; return; }
      listContainer.innerHTML = '';
      files.forEach(f => {
        const row = document.createElement('div');
        row.className = 'backup-row';
        const date = new Date(f.modified);
        row.innerHTML = `
          <div class="backup-name">${f.name}</div>
          <div class="backup-meta">${date.toLocaleString()} • ${(f.size/1024).toFixed(1)} KB</div>
          <div class="backup-actions-inline">
            <button class="backup-button" data-restore-file="${f.name}">RESTORE</button>
            <button class="backup-button" data-download-file="${f.name}">DOWNLOAD</button>
            <button class="backup-button danger" data-delete-file="${f.name}">DELETE</button>
          </div>`;
        listContainer.appendChild(row);
      });
    } catch (_) { /* ignore */ }
  }
  refreshBackupList();

  document.addEventListener('click', async (e) => {
    const restoreBtn = e.target.closest('[data-restore-file]');
    if (restoreBtn) {
      const name = restoreBtn.getAttribute('data-restore-file');
      if (!name) return;
      if (!confirm(`Restore backup ${name}? This will replace current data.`)) return;
      try {
        const res = await csrf.fetch('/admin/backup/restore-file', { method: 'POST', body: JSON.stringify({ filename: name }) });
        if (!res.ok) throw new Error(await res.text());
        showNotification('Backup restored successfully', 'success');
        location.assign(window.location.pathname + window.location.search);
      } catch (err) {
        showNotification(`Restore failed: ${err.message}`, 'error');
      }
    }
    const downloadBtn = e.target.closest('[data-download-file]');
    if (downloadBtn) {
      const name = downloadBtn.getAttribute('data-download-file');
      if (!name) return;
      try {
        const url = `/admin/backup/download?name=${encodeURIComponent(name)}`;
        const a = document.createElement('a');
        a.href = url; a.download = name; document.body.appendChild(a); a.click(); document.body.removeChild(a);
      } catch (err) {
        showNotification(`Download failed: ${err.message}`, 'error');
      }
    }
    const deleteBtn = e.target.closest('[data-delete-file]');
    if (deleteBtn) {
      const name = deleteBtn.getAttribute('data-delete-file');
      if (!name) return;
      // Styled caution modal
      const modal = document.createElement('div');
      modal.className = 'backup-confirm-modal';
      modal.innerHTML = `
        <div class="backup-confirm-content">
          <h3>Confirm Delete</h3>
          <p>Are you sure you want to delete <strong>${name}</strong>? This action cannot be undone.</p>
          <div class="backup-confirm-actions">
            <button type="button" class="btn btn-cancel">Cancel</button>
            <button type="button" class="btn btn-danger">Delete</button>
          </div>
        </div>`;
      document.body.appendChild(modal);
      const closeModal = () => { if (modal && modal.parentNode) modal.parentNode.removeChild(modal); };
      modal.addEventListener('click', (ev) => { if (ev.target === modal) closeModal(); });
      modal.querySelector('.btn-cancel')?.addEventListener('click', closeModal);
      modal.querySelector('.btn-danger')?.addEventListener('click', async () => {
        try {
          const res = await csrf.fetch('/admin/backup/delete', { method: 'POST', body: JSON.stringify({ filename: name }) });
          if (!res.ok) throw new Error(await res.text());
          showNotification('Backup deleted', 'success');
          closeModal();
          refreshBackupList();
        } catch (err) {
          showNotification(`Delete failed: ${err.message}`, 'error');
        }
      });
    }
  });

  const createNow = getEl('createServerBackup');
  if (createNow) {
    createNow.addEventListener('click', async () => {
      try {
        const res = await csrf.fetch('/admin/backup/save', { method: 'POST' });
        if (!res.ok) throw new Error(await res.text());
        const r = await res.json();
        showNotification(`Backup created: ${r.filename}`, 'success');
        refreshBackupList();
      } catch (err) {
        showNotification(`Create backup failed: ${err.message}`, 'error');
      }
    });
  }
}

function bindThemeScanning() {
  const scanButton = getEl('scanThemesButton');
  const statusDiv = getEl('themesScanStatus');
  
  if (scanButton) {
    scanButton.addEventListener('click', async () => {
      // Show loading state
      scanButton.disabled = true;
      scanButton.textContent = 'SCANNING...';
      if (statusDiv) {
        statusDiv.className = 'status loading';
        statusDiv.textContent = 'Scanning themes directory...';
      }
      
      try {
        const response = await csrf.fetch('/admin/themes/refresh', {
          method: 'POST',
          headers: {
            'Accept': 'application/json',
          },
        });
        
        if (!response.ok) {
          throw new Error(`HTTP ${response.status}: ${await response.text()}`);
        }
        
        const result = await response.json();
        
        if (result.success) {
          // Show success message
          if (statusDiv) {
            statusDiv.className = 'status success';
            statusDiv.textContent = result.message;
          }
          
          // Update the theme dropdowns with new themes
          const publicSelect = getEl('publicTheme');
          const adminSelect = getEl('adminTheme');
          
          if (publicSelect && adminSelect && result.themes) {
            const currentPublic = publicSelect.value;
            const currentAdmin = adminSelect.value;
            
            // Clear and repopulate dropdowns
            [publicSelect, adminSelect].forEach(select => {
              const currentValue = select.value;
              select.innerHTML = '';
              
              result.themes.forEach(theme => {
                const option = document.createElement('option');
                option.value = theme;
                option.textContent = theme.charAt(0).toUpperCase() + theme.slice(1);
                if (theme === currentValue) {
                  option.selected = true;
                }
                select.appendChild(option);
              });
            });
          }
          
          showNotification(result.message, 'success');
          
          // Clear status after 3 seconds
          setTimeout(() => {
            if (statusDiv) {
              statusDiv.className = 'status';
              statusDiv.textContent = '';
            }
          }, 3000);
        } else {
          throw new Error(result.message || 'Theme scan failed');
        }
        
      } catch (error) {
        if (statusDiv) {
          statusDiv.className = 'status error';
          statusDiv.textContent = `Error: ${error.message}`;
        }
        showNotification(`Theme scan failed: ${error.message}`, 'error');
      } finally {
        // Restore button state
        scanButton.disabled = false;
        scanButton.textContent = 'SCAN FOR NEW THEMES';
      }
    });
  }
}

function bindTemplateUpdates() {
  const updateButton = getEl('updateTemplatesButton');
  const statusDiv = getEl('templatesUpdateStatus');
  
  if (updateButton) {
    updateButton.addEventListener('click', async () => {
      console.log('Template update button clicked');
      // Show custom confirmation dialog
      showTemplateUpdateConfirmation(() => performTemplateUpdate(updateButton, statusDiv));
    });
  } else {
    console.log('Template update button not found');
  }
}

function showTemplateUpdateConfirmation(onConfirm) {
  console.log('Creating template update confirmation modal');
  // Create modal container
  const modal = document.createElement('div');
  modal.className = 'template-update-confirm-modal';
  
  // Create modal content  
  const content = document.createElement('div');
  content.className = 'template-update-confirm-content';
  
  content.innerHTML = `
    <h3 class="template-update-confirm-title">⚠ Confirm Template Update</h3>
    
    <p class="template-update-confirm-message">
      This will <strong>overwrite all templates and themes</strong> with embedded versions from the application.<br><br>
      Any local customizations you have made will be <strong>permanently lost</strong>.<br><br>
      Are you sure you want to continue?
    </p>
    
    <div class="template-update-confirm-actions">
      <button id="templateUpdateCancel" class="action-btn secondary">CANCEL</button>
      <button id="templateUpdateConfirm" class="action-btn primary">UPDATE</button>
    </div>
  `;
  
  modal.appendChild(content);
  document.body.appendChild(modal);
  
  // Add event handlers
  const cancelBtn = modal.querySelector('#templateUpdateCancel');
  const confirmBtn = modal.querySelector('#templateUpdateConfirm');
  
  const closeModal = () => {
    if (modal && modal.parentNode) {
      modal.parentNode.removeChild(modal);
    }
  };
  
  cancelBtn.addEventListener('click', closeModal);
  confirmBtn.addEventListener('click', () => {
    closeModal();
    onConfirm();
  });
  
  // Close on background click
  modal.addEventListener('click', (e) => {
    if (e.target === modal) {
      closeModal();
    }
  });
  
  // Close on Escape key
  const handleEscape = (e) => {
    if (e.key === 'Escape') {
      closeModal();
      document.removeEventListener('keydown', handleEscape);
    }
  };
  document.addEventListener('keydown', handleEscape);
}

async function performTemplateUpdate(updateButton, statusDiv) {
  // Show loading state
  updateButton.disabled = true;
  updateButton.textContent = 'UPDATING...';
  if (statusDiv) {
    statusDiv.className = 'status loading';
    statusDiv.textContent = 'Updating templates and themes...';
  }
  
  try {
    const response = await csrf.fetch('/admin/templates/update', {
      method: 'POST',
      headers: {
        'Accept': 'application/json',
      },
    });
    
    if (!response.ok) {
      throw new Error(`HTTP ${response.status}: ${await response.text()}`);
    }
    
    const result = await response.json();
    
    if (result.success) {
      // Show success message
      if (statusDiv) {
        statusDiv.className = 'status success';
        statusDiv.textContent = result.message;
      }
      
      // Update the theme dropdowns if themes were updated
      const publicSelect = getEl('publicTheme');
      const adminSelect = getEl('adminTheme');
      
      if (publicSelect && adminSelect && result.themes) {
        // Clear and repopulate dropdowns
        [publicSelect, adminSelect].forEach(select => {
          const currentValue = select.value;
          select.innerHTML = '';
          
          result.themes.forEach(theme => {
            const option = document.createElement('option');
            option.value = theme;
            option.textContent = theme.charAt(0).toUpperCase() + theme.slice(1);
            if (theme === currentValue) {
              option.selected = true;
            }
            select.appendChild(option);
          });
        });
      }
      
      showNotification(result.message, 'success');
      
      // Clear status after 5 seconds
      setTimeout(() => {
        if (statusDiv) {
          statusDiv.className = 'status';
          statusDiv.textContent = '';
        }
      }, 5000);
    } else {
      throw new Error(result.message || 'Template update failed');
    }
    
  } catch (error) {
    if (statusDiv) {
      statusDiv.className = 'status error';
      statusDiv.textContent = `Error: ${error.message}`;
    }
    showNotification(`Template update failed: ${error.message}`, 'error');
  } finally {
    // Restore button state
    updateButton.disabled = false;
    updateButton.textContent = 'UPDATE TEMPLATES';
  }
}

document.addEventListener('DOMContentLoaded', () => {
  initialThemeName = getCurrentThemeFromDOM();
  bindSettingsForm();
  bindPasswordForm();
  bindImageInputs();
  bindBackup();
  bindThemeScanning();
  bindTemplateUpdates();

  // Close notifications
  document.addEventListener('click', (e) => {
    const btn = e.target.closest('.notification-close');
    if (btn && btn.parentElement) btn.parentElement.remove();
  });
});


