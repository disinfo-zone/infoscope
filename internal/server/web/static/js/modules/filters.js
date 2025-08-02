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
        if (document.getElementById('filter-modal')?.style.display === 'block') {
          this.hideFilterModal();
        }
        if (document.getElementById('group-modal')?.style.display === 'block') {
          this.hideGroupModal();
        }
      }
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
        <h4>
          ${this.escapeHtml(filter.name)}
          <div class="card-actions">
            <button class="btn btn-small btn-outline" onclick="filterManager.editFilter(${filter.id})">Edit</button>
            <button class="btn btn-small btn-danger" onclick="filterManager.deleteFilter(${filter.id})">Delete</button>
          </div>
        </h4>
        <div class="filter-info">
          <div class="info-row">
            <span class="info-label">Target:</span>
            <span class="target-badge">${targetLabels[filter.target_type] || filter.target_type}</span>
          </div>
          <div class="info-row">
            <span class="info-label">Pattern:</span>
            <span class="info-value">${this.escapeHtml(filter.pattern)}</span>
          </div>
          <div class="info-row">
            <span class="info-label">Type:</span>
            <span class="pattern-badge ${filter.pattern_type}">${filter.pattern_type}</span>
          </div>
          ${filter.case_sensitive ? '<div class="info-row"><span class="info-label">Case Sensitive</span></div>' : ''}
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
            <div class="filter-item draggable-item" data-filter-id="${rule.id}" data-order="${index}">
              <div class="filter-info">
                <div class="filter-name">${this.escapeHtml(rule.name)}</div>
                <div class="filter-details">${this.escapeHtml(rule.pattern)} (${rule.pattern_type})</div>
              </div>
              <div class="filter-actions">
                <button onclick="filterManager.removeFilterFromGroup(${group.id}, ${rule.id})" title="Remove from group">×</button>
              </div>
            </div>
          `).join('')}
        </div>`
      : '<p class="no-filters">No filters in this group. Add filters to get started.</p>';
    
    return `
      <div class="filter-group" data-group-id="${group.id}">
        <div class="filter-group-header" onclick="this.parentElement.classList.toggle('expanded')">
          <h4 class="filter-group-title">
            <span class="status-indicator ${statusClass}"></span>
            ${this.escapeHtml(group.name)}
          </h4>
          <div class="filter-group-meta">
            <span class="action-badge ${group.action}">${group.action}</span>
            <span>Priority: ${group.priority}</span>
            <span>${ruleCount} filters</span>
          </div>
          <div class="filter-group-actions" onclick="event.stopPropagation()">
            <button onclick="filterManager.editGroup(${group.id})" title="Edit group">✎</button>
            <button onclick="filterManager.deleteGroup(${group.id})" title="Delete group">🗑</button>
          </div>
        </div>
        <div class="filter-group-content">
          ${rulesHtml}
        </div>
      </div>
    `;
  }

  initializeGroupDragDrop(groupElement, groupId) {
    const filterList = groupElement.querySelector('.filter-list');
    if (!filterList) return;

    initializeDragDrop(filterList, {
      itemSelector: '.draggable-item',
      handleSelector: '.drag-handle',
      onReorder: async (draggedElement, oldIndex, newIndex) => {
        try {
          const filterIds = Array.from(filterList.children).map(item => 
            parseInt(item.dataset.filterId)
          );
          
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

  async removeFilterFromGroup(groupId, filterId) {
    if (!confirm('Remove this filter from the group?')) return;
    
    try {
      await csrf.fetch(`/admin/filter-groups/${groupId}/filters/${filterId}`, {
        method: 'DELETE'
      });
      
      this.loadGroups();
      this.showSuccess('Filter removed from group');
    } catch (error) {
      console.error('Error removing filter from group:', error);
      this.showError('Failed to remove filter from group');
    }
  }

  async testFilter() {
    const testText = document.getElementById('test-text')?.value.trim();
    const targetType = document.getElementById('test-target')?.value;
    
    if (!testText) {
      this.showError('Please enter text to test');
      return;
    }
    
    try {
      const response = await csrf.fetch('/admin/filter-test', {
        method: 'POST',
        body: JSON.stringify({
          pattern: testText,
          pattern_type: 'keyword',
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