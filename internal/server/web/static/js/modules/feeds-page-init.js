import FeedsManager from '/static/js/modules/feeds.js';

// Initialize the feeds manager
window.feedsManager = new FeedsManager();

// Use event delegation for edit buttons and modal actions
document.addEventListener('click', function(e) {
  const editBtn = e.target.closest('.edit-button');
  if (editBtn) {
    const feedId = editBtn.getAttribute('data-feed-id');
    if (feedId && window.showEditModal) {
      window.showEditModal(feedId);
    }
  }

  if (e.target && e.target.id === 'cancelDelete') {
    if (window.hideDeleteModal) {
      window.hideDeleteModal();
    }
  }
});



