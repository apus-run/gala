package ui

// appJS 是文件管理页的全部前端逻辑：原生 JS，无构建步骤，数据走 /api JSON 接口。
// 放在 Go 常量里随二进制分发，避免容器里再带静态文件。
//
// shortcut: 无框架依赖；换前端框架时重写此文件即可，后端接口不变。
const appJS = `(function () {
'use strict';
var cfg = JSON.parse(document.getElementById('app-cfg').textContent);
var state = { user: String(cfg.userID), tab: 'mine', search: '', type: 'all', folder: '', folders: [], rows: [], transferring: 0 };
var $ = function (id) { return document.getElementById(id); };

function esc(s) {
  return String(s).replace(/[&<>"']/g, function (c) {
    return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
  });
}
function humanSize(n) {
  if (n < 1024) return n + ' B';
  var u = ['KB', 'MB', 'GB', 'TB'], i = -1;
  do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
  return n.toFixed(n >= 100 ? 0 : 1) + ' ' + u[i];
}
function iconFor(name) {
  var ext = (name.split('.').pop() || '').toLowerCase();
  var m = {
    png: ['🖼️', '#eef2ff'], jpg: ['🖼️', '#eef2ff'], jpeg: ['🖼️', '#eef2ff'], gif: ['🖼️', '#eef2ff'], webp: ['🖼️', '#eef2ff'], svg: ['🖼️', '#eef2ff'], heic: ['🖼️', '#eef2ff'],
    pdf: ['📕', '#fdecec'], doc: ['📘', '#e8f0fe'], docx: ['📘', '#e8f0fe'], xls: ['📗', '#e8f8f1'], xlsx: ['📗', '#e8f8f1'], csv: ['📗', '#e8f8f1'],
    ppt: ['📙', '#fff4e5'], pptx: ['📙', '#fff4e5'], txt: ['📄', '#f1f4fb'], md: ['📄', '#f1f4fb'],
    zip: ['🗜️', '#f3f0ff'], rar: ['🗜️', '#f3f0ff'], gz: ['🗜️', '#f3f0ff'], '7z': ['🗜️', '#f3f0ff'],
    mp4: ['🎬', '#fdecec'], mov: ['🎬', '#fdecec'], mkv: ['🎬', '#fdecec'], avi: ['🎬', '#fdecec'],
    mp3: ['🎵', '#f3f0ff'], wav: ['🎵', '#f3f0ff'], flac: ['🎵', '#f3f0ff'],
    go: ['📐', '#e8f8f1'], js: ['📐', '#fff4e5'], ts: ['📐', '#fff4e5'], py: ['📐', '#fff4e5'], json: ['📐', '#f1f4fb'],
  }[ext];
  if (!m) m = ['📦', '#f1f4fb'];
  return { emoji: m[0], bg: m[1] };
}
function typeOf(name) {
  var ext = (name.split('.').pop() || '').toLowerCase();
  if (['png', 'jpg', 'jpeg', 'gif', 'webp', 'svg', 'heic', 'bmp'].indexOf(ext) >= 0) return 'image';
  if (['mp4', 'mov', 'mkv', 'avi', 'webm', 'flv'].indexOf(ext) >= 0) return 'video';
  if (['pdf', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'txt', 'md', 'csv'].indexOf(ext) >= 0) return 'doc';
  return 'other';
}
function toast(msg, cls) {
  var d = document.createElement('div');
  d.className = 'toast ' + (cls || '');
  d.textContent = msg;
  $('toasts').appendChild(d);
  setTimeout(function () { d.style.opacity = '0'; d.style.transition = 'opacity .3s'; }, 2600);
  setTimeout(function () { d.remove(); }, 3000);
}

function api(method, url, jsonBody) {
  var opt = { method: method, headers: { 'X-User-ID': state.user } };
  if (jsonBody !== undefined) {
    opt.headers['Content-Type'] = 'application/json';
    opt.body = JSON.stringify(jsonBody);
  }
  return fetch(url, opt).then(function (r) {
    return r.json().catch(function () { return {}; }).then(function (j) {
      if (!r.ok) throw new Error(j.msg || j.error || ('HTTP ' + r.status));
      return j;
    });
  });
}

// ---------- 文件夹 ----------
function loadFolders() {
  return api('GET', '/api/folders').then(function (j) {
    state.folders = (j.data || []).map(function (f) { return f.Path || f.path; });
    renderTree();
  }).catch(function () { state.folders = []; renderTree(); });
}
function renderTree() {
  var set = {};
  state.folders.forEach(function (p) {
    var segs = p.split('/');
    for (var i = 1; i <= segs.length; i++) set[segs.slice(0, i).join('/')] = 1;
  });
  var paths = Object.keys(set).sort();
  var html = '<button class="tree-item' + (state.folder === '' ? ' active' : '') + '" data-path="">📁 全部文件</button>';
  for (var i = 0; i < paths.length; i++) {
    var p = paths[i], depth = p.split('/').length;
    var name = p.split('/').pop();
    var isActive = state.folder === p;
    html += '<div style="display:flex; align-items:center" class="tree-row">'
      + '<button class="tree-item' + (isActive ? ' active' : '') + '" data-path="' + esc(p) + '" style="padding-left:' + (9 + (depth - 1) * 14) + 'px">📁 <span class="nm">' + esc(name) + '</span></button>'
      + '<button class="tree-item del" data-delpath="' + esc(p) + '" title="删除空文件夹">✕</button>'
      + '</div>';
  }
  $('tree').innerHTML = html;
  renderCrumb();
}
function renderCrumb() {
  if (!state.folder) {
    $('crumb').innerHTML = '<span style="font-weight:600; color:var(--text)">全部文件</span>';
    $('dropzone-sub').textContent = '支持多文件 · 大于 10 MB 自动分块断点续传 · 相同内容秒传';
    return;
  }
  var html = '<a data-goto="">全部文件</a>', acc = '';
  var segs = state.folder.split('/');
  for (var i = 0; i < segs.length; i++) {
    acc = acc ? acc + '/' + segs[i] : segs[i];
    html += '<span class="sep">/</span>'
      + (i === segs.length - 1
        ? '<span style="font-weight:600; color:var(--text)">' + esc(segs[i]) + '</span>'
        : '<a data-goto="' + esc(acc) + '">' + esc(segs[i]) + '</a>');
  }
  $('crumb').innerHTML = html;
  $('dropzone-sub').textContent = '上传到：' + state.folder + ' · 大于 10 MB 自动分块 · 相同内容秒传';
}
$('tree').addEventListener('click', function (ev) {
  var nav = ev.target.closest('[data-path]');
  if (nav && !ev.target.dataset.delpath) {
    state.folder = nav.dataset.path;
    renderTree();
    load();
    return;
  }
  var del = ev.target.closest('[data-delpath]');
  if (del) {
    $('folder-del-path').textContent = del.dataset.delpath;
    $('folder-del-back').classList.add('open');
  }
});
$('crumb').addEventListener('click', function (ev) {
  var a = ev.target.closest('[data-goto]');
  if (!a) return;
  state.folder = a.dataset.goto;
  renderTree();
  load();
});

// 新建文件夹：在当前目录下创建，输入可用 / 继续分层。
$('new-folder').onclick = function () {
  $('folder-new-parent').textContent = state.folder ? '位于：' + state.folder : '位于根目录';
  $('folder-new-input').value = '';
  $('folder-new-back').classList.add('open');
  $('folder-new-input').focus();
};
$('folder-new-cancel').onclick = function () { $('folder-new-back').classList.remove('open'); };
$('folder-new-save').onclick = function () {
  var name = $('folder-new-input').value.trim().replace(/^\/+|\/+$/g, '');
  if (!name) return;
  var path = state.folder ? state.folder + '/' + name : name;
  api('POST', '/api/folders', { path: path }).then(function () {
    $('folder-new-back').classList.remove('open');
    toast('文件夹已创建：' + path, 'ok');
    loadFolders();
  }).catch(function (e) { toast('创建失败：' + e.message, 'err'); });
};
$('folder-del-cancel').onclick = function () { $('folder-del-back').classList.remove('open'); };
$('folder-del-save').onclick = function () {
  var path = $('folder-del-path').textContent;
  api('DELETE', '/api/folders/' + path).then(function () {
    $('folder-del-back').classList.remove('open');
    toast('文件夹已删除', 'ok');
    if (state.folder === path || path.indexOf(state.folder + '/') === 0) {
      // 删掉的是当前目录或其祖先时回到根目录。
      state.folder = path.split('/').slice(0, -1).join('/');
    }
    loadFolders().then(load);
  }).catch(function (e) { toast('删除失败：' + e.message, 'err'); });
};

// ---------- 列表 ----------
function load() {
  var url = state.tab === 'mine'
    ? '/api/files?page=1&page_size=100&folder=' + encodeURIComponent(state.folder)
    : '/api/files/public?page=1&page_size=100';
  var rowsEl = $('rows');
  rowsEl.innerHTML = '<tr><td colspan="5" class="muted"><span class="spin"></span> 加载中…</td></tr>';
  api('GET', url).then(function (j) {
    state.rows = (j.data && j.data.data) || [];
    render();
  }).catch(function (e) {
    state.rows = [];
    render();
    toast('读取列表失败：' + e.message, 'err');
  });
}
function render() {
  var kw = state.search.trim().toLowerCase();
  var rows = state.rows.filter(function (f) {
    var name = f.file_name || '';
    return (!kw || name.toLowerCase().indexOf(kw) >= 0)
      && (state.type === 'all' || typeOf(name) === state.type);
  });
  var mine = state.tab === 'mine';
  $('th-owner').textContent = mine ? '操作' : '上传者';
  var html = '';
  for (var i = 0; i < rows.length; i++) {
    var f = rows[i], ic = iconFor(f.file_name || '');
    html += '<tr>'
      + '<td><div class="fname"><div class="fico" style="background:' + ic.bg + '">' + ic.emoji + '</div>'
      + '<a href="/api/files/' + f.file_hash + '" target="_blank" rel="noreferrer">' + esc(f.file_name || '') + '</a>'
      + (f.ref_count > 1 ? ' <span class="badge dedup">× ' + f.ref_count + ' 人引用</span>' : '')
      + '</div></td>'
      + '<td>' + humanSize(f.file_size) + '</td>'
      + '<td class="muted">' + new Date(f.created_at).toLocaleString('zh-CN', { hour12: false }) + '</td>'
      + '<td class="muted" style="font-family:ui-monospace,Menlo,monospace; font-size:11px">' + f.file_hash.slice(0, 10) + '…</td>'
      + (mine
        ? '<td><div class="acts">'
          + '<a class="act" href="/api/files/' + f.file_hash + '?inline=1" target="_blank" rel="noreferrer">👁 查看</a>'
          + '<a class="act" href="/api/files/' + f.file_hash + '" download>⬇ 下载</a>'
          + '<button class="act" data-act="copy" data-hash="' + f.file_hash + '">⧉ 复制</button>'
          + '<button class="act" data-act="replace" data-hash="' + f.file_hash + '" data-name="' + esc(f.file_name || '') + '">🔄 替换</button>'
          + '<button class="act danger" data-act="delete" data-hash="' + f.file_hash + '" data-name="' + esc(f.file_name || '') + '">🗑 删除</button>'
          + '</div></td>'
        : '<td class="muted">用户 ' + (f.uploader_id || '—') + '</td>')
      + '</tr>';
  }
  $('rows').innerHTML = html;
  $('empty').hidden = rows.length > 0;
  if (mine) {
    var totalSize = 0;
    for (var k = 0; k < state.rows.length; k++) totalSize += state.rows[k].file_size;
    $('stat-count').textContent = state.rows.length;
    $('stat-size').textContent = humanSize(totalSize);
  }
}

// ---------- 上传 ----------
// pendingReplace 非 null 时，下一次选择的文件替换该哈希的内容。
var pendingReplace = '';
$('replace-input').onchange = function () {
  if (this.files && this.files[0] && pendingReplace) {
    var f = this.files[0];
    if (f.size > cfg.maxUpload) {
      toast('“' + f.name + '”超过上限 ' + humanSize(cfg.maxUpload), 'err');
    } else {
      startUpload(f, pendingReplace);
    }
  }
  pendingReplace = '';
  this.value = '';
};
function enqueue(files) {
  for (var i = 0; i < files.length; i++) {
    if (files[i].size > cfg.maxUpload) {
      toast('“' + files[i].name + '”超过上限 ' + humanSize(cfg.maxUpload), 'err');
      continue;
    }
    startUpload(files[i], '');
  }
}
function startUpload(file, replace) {
  var id = 'q' + Date.now() + Math.floor(Math.random() * 1000);
  var el = document.createElement('div');
  el.className = 'qitem';
  el.id = id;
  var where = replace ? ' 🔄 替换 → ' + esc(file.name) : (state.folder ? ' → ' + esc(state.folder) : '');
  el.innerHTML = '<div class="meta">'
    + '<div class="t"><span class="name">' + esc(file.name) + where + '</span>'
    + '<span class="pct">0%</span>'
    + '<span class="muted" style="font-size:12px">' + humanSize(file.size)
    + (file.size >= cfg.minChunked ? ' · 分块续传' : '') + '</span></div>'
    + '<div class="bar"><i></i></div>'
    + '<div class="sub"><span class="st">排队中</span><span class="sp"></span></div>'
    + '</div><button class="ghost">取消</button>';
  $('queue').prepend(el);
  $('queue-card').hidden = false;

  var t0 = 0, loaded0 = 0;
  var item = {
    el: el,
    setProgress: function (done, total) {
      var pct = total ? Math.floor(done * 100 / total) : 0;
      el.querySelector('.pct').textContent = pct + '%';
      el.querySelector('.bar > i').style.width = pct + '%';
      var now = Date.now();
      if (!t0) { t0 = now; loaded0 = done; }
      else if (now - t0 > 800) {
        var speed = (done - loaded0) / ((now - t0) / 1000);
        el.querySelector('.sp').textContent = humanSize(speed) + '/s';
        t0 = now; loaded0 = done;
      }
    },
    setStatus: function (s) { el.querySelector('.st').textContent = s; },
    done: function (dedup) {
      el.classList.add('done');
      el.querySelector('.pct').textContent = '100%';
      el.querySelector('.bar > i').style.width = '100%';
      el.querySelector('.sp').textContent = '';
      item.setStatus(dedup ? '✅ 秒传成功（内容已存在）' : '✅ 上传完成');
      el.querySelector('button').textContent = '关闭';
      state.transferring--; renderStats();
      load();
    },
    fail: function (msg) {
      el.classList.add('err');
      el.querySelector('.bar > i').style.width = '100%';
      item.setStatus('❌ ' + msg);
      el.querySelector('button').textContent = '关闭';
      state.transferring--; renderStats();
    },
  };
  state.transferring++; renderStats();

  var cancelBtn = el.querySelector('button');
  var aborted = false, xhr = null, uploadId = '';
  cancelBtn.onclick = function () {
    if (cancelBtn.textContent === '关闭') { el.remove(); return; }
    aborted = true;
    try { if (xhr) xhr.abort(); } catch (e) {}
    if (uploadId) api('DELETE', '/api/uploads/' + uploadId).catch(function () {});
    item.fail('已取消');
  };

  var p = file.size >= cfg.minChunked ? uploadChunked(file, item, function (x) { xhr = x; }, function (u) { uploadId = u; }, replace)
    : uploadSmall(file, item, function (x) { xhr = x; }, replace);
  p.then(function (dedup) { if (!aborted) item.done(dedup); })
    .catch(function (e) { if (!aborted) item.fail(e.message || '上传失败'); });
}
function uploadSmall(file, item, reg, replace) {
  return new Promise(function (resolve, reject) {
    var fd = new FormData();
    fd.append('file', file, file.name);
    fd.append('folder', state.folder);
    var x = new XMLHttpRequest();
    reg(x);
    x.open('POST', replace ? '/api/files/' + replace + '/replace' : '/api/files');
    x.setRequestHeader('X-User-ID', state.user);
    x.upload.onprogress = function (e) { if (e.lengthComputable) item.setProgress(e.loaded, e.total); };
    x.onload = function () {
      try {
        var j = JSON.parse(x.responseText);
        if (x.status >= 200 && x.status < 300) resolve(!!(j.data && j.data.is_dedup));
        else reject(new Error((j.data && j.data.msg) || j.msg || ('HTTP ' + x.status)));
      } catch (e) { reject(e); }
    };
    x.onerror = function () { reject(new Error('网络错误')); };
    x.send(fd);
  });
}
function uploadChunked(file, item, reg, regId, replace) {
  var chunkSize = cfg.chunkSize;
  var total = Math.ceil(file.size / chunkSize);
  var base = 0;
  return api('POST', '/api/uploads', {
    file_name: file.name, file_size: file.size, folder: state.folder, replace_of: replace || '',
    chunk_size: chunkSize, content_type: file.type || 'application/octet-stream',
  }).then(function (j) {
    var uid = j.data.upload_id;
    regId(uid);
    item.setStatus('分块上传 0/' + total);
    var i = 0;
    function next() {
      if (i >= total) return Promise.resolve();
      var idx = i++;
      var blob = file.slice(idx * chunkSize, Math.min((idx + 1) * chunkSize, file.size));
      return new Promise(function (resolve, reject) {
        var fd = new FormData();
        fd.append('chunk', blob, file.name + '.part' + idx);
        var x = new XMLHttpRequest();
        reg(x);
        x.open('POST', '/api/uploads/' + uid + '/chunks/' + idx);
        x.setRequestHeader('X-User-ID', state.user);
        x.upload.onprogress = function (e) {
          if (e.lengthComputable) item.setProgress(base + e.loaded, file.size);
        };
        x.onload = function () {
          if (x.status >= 200 && x.status < 300) { base += blob.size; resolve(); }
          else {
            var msg = 'HTTP ' + x.status;
            try { msg = JSON.parse(x.responseText).msg || msg; } catch (e) {}
            reject(new Error('第 ' + (idx + 1) + ' 块失败：' + msg));
          }
        };
        x.onerror = function () { reject(new Error('网络错误')); };
        x.send(fd);
      }).then(function () {
        item.setStatus('分块上传 ' + (idx + 1) + '/' + total);
        return next();
      });
    }
    return next().then(function () {
      item.setStatus('合并中…');
      return api('POST', '/api/uploads/' + uid + '/complete');
    }).then(function (j2) {
      return !!(j2.data && j2.data.is_dedup);
    });
  });
}
function renderStats() { $('stat-transferring').textContent = state.transferring; }

// ---------- 文件操作弹窗 ----------
var renameHash = '';
$('rename-cancel').onclick = function () { $('rename-back').classList.remove('open'); };
$('rename-save').onclick = function () {
  var name = $('rename-input').value.trim();
  if (!name) return;
  api('PUT', '/api/files/' + renameHash, { file_name: name })
    .then(function () { $('rename-back').classList.remove('open'); toast('已重命名', 'ok'); load(); })
    .catch(function (e) { toast('重命名失败：' + e.message, 'err'); });
};
var deleteHash = '';
$('delete-cancel').onclick = function () { $('delete-back').classList.remove('open'); };
$('delete-save').onclick = function () {
  api('DELETE', '/api/files/' + deleteHash)
    .then(function () { $('delete-back').classList.remove('open'); toast('已删除', 'ok'); load(); })
    .catch(function (e) { toast('删除失败：' + e.message, 'err'); });
};

$('rows').addEventListener('click', function (ev) {
  var t = ev.target.closest('button');
  if (!t) return;
  if (t.dataset.act === 'copy') {
    navigator.clipboard.writeText(t.dataset.hash).then(function () { toast('已复制完整 SHA-256', 'ok'); });
    return;
  }
  if (t.dataset.act === 'replace') {
    pendingReplace = t.dataset.hash;
    $('replace-input').value = '';
    $('replace-input').click();
    return;
  }
  if (t.dataset.act === 'rename') {
    renameHash = t.dataset.hash;
    $('rename-hash').textContent = 'SHA-256: ' + t.dataset.hash;
    $('rename-input').value = t.dataset.name;
    $('rename-back').classList.add('open');
    $('rename-input').focus();
  }
  if (t.dataset.act === 'delete') {
    deleteHash = t.dataset.hash;
    $('delete-name').textContent = t.dataset.name;
    $('delete-back').classList.add('open');
  }
});

// ---------- 全局事件 ----------
var dz = $('dropzone');
dz.onclick = function () { $('file-input').click(); };
dz.onkeydown = function (e) { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); $('file-input').click(); } };
['dragenter', 'dragover'].forEach(function (n) {
  dz.addEventListener(n, function (e) { e.preventDefault(); dz.classList.add('over'); });
});
['dragleave', 'drop'].forEach(function (n) {
  dz.addEventListener(n, function (e) { e.preventDefault(); dz.classList.remove('over'); });
});
dz.addEventListener('drop', function (e) {
  if (e.dataTransfer && e.dataTransfer.files) enqueue(e.dataTransfer.files);
});
$('file-input').onchange = function () { enqueue(this.files); this.value = ''; };
$('user-select').value = state.user;
$('user-select').onchange = function () {
  state.user = this.value;
  state.folder = '';
  toast('已切换到用户 ' + this.value);
  loadFolders().then(load);
};
$('tabs').addEventListener('click', function (e) {
  var b = e.target.closest('button');
  if (!b || !b.dataset.tab) return;
  state.tab = b.dataset.tab;
  var bs = $('tabs').querySelectorAll('button');
  for (var i = 0; i < bs.length; i++) bs[i].classList.toggle('active', bs[i] === b);
  load();
});
$('chips').addEventListener('click', function (e) {
  var b = e.target.closest('button');
  if (!b || !b.dataset.type) return;
  state.type = b.dataset.type;
  var bs = $('chips').querySelectorAll('button');
  for (var i = 0; i < bs.length; i++) bs[i].classList.toggle('active', bs[i] === b);
  render();
});
$('search').oninput = function () { state.search = this.value; render(); };
$('refresh').onclick = function () { loadFolders().then(load); };
$('queue-clear').onclick = function () {
  var items = $('queue').querySelectorAll('.qitem.done, .qitem.err');
  for (var i = 0; i < items.length; i++) items[i].remove();
  if (!$('queue').children.length) $('queue-card').hidden = true;
};

$('limit-hint').textContent = '单文件上限 ' + humanSize(cfg.maxUpload);
renderStats();
loadFolders().then(load);
})();`
