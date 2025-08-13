import { csrf } from './csrf.js';
import { showNotification } from './ux-enhancements.js';

document.addEventListener('DOMContentLoaded', () => {
  const form = document.getElementById('setupForm');
  const error = document.getElementById('error');
  if (!form) return;

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (error) error.textContent = '';

    const formData = {
      siteTitle: document.getElementById('siteTitle')?.value || '',
      username: document.getElementById('username')?.value || '',
      password: document.getElementById('password')?.value || '',
      confirmPassword: document.getElementById('confirmPassword')?.value || ''
    };

    if (formData.password !== formData.confirmPassword) {
      if (error) error.textContent = 'Passwords do not match';
      showNotification('Passwords do not match', 'error', 8000);
      return;
    }
    if (formData.password.length < 12) {
      if (error) error.textContent = 'Password must be at least 12 characters long';
      showNotification('Password must be at least 12 characters long', 'error', 8000);
      return;
    }
    const hasUpper = /[A-Z]/.test(formData.password);
    const hasLower = /[a-z]/.test(formData.password);
    const hasDigit = /\d/.test(formData.password);
    const hasSpecial = /[!@#$%^&*(),.?":{}|<>]/.test(formData.password);
    if (!hasUpper || !hasLower || !hasDigit || !hasSpecial) {
      const msg = 'Password must include uppercase, lowercase, digit, and special character';
      if (error) error.textContent = msg;
      showNotification(msg, 'error', 8000);
      return;
    }

    try {
      const res = await csrf.fetch('/setup', { method: 'POST', body: JSON.stringify(formData) });
      if (!res.ok) throw new Error(await res.text());
      window.location.href = '/admin/login';
    } catch (err) {
      if (error) error.textContent = err.message;
      showNotification(err.message || 'Setup failed', 'error', 10000);
    }
  });
});


