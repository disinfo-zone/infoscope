/**
 * Public Menu Module
 * Powers the public homepage hamburger menu (issues #80, #81):
 *   - live theme switching from the Theme <select>
 *   - clean URLs for the category/tag filter form
 *   - close the menu on outside-click / Escape
 *
 * Light/dark toggling is handled by theme.js, which adopts the .theme-toggle
 * button rendered inside the menu. This script is intentionally a plain
 * (non-module) IIFE so it works under the strict CSP without inline code.
 */
(function () {
    'use strict';

    const themeNamePattern = /^[a-zA-Z0-9_-]+$/;

    function applyTheme(theme) {
        const prefix = '/static/css/themes/' + encodeURIComponent(theme) + '/';
        const links = {
            themeVariablesCSS: 'variables.css',
            themePublicCSS: 'public.css',
            themeUxCSS: 'ux-enhancements.css'
        };
        Object.keys(links).forEach(function (id) {
            const link = document.getElementById(id);
            if (link) {
                link.setAttribute('href', prefix + links[id]);
            }
        });
        if (document.documentElement) {
            document.documentElement.setAttribute('data-active-public-theme', theme);
        }
    }

    function initThemeSelect() {
        const themeSelect = document.getElementById('themeSelect');
        if (!themeSelect) return; // Public theme selection disabled

        const allowed = (themeSelect.getAttribute('data-available-themes') || '')
            .split(',')
            .map(function (t) { return t.trim(); })
            .filter(function (t) { return t && themeNamePattern.test(t); });
        if (allowed.length === 0) return;

        const defaultTheme = (themeSelect.getAttribute('data-default-theme') || '').trim();

        // Resolve the current theme (a valid stored preference wins).
        let current = null;
        try {
            current = localStorage.getItem('userSelectedTheme');
        } catch (err) {
            current = null;
        }
        if (!current || allowed.indexOf(current) === -1 || !themeNamePattern.test(current)) {
            current = (defaultTheme && allowed.indexOf(defaultTheme) !== -1) ? defaultTheme : allowed[0];
        }
        themeSelect.value = current;

        themeSelect.addEventListener('change', function () {
            const selected = themeSelect.value;
            if (!selected || allowed.indexOf(selected) === -1 || !themeNamePattern.test(selected)) {
                return;
            }
            try {
                localStorage.setItem('userSelectedTheme', selected);
            } catch (err) {
                // Selection still applies for this session even if it can't persist.
            }
            applyTheme(selected);
        });
    }

    function initFilterForm() {
        const form = document.getElementById('feedFilterForm');
        if (!form) return;

        // Build a clean query string (omit empty selections) instead of submitting
        // empty params like /?category=&tag=. Falls back to native GET if JS fails.
        form.addEventListener('submit', function (e) {
            e.preventDefault();
            const params = new URLSearchParams();
            const category = form.querySelector('[name="category"]');
            const tag = form.querySelector('[name="tag"]');
            if (category && category.value) params.set('category', category.value);
            if (tag && tag.value) params.set('tag', tag.value);
            const qs = params.toString();
            window.location.assign(qs ? '/?' + qs : '/');
        });
    }

    function initMenuDismiss() {
        const menu = document.getElementById('appMenu');
        if (!menu) return;

        // Native <details> stays open on outside clicks; close it for menu-like UX.
        document.addEventListener('click', function (e) {
            if (menu.open && !menu.contains(e.target)) {
                menu.open = false;
            }
        });

        document.addEventListener('keydown', function (e) {
            if (e.key === 'Escape' && menu.open) {
                menu.open = false;
                const toggle = menu.querySelector('.app-menu-toggle');
                if (toggle && typeof toggle.focus === 'function') {
                    toggle.focus();
                }
            }
        });
    }

    function init() {
        initThemeSelect();
        initFilterForm();
        initMenuDismiss();
    }

    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', init);
    } else {
        init();
    }
})();
