/**
 * Drag and Drop Functionality
 * Provides smooth drag-and-drop reordering for filters and other list items
 */

import { csrf } from './csrf.js';

/**
 * Initialize drag and drop for a container
 * @param {HTMLElement} container - Container element with draggable items
 * @param {Object} options - Configuration options
 */
export function initializeDragDrop(container, options = {}) {
  const config = {
    itemSelector: '.draggable-item',
    handleSelector: '.drag-handle',
    onReorder: null,
    animationDuration: 300,
    ghostOpacity: 0.5,
    ...options
  };

  let draggedElement = null;
  let draggedIndex = -1;
  let placeholder = null;
  let isDragging = false;

  // Add drag handles if they don't exist
  addDragHandles(container, config);

  // Event listeners
  container.addEventListener('mousedown', handleMouseDown);
  container.addEventListener('touchstart', handleTouchStart, { passive: false });
  document.addEventListener('mousemove', handleMouseMove);
  document.addEventListener('touchmove', handleTouchMove, { passive: false });
  document.addEventListener('mouseup', handleMouseUp);
  document.addEventListener('touchend', handleTouchEnd);

  function addDragHandles(container, config) {
    const items = container.querySelectorAll(config.itemSelector);
    items.forEach(item => {
      if (!item.querySelector(config.handleSelector)) {
        const handle = document.createElement('div');
        handle.className = 'drag-handle';
        handle.innerHTML = '⋮⋮';
        handle.title = 'Drag to reorder';
        item.insertBefore(handle, item.firstChild);
      }
    });
  }

  function handleMouseDown(e) {
    handleStart(e, e.clientX, e.clientY);
  }

  function handleTouchStart(e) {
    const touch = e.touches[0];
    handleStart(e, touch.clientX, touch.clientY);
  }

  function handleStart(e, clientX, clientY) {
    const handle = e.target.closest(config.handleSelector);
    if (!handle) return;

    const item = handle.closest(config.itemSelector);
    if (!item) return;

    e.preventDefault();
    isDragging = true;
    draggedElement = item;
    draggedIndex = Array.from(container.children).indexOf(item);

    // Create placeholder
    placeholder = document.createElement('div');
    placeholder.className = 'drag-placeholder';
    placeholder.style.height = item.offsetHeight + 'px';
    placeholder.style.border = '2px dashed var(--color-accent-primary)';
    placeholder.style.borderRadius = 'var(--border-radius-md)';
    placeholder.style.margin = getComputedStyle(item).margin;
    placeholder.style.opacity = '0.5';

    // Style dragged element
    item.style.opacity = config.ghostOpacity;
    item.style.transform = 'rotate(2deg)';
    item.style.zIndex = '1000';
    item.style.pointerEvents = 'none';
    item.classList.add('dragging');

    // Insert placeholder
    item.parentNode.insertBefore(placeholder, item.nextSibling);

    // Add visual feedback
    container.classList.add('drag-active');
  }

  function handleMouseMove(e) {
    handleMove(e, e.clientX, e.clientY);
  }

  function handleTouchMove(e) {
    const touch = e.touches[0];
    handleMove(e, touch.clientX, touch.clientY);
  }

  function handleMove(e, clientX, clientY) {
    if (!isDragging || !draggedElement) return;
    e.preventDefault();

    const items = Array.from(container.children).filter(child => 
      child !== draggedElement && child !== placeholder && 
      child.matches(config.itemSelector)
    );

    let insertBefore = null;
    for (const item of items) {
      const rect = item.getBoundingClientRect();
      const itemCenter = rect.top + rect.height / 2;
      
      if (clientY < itemCenter) {
        insertBefore = item;
        break;
      }
    }

    // Move placeholder
    if (insertBefore) {
      container.insertBefore(placeholder, insertBefore);
    } else {
      container.appendChild(placeholder);
    }
  }

  function handleMouseUp() {
    handleEnd();
  }

  function handleTouchEnd() {
    handleEnd();
  }

  function handleEnd() {
    if (!isDragging || !draggedElement) return;

    const newIndex = Array.from(container.children).indexOf(placeholder);
    
    // Animate to new position
    draggedElement.style.transform = '';
    draggedElement.style.opacity = '';
    draggedElement.style.zIndex = '';
    draggedElement.style.pointerEvents = '';
    draggedElement.classList.remove('dragging');

    // Replace placeholder with dragged element
    if (placeholder.parentNode) {
      placeholder.parentNode.replaceChild(draggedElement, placeholder);
    }

    // Clean up
    container.classList.remove('drag-active');
    
    // Trigger reorder callback if positions changed
    if (draggedIndex !== newIndex && config.onReorder) {
      config.onReorder(draggedElement, draggedIndex, newIndex);
    }

    // Reset state
    isDragging = false;
    draggedElement = null;
    draggedIndex = -1;
    placeholder = null;
  }
}

/**
 * Create a smooth reorder animation
 * @param {HTMLElement} element - Element to animate
 */
export function animateReorder(element) {
  element.style.transform = 'scale(1.02)';
  element.style.transition = 'transform 0.2s ease';
  
  setTimeout(() => {
    element.style.transform = '';
    setTimeout(() => {
      element.style.transition = '';
    }, 200);
  }, 100);
}

/**
 * Update filter order via API
 * @param {string} groupId - Filter group ID
 * @param {Array} filterIds - Array of filter IDs in new order
 */
export async function updateFilterOrder(groupId, filterIds) {
  try {
    // Get current rules to preserve operators
    const res = await csrf.fetch(`/admin/filter-groups/${groupId}/rules`, {
      headers: { 'Accept': 'application/json' }
    });
    const data = await res.json();
    const existingRules = Array.isArray(data.data) ? data.data : [];

    const operatorByFilterId = {};
    existingRules.forEach((r, idx) => {
      const id = r.filter_id || (r.filter && r.filter.id);
      operatorByFilterId[id] = r.operator || (idx === 0 ? 'AND' : 'AND');
    });

    const rules = filterIds.map((id, index) => ({
      filter_id: id,
      operator: index === 0 ? 'AND' : (operatorByFilterId[id] || 'AND'),
      position: index,
    }));

    // PUT updated ordered rules back to the server
    const putRes = await csrf.fetch(`/admin/filter-groups/${groupId}/rules`, {
      method: 'PUT',
      body: JSON.stringify({ rules })
    });
    if (!putRes.ok) throw new Error(`Failed to update order: ${putRes.status}`);
    return await putRes.json();
  } catch (error) {
    console.error('Error updating filter order:', error);
    throw error;
  }
}