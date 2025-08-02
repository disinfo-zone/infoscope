/**
 * Click Tracking Module
 * Handles anonymous click tracking for RSS feed entries
 */

import { csrf } from './csrf.js';

/**
 * Track a click on an RSS entry and open the link
 * @param {number} entryId - The ID of the RSS entry
 * @param {string} url - The URL to open
 * @returns {boolean} Always returns false to prevent default link behavior
 */
export function trackClick(entryId, url) {
  // Track the click asynchronously
  csrf.fetch('/click?id=' + entryId, {
    method: 'POST'
  }).catch(err => {
    console.error('Error tracking click:', err);
  });

  // Open link in new tab
  window.open(url, '_blank');
  return false; // Prevent default link behavior
}

/**
 * Initialize favicon lazy loading optimization for desktop
 * Only applies optimizations on viewports wider than 768px
 */
export function initializeFaviconOptimization() {
  // Only apply optimizations on desktop (viewport wider than 768px)
  if (window.innerWidth <= 768) {
    return;
  }

  const favicons = document.querySelectorAll('.favicon');
  
  // Use Intersection Observer for lazy loading beyond the fold
  if ('IntersectionObserver' in window) {
    const observer = new IntersectionObserver((entries) => {
      entries.forEach(entry => {
        if (entry.isIntersecting) {
          const img = entry.target;
          if (img.dataset.src) {
            img.src = img.dataset.src;
            img.removeAttribute('data-src');
            observer.unobserve(img);
          }
        }
      });
    }, {
      rootMargin: '50px 0px'
    });

    // Only lazy load favicons beyond the first 15 entries
    favicons.forEach((img, index) => {
      if (index >= 15) {
        img.dataset.src = img.src;
        img.src = '/static/favicons/default.ico';
        observer.observe(img);
      }
    });
  }
}

// Auto-initialize favicon optimization when DOM is ready
document.addEventListener('DOMContentLoaded', initializeFaviconOptimization);