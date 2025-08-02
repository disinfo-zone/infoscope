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
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', initializeCommon);