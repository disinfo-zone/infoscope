/**
 * Early Theme Loader - CSP Compliant Version
 * Resolves a single public theme before first paint to prevent flash
 */

(function() {
    'use strict';

    // Get theme configuration from meta tags (CSP-safe)
    const allowedMeta = document.querySelector('meta[name="theme-allowed"]');
    const defaultMeta = document.querySelector('meta[name="theme-default"]');
    if (!allowedMeta || !defaultMeta) {
        return; // No theme configuration available
    }

    const themeNamePattern = /^[a-zA-Z0-9_-]+$/;
    const allowedThemes = (allowedMeta.content || '')
        .split(',')
        .map((t) => t.trim())
        .filter((t) => t && themeNamePattern.test(t));
    if (allowedThemes.length === 0) {
        return;
    }

    let defaultTheme = (defaultMeta.content || '').trim();
    if (!themeNamePattern.test(defaultTheme) || !allowedThemes.includes(defaultTheme)) {
        defaultTheme = allowedThemes[0];
    }

    let resolvedTheme = defaultTheme;

    // Get user's stored theme preference.
    try {
        const storedTheme = localStorage.getItem('userSelectedTheme');
        if (storedTheme && allowedThemes.includes(storedTheme) && themeNamePattern.test(storedTheme)) {
            resolvedTheme = storedTheme;
        } else if (storedTheme) {
            // Invalid theme, clean up.
            localStorage.removeItem('userSelectedTheme');
        }
    } catch (error) {
        // localStorage not available.
    }

    const themePrefix = `/static/css/themes/${encodeURIComponent(resolvedTheme)}/`;

    function upsertThemeLink(id, fileName) {
        const href = themePrefix + fileName;
        let link = document.getElementById(id);

        if (!link) {
            link = document.createElement('link');
            link.rel = 'stylesheet';
            link.id = id;
            link.href = href;

            const baseCSS = document.getElementById('baseCSS');
            if (baseCSS && baseCSS.parentNode) {
                baseCSS.parentNode.insertBefore(link, baseCSS);
            } else if (document.head) {
                document.head.appendChild(link);
            }
            return;
        }

        if (link.getAttribute('href') !== href) {
            link.setAttribute('href', href);
        }
    }

    // Ensure exactly one resolved theme stylesheet set is active.
    upsertThemeLink('themeVariablesCSS', 'variables.css');
    upsertThemeLink('themePublicCSS', 'public.css');
    upsertThemeLink('themeUxCSS', 'ux-enhancements.css');

    if (document.documentElement) {
        document.documentElement.setAttribute('data-active-public-theme', resolvedTheme);
    }
})();
