/**
 * CSRF Token Utilities
 * Provides functions for handling CSRF tokens in API requests
 */

export const csrf = {
  /**
   * Get CSRF token from meta tag or hidden input
   * @returns {string|null} CSRF token or null if not found
   */
  getToken() {
    const meta = document.querySelector('meta[name="csrf-token"]');
    const input = document.querySelector('input[name="csrf_token"]');
    return (meta && meta.content) || (input && input.value) || null;
  },

  /**
   * Get default headers including CSRF token
   * @returns {Object} Headers object with Content-Type and X-CSRF-Token
   */
  getHeaders() {
    const token = this.getToken();
    const headers = {
      'Content-Type': 'application/json'
    };
    if (token) {
      headers['X-CSRF-Token'] = token;
    }
    return headers;
  },

  /**
   * Enhanced fetch function with automatic CSRF token handling
   * @param {string} url - Request URL
   * @param {Object} options - Fetch options
   * @returns {Promise<Response>} Fetch response
   */
  async fetch(url, options = {}) {
    const headers = this.getHeaders();
    const finalOptions = {
      ...options,
      headers: {
        ...headers,
        ...(options.headers || {})
      },
      credentials: 'same-origin'
    };

    const response = await fetch(url, finalOptions);
    if (!response.ok) {
      let message = `Request failed: ${response.status}`;
      try {
        const bodyText = await response.text();
        if (bodyText) {
          // Try JSON first, fallback to trimmed text
          let parsed;
          try {
            parsed = JSON.parse(bodyText);
          } catch (_) {
            parsed = null;
          }
          if (parsed && typeof parsed === 'object') {
            message = parsed.message || parsed.error || message;
          } else {
            const trimmed = bodyText.replace(/\s+/g, ' ').trim();
            if (trimmed) {
              message = trimmed.length > 200 ? `${trimmed.slice(0, 200)}…` : trimmed;
            }
          }
        }
      } catch (_) {
        // Ignore body parsing failures
      }
      throw new Error(message);
    }
    return response;
  }
};
