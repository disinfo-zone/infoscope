/**
 * Login Page Module
 */

import { csrf } from './csrf.js';

document.addEventListener('DOMContentLoaded', () => {
  const form = document.getElementById('loginForm');
  const errorEl = document.getElementById('error');
  if (!form) return;

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (errorEl) errorEl.textContent = '';

    try {
      const formData = {
        username: document.getElementById('username')?.value || '',
        password: document.getElementById('password')?.value || ''
      };

      await csrf.fetch('/admin/login', {
        method: 'POST',
        body: JSON.stringify(formData)
      });

      window.location.href = '/admin';
    } catch (err) {
      if (errorEl) errorEl.textContent = err.message || 'Login failed';
    }
  });
});


