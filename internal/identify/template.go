package identify

// pageHTML is the page somebody names voices on. It saves each name as it is
// entered rather than at the end, so a tab closed halfway leaves the work done
// so far done.
const pageHTML = `<!doctype html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Quem está falando</title>
<style>
  :root {
    --bg: #f6f6f5; --card: #fff; --ink: #1a1a1a; --muted: #6b6b6b;
    --line: #e5e5e3; --accent: #2563eb; --ok: #15803d; --bad: #b91c1c;
  }
  @media (prefers-color-scheme: dark) {
    :root {
      --bg: #16161a; --card: #1f1f24; --ink: #ececed; --muted: #9b9ba1;
      --line: #303038; --accent: #6ea8ff; --ok: #4ade80; --bad: #f87171;
    }
  }
  *, *::before, *::after { box-sizing: border-box; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    background: var(--bg); color: var(--ink); line-height: 1.5;
    max-width: 780px; margin: 0 auto; padding: 2rem 1rem 5rem;
  }
  h1 { font-size: 1.35rem; margin: 0 0 .25rem; }
  .lede { color: var(--muted); margin: 0 0 1.75rem; }
  .group { margin: 0 0 .5rem; font-size: .8rem; color: var(--muted);
           text-transform: uppercase; letter-spacing: .04em; }
  .card { background: var(--card); border: 1px solid var(--line); border-radius: 10px;
          padding: 1.1rem 1.15rem; margin: 0 0 1rem; }
  .card.done { opacity: .55; }
  .who { display: flex; align-items: baseline; gap: .6rem; margin-bottom: .8rem; }
  .who b { font-size: 1rem; }
  .who span { color: var(--muted); font-size: .8rem; }
  .sample { display: flex; gap: .7rem; align-items: flex-start;
            padding: .45rem 0; border-top: 1px solid var(--line); }
  .sample:first-of-type { border-top: none; }
  .play { flex: 0 0 auto; width: 34px; height: 34px; border: none; border-radius: 50%;
          background: var(--accent); color: #fff; cursor: pointer; font-size: .8rem; }
  .play.on { background: var(--bad); }
  .said { font-size: .88rem; }
  .at { color: var(--muted); font-size: .74rem; }
  .row { display: flex; gap: .5rem; margin-top: .9rem; flex-wrap: wrap; }
  input[type=text] { flex: 1 1 12rem; min-width: 0; padding: .5rem .7rem; font-size: .9rem;
         border: 1px solid var(--line); border-radius: 7px;
         background: var(--bg); color: var(--ink); }
  input:focus { outline: 2px solid var(--accent); outline-offset: -1px; }
  button.save { padding: .5rem 1rem; border: none; border-radius: 7px; cursor: pointer;
                background: var(--accent); color: #fff; font-size: .9rem; }
  button.save:disabled { opacity: .5; cursor: default; }
  .aside { display: flex; align-items: center; gap: .35rem; color: var(--muted); font-size: .82rem; }
  .said-back { margin-top: .6rem; font-size: .85rem; }
  .said-back.ok { color: var(--ok); }
  .said-back.bad { color: var(--bad); }
  footer { position: fixed; left: 0; right: 0; bottom: 0; background: var(--card);
           border-top: 1px solid var(--line); padding: .7rem 1rem; text-align: center; }
  footer button { padding: .5rem 1.2rem; border: 1px solid var(--line); border-radius: 7px;
                  background: transparent; color: var(--ink); cursor: pointer; font-size: .9rem; }
  .count { color: var(--muted); font-size: .85rem; margin-right: .8rem; }
</style>
</head>
<body>
<h1>Quem está falando</h1>
<p class="lede">Ouça, escreva quem é, e salve. Cada nome é guardado na hora, e vale para
todas as transcrições em que essa voz aparecer. Deixe em branco o que você não souber.</p>

<div id="list"></div>
<datalist id="known"></datalist>

<footer>
  <span class="count" id="count"></span>
  <button onclick="finish()">Terminei</button>
</footer>

<script>
const VOICES = {{.VoicesJSON}};
const KNOWN = {{.KnownJSON}};

const players = {};
let playing = null;
let named = 0;

document.getElementById("known").innerHTML =
  KNOWN.map(n => '<option value="' + esc(n) + '">').join("");

function esc(s) {
  return String(s).replace(/[&<>"']/g, c =>
    ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
}

function clock(sec) {
  const s = Math.floor(sec);
  return String(Math.floor(s / 60)).padStart(2, "0") + ":" + String(s % 60).padStart(2, "0");
}

function render() {
  const list = document.getElementById("list");
  let html = "", lastFile = null;

  VOICES.forEach((v, i) => {
    if (v.file !== lastFile) {
      lastFile = v.file;
      html += '<p class="group">' + esc(v.title || v.file.split("/").pop()) + "</p>";
    }
    html += '<div class="card" id="card-' + i + '">' +
      '<div class="who"><b>' + esc(v.label) + "</b><span>" + esc(v.id) + "</span></div>";

    v.samples.forEach((s, j) => {
      html += '<div class="sample">' +
        '<button class="play" id="play-' + i + "-" + j + '" onclick="hear(' + i + "," + j + ')">▶</button>' +
        '<div><div class="said">' + esc(s.text || "(sem texto)") + "</div>" +
        '<div class="at">' + clock(s.start_sec) + " – " + clock(s.end_sec) + "</div></div></div>";
    });

    html += '<div class="row">' +
      '<input type="text" id="name-' + i + '" list="known" placeholder="Nome Sobrenome (Empresa)" ' +
      'onkeydown="if(event.key===\'Enter\')save(' + i + ')">' +
      '<button class="save" id="save-' + i + '" onclick="save(' + i + ')">Salvar</button></div>' +
      '<div class="row"><label class="aside">' +
      '<input type="checkbox" id="nosurname-' + i + '"> sobrenome desconhecido</label></div>' +
      '<div class="said-back" id="back-' + i + '"></div></div>';
  });

  list.innerHTML = html;
  tally();
}

function tally() {
  document.getElementById("count").textContent = named + " de " + VOICES.length + " nomeadas";
}

function hear(i, j) {
  const v = VOICES[i], s = v.samples[j];
  const button = document.getElementById("play-" + i + "-" + j);

  if (playing) {
    playing.audio.pause();
    document.getElementById(playing.button).classList.remove("on");
    const same = playing.button === button.id;
    playing = null;
    if (same) return;
  }

  if (!players[v.recording]) {
    players[v.recording] = new Audio("/audio/" + encodeURIComponent(v.recording));
  }
  const audio = players[v.recording];
  audio.currentTime = s.start_sec;
  audio.play();
  button.classList.add("on");
  playing = { audio, button: button.id };

  const stop = () => {
    if (audio.currentTime >= s.end_sec) {
      audio.pause();
      button.classList.remove("on");
      audio.removeEventListener("timeupdate", stop);
      playing = null;
    }
  };
  audio.addEventListener("timeupdate", stop);
}

async function save(i) {
  const typed = document.getElementById("name-" + i).value.trim();
  const back = document.getElementById("back-" + i);
  if (!typed) { back.className = "said-back bad"; back.textContent = "Escreva o nome primeiro."; return; }

  const match = typed.match(/^(.*?)\s*\(([^)]+)\)\s*$/);
  if (!match) {
    back.className = "said-back bad";
    back.textContent = "Escreva como \"Nome Sobrenome (Empresa)\" — a empresa é obrigatória.";
    return;
  }

  const button = document.getElementById("save-" + i);
  button.disabled = true;
  back.className = "said-back";
  back.textContent = "Salvando…";

  const answer = await fetch("/name", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      index: i,
      name: match[1].trim(),
      company: match[2].trim(),
      surname_unknown: document.getElementById("nosurname-" + i).checked,
    }),
  }).then(r => r.json()).catch(e => ({ error: String(e) }));

  if (answer.error) {
    back.className = "said-back bad";
    back.textContent = answer.error;
    button.disabled = false;
    return;
  }

  back.className = "said-back ok";
  back.textContent = "É " + answer.named + ".";
  document.getElementById("card-" + i).classList.add("done");
  named++;
  tally();
}

function finish() {
  fetch("/done", { method: "POST" }).then(() => {
    document.body.innerHTML =
      "<h1>Pronto</h1><p class=\"lede\">" + named + " voz(es) nomeada(s). Pode fechar esta aba.</p>";
  });
}

render();
</script>
</body>
</html>`
