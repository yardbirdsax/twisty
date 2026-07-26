var map = L.map('map').setView([window.TWISTY_CONFIG.lat, window.TWISTY_CONFIG.lon], 13);
L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
    maxZoom: 19,
    attribution: '&copy; OpenStreetMap contributors'
}).addTo(map);
map.createPane('routePane');
map.getPane('routePane').style.zIndex = 450;

var waypoints = [];
var markers = [];
var legPolylines = [];
var legs = [];
var scorePollTimer = null;
var scorePerKmMax = window.TWISTY_CONFIG.scorePerKmMax;
var statusManager = createStatusManager(document.getElementById('fetch-status'));

var labelCache = {};
var activeSlotIndex = -1;
var insertSlotIndex = -1;
var insertPlaceholderRow = null;
var waypointsPanelOpen = false;
var reverseGeocodeGeneration = 0;
var dragState = null; // { type: 'reorder'|'insert', fromIndex: number|null }

var CIRCLED_DIGITS = ['①','②','③','④','⑤','⑥','⑦','⑧','⑨','⑩'];
function waypointBadge(i) {
  return i < CIRCLED_DIGITS.length ? CIRCLED_DIGITS[i] : '#' + (i + 1);
}

function labelKey(wp) {
  return wp[0].toFixed(6) + ',' + wp[1].toFixed(6);
}

function latLonFallback(wp) {
  return wp[0].toFixed(4) + ', ' + wp[1].toFixed(4);
}

function showWpError(msg) {
  var el = document.getElementById('wp-error');
  el.textContent = msg;
  el.style.display = msg ? 'block' : 'none';
}

function toggleWaypointsPanel() {
  waypointsPanelOpen = !waypointsPanelOpen;
  var list = document.getElementById('waypoints-list');
  var toggle = document.getElementById('waypoints-toggle');
  list.className = waypointsPanelOpen ? 'open' : '';
  toggle.textContent = waypointsPanelOpen ? '▼' : '▶';
  if (waypointsPanelOpen) {
    reverseGeocodeUnlabeled();
  }
}

function initWaypointDnD() {
  var list = document.getElementById('waypoints-list');

  // Wire add-row handle so dragging it sets insert mode.
  var addHandle = document.getElementById('waypoints-add-handle');
  if (addHandle) {
    var addRow = document.getElementById('waypoints-add');
    addHandle.addEventListener('mousedown', function(e) {
      e.stopPropagation();
      addRow.draggable = true;
      dragState = { type: 'insert', fromIndex: null };
    });
    addHandle.addEventListener('mouseup', function() {
      addRow.draggable = false;
      dragState = null;
    });
  }

  list.addEventListener('dragstart', function(e) {
    var row = e.target.closest('.wp-row');
    if (!row) return;
    var idx = parseInt(row.dataset.index, 10);
    dragState = { type: 'reorder', fromIndex: idx };
    row.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
  });

  list.addEventListener('dragend', function(e) {
    var row = e.target.closest('.wp-row');
    if (row) { row.classList.remove('dragging'); row.draggable = false; }
    var addRow = document.getElementById('waypoints-add');
    if (addRow) addRow.draggable = false;
    clearDropIndicator(list);
    dragState = null;
  });

  list.addEventListener('dragover', function(e) {
    if (!dragState) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
    clearDropIndicator(list);
    var toIndex = getDropIndex(list, e.clientY);
    var addRow = document.getElementById('waypoints-add');
    var rows = list.querySelectorAll('.wp-row');
    var indicator = document.createElement('div');
    indicator.className = 'wp-drop-indicator';
    if (toIndex < rows.length) {
      list.insertBefore(indicator, rows[toIndex]);
    } else {
      list.insertBefore(indicator, addRow);
    }
  });

  list.addEventListener('drop', function(e) {
    e.preventDefault();
    if (!dragState) return;
    var type = dragState.type;
    var fromIndex = dragState.fromIndex;
    var toIndex = getDropIndex(list, e.clientY);
    clearDropIndicator(list);
    dragState = null;

    var addRow = document.getElementById('waypoints-add');
    if (addRow) addRow.draggable = false;

    if (type === 'reorder') {
      if (toIndex === fromIndex || toIndex === fromIndex + 1) return;
      var wp = waypoints.splice(fromIndex, 1)[0];
      var insertAt = toIndex > fromIndex ? toIndex - 1 : toIndex;
      waypoints.splice(insertAt, 0, wp);
      legPolylines.forEach(function(p) { map.removeLayer(p); });
      legPolylines = [];
      legs = [];
      refreshMarkers();
      renderWaypointList();
      rerouteAll();
      saveState();
    } else if (type === 'insert') {
      startInsertWaypoint(toIndex);
    }
  });
}

initWaypointDnD();

function renderWaypointList() {
  document.getElementById('waypoints-count').textContent = waypoints.length;
  var list = document.getElementById('waypoints-list');

  var existing = list.querySelectorAll('.wp-row');
  existing.forEach(function(el) { el.parentNode.removeChild(el); });

  var addRow = document.getElementById('waypoints-add');

  waypoints.forEach(function(wp, i) {
    var key = labelKey(wp);
    var label = labelCache[key] || null;
    var row = document.createElement('div');
    row.className = 'wp-row' + (i === activeSlotIndex ? ' active-slot' : '');
    row.dataset.index = i;

    var handle = document.createElement('span');
    handle.className = 'wp-handle';
    handle.textContent = '⠿';
    handle.title = 'Drag to reorder';
    (function(idx) {
      handle.addEventListener('mousedown', function() { row.draggable = true; });
      handle.addEventListener('mouseup', function() { row.draggable = false; });
    })(i);

    var badge = document.createElement('span');
    badge.className = 'wp-badge';
    badge.textContent = waypointBadge(i);
    badge.title = 'Click to set as active slot';
    badge.onclick = function(e) {
      e.stopPropagation();
      setActiveSlot(i);
    };

    var labelEl = document.createElement('span');
    labelEl.className = 'wp-label' + (label ? '' : ' fallback');
    labelEl.textContent = label || latLonFallback(wp);

    labelEl.onclick = function(e) {
      e.stopPropagation();
      startEditWaypoint(row, i, labelEl);
    };

    var deleteBtn = document.createElement('span');
    deleteBtn.className = 'wp-delete';
    deleteBtn.textContent = '×';
    deleteBtn.title = 'Remove waypoint';
    (function(idx) {
      deleteBtn.onclick = function(e) {
        e.stopPropagation();
        removeWaypoint(idx);
      };
    })(i);

    row.appendChild(handle);
    row.appendChild(badge);
    row.appendChild(labelEl);
    row.appendChild(deleteBtn);
    list.insertBefore(row, addRow);
  });
}

function removeWaypoint(i) {
  waypoints.splice(i, 1);
  activeSlotIndex = -1;
  legPolylines.forEach(function(p) { map.removeLayer(p); });
  legPolylines = [];
  legs = [];
  refreshMarkers();
  renderWaypointList();
  if (waypoints.length === 0) {
    localStorage.removeItem('twisty-build-state');
    updateStats();
    requestScore();
  } else {
    rerouteAll();
    saveState();
  }
}

function getDropIndex(list, clientY) {
  var rows = list.querySelectorAll('.wp-row');
  for (var i = 0; i < rows.length; i++) {
    var rect = rows[i].getBoundingClientRect();
    if (clientY < rect.top + rect.height / 2) return i;
  }
  return rows.length;
}

function clearDropIndicator(list) {
  var existing = list.querySelector('.wp-drop-indicator');
  if (existing) existing.parentNode.removeChild(existing);
}

function setActiveSlot(i) {
  activeSlotIndex = (activeSlotIndex === i) ? -1 : i;
  renderWaypointList();
}

function startEditWaypoint(row, index, labelEl) {
  if (row.querySelector('.wp-input')) return;
  labelEl.style.display = 'none';

  var input = document.createElement('input');
  input.className = 'wp-input';
  input.type = 'text';
  input.placeholder = 'Enter address...';
  row.appendChild(input);
  input.focus();

  input.onkeydown = function(e) {
    if (e.key === 'Enter') {
      var q = input.value.trim();
      if (!q) return;
      commitWaypointEdit(index, q, row, input, labelEl);
    } else if (e.key === 'Escape') {
      row.removeChild(input);
      labelEl.style.display = '';
      showWpError('');
    }
  };
}

function commitWaypointEdit(index, query, row, input, labelEl) {
  input.disabled = true;
  showWpError('');
  fetch('/api/geocode?q=' + encodeURIComponent(query))
    .then(function(r) {
      if (r.status === 404) throw new Error('Address not found');
      if (!r.ok) throw new Error('Geocoding error');
      return r.json();
    })
    .then(function(data) {
      delete labelCache[labelKey(waypoints[index])];
      waypoints[index] = [data.lat, data.lon];
      labelCache[labelKey(waypoints[index])] = data.display_name;
      row.removeChild(input);
      labelEl.style.display = '';
      saveState();
      refreshMarkers();
      renderWaypointList();
      rerouteAll();
    })
    .catch(function(err) {
      input.disabled = false;
      input.classList.add('shake');
      setTimeout(function() { input.classList.remove('shake'); }, 300);
      input.focus();
      showWpError(err.message || 'Address not found');
    });
}

function startAddWaypoint() {
  var addRow = document.getElementById('waypoints-add');
  if (addRow.querySelector('.wp-input')) return;

  var handle = document.getElementById('waypoints-add-handle');
  var input = document.createElement('input');
  input.className = 'wp-input';
  input.type = 'text';
  input.placeholder = 'Enter address...';
  input.style.flex = '1';
  addRow.textContent = '';
  if (handle) addRow.appendChild(handle);
  addRow.appendChild(input);
  input.focus();

  input.onkeydown = function(e) {
    if (e.key === 'Enter') {
      var q = input.value.trim();
      if (!q) return;
      commitAddWaypoint(q, addRow, input);
    } else if (e.key === 'Escape') {
      addRow.textContent = '';
      if (handle) addRow.appendChild(handle);
      addRow.appendChild(document.createTextNode('+ Add waypoint'));
      addRow.onclick = startAddWaypoint;
      showWpError('');
    }
  };
}

function commitAddWaypoint(query, addRow, input) {
  input.disabled = true;
  showWpError('');
  fetch('/api/geocode?q=' + encodeURIComponent(query))
    .then(function(r) {
      if (r.status === 404) throw new Error('Address not found');
      if (!r.ok) throw new Error('Geocoding error');
      return r.json();
    })
    .then(function(data) {
      var latlng = L.latLng(data.lat, data.lon);
      labelCache[labelKey([data.lat, data.lon])] = data.display_name;
      var handle = document.getElementById('waypoints-add-handle');
      addRow.textContent = '';
      if (handle) addRow.appendChild(handle);
      addRow.appendChild(document.createTextNode('+ Add waypoint'));
      addRow.onclick = startAddWaypoint;
      activeSlotIndex = -1;
      addWaypoint(latlng);
    })
    .catch(function(err) {
      input.disabled = false;
      input.classList.add('shake');
      setTimeout(function() { input.classList.remove('shake'); }, 300);
      input.focus();
      showWpError(err.message || 'Address not found');
    });
}

function startInsertWaypoint(insertIndex) {
  var list = document.getElementById('waypoints-list');
  var rows = list.querySelectorAll('.wp-row');
  var addRow = document.getElementById('waypoints-add');

  var placeholderRow = document.createElement('div');
  placeholderRow.className = 'wp-row';

  var input = document.createElement('input');
  input.className = 'wp-input';
  input.type = 'text';
  input.placeholder = 'Enter address...';
  input.style.flex = '1';
  placeholderRow.appendChild(input);

  if (insertIndex < rows.length) {
    list.insertBefore(placeholderRow, rows[insertIndex]);
  } else {
    list.insertBefore(placeholderRow, addRow);
  }
  insertSlotIndex = insertIndex;
  insertPlaceholderRow = placeholderRow;
  input.focus();

  input.onkeydown = function(e) {
    if (e.key === 'Enter') {
      var q = input.value.trim();
      if (!q) return;
      commitInsertWaypoint(insertIndex, q, placeholderRow, input);
    } else if (e.key === 'Escape') {
      list.removeChild(placeholderRow);
      insertSlotIndex = -1;
      insertPlaceholderRow = null;
      showWpError('');
    }
  };
}

function commitInsertWaypoint(insertIndex, query, placeholderRow, input) {
  input.disabled = true;
  showWpError('');
  fetch('/api/geocode?q=' + encodeURIComponent(query))
    .then(function(r) {
      if (r.status === 404) throw new Error('Address not found');
      if (!r.ok) throw new Error('Geocoding error');
      return r.json();
    })
    .then(function(data) {
      insertSlotIndex = -1;
      insertPlaceholderRow = null;
      var list = document.getElementById('waypoints-list');
      list.removeChild(placeholderRow);
      waypoints.splice(insertIndex, 0, [data.lat, data.lon]);
      labelCache[labelKey([data.lat, data.lon])] = data.display_name;
      legPolylines.forEach(function(p) { map.removeLayer(p); });
      legPolylines = [];
      legs = [];
      refreshMarkers();
      renderWaypointList();
      rerouteAll();
      saveState();
    })
    .catch(function(err) {
      insertSlotIndex = -1;
      insertPlaceholderRow = null;
      input.disabled = false;
      input.classList.add('shake');
      setTimeout(function() { input.classList.remove('shake'); }, 300);
      input.focus();
      showWpError(err.message || 'Address not found');
    });
}

function reverseGeocodeUnlabeled() {
  var unlabeled = waypoints.filter(function(wp) {
    return !labelCache[labelKey(wp)];
  });
  if (unlabeled.length === 0) return;

  var gen = ++reverseGeocodeGeneration;

  function next(i) {
    if (i >= unlabeled.length) return;
    if (gen !== reverseGeocodeGeneration) return;
    var wp = unlabeled[i];
    var key = labelKey(wp);
    fetch('/api/reverse-geocode?lat=' + wp[0] + '&lon=' + wp[1])
      .then(function(r) { return r.json(); })
      .then(function(data) {
        if (gen !== reverseGeocodeGeneration) return;
        if (data.display_name) {
          labelCache[key] = data.display_name;
          renderWaypointList();
        }
        next(i + 1);
      })
      .catch(function() { next(i + 1); });
  }
  next(0);
}

function rerouteAll() {
  if (waypoints.length < 2) return;

  legPolylines.forEach(function(p) { map.removeLayer(p); });
  legPolylines = [];
  legs = [];
  updateStats();
  requestScore();
  saveState();

  var gen = ++routingGeneration;
  var total = waypoints.length - 1;

  function routeNext(i) {
    if (i >= total) return;
    statusManager.set('routing', 'Routing leg ' + (i + 1) + ' of ' + total + '...', false);
    var from = waypoints[i];
    var to = waypoints[i + 1];
    fetch('/api/route-leg', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ from: { lat: from[0], lon: from[1] }, to: { lat: to[0], lon: to[1] } })
    })
    .then(function(r) {
      if (!r.ok) throw new Error('Routing failed');
      return r.json();
    })
    .then(function(data) {
      if (gen !== routingGeneration) return;
      var latLngs = data.points.map(function(p) { return [p[0], p[1]]; });
      var polyline = L.polyline(latLngs, { color: '#2563eb', weight: 3, opacity: 0.5, pane: 'routePane' }).addTo(map);
      legPolylines.push(polyline);
      legs.push({ points: data.points, duration: data.duration, distance: data.distance });
      saveState();
      updateStats();
      requestScore();
      if (i + 1 >= total) {
        statusManager.clear('routing');
      } else {
        routeNext(i + 1);
      }
    })
    .catch(function() {
      if (gen !== routingGeneration) return;
      statusManager.clear('routing');
      showToast('Could not route leg ' + (i + 1));
    });
  }
  routeNext(0);
}

function curvatureColorLevel(scorePerKm) {
  if (scorePerKm <= 0) return 0;
  var pct = scorePerKm / scorePerKmMax;
  if (pct > 1) pct = 1;
  var colorPct = 1 - 1 / Math.pow(10, pct * 0.75);
  return Math.round(510 * colorPct) + 1;
}

function gradientColorCSS(level) {
  if (level <= 0) return '#4ade80'; // green (tier 0 color)
  if (level <= 256) {
    var green = 255 - (level - 1) * 255 / 255;
    return 'rgb(255,' + Math.round(green) + ',0)';
  }
  var blue = (level - 257) * 255 / 254;
  return 'rgb(255,0,' + Math.round(blue) + ')';
}

var routingGeneration = 0;
var OVERLAY_WAYS = 'ways';
var OVERLAY_ROADS = 'roads';
var overlayMode = OVERLAY_WAYS;
var minScoreFilter = 0;
var minSpeedFilter = 0;
var overlayVisible = true;
// Logarithmic slider mapping: position 0→score 0, position 80%→score max/8, position 100%→score max.
// SLIDER_B controls the curve shape and was derived from those anchors; SLIDER_A scales to SLIDER_MAX_SCORE.
// If DefaultMaxCurvature changes, the curve re-anchors automatically. Only SLIDER_B needs revisiting if
// the 80%→max/8 anchor point should change.
var SLIDER_MAX_SCORE = window.TWISTY_CONFIG.scoreMax;
var SLIDER_B = 38.2;
var SLIDER_A = SLIDER_MAX_SCORE / (SLIDER_B - 1);
var roadPollTimer = null;

var MIN_ZOOM = 12;
var TIER_COLORS = ['#475569', '#4ade80', '#facc15', '#fb923c', '#f87171'];

// segmentGeneration is incremented whenever roadLayer/renderedSegments are
// rebuilt (mode toggle, zoom-out clear). Fetch callbacks snapshot it at
// dispatch time and drop their results if the generation has moved on.
var segmentGeneration = 0;

function segmentStyle(feature) {
  if (overlayMode === OVERLAY_ROADS) {
    var score = feature && feature.properties ? (feature.properties.score || 0) : 0;
    if (score < minScoreFilter) {
      return { opacity: 0, weight: 0 };
    }
    if (minSpeedFilter > 0) {
      var spd = feature && feature.properties ? feature.properties.max_speed_mph : null;
      if (spd === null || spd === undefined || spd < minSpeedFilter) {
        return { opacity: 0, weight: 0 };
      }
    }
    var color = feature && feature.properties ? feature.properties.color : '#475569';
    return { color: color || '#475569', weight: 3, opacity: 0.8 };
  }
  var tier = feature ? feature.properties.tier : 0;
  return { color: TIER_COLORS[Math.max(0, Math.min(tier, 4))], weight: 3, opacity: 0.8 };
}

function onMinTwistInput(value) {
  var p = parseFloat(value) / 100;
  minScoreFilter = p <= 0 ? 0 : SLIDER_A * (Math.pow(SLIDER_B, p) - 1);
  document.getElementById('min-twist-label').textContent = Math.round(minScoreFilter);
  roadLayer.setStyle(segmentStyle);
}

function onMinSpeedInput(value) {
  minSpeedFilter = parseFloat(value);
  document.getElementById('min-speed-label').textContent = Math.round(minSpeedFilter) + ' mph';
  roadLayer.setStyle(segmentStyle);
}

var osmTooltipEl = document.getElementById('osm-tooltip');
function attachTooltipHandlers(layer) {
  layer.on('mouseover', function(e) {
    if (overlayMode !== OVERLAY_WAYS) return;
    var p = e.layer.feature && e.layer.feature.properties;
    var hw = (p && p.highway) || '—';
    var ms = (p && p.maxspeed) || '—';
    osmTooltipEl.textContent = '';
    var b = document.createElement('b');
    b.textContent = hw;
    osmTooltipEl.appendChild(b);
    osmTooltipEl.appendChild(document.createTextNode(' · ' + ms));
    osmTooltipEl.style.display = 'block';
  });
  layer.on('mouseout', function() {
    osmTooltipEl.style.display = 'none';
  });
}
var roadLayer = L.geoJSON(null, { style: segmentStyle }).addTo(map);
attachTooltipHandlers(roadLayer);
var renderedSegments = new Set();

function getBboxString() {
  var b = map.getBounds();
  return b.getWest().toFixed(6) + ',' + b.getSouth().toFixed(6) + ',' +
         b.getEast().toFixed(6) + ',' + b.getNorth().toFixed(6);
}

// rebuildSegmentLayer tears down the current overlay and returns a new
// segmentGeneration value. All in-flight fetch/SSE callbacks that captured
// the old generation will discard their results.
function rebuildSegmentLayer() {
  if (roadPollTimer) { clearTimeout(roadPollTimer); roadPollTimer = null; }
  segmentGeneration++;
  map.removeLayer(roadLayer);
  roadLayer = L.geoJSON(null, { style: segmentStyle });
  attachTooltipHandlers(roadLayer);
  if (overlayVisible) roadLayer.addTo(map);
  renderedSegments.clear();
  return segmentGeneration;
}

// mergeFeatures adds GeoJSON features to the layer, but only if the
// snapshot values (gen, mode, layer, seen) still match the current globals.
// This prevents stale in-flight callbacks from polluting the current layer.
function mergeFeatures(features, gen, mode, layer, seen) {
  if (!features) return;
  if (gen !== segmentGeneration) {
    console.log('[twisty] mergeFeatures: discarding stale response (gen=' + gen + ' current=' + segmentGeneration + ')');
    return;
  }
  features.forEach(function(f) {
    if (!f.geometry || !f.geometry.coordinates || f.geometry.coordinates.length < 1) return;
    // Re-check generation inside the loop: a concurrent rebuild between
    // forEach iterations should also abort.
    if (gen !== segmentGeneration) return;
    var key;
    if (mode === OVERLAY_ROADS) {
      // Use road_name + first coordinate as key so that distinct sub-collections
      // of the same named road (same DisplayName, different geometry) are not
      // collapsed. The coordinate tiebreaker also covers unnamed roads.
      var name = f.properties && f.properties.road_name;
      var firstPair = f.geometry.coordinates[0] && f.geometry.coordinates[0][0];
      key = 'road:' + (name != null ? name : 'unnamed') + ':' + firstPair;
    } else {
      var firstCoord = f.geometry.coordinates[0];
      key = f.properties.way_id + ':' + firstCoord[0] + ':' + firstCoord[1];
    }
    if (seen.has(key)) return;
    seen.add(key);
    layer.addData(f);
  });
}

function loadVisibleSegments() {
  if (roadPollTimer) { clearTimeout(roadPollTimer); roadPollTimer = null; }
  var zoom = map.getZoom();
  console.log('[twisty] loadVisibleSegments: zoom=' + zoom + ' gen=' + segmentGeneration);
  var toggleBtn = document.getElementById('btn-overlay-toggle');
  if (zoom < MIN_ZOOM) {
    // Close SSE before rebuilding so pushed data doesn't repopulate the
    // fresh empty layer while we're zoomed out.
    if (evtSource) { evtSource.close(); evtSource = null; }
    rebuildSegmentLayer();
    if (toggleBtn) toggleBtn.disabled = true;
    return;
  }
  if (toggleBtn) toggleBtn.disabled = false;

  // Snapshot all mutable state at dispatch time. The callback closures use
  // these snapshots rather than reading globals at resolution time, so a
  // mode toggle or zoom-out that runs while a fetch is in-flight will not
  // corrupt the new layer.
  var gen = segmentGeneration;
  var mode = overlayMode;
  var layer = roadLayer;
  var seen = renderedSegments;
  var bbox = getBboxString();

  if (mode === OVERLAY_ROADS) {
    statusManager.set('roads', '⏳ Loading roads...', false);
    fetch('/api/road-segments?bbox=' + bbox)
      .then(function(r) { return r.json(); })
      .then(function(data) {
        console.log('[twisty] road-segments response: gen=' + gen + ' features=' + (data.features ? data.features.length : 0) + ' pending=' + data.pending_tiles + ' failed=' + data.failed_tiles);
        mergeFeatures(data.features, gen, mode, layer, seen);
        if (gen !== segmentGeneration) return;
        if (data.pending_tiles > 0) {
          statusManager.set('roads', '⏳ Loading ' + data.pending_tiles + ' tile' + (data.pending_tiles > 1 ? 's' : '') + '...', false);
          roadPollTimer = setTimeout(loadVisibleSegments, 2000);
        } else if (data.failed_tiles > 0) {
          statusManager.set('roads', '⚠ ' + data.failed_tiles + ' tile(s) failed', false);
        } else {
          statusManager.clear('roads');
        }
      })
      .catch(function() {
        if (gen === segmentGeneration) { statusManager.clear('roads'); }
      });
  } else {
    statusManager.set('roads', '⏳ Loading roads...', false);
    fetch('/api/segments?bbox=' + bbox)
      .then(function(r) { return r.json(); })
      .then(function(data) {
        console.log('[twisty] segments response: gen=' + gen + ' features=' + (data.features ? data.features.length : 0) + ' pending=' + data.pending_tiles + ' failed=' + data.failed_tiles);
        mergeFeatures(data.features, gen, mode, layer, seen);
        if (gen !== segmentGeneration) return;
        if (data.pending_tiles > 0) {
          statusManager.set('roads', '⏳ Loading ' + data.pending_tiles + ' tile' + (data.pending_tiles > 1 ? 's' : '') + '...', false);
          roadPollTimer = setTimeout(loadVisibleSegments, 2000);
        } else if (data.failed_tiles > 0) {
          statusManager.set('roads', '⚠ ' + data.failed_tiles + ' tile(s) failed', false);
        } else {
          statusManager.clear('roads');
        }
      })
      .catch(function() {
        if (gen === segmentGeneration) { statusManager.clear('roads'); }
      });
  }
}

map.on('mousemove', function(e) {
  if (osmTooltipEl.style.display === 'none') return;
  var offset = 14;
  osmTooltipEl.style.left = (e.originalEvent.clientX + offset) + 'px';
  osmTooltipEl.style.top  = (e.originalEvent.clientY + offset) + 'px';
});

map.on('moveend zoomend', function() {
  if (!applyingRouteState) loadVisibleSegments();
});
loadVisibleSegments();

function saveState() {
  try {
    var center = map.getCenter();
    var state = {
      zoom: map.getZoom(),
      center: [center.lat, center.lng],
      waypoints: waypoints,
      legs: legs
    };
    localStorage.setItem('twisty-build-state', JSON.stringify(state));
  } catch(e) {}
}

map.on('moveend zoomend', function() {
  if (!applyingRouteState) saveState();
});

var evtSource = null;

function connectSSE() {
  if (evtSource) { evtSource.close(); }
  evtSource = new EventSource('/api/segments/stream');
  // Snapshot generation/mode/layer/seen at connect time. If a rebuild
  // happens the handler's captured gen will no longer match segmentGeneration
  // and mergeFeatures will discard the message.
  var gen = segmentGeneration;
  var mode = overlayMode;
  var layer = roadLayer;
  var seen = renderedSegments;
  evtSource.onmessage = function(e) {
    try {
      var d = JSON.parse(e.data);
      console.log('[twisty] SSE push: gen=' + gen + ' features=' + (d.features ? d.features.length : 0));
      mergeFeatures(d.features, gen, mode, layer, seen);
    } catch(err) {}
  };
}

connectSSE();

function toggleOverlayMode() {
  osmTooltipEl.style.display = 'none';
  overlayMode = overlayMode === OVERLAY_WAYS ? OVERLAY_ROADS : OVERLAY_WAYS;
  var btn = document.getElementById('btn-overlay-toggle');
  btn.textContent = overlayMode === OVERLAY_WAYS ? 'Switch to Road view' : 'Switch to Way view';
  var slider = document.getElementById('min-twist-slider');
  slider.disabled = overlayMode === OVERLAY_WAYS;
  document.getElementById('min-speed-slider').disabled = overlayMode === OVERLAY_WAYS;
  // rebuildSegmentLayer increments segmentGeneration, which invalidates all
  // in-flight fetch callbacks and the old SSE handler's captured gen.
  rebuildSegmentLayer();
  if (overlayMode === OVERLAY_ROADS) {
    if (evtSource) { evtSource.close(); evtSource = null; }
    roadLayer.setStyle(segmentStyle);
  } else {
    connectSSE();
  }
  loadVisibleSegments();
}

function toggleOverlayVisibility() {
  var btn = document.getElementById('btn-overlay-visibility');
  if (overlayVisible) {
    map.removeLayer(roadLayer);
    overlayVisible = false;
    btn.textContent = 'Show overlay';
    osmTooltipEl.style.display = 'none';
  } else {
    roadLayer.addTo(map);
    overlayVisible = true;
    btn.textContent = 'Hide overlay';
  }
}

function restoreState() {
  var raw = localStorage.getItem('twisty-build-state');
  if (!raw) return;
  var state;
  try {
    state = JSON.parse(raw);
  } catch(e) {
    localStorage.removeItem('twisty-build-state');
    return;
  }
  if (!state || !Array.isArray(state.waypoints) || !Array.isArray(state.legs)) {
    localStorage.removeItem('twisty-build-state');
    return;
  }
  var valid = state.legs.every(function(leg) {
    return leg && Array.isArray(leg.points);
  });
  if (!valid) {
    localStorage.removeItem('twisty-build-state');
    return;
  }
  if (state.waypoints.length > state.legs.length + 1) {
    state.waypoints = state.waypoints.slice(0, state.legs.length + 1);
  }
  applyRouteState(state);
}

// applyingRouteState suppresses the moveend/zoomend handlers while
// applyRouteState runs, so that map.setView doesn't fire loadVisibleSegments
// before the route is drawn.
var applyingRouteState = false;

function applyRouteState(state) {
  clearRoute();
  waypoints = state.waypoints;
  legs = state.legs;
  applyingRouteState = true;
  if (state.center != null && state.zoom != null) {
    map.setView(state.center, state.zoom);
  }
  applyingRouteState = false;
  legs.forEach(function(leg) {
    var latLngs = leg.points.map(function(p) { return [p[0], p[1]]; });
    var polyline = L.polyline(latLngs, { color: '#2563eb', weight: 3, opacity: 0.5, pane: 'routePane' }).addTo(map);
    legPolylines.push(polyline);
  });
  refreshMarkers();
  renderWaypointList();
  updateStats();
  if (legs.length > 0) {
    requestScore();
  }
  // Now that the route is fully drawn, load segments for the restored view.
  loadVisibleSegments();
}

restoreState();

function markerColor(index, total) {
  if (index === 0) return '#16a34a';
  if (index === total - 1) return '#dc2626';
  return '#2563eb';
}

function createMarkerIcon(index, total) {
  var color = markerColor(index, total);
  var isLast = (index === total - 1 && total > 1);
  var size = isLast ? 28 : 24;
  var border = isLast ? '3px solid #fca5a5' : '2px solid white';
  var shadow = isLast ? 'box-shadow:0 0 8px rgba(220,38,38,0.5);' : '';
  return L.divIcon({
    className: '',
    iconSize: [size, size],
    iconAnchor: [size/2, size/2],
    html: '<div style="width:'+size+'px;height:'+size+'px;background:'+color+
          ';border-radius:50%;border:'+border+';display:flex;align-items:center;'+
          'justify-content:center;color:white;font-size:11px;font-weight:bold;'+
          shadow+'">'+(index+1)+'</div>'
  });
}

function refreshMarkers() {
  markers.forEach(function(m) { map.removeLayer(m); });
  markers = [];
  for (var i = 0; i < waypoints.length; i++) {
    var m = L.marker(waypoints[i], { icon: createMarkerIcon(i, waypoints.length) }).addTo(map);
    (function(idx) {
      m.on('click', function() {
        if (idx === waypoints.length - 1 && waypoints.length > 0) {
          removeLastWaypoint();
        }
      });
    })(i);
    markers.push(m);
  }
}

function updateStats() {
  var totalDist = 0, totalTime = 0;
  legs.forEach(function(leg) {
    totalDist += leg.distance;
    totalTime += leg.duration;
  });

  document.getElementById('dist-val').textContent = totalDist > 0 ? (totalDist / 1000).toFixed(1) + ' km' : '—';
  document.getElementById('time-val').textContent = totalTime > 0 ? Math.round(totalTime / 60) + ' min' : '—';

  var hasRoute = waypoints.length >= 2;
  document.getElementById('btn-gpx').disabled = !hasRoute;
  document.getElementById('btn-kml').disabled = !hasRoute;
  document.getElementById('btn-clear').disabled = waypoints.length === 0;
  document.getElementById('btn-save').disabled = waypoints.length === 0;
}

// scoreGeneration is incremented by clearRoute so that in-flight score
// responses (including pending-tiles poll callbacks) discard their results
// rather than re-arming the timer or updating the UI for a cleared route.
var scoreGeneration = 0;

function requestScore() {
  if (scorePollTimer) { clearTimeout(scorePollTimer); scorePollTimer = null; }

  var allPoints = [];
  legs.forEach(function(leg) {
    leg.points.forEach(function(p, i) {
      if (i === 0 && allPoints.length > 0) {
        var last = allPoints[allPoints.length - 1];
        if (last[0] === p[0] && last[1] === p[1]) return;
      }
      allPoints.push(p);
    });
  });

  if (allPoints.length < 2) {
    document.getElementById('score-val').textContent = '—';
    document.getElementById('score-val').style.color = '';
    statusManager.clear('scoring');
    return;
  }

  var gen = scoreGeneration;

  fetch('/api/score', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ points: allPoints })
  })
  .then(function(r) { return r.json(); })
  .then(function(data) {
    if (gen !== scoreGeneration) return;
    var spk = data.score_per_km || 0;
    var scoreEl = document.getElementById('score-val');
    if (spk > 0) {
      scoreEl.textContent = Math.round(spk).toLocaleString();
      scoreEl.style.color = gradientColorCSS(curvatureColorLevel(spk));
    } else {
      scoreEl.textContent = '—';
      scoreEl.style.color = '';
    }

    if (data.pending_tiles > 0) {
      statusManager.set('scoring', '⏳ Scoring ' + data.pending_tiles + ' tile' + (data.pending_tiles > 1 ? 's' : '') + '...', false);
      scorePollTimer = setTimeout(requestScore, 2000);
    } else if (data.failed_tiles > 0) {
      statusManager.set('scoring', '⚠ ' + data.failed_tiles + ' tile(s) failed', false);
    } else {
      statusManager.set('scoring', '✅ Score complete', true);
    }
  })
  .catch(function(err) {
    if (gen !== scoreGeneration) return;
    statusManager.set('scoring', 'Score error', false);
  });
}

function pollViewportScore(payload) {
  fetch('/api/score/viewport', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  })
  .then(function(r) { return r.json(); })
  .then(function(data) {
    document.getElementById('score-val').textContent = '—';
    document.getElementById('score-val').style.color = '';

    if (data.pending_tiles > 0) {
      statusManager.set('scoring', '⏳ Scoring ' + data.pending_tiles + ' tile' + (data.pending_tiles > 1 ? 's' : '') + '...', false);
      scorePollTimer = setTimeout(function() { pollViewportScore(payload); }, 2000);
    } else if (data.failed_tiles > 0) {
      statusManager.set('scoring', '⚠ ' + data.failed_tiles + ' tile(s) failed', false);
    } else {
      statusManager.set('scoring', '✅ Score complete', true);
    }
  })
  .catch(function(err) {
    statusManager.set('scoring', 'Score error', false);
  });
}

function requestViewportScore() {
  if (scorePollTimer) { clearTimeout(scorePollTimer); scorePollTimer = null; }

  var bounds = map.getBounds();
  var payload = {
    west:  bounds.getWest(),
    south: bounds.getSouth(),
    east:  bounds.getEast(),
    north: bounds.getNorth()
  };

  statusManager.set('scoring', '⏳ Refreshing viewport...', false);

  fetch('/api/refresh-viewport', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload)
  })
  .then(function() {
    rebuildSegmentLayer();
    if (overlayMode !== OVERLAY_ROADS) { connectSSE(); }
    loadVisibleSegments();

    statusManager.set('scoring', '⏳ Scoring viewport...', false);
    pollViewportScore(payload);
  })
  .catch(function(err) {
    statusManager.set('scoring', 'Score error', false);
  });
}

function showToast(msg) {
  var el = document.getElementById('toast');
  el.textContent = msg;
  el.style.display = 'block';
  setTimeout(function() { el.style.display = 'none'; }, 4000);
}

function addWaypoint(latlng) {
  if (insertSlotIndex >= 0) {
    var idx = insertSlotIndex;
    var row = insertPlaceholderRow;
    insertSlotIndex = -1;
    insertPlaceholderRow = null;
    if (row && row.parentNode) row.parentNode.removeChild(row);
    waypoints.splice(idx, 0, [latlng.lat, latlng.lng]);
    legPolylines.forEach(function(p) { map.removeLayer(p); });
    legPolylines = [];
    legs = [];
    refreshMarkers();
    renderWaypointList();
    reverseGeocodeUnlabeled();
    rerouteAll();
    saveState();
    return;
  }

  var prev = waypoints.length > 0 ? waypoints[waypoints.length - 1] : null;

  if (activeSlotIndex >= 0 && activeSlotIndex < waypoints.length) {
    waypoints[activeSlotIndex] = [latlng.lat, latlng.lng];
    var replacedIndex = activeSlotIndex;
    activeSlotIndex = -1;
    refreshMarkers();
    saveState();
    renderWaypointList();
    reverseGeocodeUnlabeled();
    rerouteAll();
    return;
  }

  waypoints.push([latlng.lat, latlng.lng]);
  refreshMarkers();
  saveState();
  renderWaypointList();
  reverseGeocodeUnlabeled();

  if (waypoints.length === 1 && !waypointsPanelOpen) {
    toggleWaypointsPanel();
  }

  if (prev) {
    var gen = ++routingGeneration;
    statusManager.set('routing', 'Routing leg 1 of 1...', false);

    fetch('/api/route-leg', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        from: { lat: prev[0], lon: prev[1] },
        to: { lat: latlng.lat, lon: latlng.lng }
      })
    })
    .then(function(r) {
      if (!r.ok) throw new Error('Routing failed');
      return r.json();
    })
    .then(function(data) {
      if (gen !== routingGeneration) return;
      statusManager.clear('routing');
      var latLngs = data.points.map(function(p) { return [p[0], p[1]]; });
      var polyline = L.polyline(latLngs, { color: '#2563eb', weight: 3, opacity: 0.5, pane: 'routePane' }).addTo(map);
      legPolylines.push(polyline);
      legs.push({ points: data.points, duration: data.duration, distance: data.distance });
      saveState();
      updateStats();
      requestScore();
    })
    .catch(function(err) {
      if (gen !== routingGeneration) return;
      statusManager.clear('routing');
      waypoints.pop();
      refreshMarkers();
      saveState();
      renderWaypointList();
      showToast('Could not route between these points — try a different location');
    });
  }
}

function removeLastWaypoint() {
  if (waypoints.length === 0) return;
  waypoints.pop();

  if (legPolylines.length > 0) {
    map.removeLayer(legPolylines.pop());
    legs.pop();
  }

  refreshMarkers();
  updateStats();
  requestScore();

  if (waypoints.length === 0) {
    localStorage.removeItem('twisty-build-state');
  } else {
    saveState();
  }
  renderWaypointList();
}

function clearRoute() {
  routingGeneration++;
  reverseGeocodeGeneration++;
  scoreGeneration++;
  legPolylines.forEach(function(p) { map.removeLayer(p); });
  legPolylines = [];
  markers.forEach(function(m) { map.removeLayer(m); });
  markers = [];
  waypoints = [];
  legs = [];
  labelCache = {};
  localStorage.removeItem('twisty-build-state');
  updateStats();
  requestScore();
  renderWaypointList();
}

function saveRoute() {
  var center = map.getCenter();
  var state = {
    version: 1,
    center: [center.lat, center.lng],
    zoom: map.getZoom(),
    waypoints: waypoints,
    legs: legs
  };
  var json = JSON.stringify(state, null, 2);

  if (window.showSaveFilePicker) {
    window.showSaveFilePicker({
      suggestedName: 'route.twisty.json',
      types: [{
        description: 'Twisty route',
        accept: { 'application/json': ['.json'] }
      }]
    }).then(function(fileHandle) {
      return fileHandle.createWritable();
    }).then(function(writable) {
      return writable.write(json).then(
        function() { return writable.close(); },
        function(err) { return writable.close().then(function() { throw err; }); }
      );
    }).catch(function(e) {
      if (e && e.name === 'AbortError') return;
      showToast('Could not save route');
    });
    return;
  }

  try {
    var blob = new Blob([json], { type: 'application/json' });
    var url = URL.createObjectURL(blob);
    var a = document.createElement('a');
    a.href = url;
    a.download = 'route.twisty.json';
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  } catch(e) {
    showToast('Could not save route');
  }
}

function loadRoute() {
  var input = document.getElementById('file-input');
  input.value = '';
  input.click();
}

function onFileSelected(event) {
  var file = event.target.files[0];
  if (!file) return;
  var reader = new FileReader();
  reader.onload = function(e) {
    var state;
    try {
      state = JSON.parse(e.target.result);
    } catch(err) {
      showToast('Invalid route file');
      return;
    }
    if (
      !state ||
      state.version !== 1 ||
      !Array.isArray(state.waypoints) ||
      !state.waypoints.every(function(wp) { return Array.isArray(wp) && wp.length >= 2; }) ||
      !Array.isArray(state.legs) ||
      !state.legs.every(function(leg) {
        return leg &&
          Array.isArray(leg.points) &&
          leg.points.every(function(p) { return Array.isArray(p) && p.length >= 2; });
      }) ||
      state.waypoints.length > state.legs.length + 1
    ) {
      showToast('Invalid route file');
      return;
    }
    applyRouteState(state);
    saveState();
  };
  reader.onerror = function() {
    showToast('Could not read route file');
  };
  reader.readAsText(file);
}

map.on('click', function(e) {
  addWaypoint(e.latlng);
});

function exportRoute(format) {
  var exportLegs = legs.map(function(leg) {
    return { points: leg.points };
  });
  var exportWaypoints = waypoints.map(function(wp) {
    return { lat: wp[0], lon: wp[1] };
  });

  fetch('/api/export', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ format: format, waypoints: exportWaypoints, legs: exportLegs })
  })
  .then(function(r) {
    if (!r.ok) throw new Error('Export failed');
    return r.blob();
  })
  .then(function(blob) {
    var url = URL.createObjectURL(blob);
    var a = document.createElement('a');
    a.href = url;
    a.download = 'twisty-route.' + format;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
    URL.revokeObjectURL(url);
  })
  .catch(function(err) {
    showToast('Export failed: ' + err.message);
  });
}
