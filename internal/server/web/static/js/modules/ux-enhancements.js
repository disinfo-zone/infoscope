/**
 * UX Enhancements Module
 * Adds delightful micro-interactions and user experience improvements
 */

/**
 * Initialize all UX enhancements
 */
export function initializeUXEnhancements() {
  addClickRippleEffect();
  addFormValidationFeedback();
  addLoadingStates();
  addTooltips();
  addScrollEnhancements();
  addKeyboardShortcuts();
}

/**
 * Add ripple effect to buttons and clickable elements
 */
function addClickRippleEffect() {
  document.addEventListener('click', (e) => {
    const button = e.target.closest('button, .btn, .clickable');
    if (!button || button.disabled) return;

    const ripple = document.createElement('span');
    const rect = button.getBoundingClientRect();
    const size = Math.max(rect.width, rect.height);
    const x = e.clientX - rect.left - size / 2;
    const y = e.clientY - rect.top - size / 2;

    ripple.style.width = ripple.style.height = size + 'px';
    ripple.style.left = x + 'px';
    ripple.style.top = y + 'px';
    ripple.classList.add('ripple');

    button.appendChild(ripple);

    setTimeout(() => {
      ripple.remove();
    }, 600);
  });
}

/**
 * Enhanced form validation with visual feedback
 */
function addFormValidationFeedback() {
  document.addEventListener('input', (e) => {
    const input = e.target;
    if (!input.matches('input, textarea, select')) return;
    
    // Skip validation indicators for theme selector
    if (input.id === 'themeSelect') return;

    // Remove existing validation classes
    input.classList.remove('valid', 'invalid');

    if (input.value.trim() === '' && !input.required) return;

    // Check validity
    const isValid = input.checkValidity();
    input.classList.add(isValid ? 'valid' : 'invalid');

    // Add animated checkmark or x for visual feedback
    let indicator = input.parentNode.querySelector('.validation-indicator');
    if (!indicator) {
      indicator = document.createElement('span');
      indicator.className = 'validation-indicator';
      input.parentNode.style.position = 'relative';
      input.parentNode.appendChild(indicator);
    }

    indicator.textContent = isValid ? '✓' : '✗';
    indicator.className = `validation-indicator ${isValid ? 'valid' : 'invalid'}`;
  });

  // Form submission feedback
  document.addEventListener('submit', (e) => {
    const form = e.target;
    const submitButton = form.querySelector('button[type="submit"]');
    
    if (submitButton) {
      submitButton.classList.add('loading');
      submitButton.disabled = true;
      
      // Reset after 3 seconds as fallback
      setTimeout(() => {
        submitButton.classList.remove('loading');
        submitButton.disabled = false;
      }, 3000);
    }
  });
}

/**
 * Add loading states to buttons and forms
 */
function addLoadingStates() {
  // Intercept fetch requests to show loading states
  const originalFetch = window.fetch;
  window.fetch = function(...args) {
    const button = document.activeElement;
    if (button?.tagName === 'BUTTON') {
      button.classList.add('loading');
      button.disabled = true;
    }

    return originalFetch.apply(this, args).finally(() => {
      if (button?.tagName === 'BUTTON') {
        setTimeout(() => {
          button.classList.remove('loading');
          button.disabled = false;
        }, 300);
      }
    });
  };
}

/**
 * Add tooltips to elements with title attributes
 */
function addTooltips() {
  let tooltip = null;

  document.addEventListener('mouseenter', (e) => {
    const element = e.target.closest('[title], [data-tooltip]');
    if (!element) return;

    const text = element.getAttribute('title') || element.getAttribute('data-tooltip');
    if (!text) return;

    // Remove title to prevent browser tooltip
    if (element.hasAttribute('title')) {
      element.setAttribute('data-original-title', text);
      element.removeAttribute('title');
    }

    tooltip = document.createElement('div');
    tooltip.className = 'tooltip';
    tooltip.textContent = text;
    document.body.appendChild(tooltip);

    const rect = element.getBoundingClientRect();
    const tooltipRect = tooltip.getBoundingClientRect();
    
    tooltip.style.left = (rect.left + rect.width / 2 - tooltipRect.width / 2) + 'px';
    tooltip.style.top = (rect.top - tooltipRect.height - 8) + 'px';

    // Adjust if tooltip goes off screen
    if (tooltip.offsetLeft < 0) {
      tooltip.style.left = '8px';
    }
    if (tooltip.offsetLeft + tooltip.offsetWidth > window.innerWidth) {
      tooltip.style.left = (window.innerWidth - tooltip.offsetWidth - 8) + 'px';
    }

    setTimeout(() => tooltip?.classList.add('visible'), 10);
  });

  document.addEventListener('mouseleave', (e) => {
    const element = e.target.closest('[data-original-title], [data-tooltip]');
    if (!element) return;

    if (tooltip) {
      tooltip.remove();
      tooltip = null;
    }

    // Restore original title
    const originalTitle = element.getAttribute('data-original-title');
    if (originalTitle) {
      element.setAttribute('title', originalTitle);
      element.removeAttribute('data-original-title');
    }
  });
}

/**
 * Smooth scroll enhancements
 */
function addScrollEnhancements() {
  // Smooth scroll to anchors
  document.addEventListener('click', (e) => {
    const link = e.target.closest('a[href^="#"]');
    if (!link) return;

    const targetId = link.getAttribute('href').slice(1);
    const target = document.getElementById(targetId);
    if (!target) return;

    e.preventDefault();
    target.scrollIntoView({ 
      behavior: 'smooth',
      block: 'start'
    });
  });

  // Add scroll-based animations
  const observerOptions = {
    threshold: 0.1,
    rootMargin: '0px 0px -50px 0px'
  };

  const observer = new IntersectionObserver((entries) => {
    entries.forEach(entry => {
      if (entry.isIntersecting) {
        entry.target.classList.add('animate-in');
      }
    });
  }, observerOptions);

  // Observe elements that should animate in
  document.querySelectorAll('.panel, .filter-card, .group-card, .entry').forEach(el => {
    el.classList.add('animate-on-scroll');
    observer.observe(el);
  });
}

/**
 * Add keyboard shortcuts
 */
function addKeyboardShortcuts() {
  document.addEventListener('keydown', (e) => {
    // Ctrl/Cmd + / for search focus
    if ((e.ctrlKey || e.metaKey) && e.key === '/') {
      e.preventDefault();
      const searchInput = document.querySelector('input[type="search"], input[placeholder*="search" i]');
      if (searchInput) {
        searchInput.focus();
        searchInput.select();
      }
    }

    // Escape to close modals (handled by individual modules)
    // Ctrl/Cmd + Enter to submit forms
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
      const activeForm = document.activeElement?.closest('form');
      if (activeForm) {
        const submitButton = activeForm.querySelector('button[type="submit"]');
        if (submitButton && !submitButton.disabled) {
          submitButton.click();
        }
      }
    }
  });
}

/**
 * Add notification system for better user feedback
 */
export function showNotification(message, type = 'info', duration = null) {
  // Set longer, consistent defaults to ensure visibility
  if (duration === null) {
    duration = type === 'error' ? 10000 : 8000;
  }
  const notification = document.createElement('div');
  notification.className = `notification notification-${type}`;
  notification.innerHTML = `
    <span class="notification-message">${message}</span>
    <button class="notification-close">×</button>
  `;

  // Add to container or create one
  let container = document.querySelector('.notification-container');
  if (!container) {
    container = document.createElement('div');
    container.className = 'notification-container';
    document.body.appendChild(container);
  }

  container.appendChild(notification);

  // Animate in
  setTimeout(() => notification.classList.add('visible'), 10);

  // Auto remove
  const timer = setTimeout(() => {
    notification.classList.remove('visible');
    setTimeout(() => notification.remove(), 300);
  }, duration);

  // Manual close
  notification.querySelector('.notification-close')?.addEventListener('click', () => {
    clearTimeout(timer);
    notification.classList.remove('visible');
    setTimeout(() => notification.remove(), 300);
  });

  return notification;
}

/**
 * Debounce function for performance
 */
export function debounce(func, wait) {
  let timeout;
  return function executedFunction(...args) {
    const later = () => {
      clearTimeout(timeout);
      func(...args);
    };
    clearTimeout(timeout);
    timeout = setTimeout(later, wait);
  };
}

/**
 * Add focus trap for modals
 */
export function addFocusTrap(modal) {
  const focusableElements = modal.querySelectorAll(
    'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])'
  );
  
  const firstElement = focusableElements[0];
  const lastElement = focusableElements[focusableElements.length - 1];

  modal.addEventListener('keydown', (e) => {
    if (e.key === 'Tab') {
      if (e.shiftKey) {
        if (document.activeElement === firstElement) {
          e.preventDefault();
          lastElement.focus();
        }
      } else {
        if (document.activeElement === lastElement) {
          e.preventDefault();
          firstElement.focus();
        }
      }
    }
  });

  // Focus first element when modal opens
  setTimeout(() => firstElement?.focus(), 100);
}

// Auto-initialize when DOM is ready
document.addEventListener('DOMContentLoaded', initializeUXEnhancements);