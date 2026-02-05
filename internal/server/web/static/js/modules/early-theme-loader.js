/**
 * Early Theme Loader - CSP Compliant Version
 * Applies user theme immediately to prevent flash, works with strict CSP
 */

(function() {
    'use strict';
    
    // Get theme configuration from meta tags (CSP-safe)
    const allowedMeta = document.querySelector('meta[name="theme-allowed"]');
    const defaultMeta = document.querySelector('meta[name="theme-default"]');
    if (!allowedMeta || !defaultMeta) {
        return; // No theme configuration available
    }

    const allowedThemes = (allowedMeta.content || '')
        .split(',')
        .map((t) => t.trim())
        .filter(Boolean);
    const defaultTheme = (defaultMeta.content || '').trim();
    if (allowedThemes.length === 0 || !defaultTheme) {
        return;
    }
    
    // Get user's stored theme preference
    let userTheme = null;
    try {
        const storedTheme = localStorage.getItem('userSelectedTheme');
        if (storedTheme && allowedThemes.includes(storedTheme) && /^[a-zA-Z0-9_-]+$/.test(storedTheme)) {
            userTheme = storedTheme;
        } else if (storedTheme) {
            // Invalid theme, clean up
            localStorage.removeItem('userSelectedTheme');
        }
    } catch (error) {
        // localStorage not available
    }
    
    // If user has a valid theme preference and it's different from current, switch immediately
    if (userTheme && userTheme !== defaultTheme) {
        const links = {
            variables: document.getElementById('themeVariablesCSS'),
            public: document.getElementById('themePublicCSS'),
            ux: document.getElementById('themeUxCSS')
        };
        
        // Update CSS links to user's theme
        const themePrefix = `/static/css/themes/${encodeURIComponent(userTheme)}/`;
        
        if (links.variables) {
            links.variables.href = themePrefix + 'variables.css';
        }
        if (links.public) {
            links.public.href = themePrefix + 'public.css';
        }
        if (links.ux) {
            links.ux.href = themePrefix + 'ux-enhancements.css';
        }
    }
})();
