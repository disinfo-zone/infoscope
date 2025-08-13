/**
 * Main JavaScript Entry Point
 * Handles common functionality across all templates
 */

// Make trackClick globally available for onclick handlers in templates
import { trackClick } from './modules/tracking.js';
import { showNotification } from './modules/ux-enhancements.js';

window.trackClick = trackClick;
window.showNotification = showNotification;

/**
 * Initialize common functionality
 */
function initializeCommon() {
  // Add any common initialization here
  console.log('Infoscope v0.3.0 - Template system initialized');

  // Delegate click handling for tracked links
  document.addEventListener('click', (event) => {
    const target = event.target.closest('a.tracked-link');
    if (target) {
      event.preventDefault();
      const id = parseInt(target.getAttribute('data-entry-id'), 10);
      const url = target.getAttribute('href');
      try { trackClick(id, url); } catch (_) {}
    }
  });
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', initializeCommon);