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
var visibleModal = null;

// Toggle modal
var toggleModal = function toggleModal(event) {
  event.preventDefault();
  var modal = document.getElementById(event.target.getAttribute('data-target'));
  typeof modal != 'undefined' && modal != null && isModalOpen(modal) ? closeModal(modal) : openModal(modal);
};

// Is modal open
var isModalOpen = function isModalOpen(modal) {
  return modal.hasAttribute('open') && modal.getAttribute('open') != 'false' ? true : false;
};

// Open modal
var openModal = function openModal(modal) {
  document.documentElement.classList.add(isOpenClass, openingClass);
  setTimeout(function () {
    visibleModal = modal;
    document.documentElement.classList.remove(openingClass);
  }, animationDuration);
  modal.setAttribute('open', true);
};

// Close modal
var closeModal = function closeModal(modal) {
  visibleModal = null;
  document.documentElement.classList.add(closingClass);
  setTimeout(function () {
    document.documentElement.classList.remove(closingClass, isOpenClass);
    modal.removeAttribute('open');
  }, animationDuration);
};

// Close with a click outside
document.addEventListener('click', function (event) {
  if (visibleModal != null) {
    var modalContent = visibleModal.querySelector('figure');
    var isClickInside = modalContent.contains(event.target);
    !isClickInside && closeModal(visibleModal);
  }
});

// Close with Esc key
document.addEventListener('keydown', function (event) {
  if (event.key === 'Escape' && visibleModal != null) {
    closeModal(visibleModal);
  }
});
