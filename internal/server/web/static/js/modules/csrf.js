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
    return {
      'Content-Type': 'application/json',
      'X-CSRF-Token': token || ''
    };
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
      throw new Error(`Request failed: ${response.status}`);
    }
    return response;
  }
};