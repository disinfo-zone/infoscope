/**
 * Public Theme Selection Module
 * Handles the theme selection modal for public users
 */

// Theme selection functionality - use named function to prevent duplicates
let isInitialized = false;

function initializeThemeSelection() {
    if (isInitialized) {
        return; // Prevent double initialization
    }
    isInitialized = true;
    const settingsButton = document.getElementById('settingsButton');
    const settingsModal = document.getElementById('settingsModal');
    const closeButton = document.getElementById('closeSettingsModal');
    const applyButton = document.getElementById('applySettings');
    
    // Debug logging
    console.log('Theme selection initialization started');
    console.log('Settings button found:', !!settingsButton);
    console.log('Settings modal found:', !!settingsModal);
    console.log('Close button found:', !!closeButton);
    console.log('Apply button found:', !!applyButton);
    
    // Safety check: Ensure all required elements exist
    if (!settingsButton || !settingsModal || !closeButton || !applyButton) {
        console.error('Required theme selection elements not found');
        console.error('Missing elements:', {
            settingsButton: !settingsButton,
            settingsModal: !settingsModal,
            closeButton: !closeButton,
            applyButton: !applyButton
        });
        return;
    }
    
    // Get available themes from data attribute on settings button
    const themesString = settingsButton.getAttribute('data-available-themes') || '';
    const allowedThemes = themesString ? themesString.split(',') : [];
    console.log('Available themes:', allowedThemes);
    let currentTheme;
    try {
        currentTheme = localStorage.getItem('userSelectedTheme');
    } catch (error) {
        console.warn('localStorage access blocked:', error);
        currentTheme = null; // Continue with null currentTheme
    }
    
    // Edge case: If no themes are available, disable functionality
    if (allowedThemes.length === 0) {
        console.error('No themes available for selection');
        return;
    }
    console.log('Themes available, continuing initialization...');
    
    // Security: Validate and sanitize theme selection
    if (!currentTheme || !allowedThemes.includes(currentTheme) || !/^[a-zA-Z0-9_-]+$/.test(currentTheme)) {
        currentTheme = allowedThemes.length > 0 ? allowedThemes[0] : null;
        try {
            if (localStorage.getItem('userSelectedTheme')) {
                localStorage.removeItem('userSelectedTheme'); // Remove invalid theme
            }
        } catch (error) {
            // Ignore localStorage errors during cleanup
        }
    }
    
    // Additional safety check - if no valid theme available, exit
    if (!currentTheme) {
        console.error('Unable to determine a valid current theme');
        return;
    }
    
    // Apply stored theme immediately on page load
    if (currentTheme) {
        const themePrefix = `/static/css/themes/${CSS.escape(currentTheme)}/`;
        const publicCSSLink = document.getElementById('themePublicCSS');
        const uxCSSLink = document.getElementById('themeUxCSS');
        
        if (publicCSSLink) {
            publicCSSLink.href = themePrefix + 'public.css';
        }
        if (uxCSSLink) {
            uxCSSLink.href = themePrefix + 'ux-enhancements.css';
        }
        
        // Load variables CSS too
        const existingVariablesLink = document.querySelector('link[href*="/variables.css"]');
        if (existingVariablesLink) {
            existingVariablesLink.href = themePrefix + 'variables.css';
        }
    }
    
    // Use requestAnimationFrame to ensure DOM is ready
    requestAnimationFrame(() => {
        const themeSelect = document.getElementById('themeSelect');
        if (themeSelect && currentTheme) {
            themeSelect.value = currentTheme;
        }
    });
    
    // Show modal
    settingsButton.addEventListener('click', function() {
        console.log('Settings button clicked!');
        settingsModal.style.display = 'flex';
        settingsModal.setAttribute('aria-hidden', 'false');
        document.body.style.overflow = 'hidden';
        
        // Focus management for accessibility
        const themeSelect = settingsModal.querySelector('#themeSelect');
        if (themeSelect) {
            themeSelect.focus();
        }
    });
    
    // Hide modal
    function hideModal() {
        settingsModal.style.display = 'none';
        settingsModal.setAttribute('aria-hidden', 'true');
        document.body.style.overflow = '';
        
        // Return focus to the settings button for accessibility
        settingsButton.focus();
    }
    
    closeButton.addEventListener('click', hideModal);
    
    // Close modal when clicking background
    settingsModal.addEventListener('click', function(e) {
        if (e.target === settingsModal) {
            hideModal();
        }
    });
    
    // Handle keyboard navigation and focus trap
    document.addEventListener('keydown', function(e) {
        if (settingsModal.style.display === 'flex') {
            if (e.key === 'Escape') {
                hideModal();
                return;
            }
            
            // Focus trap: keep focus within modal
            if (e.key === 'Tab') {
                const focusableElements = settingsModal.querySelectorAll(
                    'select:not([disabled]), button:not([disabled])'
                );
                const firstFocusable = focusableElements[0];
                const lastFocusable = focusableElements[focusableElements.length - 1];
                
                if (e.shiftKey) {
                    // Shift + Tab
                    if (document.activeElement === firstFocusable) {
                        e.preventDefault();
                        lastFocusable.focus();
                    }
                } else {
                    // Tab
                    if (document.activeElement === lastFocusable) {
                        e.preventDefault();
                        firstFocusable.focus();
                    }
                }
            }
        }
    });
    
    // Apply theme when user selects one
    applyButton.addEventListener('click', function() {
        const themeSelect = document.getElementById('themeSelect');
        const selectedTheme = themeSelect?.value;
        if (selectedTheme && allowedThemes.includes(selectedTheme) && /^[a-zA-Z0-9_-]+$/.test(selectedTheme)) {
            // Security: Only allow pre-validated themes
            const sanitizedTheme = selectedTheme;
            
            // Store selection
            try {
                localStorage.setItem('userSelectedTheme', sanitizedTheme);
            } catch (error) {
                console.warn('localStorage access blocked, theme will not persist:', error);
            }
            
            // Apply theme CSS changes
            const themePrefix = `/static/css/themes/${CSS.escape(sanitizedTheme)}/`;
            const publicCSSLink = document.getElementById('themePublicCSS');
            const uxCSSLink = document.getElementById('themeUxCSS');
            
            if (publicCSSLink) {
                publicCSSLink.href = themePrefix + 'public.css';
            }
            if (uxCSSLink) {
                uxCSSLink.href = themePrefix + 'ux-enhancements.css';
            }
            
            // Load variables CSS too
            const existingVariablesLink = document.querySelector('link[href*="/variables.css"]');
            if (existingVariablesLink) {
                existingVariablesLink.href = themePrefix + 'variables.css';
            }
            
            hideModal();
        }
    });
}

// Handle browser back/forward button
window.addEventListener('pageshow', function(event) {
    // Re-initialize on page show to handle browser back/forward
    if (event.persisted || (window.performance && window.performance.navigation.type === 2)) {
        initializeThemeSelection();
    }
});

// Apply theme immediately if available, even before full DOM ready
function applyStoredThemeEarly() {
    try {
        const storedTheme = localStorage.getItem('userSelectedTheme');
        if (storedTheme && /^[a-zA-Z0-9_-]+$/.test(storedTheme)) {
            const themePrefix = `/static/css/themes/${CSS.escape(storedTheme)}/`;
            const publicCSSLink = document.getElementById('themePublicCSS');
            const uxCSSLink = document.getElementById('themeUxCSS');
            const variablesLink = document.querySelector('link[href*="/variables.css"]');
            
            if (publicCSSLink) {
                publicCSSLink.href = themePrefix + 'public.css';
            }
            if (uxCSSLink) {
                uxCSSLink.href = themePrefix + 'ux-enhancements.css';
            }
            if (variablesLink) {
                variablesLink.href = themePrefix + 'variables.css';
            }
        }
    } catch (error) {
        // Silently handle localStorage errors
    }
}

// Apply theme as early as possible
applyStoredThemeEarly();

// Initialize only once when DOM is ready
if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', initializeThemeSelection);
} else {
    initializeThemeSelection();
}