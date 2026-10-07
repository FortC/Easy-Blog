/* AI 助手前端 — 懒加载于首次点击悬浮按钮时 */
(function () {
  var d = document;
  var panel = d.getElementById('ai-panel');
  var fab = d.getElementById('ai-fab');
  var closeBtn = d.getElementById('ai-close');
  var msgs = d.getElementById('ai-msgs');
  var input = d.getElementById('ai-text');
  var send = d.getElementById('ai-send');
  var cfg = window.__AI_CFG || {};
  var history = [];
  var busy = false, indexLoaded = false, siteIndex = '';

  /* ---------- 面板开关 ---------- */
  function open() {
    panel.hidden = false;
    panel.classList.add('open');
    if (!history.length) addAI(cfg.hello || '嗨，我是站长的 AI 助手。');
    setTimeout(function () { input.focus() }, 60);
  }
  function close() { panel.classList.remove('open'); panel.hidden = true; }
  fab.addEventListener('click', function () {
    panel.classList.contains('open') ? close() : open();
  });
  closeBtn.addEventListener('click', close);
  d.addEventListener('keydown', function (e) { if (e.key === 'Escape') close() });

  /* ---------- 渲染 ---------- */
  function el(cls, text) {
    var p = d.createElement('p');
    p.className = 'msg ' + cls;
    p.textContent = text;
    msgs.appendChild(p);
    msgs.scrollTop = msgs.scrollHeight;
    return p;
  }
  function addAI(t) { el('ai', t) }
  function fmt(s) { /* 简易 markdown：粗体/行内码/链接 */
    return s
      .replace(/&/g, '&amp;').replace(/</g, '&lt;')
      .replace(/\*\*([^*]+)\*\*/g, '<b>$1</b>')
      .replace(/`([^`]+)`/g, '<code>$1</code>')
      .replace(/\[([^\]]+)\]\((https?:[^)]+)\)/g, '<a href="$2" target="_blank" rel="noopener">$1</a>')
      .replace(/\n/g, '<br>');
  }

  /* ---------- 站点索引（首次对话时拉取文章列表给 AI 当地图） ---------- */
  function loadIndex() {
    if (indexLoaded) return Promise.resolve();
    indexLoaded = true;
    return fetch('/index.json').then(function (r) { return r.json() }).then(function (list) {
      siteIndex = list.map(function (p) {
        return '- 《' + p.title + '》(' + p.date + '，标签:' + (p.tags || []).join('/') + ')：' + p.summary.slice(0, 60);
      }).join('\n');
    }).catch(function () { siteIndex = '' });
  }

  /* ---------- 发送 ---------- */
  function ask() {
    var q = input.value.trim();
    if (!q || busy) return;
    busy = true; send.disabled = true;
    input.value = '';
    el('me', q);
    var node = el('ai typing', '');
    var acc = '';

    loadIndex().then(function () {
      var sys = '你是个人网站「' + (cfg.site || 'Easy Blog') + '」的 AI 导览员' +
        (cfg.name ? '，名字叫' + cfg.name : '') +
        '。语气轻松口语化，答案简洁（尽量 3 句以内），不用列大段清单。\n' +
        '站点页面：/posts/ 文章、/sayings/ 说说、/treasure/ 百宝库(好物推荐)、/archives/ 归档、/search/ 搜索、/board/ 留言板、/about/ 关于。\n' +
        (siteIndex ? '现有文章列表：\n' + siteIndex + '\n' : '') +
        '问题与文章相关就结合列表回答并给出 /posts/xxx/ 路径；无关问题也可以聊，但记住自己的人设。';

      history.push({ role: 'user', content: q });
      var body = JSON.stringify({ messages: [{ role: 'system', content: sys }].concat(history.slice(-12)) });

      return fetch('/api/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: body
      });
    }).then(function (resp) {
      if (!resp.ok) throw new Error(resp.status === 429 ? '问得太快啦，喝口水再来。' : resp.status === 503 ? '站长还没给 AI 充值（未配置 API Key）。' : '网络开小差了。');
      var reader = resp.body.getReader();
      var dec = new TextDecoder();
      var buf = '';
      function pump() {
        return reader.read().then(function (r) {
          if (r.done) { finish(); return }
          buf += dec.decode(r.value, { stream: true });
          var lines = buf.split('\n');
          buf = lines.pop();
          for (var i = 0; i < lines.length; i++) {
            var line = lines[i].trim();
            if (line.indexOf('data:') !== 0) continue;
            try {
              var ev = JSON.parse(line.slice(5));
              if (ev.delta) { acc += ev.delta; node.innerHTML = fmt(acc); msgs.scrollTop = msgs.scrollHeight }
              if (ev.done || ev.error) { if (ev.error) acc += '\n(' + ev.error + ')'; finish(); return }
            } catch (e) { /* 忽略半包 */ }
          }
          return pump();
        });
      }
      return pump();
    }).catch(function (err) {
      acc = acc || (err && err.message) || '出错了，稍后再试。';
      node.innerHTML = fmt(acc);
      finish();
    });

    function finish() {
      node.classList.remove('typing');
      if (acc) history.push({ role: 'assistant', content: acc });
      busy = false; send.disabled = false; input.focus();
    }
  }

  send.addEventListener('click', ask);
  input.addEventListener('keydown', function (e) { if (e.key === 'Enter') ask() });

  /* 首次点击（触发本脚本加载）直接打开面板 */
  open();
})();
