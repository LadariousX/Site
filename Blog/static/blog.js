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

    function stopAllVideos() {
      // Stop ALL videos in this carousel
      const allVideos = carousel.querySelectorAll('video');
      allVideos.forEach(video => {
        video.pause();
        video.currentTime = 0;
      });
    }

    function updateCarousel() {
      // Stop all videos first
      stopAllVideos();

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

    // Open lightbox on click (not on buttons)
    carousel.addEventListener('click', (e) => {
      // Don't trigger lightbox if clicking on buttons
      if (e.target.closest('.carousel-prev') || e.target.closest('.carousel-next')) {
        return;
      }

      // Open carousel in lightbox
      openCarouselLightbox(slides, currentIndex);
    });
  });
}

// ============================================
// Carousel Lightbox (Post Page)
// ============================================

function openCarouselLightbox(slides, startIndex) {
  // Create lightbox if it doesn't exist
  let lightbox = document.getElementById('carousel-lightbox');
  if (!lightbox) {
    lightbox = document.createElement('div');
    lightbox.id = 'carousel-lightbox';
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
  }

  const lightboxMedia = lightbox.querySelector('.lightbox-media');
  const lightboxCounter = lightbox.querySelector('.lightbox-counter');
  const closeBtn = lightbox.querySelector('.lightbox-close');
  const prevBtn = lightbox.querySelector('.lightbox-prev');
  const nextBtn = lightbox.querySelector('.lightbox-next');

  let currentIndex = startIndex;

  function isVideo(element) {
    return element.querySelector('video') !== null;
  }

  function stopAllVideos() {
    const allVideos = document.querySelectorAll('video');
    allVideos.forEach(video => {
      video.pause();
      video.currentTime = 0;
    });
  }

  function updateLightbox() {
    stopAllVideos();
    lightboxMedia.innerHTML = '';

    const slide = slides[currentIndex];
    const video = slide.querySelector('video');
    const img = slide.querySelector('img');

    if (video) {
      const videoClone = video.cloneNode(true);
      videoClone.className = 'lightbox-video';
      videoClone.setAttribute('autoplay', '');
      lightboxMedia.appendChild(videoClone);
    } else if (img) {
      const imgClone = img.cloneNode(true);
      imgClone.className = 'lightbox-img';
      lightboxMedia.appendChild(imgClone);
    }

    lightboxCounter.textContent = `${currentIndex + 1} / ${slides.length}`;
  }

  function closeLightbox() {
    stopAllVideos();
    lightboxMedia.innerHTML = '';
    lightbox.hidden = true;
    document.body.style.overflow = '';
  }

  function next() {
    currentIndex = (currentIndex + 1) % slides.length;
    updateLightbox();
  }

  function prev() {
    currentIndex = (currentIndex - 1 + slides.length) % slides.length;
    updateLightbox();
  }

  // Event listeners
  closeBtn.onclick = closeLightbox;
  prevBtn.onclick = prev;
  nextBtn.onclick = next;

  lightbox.onclick = (e) => {
    if (e.target === lightbox) closeLightbox();
  };

  // Keyboard navigation
  const keyHandler = (e) => {
    if (lightbox.hidden) return;
    if (e.key === 'Escape') closeLightbox();
    if (e.key === 'ArrowLeft') prev();
    if (e.key === 'ArrowRight') next();
  };
  document.removeEventListener('keydown', keyHandler);
  document.addEventListener('keydown', keyHandler);

  // Open the lightbox
  updateLightbox();
  lightbox.hidden = false;
  document.body.style.overflow = 'hidden';
}

// ============================================
// Lightbox (Gallery Page)
// ============================================

function initLightbox() {
  const galleryGrid = document.querySelector('.gallery-grid');
  if (!galleryGrid) return;

  // Force video thumbnails to load first frame
  const galleryVideos = document.querySelectorAll('.gallery-thumb video');
  galleryVideos.forEach(video => {
    // Wait for metadata to load before seeking
    video.addEventListener('loadedmetadata', function() {
      this.currentTime = 0.1;
    });
    video.load();
  });

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
    // Stop all videos before opening
    stopAllVideos();

    currentIndex = index;
    updateLightbox();
    lightbox.hidden = false;
    document.body.style.overflow = 'hidden';
  }

  function stopAllVideos() {
    // Stop ALL videos on the entire page
    const allVideos = document.querySelectorAll('video');
    allVideos.forEach(video => {
      video.pause();
      video.currentTime = 0;
    });
  }

  function closeLightbox() {
    const lightboxVideos = lightboxMedia.querySelectorAll('video');
    lightboxVideos.forEach(video => {
      video.pause();
      video.src = '';
      video.load();
    });

    lightboxMedia.innerHTML = '';

    lightbox.hidden = true;
    document.body.style.overflow = '';
  }

  function updateLightbox() {
    const existingVideos = lightboxMedia.querySelectorAll('video');
    existingVideos.forEach(video => {
      video.pause();
      video.src = '';
      video.load();
    });

    // Clear and rebuild media container
    lightboxMedia.innerHTML = '';

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
