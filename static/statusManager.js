'use strict';
(function(global) {
  var SLOTS = ['routing', 'scoring', 'roads'];

  function createStatusManager(el) {
    var state = {};
    SLOTS.forEach(function(s) { state[s] = null; });

    function render() {
      for (var i = 0; i < SLOTS.length; i++) {
        var s = state[SLOTS[i]];
        if (s !== null) {
          el.textContent = s.text;
          el.className = s.done ? 'fetch-status done' : 'fetch-status';
          return;
        }
      }
      el.textContent = 'Click map to start';
      el.className = 'fetch-status done';
    }

    render();

    return {
      set: function(slot, text, done) {
        if (SLOTS.indexOf(slot) === -1) {
          throw new Error('Unknown slot: ' + slot);
        }
        state[slot] = { text: text, done: !!done };
        render();
      },
      clear: function(slot) {
        if (SLOTS.indexOf(slot) === -1) {
          throw new Error('Unknown slot: ' + slot);
        }
        state[slot] = null;
        render();
      }
    };
  }

  global.createStatusManager = createStatusManager;
}(globalThis));
