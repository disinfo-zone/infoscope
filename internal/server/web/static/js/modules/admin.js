/**
 * Admin Interface JavaScript
 * Handles sidebar navigation, logout, and admin-specific functionality
 */

import { csrf } from './csrf.js';
import { showNotification, addFocusTrap } from './ux-enhancements.js';

/**
 * Initialize admin interface components
 */
export function initializeAdmin() {
  initializeSidebar();
  initializeLogout();
}

/**
 * Initialize sidebar toggle functionality
 */
function initializeSidebar() {
  const menuToggle = document.getElementById('menuToggle');
  const sidebar = document.querySelector('.sidebar');
  const backdrop = document.querySelector('.backdrop');

  if (!menuToggle || !sidebar || !backdrop) {
    return; // Elements not found, skip initialization
  }

  // Toggle sidebar on menu button click
  menuToggle.addEventListener('click', () => {
    sidebar.classList.toggle('active');
    backdrop.classList.toggle('active');
  });

  // Close sidebar when backdrop is clicked
  backdrop.addEventListener('click', () => {
    sidebar.classList.remove('active');
    backdrop.classList.remove('active');
  });

  // Close sidebar on escape key
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && sidebar.classList.contains('active')) {
      sidebar.classList.remove('active');
      backdrop.classList.remove('active');
    }
  });
}

/**
 * Initialize logout functionality
 */
function initializeLogout() {
  const logoutForm = document.getElementById('logoutForm');
  
  if (!logoutForm) {
    return; // Logout form not found, skip initialization
  }

  logoutForm.addEventListener('submit', async (e) => {
    e.preventDefault();
    
    try {
      const response = await csrf.fetch('/admin/logout', {
        method: 'POST'
      });
      
      if (response.ok) {
        window.location.href = '/admin/login';
      }
    } catch (err) {
      console.error('Logout failed:', err);
      // Still redirect on error to handle edge cases
      window.location.href = '/admin/login';
    }
  });
}

/**
 * Show loading state for buttons
 * @param {HTMLButtonElement} button - Button element to show loading state
 * @param {boolean} loading - Whether to show loading state
 */
export function setButtonLoading(button, loading) {
  if (!button) return;
  
  if (loading) {
    button.disabled = true;
    button.dataset.originalText = button.textContent;
    button.textContent = 'Loading...';
  } else {
    button.disabled = false;
    button.textContent = button.dataset.originalText || button.textContent;
  }
}

/**
 * Display error message
 * @param {string} message - Error message to display
 * @param {HTMLElement} container - Container element for the error
 */
export function showError(message, container) {
  if (!container) return;
  
  container.textContent = message;
  container.style.display = 'block';
  
  // Clear error after 5 seconds
  setTimeout(() => {
    container.textContent = '';
    container.style.display = 'none';
  }, 5000);
}

/**
 * Display success message
 * @param {string} message - Success message to display
 * @param {HTMLElement} container - Container element for the message
 */
export function showSuccess(message, container) {
  if (!container) return;
  
  container.textContent = message;
  container.style.color = 'var(--color-success)';
  container.style.display = 'block';
  
  // Clear message after 3 seconds
  setTimeout(() => {
    container.textContent = '';
    container.style.display = 'none';
  }, 3000);
}

// Auto-initialize when DOM is ready
document.addEventListener('DOMContentLoaded', initializeAdmin);