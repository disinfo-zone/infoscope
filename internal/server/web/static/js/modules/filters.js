/**
 * Filter Management Module 
 * Handles filter and filter group CRUD operations with drag-and-drop reordering
 */

import { csrf } from './csrf.js';
import { initializeDragDrop, updateFilterOrder, animateReorder } from './drag-drop.js';

class FilterManager {
  constructor() {
    this.csrfToken = csrf.getToken();
    this.currentFilter = null;
    this.currentGroup = null;
    this.init();
  }

  init() {
    this.bindEvents();
    this.loadFilters();
    this.loadGroups();
    this.loadCategories();
  }

  bindEvents() {
    // Modal controls
    document.getElementById('new-filter-btn')?.addEventListener('click', () => this.showFilterModal());
    document.getElementById('new-group-btn')?.addEventListener('click', () => this.showGroupModal());
    
    document.getElementById('filter-modal-close')?.addEventListener('click', () => this.hideFilterModal());
    document.getElementById('group-modal-close')?.addEventListener('click', () => this.hideGroupModal());
    
    document.getElementById('filter-cancel')?.addEventListener('click', () => this.hideFilterModal());
    document.getElementById('group-cancel')?.addEventListener('click', () => this.hideGroupModal());
    
    // Form submissions
    document.getElementById('filter-form')?.addEventListener('submit', (e) => this.handleFilterSubmit(e));
    document.getElementById('group-form')?.addEventListener('submit', (e) => this.handleGroupSubmit(e));
    
    // Test functionality
    document.getElementById('test-btn')?.addEventListener('click', () => this.testFilter());
    
    // Close modals on outside click
    document.getElementById('filter-modal')?.addEventListener('click', (e) => {
      if (e.target.id === 'filter-modal') this.hideFilterModal();
    });
    document.getElementById('group-modal')?.addEventListener('click', (e) => {
      if (e.target.id === 'group-modal') this.hideGroupModal();
    });

    // ESC key to close modals
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        const filterModal = document.getElementById('filter-modal');
        const groupModal = document.getElementById('group-modal');
        const addFilterModal = document.getElementById('add-filter-modal');
        if (filterModal?.classList.contains('show')) this.hideFilterModal();
        if (groupModal?.classList.contains('show')) this.hideGroupModal();
        if (addFilterModal?.classList.contains('show')) this.hideAddFilterModal();
      }
    });

    // Delegated click handling for all filter/group actions
    document.addEventListener('click', (e) => {
      const btn = e.target.closest('[data-action]');
      if (!btn) return;
      const action = btn.getAttribute('data-action');
      try {
        switch (action) {
          case 'edit-filter':
            this.editFilter(btn.getAttribute('data-filter-id'));
            break;
          case 'delete-filter':
            this.deleteFilter(btn.getAttribute('data-filter-id'));
            break;
          case 'edit-group':
            this.editGroup(btn.getAttribute('data-group-id'));
            break;
          case 'delete-group':
            this.deleteGroup(btn.getAttribute('data-group-id'));
            break;
          case 'toggle-group': {
            const groupId = btn.getAttribute('data-group-id');
            const isActive = btn.getAttribute('data-is-active') === 'true';
            this.toggleGroup(groupId, isActive);
            break;
          }
          case 'remove-filter-from-group': {
            const groupId = btn.getAttribute('data-group-id');
            const filterId = btn.getAttribute('data-filter-id');
            this.removeFilterFromGroup(groupId, filterId);
            break;
          }
          case 'add-filter-to-group': {
            const groupId = btn.getAttribute('data-group-id');
            this.showAddFilterToGroupModal(groupId);
            break;
          }
        }
      } catch (err) {
        console.error('Action failed', action, err);
      }
    });

    // Operator changes in group rules
    document.addEventListener('change', (e) => {
      const select = e.target.closest('.operator-select');
      if (!select) return;
      const groupId = select.getAttribute('data-group-id');
      const filterId = select.getAttribute('data-filter-id');
      this.updateRuleOperator(groupId, filterId, select.value);
    });

    // Delegated toggle for group expand/collapse without inline handlers
    document.addEventListener('click', (e) => {
      const header = e.target.closest('.filter-group-header');
      if (!header) return;
      if (e.target.closest('.group-actions')) return;
      const groupEl = header.closest('.filter-group');
      if (groupEl) groupEl.classList.toggle('expanded');
    });
  }

  async loadFilters() {
    try {
      const response = await csrf.fetch('/admin/filters', {
        headers: { 'Accept': 'application/json' }
      });
      
      const data = await response.json();
      this.renderFilters(data.data || []);
    } catch (error) {
      console.error('Error loading filters:', error);
      this.showError('Failed to load filters');
    }
  }

  async loadGroups() {
    try {
      const response = await csrf.fetch('/admin/filter-groups', {
        headers: { 'Accept': 'application/json' }
      });
      
      const data = await response.json();
      this.renderGroups(data.data || []);
    } catch (error) {
      console.error('Error loading filter groups:', error);
      this.showError('Failed to load filter groups');
    }
  }

  async loadCategories() {
    try {
      const response = await fetch('/admin/api/categories');
      if (!response.ok) return;
      
      const data = await response.json();
      const categoryInput = document.getElementById('group-category');
      
      if (data.categories && data.categories.length > 0 && categoryInput) {
        categoryInput.setAttribute('list', 'categories-list');
        
        let datalist = document.getElementById('categories-list');
        if (!datalist) {
          datalist = document.createElement('datalist');
          datalist.id = 'categories-list';
          categoryInput.parentNode.appendChild(datalist);
        }
        
        datalist.innerHTML = data.categories.map(cat => 
          `<option value="${cat}"></option>`
        ).join('');
      }
    } catch (error) {
      console.error('Error loading categories:', error);
    }
  }

  renderFilters(filters) {
    const container = document.getElementById('filters-list');
    if (!container) return;
    
    if (filters.length === 0) {
      container.innerHTML = '<p class="no-items">No filters configured. Create your first filter to get started.</p>';
      return;
    }
    
    container.innerHTML = filters.map(filter => this.renderFilterCard(filter)).join('');
  }

  renderFilterCard(filter) {
    const targetLabels = {
      'title': 'Title',
      'content': 'Content', 
      'feed_category': 'Category',
      'feed_tags': 'Tags'
    };
    
    return `
      <div class="filter-card" data-filter-id="${filter.id}">
        <div class="filter-header">
          <h4 class="filter-name">${this.escapeHtml(filter.name)}</h4>
          <div class="filter-actions">
            <button class="btn btn-small btn-outline" data-action="edit-filter" data-filter-id="${filter.id}">Edit</button>
            <button class="btn btn-small btn-danger" data-action="delete-filter" data-filter-id="${filter.id}">Delete</button>
          </div>
        </div>
        <div class="filter-details">
          <div class="detail-row">
            <span class="detail-label">Target:</span>
            <span class="target-badge">${targetLabels[filter.target_type] || filter.target_type}</span>
          </div>
          <div class="detail-row">
            <span class="detail-label">Pattern:</span>
            <code class="pattern-text">${this.escapeHtml(filter.pattern)}</code>
          </div>
          <div class="detail-row">
            <span class="detail-label">Type:</span>
            <span class="type-badge ${filter.pattern_type}">${filter.pattern_type}</span>
            ${filter.case_sensitive ? '<span class="case-badge">Case Sensitive</span>' : ''}
          </div>
        </div>
      </div>
    `;
  }

  renderGroups(groups) {
    const container = document.getElementById('filter-groups');
    if (!container) return;
    
    if (groups.length === 0) {
      container.innerHTML = '<p class="no-items">No filter groups configured. Create a group to combine multiple filters.</p>';
      return;
    }
    
    container.innerHTML = groups.map(group => this.renderGroupCard(group)).join('');

    // Initialize drag-and-drop for each group's filter list
    groups.forEach(group => {
      const groupElement = container.querySelector(`[data-group-id="${group.id}"]`);
      if (groupElement && group.rules && group.rules.length > 1) {
        this.initializeGroupDragDrop(groupElement, group.id);
      }
    });
  }

  renderGroupCard(group) {
    const ruleCount = group.rules ? group.rules.length : 0;
    const statusClass = group.is_active ? 'status-active' : 'status-inactive';
    
    const rulesHtml = group.rules && group.rules.length > 0 
      ? `<div class="filter-list" data-group-id="${group.id}">
          ${group.rules.map((rule, index) => `
            <div class="filter-rule draggable-item" data-filter-id="${rule.filter_id}" data-order="${index}">
              <div class="rule-drag-handle" title="Drag to reorder">⋮⋮</div>
              ${index > 0 ? `
                <div class="rule-operator">
                  <select class="operator-select" data-group-id="${group.id}" data-filter-id="${rule.filter_id}">
                    <option value="AND" ${((rule.operator || 'AND') === 'AND') ? 'selected' : ''}>AND</option>
                    <option value="OR" ${((rule.operator || 'AND') === 'OR') ? 'selected' : ''}>OR</option>
                  </select>
                </div>
              ` : '<div class="rule-operator-spacer"></div>'}
              <div class="rule-info">
                <div class="rule-name">${this.escapeHtml(rule.filter?.name || '')}</div>
                <div class="rule-pattern">${this.escapeHtml(rule.filter?.pattern || '')} <span class="rule-type">(${rule.filter?.pattern_type || ''})</span></div>
              </div>
              <div class="rule-actions">
                <button class="btn btn-small btn-outline" data-action="remove-filter-from-group" data-group-id="${group.id}" data-filter-id="${rule.filter_id}">Remove</button>
              </div>
            </div>
          `).join('')}
        </div>`
      : '<p class="no-filters">No filters in this group. Add filters to get started.</p>';
    
    // Add "Add Filter" button to group content
    const addFilterButton = `
      <div class="group-add-filter">
        <button class="btn btn-small btn-primary" data-action="add-filter-to-group" data-group-id="${group.id}">
          <span class="icon">+</span> Add Filter
        </button>
      </div>
    `;
    
    return `
      <div class="filter-group expanded" data-group-id="${group.id}">
        <div class="filter-group-header" data-role="group-header">
          <div class="group-title-section">
            <span class="status-indicator ${statusClass}"></span>
            <h4 class="filter-group-title">${this.escapeHtml(group.name)}</h4>
            <div class="group-meta">
              <span class="action-badge ${group.action}">${group.action}</span>
              <span class="priority-text">Priority: ${group.priority}</span>
              <span class="filter-count">${ruleCount} filters</span>
            </div>
          </div>
          <div class="group-actions">
            <button class="btn btn-small btn-outline" data-action="toggle-group" data-group-id="${group.id}" data-is-active="${group.is_active}">${group.is_active ? 'Disable' : 'Enable'}</button>
            <button class="btn btn-small btn-outline" data-action="edit-group" data-group-id="${group.id}">Edit</button>
            <button class="btn btn-small btn-danger" data-action="delete-group" data-group-id="${group.id}">Delete</button>
          </div>
        </div>
        <div class="filter-group-content">
          ${rulesHtml}
          ${addFilterButton}
        </div>
      </div>
    `;
  }

  initializeGroupDragDrop(groupElement, groupId) {
    const filterList = groupElement.querySelector('.filter-list');
    if (!filterList) return;

    initializeDragDrop(filterList, {
      itemSelector: '.draggable-item',
      handleSelector: '.rule-drag-handle',
      onReorder: async (draggedElement, oldIndex, newIndex) => {
        try {
          const filterIds = Array.from(filterList.children)
            .filter(el => el.classList.contains('draggable-item'))
            .map(item => parseInt(item.dataset.filterId));
          
          await updateFilterOrder(groupId, filterIds);
          animateReorder(draggedElement);
          this.showSuccess('Filter order updated successfully');
        } catch (error) {
          console.error('Error updating filter order:', error);
          this.showError('Failed to update filter order');
          // Reload to restore original order
          this.loadGroups();
        }
      }
    });
  }

  showFilterModal(filter = null) {
    this.currentFilter = filter;
    const modal = document.getElementById('filter-modal');
    const title = document.getElementById('filter-modal-title');
    const form = document.getElementById('filter-form');
    
    if (!modal || !title || !form) return;
    
    if (filter) {
      title.textContent = 'Edit Filter';
      this.populateFilterForm(filter);
    } else {
      title.textContent = 'Create New Filter';
      form.reset();
    }
    
    modal.classList.add('show');
    document.body.style.overflow = 'hidden';
  }

  hideFilterModal() {
    const modal = document.getElementById('filter-modal');
    if (modal) {
      modal.classList.remove('show');
      document.body.style.overflow = '';
      this.currentFilter = null;
    }
  }

  showGroupModal(group = null) {
    this.currentGroup = group;
    const modal = document.getElementById('group-modal');
    const title = document.getElementById('group-modal-title');
    const form = document.getElementById('group-form');
    
    if (!modal || !title || !form) return;
    
    if (group) {
      title.textContent = 'Edit Filter Group';
      this.populateGroupForm(group);
    } else {
      title.textContent = 'Create New Filter Group';
      form.reset();
      const activeCheckbox = document.getElementById('group-active');
      if (activeCheckbox) activeCheckbox.checked = true;
    }
    
    modal.classList.add('show');
    document.body.style.overflow = 'hidden';
  }

  hideGroupModal() {
    const modal = document.getElementById('group-modal');
    if (modal) {
      modal.classList.remove('show');
      document.body.style.overflow = '';
      this.currentGroup = null;
    }
  }

  async showAddFilterToGroupModal(groupId) {
    try {
      // Get available filters (filters not already in this group)
      const [filtersResponse, groupResponse] = await Promise.all([
        csrf.fetch('/admin/filters', { headers: { 'Accept': 'application/json' } }),
        csrf.fetch(`/admin/filter-groups/${groupId}`, { headers: { 'Accept': 'application/json' } })
      ]);
      
      const filtersData = await filtersResponse.json();
      const groupData = await groupResponse.json();
      
      const allFilters = filtersData.data || [];
      const group = groupData.data || groupData;
      const usedFilterIds = new Set((group.rules || []).map(rule => rule.filter_id || (rule.filter && rule.filter.id)));
      const availableFilters = allFilters.filter(filter => !usedFilterIds.has(filter.id));
      
      if (availableFilters.length === 0) {
        this.showError('No available filters to add to this group');
        return;
      }
      
      // Create and show modal
      this.createAddFilterModal(groupId, group.name, availableFilters);
    } catch (error) {
      console.error('Error loading filters for group:', error);
      this.showError('Failed to load available filters');
    }
  }

  createAddFilterModal(groupId, groupName, availableFilters) {
    // Remove existing modal if present
    const existingModal = document.getElementById('add-filter-modal');
    if (existingModal) existingModal.remove();
    
    const modalHtml = `
      <div id="add-filter-modal" class="modal">
        <div class="modal-content">
          <div class="modal-header">
            <h3>Add Filter to "${this.escapeHtml(groupName)}"</h3>
            <button class="modal-close" id="add-filter-modal-close">&times;</button>
          </div>
          <div class="form-group">
            <label for="available-filters">Select filters to add:</label>
            <div class="filter-selection-list">
              ${availableFilters.map(filter => `
                <label class="filter-option">
                  <input type="checkbox" value="${filter.id}" data-filter-name="${this.escapeHtml(filter.name)}">
                  <div class="filter-option-details">
                    <div class="filter-option-name">${this.escapeHtml(filter.name)}</div>
                    <div class="filter-option-pattern">${this.escapeHtml(filter.pattern)} (${filter.pattern_type})</div>
                  </div>
                </label>
              `).join('')}
            </div>
          </div>
          <div class="modal-actions">
            <button class="btn btn-secondary" id="add-filter-cancel">Cancel</button>
            <button class="btn btn-primary" id="add-filter-confirm">Add Selected</button>
          </div>
        </div>
      </div>
    `;
    
    document.body.insertAdjacentHTML('beforeend', modalHtml);
    
    const modal = document.getElementById('add-filter-modal');
    modal.classList.add('show');
    document.body.style.overflow = 'hidden';
    
    // Bind events
    document.getElementById('add-filter-modal-close').addEventListener('click', () => this.hideAddFilterModal());
    document.getElementById('add-filter-cancel').addEventListener('click', () => this.hideAddFilterModal());
    document.getElementById('add-filter-confirm').addEventListener('click', () => this.addSelectedFiltersToGroup(groupId));
    
    // Close on backdrop click
    modal.addEventListener('click', (e) => {
      if (e.target.id === 'add-filter-modal') this.hideAddFilterModal();
    });
  }

  hideAddFilterModal() {
    const modal = document.getElementById('add-filter-modal');
    if (modal) {
      modal.classList.remove('show');
      document.body.style.overflow = '';
      setTimeout(() => modal.remove(), 300);
    }
  }

  async addSelectedFiltersToGroup(groupId) {
    const checkboxes = document.querySelectorAll('#add-filter-modal input[type="checkbox"]:checked');
    if (checkboxes.length === 0) {
      this.showError('Please select at least one filter to add');
      return;
    }
    
    try {
      // Get current group rules
      const response = await csrf.fetch(`/admin/filter-groups/${groupId}/rules`, { 
        headers: { 'Accept': 'application/json' } 
      });
      const data = await response.json();
      const currentRules = Array.isArray(data.data) ? data.data : (Array.isArray(data) ? data : []);
      
      // Create new rules for selected filters
      const newRules = Array.from(checkboxes).map((checkbox, index) => ({
        filter_id: parseInt(checkbox.value),
        operator: 'AND',
        position: currentRules.length + index
      }));
      
      // Combine existing and new rules
      const allRules = [
        ...currentRules.map((rule, index) => ({
          filter_id: rule.filter_id || (rule.filter && rule.filter.id),
          operator: rule.operator || (index === 0 ? 'AND' : 'AND'),
          position: index
        })),
        ...newRules
      ];
      
      // Update group rules
      await csrf.fetch(`/admin/filter-groups/${groupId}/rules`, {
        method: 'PUT',
        body: JSON.stringify({ rules: allRules })
      });
      
      this.hideAddFilterModal();
      this.loadGroups();
      this.showSuccess(`Added ${checkboxes.length} filter${checkboxes.length > 1 ? 's' : ''} to group`);
    } catch (error) {
      console.error('Error adding filters to group:', error);
      this.showError('Failed to add filters to group');
    }
  }

  populateFilterForm(filter) {
    const elements = {
      'filter-name': filter.name,
      'filter-target': filter.target_type,
      'filter-pattern': filter.pattern,
      'filter-type': filter.pattern_type
    };

    Object.entries(elements).forEach(([id, value]) => {
      const element = document.getElementById(id);
      if (element) element.value = value;
    });

    const caseSensitive = document.getElementById('filter-case-sensitive');
    if (caseSensitive) caseSensitive.checked = filter.case_sensitive;
  }

  populateGroupForm(group) {
    const elements = {
      'group-name': group.name,
      'group-action': group.action,
      'group-priority': group.priority,
      'group-category': group.apply_to_category || ''
    };

    Object.entries(elements).forEach(([id, value]) => {
      const element = document.getElementById(id);
      if (element) element.value = value;
    });

    const activeCheckbox = document.getElementById('group-active');
    if (activeCheckbox) activeCheckbox.checked = group.is_active;
  }

  async handleFilterSubmit(e) {
    e.preventDefault();
    
    const formData = new FormData(e.target);
    const data = {
      name: formData.get('name'),
      pattern: formData.get('pattern'),
      pattern_type: formData.get('pattern_type'),
      target_type: formData.get('target_type'),
      case_sensitive: formData.has('case_sensitive')
    };
    
    try {
      const url = this.currentFilter 
        ? `/admin/filters/${this.currentFilter.id}`
        : '/admin/filters';
      const method = this.currentFilter ? 'PUT' : 'POST';
      
      await csrf.fetch(url, {
        method,
        body: JSON.stringify(data)
      });
      
      this.hideFilterModal();
      this.loadFilters();
      this.showSuccess(this.currentFilter ? 'Filter updated successfully' : 'Filter created successfully');
    } catch (error) {
      console.error('Error saving filter:', error);
      this.showError(error.message || 'Failed to save filter');
    }
  }

  async handleGroupSubmit(e) {
    e.preventDefault();
    
    const formData = new FormData(e.target);
    const data = {
      name: formData.get('name'),
      action: formData.get('action'),
      priority: parseInt(formData.get('priority')) || 0,
      apply_to_category: formData.get('apply_to_category') || '',
      is_active: formData.has('is_active')
    };
    
    try {
      const url = this.currentGroup 
        ? `/admin/filter-groups/${this.currentGroup.id}`
        : '/admin/filter-groups';
      const method = this.currentGroup ? 'PUT' : 'POST';
      
      await csrf.fetch(url, {
        method,
        body: JSON.stringify(data)
      });
      
      this.hideGroupModal();
      this.loadGroups();
      this.showSuccess(this.currentGroup ? 'Filter group updated successfully' : 'Filter group created successfully');
    } catch (error) {
      console.error('Error saving filter group:', error);
      this.showError(error.message || 'Failed to save filter group');
    }
  }

  async editFilter(filterId) {
    try {
      const response = await csrf.fetch(`/admin/filters/${filterId}`, {
        headers: { 'Accept': 'application/json' }
      });
      
      const data = await response.json();
      this.showFilterModal(data.data);
    } catch (error) {
      console.error('Error loading filter:', error);
      this.showError('Failed to load filter for editing');
    }
  }

  async editGroup(groupId) {
    try {
      const response = await csrf.fetch(`/admin/filter-groups/${groupId}`, {
        headers: { 'Accept': 'application/json' }
      });
      
      const data = await response.json();
      this.showGroupModal(data.data);
    } catch (error) {
      console.error('Error loading filter group:', error);
      this.showError('Failed to load filter group for editing');
    }
  }

  async deleteFilter(filterId) {
    if (!confirm('Are you sure you want to delete this filter?')) return;
    
    try {
      await csrf.fetch(`/admin/filters/${filterId}`, {
        method: 'DELETE'
      });
      
      this.loadFilters();
      this.showSuccess('Filter deleted successfully');
    } catch (error) {
      console.error('Error deleting filter:', error);
      this.showError('Failed to delete filter');
    }
  }

  async deleteGroup(groupId) {
    if (!confirm('Are you sure you want to delete this filter group?')) return;
    
    try {
      await csrf.fetch(`/admin/filter-groups/${groupId}`, {
        method: 'DELETE'
      });
      
      this.loadGroups();
      this.showSuccess('Filter group deleted successfully');
    } catch (error) {
      console.error('Error deleting filter group:', error);
      this.showError('Failed to delete filter group');
    }
  }

  async toggleGroup(groupId, currentStatus) {
    try {
      const res = await csrf.fetch(`/admin/filter-groups/${groupId}`, { headers: { 'Accept': 'application/json' } });
      const data = await res.json();
      const group = data.data || data;
      await csrf.fetch(`/admin/filter-groups/${groupId}`, {
        method: 'PUT',
        body: JSON.stringify({
          name: group.name,
          action: group.action,
          priority: group.priority,
          apply_to_category: group.apply_to_category || '',
          is_active: !currentStatus
        })
      });
      this.showSuccess(`Group ${!currentStatus ? 'enabled' : 'disabled'} successfully`);
      this.loadGroups();
    } catch (error) {
      console.error('Error toggling group:', error);
      this.showError('Failed to toggle group');
    }
  }

  async removeFilterFromGroup(groupId, filterId) {
    if (!confirm('Remove this filter from the group?')) return;
    try {
      // Fetch current rules
      const res = await csrf.fetch(`/admin/filter-groups/${groupId}/rules`, { headers: { 'Accept': 'application/json' } });
      const data = await res.json();
      const currentRules = Array.isArray(data.data) ? data.data : (Array.isArray(data) ? data : []);
      const newRules = currentRules
        .filter(r => (r.filter_id || (r.filter && r.filter.id)) !== parseInt(filterId, 10))
        .map((r, index) => ({
          filter_id: r.filter_id || (r.filter && r.filter.id),
          operator: r.operator || (index === 0 ? 'AND' : 'AND'),
          position: index
        }));
      await csrf.fetch(`/admin/filter-groups/${groupId}/rules`, {
        method: 'PUT',
        body: JSON.stringify({ rules: newRules })
      });
      this.loadGroups();
      this.showSuccess('Filter removed from group');
    } catch (error) {
      console.error('Error removing filter from group:', error);
      this.showError('Failed to remove filter from group');
    }
  }

  async updateRuleOperator(groupId, filterId, newOperator) {
    try {
      if (newOperator !== 'AND' && newOperator !== 'OR') throw new Error('Invalid operator');
      const res = await csrf.fetch(`/admin/filter-groups/${groupId}/rules`, { headers: { 'Accept': 'application/json' } });
      const data = await res.json();
      const current = Array.isArray(data.data) ? data.data : (Array.isArray(data) ? data : []);
      const updated = current.map((r, index) => {
        const id = r.filter_id || (r.filter && r.filter.id);
        return {
          filter_id: id,
          operator: id === parseInt(filterId, 10) ? newOperator : (r.operator || (index === 0 ? 'AND' : 'AND')),
          position: index
        };
      });
      const put = await csrf.fetch(`/admin/filter-groups/${groupId}/rules`, {
        method: 'PUT',
        body: JSON.stringify({ rules: updated })
      });
      if (!put.ok) throw new Error(await put.text());
      this.showSuccess(`Operator set to ${newOperator}`);
    } catch (error) {
      console.error('Error updating operator:', error);
      this.showError('Failed to update operator');
    }
  }

  async testFilter() {
    const pattern = document.getElementById('test-pattern')?.value.trim();
    const testText = document.getElementById('test-text')?.value.trim();
    const targetType = document.getElementById('test-target')?.value;
    const patternType = document.getElementById('test-pattern-type')?.value;
    
    if (!pattern) {
      this.showError('Please enter a pattern to test');
      return;
    }
    
    if (!testText) {
      this.showError('Please enter text to test against the pattern');
      return;
    }
    
    try {
      const response = await csrf.fetch('/admin/filter-test', {
        method: 'POST',
        body: JSON.stringify({
          pattern: pattern,
          pattern_type: patternType,
          target_type: targetType,
          case_sensitive: false,
          test_text: testText
        })
      });
      
      const data = await response.json();
      this.showTestResults(data.data);
    } catch (error) {
      console.error('Error testing filter:', error);
      this.showError('Failed to test filter');
    }
  }

  showTestResults(results) {
    const container = document.getElementById('test-results');
    if (!container) return;
    
    container.innerHTML = `
      <strong>Test Results:</strong><br>
      Pattern matches: ${results.matches ? 'Yes' : 'No'}<br>
      Test text: "${this.escapeHtml(results.test_text)}"<br>
      Pattern: "${this.escapeHtml(results.pattern)}"
    `;
    container.className = `test-results ${results.matches ? 'success' : 'error'} show`;
  }

  showError(message) {
    this.showMessage(message, 'error-message', 5000);
  }

  showSuccess(message) {
    this.showMessage(message, 'success-message', 3000);
  }

  showMessage(message, className, duration) {
    // Remove existing message
    const existing = document.querySelector(`.${className}`);
    if (existing) existing.remove();

    // Create new message
    const messageDiv = document.createElement('div');
    messageDiv.className = className;
    messageDiv.textContent = message;
    
    const dashboard = document.querySelector('.filters-dashboard');
    if (dashboard) {
      dashboard.insertBefore(messageDiv, dashboard.firstChild);
      
      // Auto-hide
      setTimeout(() => {
        if (messageDiv.parentNode) {
          messageDiv.remove();
        }
      }, duration);
    }
  }

  escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
  }
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
  window.filterManager = new FilterManager();
});

export default FilterManager;