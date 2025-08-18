/**
 * Early Theme Loader - CSP Compliant Version
 * Applies user theme immediately to prevent flash, works with strict CSP
 */

(function() {
    'use strict';
    
    // Get theme configuration from JSON script tag
    const themeDataElement = document.getElementById('theme-data');
    if (!themeDataElement) {
        return; // No theme configuration available
    }
    
    let themeConfig;
    try {
        themeConfig = JSON.parse(themeDataElement.textContent);
    } catch (error) {
        console.warn('Failed to parse theme configuration:', error);
        return;
    }
    
    const { allowedThemes, defaultTheme } = themeConfig;
    
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