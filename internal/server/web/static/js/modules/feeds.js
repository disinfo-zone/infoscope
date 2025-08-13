/**
 * Feeds Management Module
 * Handles feed CRUD operations and modal interactions
 */

import { csrf } from './csrf.js';
import { showNotification, addFocusTrap } from './ux-enhancements.js';

class FeedsManager {
  constructor() {
    this.currentFeedId = null;
    this.validateTimeout = null;
    this.currentTags = [];
    this.availableTags = [];
    this.availableCategories = [];
    this.init();
  }

  init() {
    this.bindEvents();
    this.loadAvailableTags();
    this.loadAvailableCategories();

    // Delegate cancel for edit modal
    document.addEventListener('click', (e) => {
      const btn = e.target.closest('[data-action="cancel-edit-feed"]');
      if (btn) {
        e.preventDefault();
        this.hideEditModal();
      }
    });
  }

  bindEvents() {
    // Add Feed Form
    const addFeedForm = document.getElementById('addFeedForm');
    if (addFeedForm) {
      addFeedForm.addEventListener('submit', (e) => this.handleAddFeed(e));
      
      // Enable submit button when URL is entered
      const feedUrlInput = document.getElementById('feedUrl');
      if (feedUrlInput) {
        feedUrlInput.addEventListener('input', (e) => {
          const submitButton = document.getElementById('submitButton');
          if (submitButton) {
            submitButton.disabled = !e.target.value.trim();
          }
        });
      }
    }

    // Edit Feed Form
    const editFeedForm = document.getElementById('editFeedForm');
    if (editFeedForm) {
      editFeedForm.addEventListener('submit', (e) => this.handleEditFeed(e));
    }

    // Modal close events
    const modals = document.querySelectorAll('.modal');
    modals.forEach(modal => {
      modal.addEventListener('click', (e) => {
        if (e.target === modal) {
          this.hideModal(modal);
        }
      });
    });

    // ESC key to close modals
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        const activeModal = document.querySelector('.modal.show');
        if (activeModal) {
          this.hideModal(activeModal);
        }
      }
    });

    // Tag input handling
    const editTagsInput = document.getElementById('editTags');
    if (editTagsInput) {
      editTagsInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
          e.preventDefault();
          this.addTag(e.target.value.trim());
          e.target.value = '';
        }
      });
    }

    // Event delegation for adding/removing tags without inline handlers
    document.addEventListener('click', (e) => {
      const addEl = e.target.closest('.available-tags-list .available-tag');
      if (addEl) {
        const tag = addEl.getAttribute('data-tag');
        this.addTag(tag);
        return;
      }
      const removeEl = e.target.closest('#tagTokens .remove');
      if (removeEl) {
        const tag = removeEl.getAttribute('data-tag');
        this.removeTag(tag);
      }
    });

    // Category autocomplete
    const categoryInput = document.getElementById('editCategory');
    if (categoryInput) {
      categoryInput.addEventListener('input', (e) => this.handleCategoryInput(e));
    }

    // Delegate suggestion clicks
    const suggestions = document.getElementById('categorySuggestions');
    if (suggestions) {
      suggestions.addEventListener('click', (e) => {
        const item = e.target.closest('.category-suggestion');
        if (item) {
          const value = item.getAttribute('data-category');
          this.selectCategory(value);
        }
      });
    }
  }

  async handleAddFeed(e) {
    e.preventDefault();
    
    const url = document.getElementById('feedUrl').value.trim();
    const errorElement = document.getElementById('feedError');
    const previewElement = document.getElementById('feedPreview');
    const inputWrapper = document.querySelector('.input-wrapper');
    const submitButton = document.getElementById('submitButton');

    if (!url) {
      this.showError('Please enter a feed URL', errorElement);
      return;
    }

    // Clear previous state
    this.clearError(errorElement);
    previewElement?.classList.remove('show');
    inputWrapper?.classList.add('loading');
    if (submitButton) submitButton.disabled = true;

    try {
      const response = await csrf.fetch('/admin/feeds', {
        method: 'POST',
        body: JSON.stringify({ url: url })
      });

      if (!response.ok) {
        const contentType = response.headers.get('content-type');
        let errorMsg = 'Failed to add feed';
        
        if (contentType && contentType.includes('application/json')) {
          const data = await response.json();
          errorMsg = data.message || errorMsg;
        } else {
          const text = await response.text();
          errorMsg = text || errorMsg;
        }
        throw new Error(`${errorMsg} (Status: ${response.status})`);
      }

      showNotification('Feed added successfully!', 'success', 6000);
      // Soft-refresh table by reloading data instead of page reload
      try { await this.loadAvailableCategories(); } catch(_) {}
      try { await this.loadAvailableTags(); } catch(_) {}
      // Simpler approach: just refresh the page content list
      location.assign(window.location.pathname + window.location.search);
      
    } catch (error) {
      console.error('Error adding feed:', error);
      this.showError(error.message, errorElement);
    } finally {
      inputWrapper?.classList.remove('loading');
      if (submitButton) submitButton.disabled = false;
    }
  }

  async handleEditFeed(e) {
    e.preventDefault();
    
    const formData = new FormData(e.target);
    const data = {
      title: formData.get('title'),
      url: formData.get('url'),
      category: formData.get('category'),
      tags: this.currentTags
    };

    const errorElement = document.getElementById('editError');
    this.clearError(errorElement);

    try {
      const response = await csrf.fetch(`/admin/api/feeds/${this.currentFeedId}`, {
        method: 'PUT',
        body: JSON.stringify(data)
      });

      if (!response.ok) {
        const errorData = await response.json();
        throw new Error(errorData.message || 'Failed to update feed');
      }

      showNotification('Feed updated successfully!', 'success', 6000);
      this.hideEditModal();
      location.assign(window.location.pathname + window.location.search);
      
    } catch (error) {
      console.error('Error updating feed:', error);
      this.showError(error.message, errorElement);
    }
  }

  async loadAvailableTags() {
    try {
      const response = await fetch('/admin/api/tags');
      if (response.ok) {
        const data = await response.json();
        this.availableTags = data.tags || [];
        this.renderAvailableTags();
      }
    } catch (error) {
      console.error('Error loading tags:', error);
    }
  }

  async loadAvailableCategories() {
    try {
      const response = await fetch('/admin/api/categories');
      if (response.ok) {
        const data = await response.json();
        this.availableCategories = data.categories || [];
      }
    } catch (error) {
      console.error('Error loading categories:', error);
    }
  }

  renderAvailableTags() {
    const container = document.getElementById('availableTagsList');
    if (!container) return;

    container.innerHTML = this.availableTags
      .filter(tag => !this.currentTags.includes(tag))
      .map(tag => `<span class="available-tag" data-tag="${this.escapeHtml(tag)}">${this.escapeHtml(tag)}</span>`)
      .join('');
  }

  addTag(tagName) {
    if (!tagName || this.currentTags.includes(tagName)) return;
    
    this.currentTags.push(tagName);
    this.renderCurrentTags();
    this.renderAvailableTags();
  }

  removeTag(tagName) {
    this.currentTags = this.currentTags.filter(tag => tag !== tagName);
    this.renderCurrentTags();
    this.renderAvailableTags();
  }

  renderCurrentTags() {
    const container = document.getElementById('tagTokens');
    if (!container) return;

    container.innerHTML = this.currentTags
      .map(tag => `
        <span class="tag-token">
          ${this.escapeHtml(tag)}
          <span class="remove" data-tag="${this.escapeHtml(tag)}">&times;</span>
        </span>
      `).join('');
  }

  handleCategoryInput(e) {
    const input = e.target;
    const value = input.value.toLowerCase();
    const suggestions = document.getElementById('categorySuggestions');
    
    if (!suggestions) return;

    if (value.length < 2) {
      suggestions.style.display = 'none';
      return;
    }

    const matches = this.availableCategories.filter(cat => 
      cat.toLowerCase().includes(value)
    );

    if (matches.length === 0) {
      suggestions.style.display = 'none';
      return;
    }

    suggestions.innerHTML = matches
      .map(cat => `
        <div class="category-suggestion" data-category="${this.escapeHtml(cat)}">
          ${this.escapeHtml(cat)}
        </div>
      `).join('');
    
    suggestions.style.display = 'block';
  }

  selectCategory(category) {
    const input = document.getElementById('editCategory');
    const suggestions = document.getElementById('categorySuggestions');
    
    if (input) input.value = category;
    if (suggestions) suggestions.style.display = 'none';
  }

  showError(message, container) {
    if (container) {
      container.textContent = message;
      container.style.display = 'block';
    } else {
      showNotification(message, 'error');
    }
  }

  clearError(container) {
    if (container) {
      container.textContent = '';
      container.style.display = 'none';
    }
  }

  hideModal(modal) {
    if (modal) {
      modal.classList.remove('show');
      document.body.style.overflow = '';
    }
  }

  hideEditModal() {
    const modal = document.getElementById('editModal');
    this.hideModal(modal);
    this.currentFeedId = null;
    this.currentTags = [];
  }

  hideDeleteModal() {
    const modal = document.getElementById('deleteModal');
    this.hideModal(modal);
  }

  escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
  }
}

// Global functions for onclick handlers in templates
window.showEditModal = async function(feedId) {
  console.log('showEditModal called with feedId:', feedId);
  if (!window.feedsManager) {
    console.error('feedsManager not found');
    return;
  }
  
  try {
    const response = await csrf.fetch(`/admin/api/feeds/${feedId}`);
    if (!response.ok) throw new Error('Failed to load feed data');
    
    const feed = await response.json();
    console.log('Feed data loaded:', feed);
    
    // Populate form
    document.getElementById('editFeedId').value = feedId;
    document.getElementById('editTitle').value = feed.title || '';
    document.getElementById('editUrl').value = feed.url || '';
    document.getElementById('editCategory').value = feed.category || '';
    
    // Set current tags
    window.feedsManager.currentTags = feed.tags || [];
    window.feedsManager.currentFeedId = feedId;
    window.feedsManager.renderCurrentTags();
    window.feedsManager.renderAvailableTags();
    
    // Show modal
    const modal = document.getElementById('editModal');
    modal.classList.add('show');
    document.body.style.overflow = 'hidden';
    
    // Add focus trap if available
    if (typeof addFocusTrap === 'function') {
      addFocusTrap(modal);
    }
    
  } catch (error) {
    console.error('Error loading feed:', error);
    if (typeof showNotification === 'function') {
      showNotification('Failed to load feed data', 'error');
    } else {
      alert('Error: ' + error.message);
    }
  }
};

window.hideEditModal = function() {
  if (window.feedsManager) {
    window.feedsManager.hideEditModal();
  }
};

window.hideDeleteModal = function() {
  if (window.feedsManager) {
    window.feedsManager.hideDeleteModal();
  }
};

// Module will be initialized by the template

export default FeedsManager;