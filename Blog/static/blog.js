// ============================================
// Carousel
// ============================================

function initCarousels() {
  const carousels = document.querySelectorAll('.carousel[data-index]');

  carousels.forEach(carousel => {
    const track = carousel.querySelector('.carousel-track');
    const slides = carousel.querySelectorAll('.carousel-slide');
    const prevBtn = carousel.querySelector('.carousel-prev');
    const nextBtn = carousel.querySelector('.carousel-next');
    const counter = carousel.querySelector('.carousel-counter');

    if (!track || slides.length === 0) return;

    let currentIndex = 0;

    function updateCarousel() {
      track.style.transform = `translateX(-${currentIndex * 100}%)`;
      carousel.dataset.index = currentIndex;

      // Update counter
      if (counter) {
        counter.textContent = `${currentIndex + 1} / ${slides.length}`;
      }
    }

    function next() {
      currentIndex = (currentIndex + 1) % slides.length;
      updateCarousel();
    }

    function prev() {
      currentIndex = (currentIndex - 1 + slides.length) % slides.length;
      updateCarousel();
    }

    // Button navigation
    if (prevBtn) prevBtn.addEventListener('click', prev);
    if (nextBtn) nextBtn.addEventListener('click', next);

    // Keyboard navigation
    carousel.addEventListener('keydown', (e) => {
      if (e.key === 'ArrowLeft') prev();
      if (e.key === 'ArrowRight') next();
    });

    // Touch/swipe support
    let touchStartX = 0;
    let touchEndX = 0;

    carousel.addEventListener('touchstart', (e) => {
      touchStartX = e.touches[0].clientX;
    });

    carousel.addEventListener('touchend', (e) => {
      touchEndX = e.changedTouches[0].clientX;
      const delta = touchStartX - touchEndX;

      if (Math.abs(delta) > 40) {
        if (delta > 0) next();
        else prev();
      }
    });
  });
}

// ============================================
// Lightbox (Gallery Page)
// ============================================

function initLightbox() {
  const galleryGrid = document.querySelector('.gallery-grid');
  if (!galleryGrid) return;

  // Create lightbox markup
  const lightbox = document.createElement('div');
  lightbox.id = 'lightbox';
  lightbox.className = 'lightbox';
  lightbox.hidden = true;
  lightbox.innerHTML = `
    <button class="lightbox-close">✕</button>
    <button class="lightbox-prev">‹</button>
    <div class="lightbox-media"></div>
    <div class="lightbox-counter"></div>
    <button class="lightbox-next">›</button>
  `;
  document.body.appendChild(lightbox);

  const lightboxMedia = lightbox.querySelector('.lightbox-media');
  const lightboxCounter = lightbox.querySelector('.lightbox-counter');
  const closeBtn = lightbox.querySelector('.lightbox-close');
  const prevBtn = lightbox.querySelector('.lightbox-prev');
  const nextBtn = lightbox.querySelector('.lightbox-next');

  const thumbs = Array.from(document.querySelectorAll('.gallery-thumb'));
  let currentIndex = 0;

  function isVideo(src) {
    const videoExts = ['.mov', '.mp4', '.webm', '.MOV', '.MP4'];
    return videoExts.some(ext => src.endsWith(ext));
  }

  function openLightbox(index) {
    currentIndex = index;
    updateLightbox();
    lightbox.hidden = false;
    document.body.style.overflow = 'hidden';
  }

  function closeLightbox() {
    lightbox.hidden = true;
    document.body.style.overflow = '';
  }

  function updateLightbox() {
    // Pause any playing videos before switching
    const existingVideo = lightboxMedia.querySelector('video');
    if (existingVideo) {
      existingVideo.pause();
    }

    const thumb = thumbs[currentIndex];
    const src = thumb.dataset.src;

    if (isVideo(src)) {
      lightboxMedia.innerHTML = `<video class="lightbox-video" src="${src}" controls autoplay></video>`;
    } else {
      lightboxMedia.innerHTML = `<img class="lightbox-img" src="${src}" alt="">`;
    }

    lightboxCounter.textContent = `${currentIndex + 1} / ${thumbs.length}`;
  }

  function next() {
    currentIndex = (currentIndex + 1) % thumbs.length;
    updateLightbox();
  }

  function prev() {
    currentIndex = (currentIndex - 1 + thumbs.length) % thumbs.length;
    updateLightbox();
  }

  // Thumb click handlers
  thumbs.forEach((thumb, i) => {
    thumb.addEventListener('click', () => openLightbox(i));
    thumb.dataset.index = i;
  });

  // Navigation
  closeBtn.addEventListener('click', closeLightbox);
  prevBtn.addEventListener('click', prev);
  nextBtn.addEventListener('click', next);

  // Click backdrop to close
  lightbox.addEventListener('click', (e) => {
    if (e.target === lightbox) closeLightbox();
  });

  // Keyboard navigation
  document.addEventListener('keydown', (e) => {
    if (lightbox.hidden) return;

    if (e.key === 'Escape') closeLightbox();
    if (e.key === 'ArrowLeft') prev();
    if (e.key === 'ArrowRight') next();
  });

  // Touch/swipe support
  let touchStartX = 0;
  let touchEndX = 0;

  lightbox.addEventListener('touchstart', (e) => {
    touchStartX = e.touches[0].clientX;
  });

  lightbox.addEventListener('touchend', (e) => {
    touchEndX = e.changedTouches[0].clientX;
    const delta = touchStartX - touchEndX;

    if (Math.abs(delta) > 40) {
      if (delta > 0) next();
      else prev();
    }
  });
}

// ============================================
// Initialize on DOM ready
// ============================================

document.addEventListener('DOMContentLoaded', () => {
  initCarousels();
  initLightbox();
});
