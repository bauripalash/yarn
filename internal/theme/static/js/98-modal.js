"use strict";

/*
 * Modal
 *
 * Pico.css - https://picocss.com
 * Copyright 2019-2021 - Licensed under MIT
 */
// Config
var isOpenClass = 'modal-is-open';
var openingClass = 'modal-is-opening';
var closingClass = 'modal-is-closing';
var animationDuration = 400; // ms

var visibleModal = null; // Toggle modal

var toggleModal = function toggleModal(event) {
  event.preventDefault();
  var modal = document.getElementById(event.target.getAttribute('data-target'));
  typeof modal != 'undefined' && modal != null && isModalOpen(modal) ? closeModal(modal) : openModal(modal);
}; // Is modal open


var isModalOpen = function isModalOpen(modal) {
  return modal.hasAttribute('open') && modal.getAttribute('open') != 'false' ? true : false;
}; // Open modal


var openModal = function openModal(modal) {
  if (isScrollbarVisible()) {
    document.documentElement.style.setProperty('--scrollbar-width', "".concat(getScrollbarWidth(), "px"));
  }

  document.documentElement.classList.add(isOpenClass, openingClass);
  setTimeout(function () {
    visibleModal = modal;
    document.documentElement.classList.remove(openingClass);
  }, animationDuration);
  modal.setAttribute('open', true);
  modal.addEventListener("click", function(e) {
    e.preventDefault();
    closeModal(modal);
  });
}; // Close modal


var closeModal = function closeModal(modal) {
  visibleModal = null;
  document.documentElement.classList.add(closingClass);
  setTimeout(function () {
    document.documentElement.classList.remove(closingClass, isOpenClass);
    document.documentElement.style.removeProperty('--scrollbar-width');
    modal.removeAttribute('open');
  }, animationDuration);
}; // Close with a click outside


document.addEventListener('click', function (event) {
  if (visibleModal != null) {
    var modalContent = visibleModal.querySelector('article');
    var isClickInside = modalContent.contains(event.target);
    !isClickInside && closeModal(visibleModal);
  }
}); // Close with Esc key

document.addEventListener('keydown', function (event) {
  if (event.key === 'Escape' && visibleModal != null) {
    closeModal(visibleModal);
  }
}); // Get scrollbar width

var getScrollbarWidth = function getScrollbarWidth() {
  // Creating invisible container
  var outer = document.createElement('div');
  outer.style.visibility = 'hidden';
  outer.style.overflow = 'scroll'; // forcing scrollbar to appear

  outer.style.msOverflowStyle = 'scrollbar'; // needed for WinJS apps

  document.body.appendChild(outer); // Creating inner element and placing it in the container

  var inner = document.createElement('div');
  outer.appendChild(inner); // Calculating difference between container's full width and the child width

  var scrollbarWidth = outer.offsetWidth - inner.offsetWidth; // Removing temporary elements from the DOM

  outer.parentNode.removeChild(outer);
  return scrollbarWidth;
}; // Is scrollbar visible


var isScrollbarVisible = function isScrollbarVisible() {
  return document.body.scrollHeight > screen.height;
};