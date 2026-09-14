(function () {
  var body = document.getElementById('termBody');
  var label = document.getElementById('modeLabel');
  var bar = document.getElementById('progress');
  var meta = document.getElementById('termMeta');
  var reduce = window.matchMedia('(prefers-reduced-motion: reduce)').matches;

  var scenes = [
    {
      name: 'interactive',
      cmd: 'go run .',
      lines: [
        [{ t: '$ ', c: 'cy' }, { t: 'go run .', c: 'cmd' }],
        [{ t: '◆ myagent · ollama/qwen3 · 9f3a', c: 'dim' }],
        [{ t: 'you ', c: 'cy' }, { t: 'add retries with backoff', c: 'cmd' }],
        [{ t: '✓ edited internal/llm/client.go (+84)', c: 'ok' }],
        [{ t: '✓ tests green, interface unchanged', c: 'ok' }]
      ]
    },
    {
      name: 'print',
      cmd: '-p "prompt"',
      lines: [
        [{ t: '$ ', c: 'cy' }, { t: 'go run . -p "summarize the readme"', c: 'cmd' }],
        [{ t: 'Go TUI + headless agent, OpenAI-compatible…', c: 'cmd' }],
        [{ t: '[retry] 503 · backing off 2s', c: 'dim' }],
        [{ t: '✓ done in 4.2s', c: 'ok' }]
      ]
    },
    {
      name: 'server',
      cmd: 'serve',
      lines: [
        [{ t: '$ ', c: 'cy' }, { t: 'go run . serve --port 8080', c: 'cmd' }],
        [{ t: '● listening on :8080', c: 'ok' }],
        [{ t: '→ ', c: 'cy' }, { t: 'session.create  {"id": "a41f"}', c: 'cmd' }],
        [{ t: '→ session.prompt · streams AgentEvent', c: 'dim' }]
      ]
    }
  ];

  function lineEl(segs, caret) {
    var p = document.createElement('p');
    p.className = 'tl';
    segs.forEach(function (s) {
      var span = document.createElement('span');
      span.className = 'tc-' + s.c;
      span.style.margin = '0';
      span.textContent = s.t;
      p.appendChild(span);
    });
    if (caret) {
      var c = document.createElement('span');
      c.className = 'caret';
      p.appendChild(document.createTextNode(' '));
      p.appendChild(c);
    }
    return p;
  }

  function renderStatic(scene) {
    body.innerHTML = '';
    scene.lines.forEach(function (segs, i) {
      body.appendChild(lineEl(segs, i === scene.lines.length - 1));
    });
  }

  function setChrome(i) {
    label.textContent = scenes[i].name + ' — ' + scenes[i].cmd;
    meta.textContent = (i + 1) + '/' + scenes.length + ' · esc aborts';
    bar.style.width = ((i + 1) / scenes.length * 100) + '%';
  }

  if (reduce || !body) {
    if (body) { renderStatic(scenes[0]); setChrome(0); bar.style.width = '100%'; }
    return;
  }

  var si = 0;
  var cancelled = false;

  function sleep(ms) { return new Promise(function (r) { setTimeout(r, ms); }); }

  async function typeScene(idx) {
    setChrome(idx);
    var scene = scenes[idx];
    body.innerHTML = '';
    for (var li = 0; li < scene.lines.length; li++) {
      var segs = scene.lines[li];
      var p = document.createElement('p');
      p.className = 'tl';
      body.appendChild(p);
      var spans = segs.map(function (s) {
        var span = document.createElement('span');
        span.style.margin = '0';
        p.appendChild(span);
        return { el: span, full: s.t, cls: s.c };
      });
      // type char by char across segments
      for (var k = 0; k < spans.length; k++) {
        spans[k].el.className = 'tc-' + spans[k].cls;
        var txt = spans[k].full;
        for (var ch = 1; ch <= txt.length; ch++) {
          if (cancelled) return;
          spans[k].el.textContent = txt.slice(0, ch);
          await sleep(txt[0] === '$' ? 26 : 14);
        }
      }
      await sleep(300);
    }
    var caret = document.createElement('span');
    caret.className = 'caret';
    body.lastChild.appendChild(document.createTextNode(' '));
    body.lastChild.appendChild(caret);
    await sleep(2800);
    // soft swap
    body.classList.add('fade');
    label.classList.add('swap');
    await sleep(220);
    body.classList.remove('fade');
    label.classList.remove('swap');
  }

  (async function loop() {
    while (!cancelled) {
      await typeScene(si);
      si = (si + 1) % scenes.length;
    }
  })();

  // copy button
  var copyBtn = document.getElementById('copyBtn');
  if (copyBtn) {
    copyBtn.addEventListener('click', async function () {
      var cmd = document.getElementById('installCmd');
      var text = cmd ? cmd.textContent.trim() : '';
      try { await navigator.clipboard.writeText(text); }
      catch (e) {
        var ta = document.createElement('textarea');
        ta.value = text;
        document.body.appendChild(ta);
        ta.select();
        document.execCommand('copy');
        ta.remove();
      }
      var prev = copyBtn.textContent;
      copyBtn.textContent = 'Copied';
      setTimeout(function () { copyBtn.textContent = prev; }, 1500);
    });
  }

  // scroll reveal
  var io = new IntersectionObserver(function (entries) {
    entries.forEach(function (en) {
      if (en.isIntersecting) { en.target.classList.add('in'); io.unobserve(en.target); }
    });
  }, { threshold: 0.15 });
  document.querySelectorAll('.rv').forEach(function (el) { io.observe(el); });
})();
