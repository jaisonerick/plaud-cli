package identify

const pageHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Identify Speakers</title>
<style>
  *, *::before, *::after { box-sizing: border-box; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    background: #f5f5f5; color: #1a1a1a;
    max-width: 720px; margin: 0 auto; padding: 2rem 1rem;
    line-height: 1.5;
  }
  h1 { font-size: 1.5rem; margin-bottom: 0.25rem; }
  .subtitle { color: #666; margin-bottom: 2rem; }
  .speaker-card {
    background: #fff; border-radius: 8px; padding: 1.25rem;
    margin-bottom: 1.25rem; box-shadow: 0 1px 3px rgba(0,0,0,0.1);
  }
  .speaker-card h2 {
    font-size: 1rem; margin: 0 0 0.75rem;
    color: #444; font-weight: 600;
  }
  .sample {
    display: flex; align-items: flex-start; gap: 0.75rem;
    padding: 0.5rem 0; border-bottom: 1px solid #eee;
  }
  .sample:last-of-type { border-bottom: none; }
  .play-btn {
    flex-shrink: 0; width: 36px; height: 36px;
    border: none; border-radius: 50%; cursor: pointer;
    background: #2563eb; color: #fff; font-size: 0.85rem;
    display: flex; align-items: center; justify-content: center;
    transition: background 0.15s;
  }
  .play-btn:hover { background: #1d4ed8; }
  .play-btn.playing { background: #dc2626; }
  .sample-text {
    font-size: 0.9rem; color: #333;
    flex: 1; padding-top: 0.35rem;
  }
  .sample-time {
    font-size: 0.75rem; color: #999; margin-top: 0.15rem;
  }
  .name-row {
    margin-top: 0.75rem; display: flex; gap: 0.5rem; align-items: center;
  }
  .name-row label { font-size: 0.85rem; color: #666; white-space: nowrap; }
  .name-row input {
    flex: 1; padding: 0.5rem 0.75rem; border: 1px solid #ddd;
    border-radius: 6px; font-size: 0.9rem; outline: none;
    transition: border-color 0.15s;
  }
  .name-row input:focus { border-color: #2563eb; }
  .actions {
    display: flex; gap: 0.75rem; margin-top: 1.5rem;
  }
  .btn {
    padding: 0.6rem 1.5rem; border: none; border-radius: 6px;
    font-size: 0.9rem; font-weight: 500; cursor: pointer;
    transition: background 0.15s;
  }
  .btn-primary { background: #2563eb; color: #fff; }
  .btn-primary:hover { background: #1d4ed8; }
  .btn-primary:disabled { background: #93c5fd; cursor: not-allowed; }
  .btn-secondary { background: #e5e7eb; color: #333; }
  .btn-secondary:hover { background: #d1d5db; }
  .done-msg {
    text-align: center; padding: 3rem 1rem;
  }
  .done-msg h2 { color: #16a34a; margin-bottom: 0.5rem; }
  .done-msg p { color: #666; }
</style>
</head>
<body>

<div id="app">
  <h1>Identify Speakers</h1>
  <p class="subtitle">Play audio samples to identify each speaker, then assign names below.</p>

  <form id="form">
    {{range .Speakers}}
    <div class="speaker-card">
      <h2>{{.ID}}</h2>
      {{range .Samples}}
      <div class="sample">
        <button type="button" class="play-btn" data-start="{{.StartSec}}" data-end="{{.EndSec}}">&#9654;</button>
        <div>
          <div class="sample-text">"{{.Text}}"</div>
          <div class="sample-time">{{printf "%.1f" .StartSec}}s – {{printf "%.1f" .EndSec}}s</div>
        </div>
      </div>
      {{end}}
      <div class="name-row">
        <label>Name:</label>
        <input type="text" name="{{.ID}}" placeholder="Enter name for {{.ID}}…" autocomplete="off">
      </div>
    </div>
    {{end}}

    <div class="actions">
      <button type="submit" class="btn btn-primary" id="save-btn">Save Names</button>
      <button type="button" class="btn btn-secondary" id="skip-btn">Skip</button>
    </div>
  </form>
</div>

<script>
(function() {
  const audio = new Audio('/audio');
  let stopTimer = null;
  let activeBtn = null;

  function stopPlayback() {
    audio.pause();
    if (stopTimer) { clearTimeout(stopTimer); stopTimer = null; }
    if (activeBtn) { activeBtn.classList.remove('playing'); activeBtn.innerHTML = '\u25B6'; activeBtn = null; }
  }

  document.querySelectorAll('.play-btn').forEach(function(btn) {
    btn.addEventListener('click', function() {
      if (activeBtn === btn) { stopPlayback(); return; }
      stopPlayback();

      activeBtn = btn;
      btn.classList.add('playing');
      btn.innerHTML = '\u25A0';

      var start = parseFloat(btn.dataset.start);
      var end = parseFloat(btn.dataset.end);
      audio.currentTime = start;
      audio.play();
      stopTimer = setTimeout(stopPlayback, (end - start) * 1000);
    });
  });

  audio.addEventListener('ended', stopPlayback);

  document.getElementById('form').addEventListener('submit', function(e) {
    e.preventDefault();
    stopPlayback();

    var names = {};
    document.querySelectorAll('.name-row input').forEach(function(input) {
      var v = input.value.trim();
      if (v) names[input.name] = v;
    });

    if (Object.keys(names).length === 0) {
      alert('Please enter at least one speaker name.');
      return;
    }

    var btn = document.getElementById('save-btn');
    btn.disabled = true;
    btn.textContent = 'Saving…';

    fetch('/save', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify(names)
    }).then(function() {
      document.getElementById('app').innerHTML =
        '<div class="done-msg"><h2>Names submitted</h2>' +
        '<p>Check your terminal for confirmation. You can close this tab.</p></div>';
    });
  });

  document.getElementById('skip-btn').addEventListener('click', function() {
    stopPlayback();
    fetch('/skip', {method: 'POST'}).then(function() {
      document.getElementById('app').innerHTML =
        '<div class="done-msg"><h2>Skipped</h2>' +
        '<p>No changes made. You can close this tab.</p></div>';
    });
  });
})();
</script>

</body>
</html>`
