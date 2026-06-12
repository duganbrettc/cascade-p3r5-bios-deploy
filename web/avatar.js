/* Deterministic initials avatar — same username always yields same color */
(function(global) {
  var PALETTE = [
    '#e91e63', '#9c27b0', '#673ab7', '#3f51b5', '#2196f3',
    '#03a9f4', '#00bcd4', '#009688', '#4caf50', '#8bc34a',
    '#ff9800', '#ff5722', '#795548', '#607d8b', '#c0392b'
  ];

  function hashStr(s) {
    var h = 0;
    for (var i = 0; i < s.length; i++) {
      h = Math.imul(31, h) + s.charCodeAt(i) | 0;
    }
    return h;
  }

  function avatarColor(username) {
    var h = hashStr(username || '?');
    return PALETTE[Math.abs(h) % PALETTE.length];
  }

  function avatarInitials(username) {
    if (!username) return '?';
    return username.slice(0, 2).toUpperCase();
  }

  function makeAvatar(username, large) {
    var div = document.createElement('div');
    div.className = 'avatar' + (large ? ' avatar-lg' : '');
    div.style.backgroundColor = avatarColor(username);
    div.textContent = avatarInitials(username);
    div.setAttribute('aria-label', username + ' avatar');
    return div;
  }

  global.avatarColor = avatarColor;
  global.avatarInitials = avatarInitials;
  global.makeAvatar = makeAvatar;
})(window);
